package agent

import (
	"context"
	"log/slog"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// PrepareAgentRuntime builds the per-thread execution context that a
// delegated worker thread needs to run with the same vault-and-toolbelt
// fidelity as the orchestrator's own chat loop. It is the threadmgr-facing
// analogue of the inline setup inside ChatWithAgent: connect the agent's
// MuninnDB vault, fork the tool registry so vault tools are session-local,
// resolve the agent's toolbelt schemas, build a session env so MCP tools see
// the right profile credentials, and assemble a memory_mode + memory_block
// addendum for the persona system prompt.
//
// Without this wiring, a spawned thread (e.g. a worker invoked via @mention)
// inherits only the orchestrator's global toolset — no muninn_* tools,
// no MCP connections, no memory_mode prompt — so child agents can never
// read from or write to their own memory vault and effectively run
// stateless. That regression is what made delegation feel "dead" in the UI.
//
// Behaviour:
//   - Returns (nil, nil) when the orchestrator has no tool registry, has no
//     agent registry, or the named agent is not registered. Threadmgr falls
//     back to its legacy global-toolset path in that case (used by tests
//     and by agents that explicitly opt out of memory).
//   - On vault-unavailable (offline, no token, MCP timeout), connectAgentVault
//     still returns a usable forked registry without muninn tools so the
//     thread degrades gracefully rather than failing hard. The ExtraSystem
//     addendum is omitted in that case so the model is not instructed to
//     use tools that don't exist.
//   - Cleanup closes the per-thread MuninnDB MCP client and tears down the
//     session env. Threadmgr defers Cleanup so it always runs at thread exit.
func (o *Orchestrator) PrepareAgentRuntime(ctx context.Context, agentName string) (*threadmgr.AgentRuntime, error) {
	o.mu.RLock()
	sharedReg := o.toolRegistry
	permGate := o.permGate
	agentReg := o.agentReg
	o.mu.RUnlock()

	if sharedReg == nil || agentReg == nil {
		return nil, nil
	}
	ag, ok := agentReg.ByName(agentName)
	if !ok {
		slog.Warn("PrepareAgentRuntime: agent not in registry; threadmgr will fall back to global toolset",
			"agent", agentName)
		return nil, nil
	}

	// Connect the vault. On any failure path (no token, endpoint unreachable,
	// timeout) connectAgentVault still returns a forked registry without
	// muninn tools — the thread runs without memory rather than failing.
	vr := o.connectAgentVault(ctx, ag, sharedReg)

	// Build per-agent schemas: local builtins + toolbelt providers + vault
	// tools (always included regardless of toolbelt filtering). The forked
	// gate isolates per-agent allowed/watched provider sets.
	schemas, _ := applyToolbelt(ag, vr.sessionReg, permGate)

	// Build a session env so external MCP tools (GitHub, AWS, etc.) see the
	// right per-profile credentials. Falls back to an empty session on error
	// — those tools simply can't authenticate, but the thread still runs.
	agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
	if sessErr != nil {
		slog.Warn("PrepareAgentRuntime: session env setup failed", "agent", ag.Name, "err", sessErr)
		agentSess = &session.Session{}
	}

	// Per-agent tool executor: dispatches against the agent's session-local
	// registry fork (which holds vault + toolbelt + builtins) with the
	// agent's session env layered onto the context. This mirrors the
	// orchestrator's primary chat loop exactly.
	executor := func(ctx context.Context, name string, args map[string]any) (string, error) {
		ctx = session.WithEnv(ctx, agentSess.Env)
		return vr.sessionReg.Execute(ctx, name, args)
	}

	// Persona prompt addendum. We only emit memory_mode instructions when
	// the vault is actually reachable (muninn_recall registered in the
	// session-local fork). Otherwise the model would be told to use tools
	// that aren't there. memoryBlock is non-empty only on vault success.
	var extraSystem string
	if _, hasRecall := vr.sessionReg.Get("muninn_recall"); hasRecall {
		extraSystem = memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
	}
	if vr.memoryBlock != "" {
		if extraSystem != "" {
			extraSystem += "\n"
		}
		extraSystem += vr.memoryBlock
	}

	cleanup := func() {
		if vr.cancel != nil {
			vr.cancel()
		}
		agentSess.Teardown()
	}

	return &threadmgr.AgentRuntime{
		Schemas:     schemas,
		ExecuteTool: executor,
		ExtraSystem: extraSystem,
		Cleanup:     cleanup,
	}, nil
}
