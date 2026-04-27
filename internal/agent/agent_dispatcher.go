package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/swarm"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/workforce"
)

// WithDelegationContext attaches a DelegationContext to the context.
// Delegates to workforce.WithDelegationContext.
func WithDelegationContext(ctx context.Context, dc *workforce.DelegationContext) context.Context {
	return workforce.WithDelegationContext(ctx, dc)
}

// GetDelegationContext retrieves the DelegationContext from the context, if any.
// Delegates to workforce.GetDelegationContext.
func GetDelegationContext(ctx context.Context) *workforce.DelegationContext {
	return workforce.GetDelegationContext(ctx)
}

// applyToolbelt resolves the tool schemas for an agent run and returns a
// per-run forked gate. The original gate is never mutated, which eliminates the
// concurrent mutation race that arises when multiple ChatWithAgent/TaskWithAgent
// calls run in parallel and each calls SetWatchedProviders/SetAllowedProviders
// on the shared orchestrator gate.
//
// LocalTools:
//   - nil/empty → no local tools
//   - ["*"]     → all builtin tools (AllBuiltinSchemas)
//   - named     → specific tools by name (SchemasByNames)
//
// Toolbelt (external connections):
//   - nil/empty → no external tools (default-deny)
//   - entries   → tools for the listed providers
//
// Vault tools (tagged "muninndb") are always included regardless of toolbelt
// configuration. They are session-local, registered by connectAgentVault into
// the forked registry. Without this bypass, an agent with a non-empty toolbelt
// (e.g. only "aws") would have allowedProviders={"aws"}, causing the gate to
// reject every muninn tool call with "permission denied".
func applyToolbelt(ag *agents.Agent, reg *tools.Registry, gate *permissions.Gate) ([]backend.Tool, *permissions.Gate) {
	var schemas []backend.Tool

	// 1. Resolve local builtin tools from LocalTools allowlist.
	if len(ag.LocalTools) == 1 && ag.LocalTools[0] == "*" {
		schemas = append(schemas, reg.AllBuiltinSchemas()...)
	} else if len(ag.LocalTools) > 0 {
		schemas = append(schemas, reg.SchemasByNames(ag.LocalTools)...)
	}

	// 2. Resolve external tools from toolbelt providers (default-deny: empty = none).
	if len(ag.Toolbelt) > 0 {
		providers := agents.ToolbeltProviders(ag.Toolbelt)
		schemas = append(schemas, reg.AllSchemasForProviders(providers)...)
	}

	// 3. Always include vault/memory tools regardless of toolbelt filtering.
	// Vault tools are tagged "muninndb" and registered session-locally by connectAgentVault.
	// They are not in local_tools by name or in the toolbelt by provider, so without
	// this bypass they are silently excluded — causing the model to never see muninn tools.
	if vaultSchemas := reg.AllSchemasForProviders([]string{"muninndb"}); len(vaultSchemas) > 0 {
		seen := make(map[string]bool, len(schemas))
		for _, s := range schemas {
			seen[s.Function.Name] = true
		}
		for _, s := range vaultSchemas {
			if !seen[s.Function.Name] {
				schemas = append(schemas, s)
			}
		}
	}

	// 4. Fork the permission gate so each agent run gets isolated provider maps.
	// When gate is nil (no permission gate configured), the forked gate is also nil.
	var agentGate *permissions.Gate
	if gate != nil {
		// Always allow "muninndb" (vault tools) even when the agent has an explicit
		// toolbelt. The vault schemas are already included in step 3 above; without
		// adding "muninndb" to allowedProviders, the gate would reject every vault
		// tool call with "permission denied" for agents that have a non-empty toolbelt.
		allowed := agents.AllowedProviders(ag.Toolbelt)
		if allowed != nil {
			allowed["muninndb"] = true
		}
		agentGate = gate.Fork(
			agents.WatchedProviders(ag.Toolbelt),
			allowed,
		)
	}

	return schemas, agentGate
}

// agentToolbelt translates the agent's toolbelt entries into the session
// package's ToolbeltEntry type, avoiding an import cycle between session
// and agents.
func agentToolbelt(ag *agents.Agent) []session.ToolbeltEntry {
	if len(ag.Toolbelt) == 0 {
		return nil
	}
	out := make([]session.ToolbeltEntry, len(ag.Toolbelt))
	for i, e := range ag.Toolbelt {
		out[i] = session.ToolbeltEntry{Provider: e.Provider, Profile: e.Profile}
	}
	return out
}

// swarmColors provides distinct TUI colors for swarm agents (cycles if more than 6 agents).
var swarmColors = []string{"#58a6ff", "#3fb950", "#e3b341", "#f85149", "#8b949e", "#a5d6ff"}

// buildSwarmTasks resolves agent names and constructs the swarm task list.
// sessionIDPrefix is prepended to each agent's per-run context session ID so that
// concurrent agents don't share history or run-slot contention:
//   - TUI path: prefix="swarm-"   → context IDs: "swarm-agent-0", "swarm-agent-1", ...
//   - Web path: prefix="<sess>-"  → context IDs: "<sess>-agent-0", "<sess>-agent-1", ...
func (o *Orchestrator) buildSwarmTasks(agentNames, prompts []string, sessionIDPrefix string) ([]swarm.SwarmTask, error) {
	if len(agentNames) != len(prompts) {
		return nil, fmt.Errorf("swarm: agentNames and prompts must have the same length")
	}
	if len(agentNames) == 0 {
		return nil, fmt.Errorf("swarm: no agents specified")
	}

	o.mu.RLock()
	reg := o.agentReg
	o.mu.RUnlock()
	if reg == nil {
		return nil, fmt.Errorf("swarm: agent registry not available")
	}

	agList := make([]*agents.Agent, len(agentNames))
	for i, name := range agentNames {
		ag, ok := reg.ByName(name)
		if !ok {
			return nil, fmt.Errorf("swarm: agent %q not found", name)
		}
		agList[i] = ag
	}

	tasks := make([]swarm.SwarmTask, len(agentNames))
	for i, ag := range agList {
		agentID := fmt.Sprintf("agent-%d", i)
		prompt := prompts[i]
		color := swarmColors[i%len(swarmColors)]
		ctxSessionID := sessionIDPrefix + agentID

		tasks[i] = swarm.SwarmTask{
			ID:    agentID,
			Name:  ag.Name,
			Color: color,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				ctx = SetSessionID(ctx, ctxSessionID)
				return o.ChatWithAgent(ctx, ag, prompt, ctxSessionID,
					func(token string) {
						emit(swarm.SwarmEvent{
							AgentID:   agentID,
							AgentName: ag.Name,
							Type:      swarm.EventToken,
							Payload:   token,
						})
					},
					nil,
					nil,
				)
			},
		}
	}
	return tasks, nil
}

// SwarmWithAgents runs multiple named agents in parallel, each with its own prompt, and
// streams events on the returned channel. The channel is closed once all agents complete.
//
// The first event on the returned channel is always EventSwarmReady (payload []swarm.SwarmTaskSpec),
// which the TUI uses to register all agents before any work events arrive.
//
// agentNames and prompts must have the same length. If an agent name is not found in the
// registry the error is returned immediately and no channel is created.
func (o *Orchestrator) SwarmWithAgents(ctx context.Context, agentNames, prompts []string, maxParallel int) (<-chan swarm.SwarmEvent, error) {
	tasks, err := o.buildSwarmTasks(agentNames, prompts, "swarm-")
	if err != nil {
		return nil, err
	}

	// Build specs for the EventSwarmReady seed event (TUI registration).
	specs := make([]swarm.SwarmTaskSpec, len(tasks))
	for i, t := range tasks {
		specs[i] = swarm.SwarmTaskSpec{ID: t.ID, Name: t.Name, Color: t.Color}
	}

	combined := make(chan swarm.SwarmEvent, 512)
	combined <- swarm.SwarmEvent{
		Type:    swarm.EventSwarmReady,
		Payload: specs,
		At:      time.Now(),
	}

	s := swarm.NewSwarm(maxParallel)
	go func() {
		defer close(combined)
		done := make(chan struct{})
		var swarmDropped int64
		go func() {
			defer close(done)
			_, _, _, swarmDropped, _ = s.Run(ctx, tasks)
		}()
		var dropped int
		for ev := range s.Events() {
			select {
			case combined <- ev:
			default:
				dropped++
				slog.Debug("SwarmWithAgents: combined channel full, event dropped",
					"type", ev.Type, "agent", ev.AgentName)
			}
		}
		if dropped > 0 {
			slog.Warn("SwarmWithAgents: events dropped due to slow consumer", "count", dropped)
		}
		<-done
		if swarmDropped > 0 {
			slog.Warn("SwarmWithAgents: swarm internal events dropped", "count", swarmDropped)
		}
	}()

	return combined, nil
}

// ErrSessionBusy is returned when a swarm is requested on a session that is already running.
var ErrSessionBusy = fmt.Errorf("session is busy: concurrent runs are not supported")

// SwarmWithAgentsBroadcast runs a multi-agent swarm and broadcasts WebSocket events to the
// given session. Returns nil immediately (202-style async); the swarm runs in background
// goroutines tied to the server-lifetime ctx rather than the request context.
//
// broadcast must be constructed by the HTTP handler from s.BroadcastToSession.
// Uses tryBeginRun/endRun to prevent concurrent swarms on the same session (returns
// ErrSessionBusy if one is already running).
func (o *Orchestrator) SwarmWithAgentsBroadcast(
	ctx context.Context,
	agentNames, prompts []string,
	maxParallel int,
	sessionID string,
	broadcast threadmgr.BroadcastFn,
	snapshotFn func(sessionID string, payload map[string]any),
) error {
	// Each agent gets a unique sub-session ID derived from the main session ID
	// so that agents don't contend on each other's run slots or history.
	tasks, err := o.buildSwarmTasks(agentNames, prompts, sessionID+"-")
	if err != nil {
		return err
	}

	o.mu.Lock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		sess = newSession(sessionID)
		o.sessions[sessionID] = sess
	}
	o.mu.Unlock()

	if !sess.tryBeginRun() {
		return ErrSessionBusy
	}

	swarmCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	sess.setActiveSwarm(cancel, doneCh)

	sw := swarm.NewSwarm(maxParallel)
	go func() {
		defer close(doneCh)
		defer sess.endRun()
		defer sess.clearActiveSwarm()
		go BridgeSwarmEvents(swarmCtx, sw, sessionID, tasks, broadcast, snapshotFn)
		_, _, _, _, _ = sw.Run(swarmCtx, tasks)
	}()

	return nil
}

// CancelSwarm cancels an in-progress swarm for the given session.
// No-op if no swarm is running. Returns the done channel (nil if no swarm was running).
func (o *Orchestrator) CancelSwarm(sessionID string) <-chan struct{} {
	o.mu.RLock()
	sess, ok := o.sessions[sessionID]
	o.mu.RUnlock()
	if !ok {
		return nil
	}
	return sess.cancelSwarm()
}

// ExecuteAgentTool executes an agent-mode tool by making an LLM call.
// This implements the skills.AgentExecutor interface.
func (o *Orchestrator) ExecuteAgentTool(ctx context.Context, model string, budgetTokens int, prompt string) (string, error) {
	// budgetTokens is accepted for future use but not yet propagated to ChatRequest,
	// which does not expose a per-call MaxTokens field. When ChatRequest gains that
	// field, wire budgetTokens through here.
	_ = budgetTokens

	o.mu.RLock()
	b := o.backend
	o.mu.RUnlock()

	if b == nil {
		return "", fmt.Errorf("agent tool execution: no backend configured")
	}

	msgs := []backend.Message{
		{Role: "user", Content: prompt},
	}

	var result strings.Builder
	resp, err := b.ChatCompletion(ctx, backend.ChatRequest{
		Model:    model,
		Messages: msgs,
		OnToken: func(token string) {
			result.WriteString(token)
		},
	})
	if err != nil {
		return "", err
	}

	// Record usage if available
	if resp != nil {
		o.lastUsagePrompt.Store(int64(resp.PromptTokens))
		o.lastUsageCompletion.Store(int64(resp.CompletionTokens))
	}

	return result.String(), nil
}

// Dispatch parses input for agent directives and executes them.
// Returns (handled=true, nil) if it was an agent directive.
// Returns (handled=false, nil) if it was normal chat — caller should route normally.
func (o *Orchestrator) Dispatch(
	ctx context.Context,
	input string,
	onToken func(string),
	onToolCall func(string, map[string]any),
	onToolDone func(string, tools.ToolResult),
	onPermDenied func(string),
	maxTurnsPtr *int,
	onEvent func(backend.StreamEvent),
) (bool, error) {
	o.mu.RLock()
	reg := o.agentReg
	o.mu.RUnlock()

	if reg == nil {
		return false, nil
	}

	directive := agents.ParseDirective(input, reg)
	if directive == nil {
		// Tier 1 regex missed — try Tier 2 model fallback (one cheap structured LLM call).
		o.mu.RLock()
		dispatchBackend := o.backend
		dispatchCache := o.backendCache
		o.mu.RUnlock()
		if dispatchCache != nil {
			if b, e := dispatchCache.For("", "", "", ""); e == nil {
				dispatchBackend = b
			}
		}
		directive = agents.ParseDirectiveFallback(ctx, input, reg, dispatchBackend, o.defaultModelName())
		if directive == nil {
			return false, nil
		}
	}

	maxTurns := 50
	if maxTurnsPtr != nil {
		maxTurns = *maxTurnsPtr
	}

	for _, step := range directive.Steps {
		ag, ok := reg.ByName(step.AgentName)
		if !ok {
			continue
		}
		var err error
		switch step.Action {
		case "task", "code": // "code" kept as alias for backward compatibility
			err = o.TaskWithAgent(ctx, ag, step.Payload, maxTurns, onToken, onToolCall, onToolDone, onPermDenied, onEvent)
		case "reason":
			err = o.ReasonWithAgent(ctx, ag, step.Payload, onToken, onEvent)
		default:
			err = o.ChatWithAgent(ctx, ag, step.Payload, GetSessionID(ctx), onToken, nil, onEvent)
		}
		if err != nil {
			return true, err
		}
	}
	return true, nil
}

// TaskWithAgent runs an agent on a bounded task with session isolation (isolated temp dir + env).
// Use this for any @agent directive that needs tool-calling with clean workspace isolation —
// coding, investigation, delegation, refactoring, etc.
func (o *Orchestrator) TaskWithAgent(
	ctx context.Context,
	ag *agents.Agent,
	userMsg string,
	maxTurns int,
	onToken func(string),
	onToolCall func(string, map[string]any),
	onToolDone func(string, tools.ToolResult),
	onPermDenied func(string),
	onEvent func(backend.StreamEvent),
) error {
	o.mu.RLock()
	reg := o.toolRegistry
	gate := o.permGate
	sess := o.defaultSession()
	o.mu.RUnlock()
	sess.setState(StateAgentLoop)

	defer sess.setState(StateIdle)

	if reg == nil {
		return o.ChatWithAgent(ctx, ag, userMsg, GetSessionID(ctx), onToken, nil, onEvent)
	}

	// Connect to MuninnDB vault for this session — forks the shared registry so
	// vault tools are isolated per session. Always safe; degrades gracefully.
	vr := o.connectAgentVault(ctx, ag, reg)
	defer vr.cancel()

	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{
			Type:    backend.StreamWarning,
			Content: fmt.Sprintf("\u26a0\ufe0f Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
		})
	}

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())
	recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
	systemPrompt := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)
	// Inject memory mode instructions only when vault tools are available this session.
	if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
		systemPrompt += memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
	}
	// Pre-fetch memory orientation and inject into system prompt. Surface
	// synthetic tool events so the UI can show that memory recall happened.
	taskPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
		if cached {
			return
		}
		if onToolCall != nil {
			onToolCall(toolName, args)
		}
		if onToolDone != nil {
			onToolDone(toolName, tools.ToolResult{Output: output})
		}
	}
	if memCtx := o.prefetchMemoryContextWithEvents(ctx, vr.sessionReg, ag.Name, ag.VaultName, userMsg, taskPrefetchCallback); memCtx != "" {
		systemPrompt += memCtx
	}

	history := sess.snapshotHistory()

	messages := []backend.Message{{Role: "system", Content: systemPrompt}}
	messages = append(messages, history...)
	messages = append(messages, backend.Message{Role: "user", Content: userMsg})

	schemas, agentGate := applyToolbelt(ag, vr.sessionReg, gate)

	// Auto-inject read-only team tools when the agent is in a channel context.
	// delegate_to_agent is NOT injected — channels use @mention-based delegation
	// so the lead agent writes natural messages like "@Sam, please do X" and the
	// mention parser (CreateFromMentions) spawns the thread automatically.
	if spaceCtx := workforce.GetSpaceContext(ctx); spaceCtx != "" {
		delegationToolNames := []string{"list_team_status", "recall_thread_result"}
		seen := make(map[string]bool, len(schemas))
		for _, s := range schemas {
			seen[s.Function.Name] = true
		}
		for _, name := range delegationToolNames {
			if !seen[name] {
				if t, ok := vr.sessionReg.Get(name); ok {
					schemas = append(schemas, t.Schema())
				}
			}
		}
	}

	// Create isolated session environment for this agent run.
	agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
	if sessErr != nil {
		// Non-fatal: log warning but continue without session isolation.
		slog.Warn("agent session setup failed", "agent", ag.Name, "err", sessErr)
		agentSess = &session.Session{} // empty session
	}
	defer agentSess.Teardown()
	ctx = session.WithEnv(ctx, agentSess.Env)

	agCodeBackend, agCodeErr := o.backendFor(ag)
	if agCodeErr != nil {
		return agCodeErr
	}
	cfg := RunLoopConfig{
		MaxTurns:           maxTurns,
		ModelName:          ag.GetModelID(),
		Messages:           messages,
		Tools:              vr.sessionReg,
		ToolSchemas:        schemas,
		Gate:               agentGate,
		Backend:            agCodeBackend,
		OnToken:            onToken,
		OnToolCall:         onToolCall,
		OnToolDone:         onToolDone,
		OnPermissionDenied: onPermDenied,
		OnEvent:            onEvent,
		VaultWarnOnce:      &sync.Once{},
		VaultReconnector:   vr.reconnector,
	}

	agentLoopStart := time.Now().UnixNano()
	loopResult, err := RunLoop(ctx, cfg)
	o.recordLLMLatency(agentLoopStart, "agent-loop")
	if err != nil {
		return fmt.Errorf("task(%s): %w", ag.Name, err)
	}

	initialCount := 1 + len(history) + 1 // system msg + history msgs + user msg
	if loopResult.Messages != nil && len(loopResult.Messages) > initialCount {
		sess.appendHistory(loopResult.Messages[initialCount:]...)
	} else {
		sess.appendHistory(
			backend.Message{Role: "user", Content: userMsg},
			backend.Message{Role: "assistant", Content: loopResult.FinalContent},
		)
	}
	o.compactHistory(ctx, sess)
	return nil
}

// ReasonWithAgent runs the reasoner using the given agent's persona and model.
func (o *Orchestrator) ReasonWithAgent(ctx context.Context, ag *agents.Agent, userMsg string, onToken func(string), onEvent func(backend.StreamEvent)) error {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	return o.ChatWithAgent(ctx, ag, userMsg, GetSessionID(ctx), onToken, nil, onEvent)
}

// ChatWithAgent sends a chat message using the given agent's persona and model.
// When a tool registry is configured, it runs the full tool-calling loop so that
// tools like delegate_to_agent can be invoked. sessionID is used to propagate the
// active session through context to any tools that need it.
func (o *Orchestrator) ChatWithAgent(ctx context.Context, ag *agents.Agent, userMsg string, sessionID string,
	onToken func(string),
	onToolEvent func(eventType string, payload map[string]any),
	onEvent func(backend.StreamEvent)) error {
	o.mu.Lock()
	var sess *Session
	if sessionID != "" {
		if s, ok := o.sessions[sessionID]; ok {
			sess = s
		} else {
			// Session not in memory (e.g. after server restart). Create a fresh
			// in-memory session for this ID rather than falling back to the shared
			// default session, which may contain stale/malformed history that causes
			// HTTP 400 errors from the LLM API (e.g. tool_use blocks with empty input).
			sess = newSession(sessionID)
			o.sessions[sessionID] = sess
		}
	}
	if sess == nil {
		sess = o.defaultSession()
	}
	reg := o.toolRegistry
	gate := o.permGate
	o.mu.Unlock()

	// Guard against concurrent calls on the same session. Only one agentic loop
	// may run at a time per session — concurrent calls would interleave history
	// appends, producing garbled context for future turns.
	// Uses a separate atomic flag (not the state machine) so outer callers like
	// ReasonWithAgent can pre-set state to StateAgentLoop without conflict.
	if !sess.tryBeginRun() {
		return fmt.Errorf("chat(%s): session %s is already running; concurrent calls are not supported", ag.Name, sessionID)
	}
	defer sess.endRun()
	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	if ag.GetModelID() == "" {
		return fmt.Errorf("agent %q has no model configured — open Agent settings to assign a model", ag.Name)
	}

	ctxText := o.contextBuilder.Build(userMsg, ag.GetModelID())
	recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
	systemPrompt := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)

	// Per-agent skills fragment. Non-default agents (workflow steps, delegated
	// workers) need their assigned skills appended just like the default agent
	// does in mcp_agent_chat.go. Without this they execute with no skills,
	// which is a major parity gap for scheduled workflows.
	if skillsFrag := o.SkillsFragmentForAgent(ag); skillsFrag != "" {
		systemPrompt += "\n\n" + skillsFrag
	}

	// Inject space context (channel/DM metadata) if available.
	if spaceCtx := workforce.GetSpaceContext(ctx); spaceCtx != "" {
		systemPrompt += "\n\n" + spaceCtx
	}
	// Inject channel-recent summary (channels only, not DMs).
	if recentCtx := workforce.GetChannelRecent(ctx); recentCtx != "" {
		systemPrompt += "\n\n" + recentCtx
	}

	// Inject per-step pre-authorised connection picks (Phase 1.4). When a
	// workflow step declares `connections: { github: my-personal-gh, ... }`
	// the runner places that map into ctx via WithStepConnections; surface it
	// as a system addendum so the agent uses those account labels in tool calls.
	if connHint := stepConnectionsAddendum(ctx); connHint != "" {
		systemPrompt += "\n\n" + connHint
	}

	history := sess.snapshotHistory()

	msgs := []backend.Message{{Role: "system", Content: systemPrompt}}
	msgs = append(msgs, history...)
	msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

	// When a tool registry is configured, use the full RunLoop so tools like
	// delegate_to_agent can be called during the conversation. If the model
	// rejects tool schemas (e.g. deepseek-r1 on Ollama), fall back to plain chat.
	if reg != nil {
		// Connect to MuninnDB vault for this session — forks the shared registry so
		// vault tools are isolated per session. Always safe; degrades gracefully.
		vr := o.connectAgentVault(ctx, ag, reg)
		defer vr.cancel()

		if vr.warning != "" {
			slog.Warn("vault unavailable for agent session", "agent", ag.Name, "session_id", sessionID, "warning", vr.warning)
			if onEvent != nil {
				onEvent(backend.StreamEvent{
					Type:    backend.StreamWarning,
					Content: fmt.Sprintf("\u26a0\ufe0f Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
				})
			}
		}
		// Inject memory mode instructions only when vault tools are available this session.
		if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
			slog.Info("vault tools available", "agent", ag.Name, "session_id", sessionID, "vault", ag.VaultName)
			msgs[0].Content += memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
		}
		// Pre-fetch memory orientation and inject into system prompt. Surface
		// synthetic tool events so the UI can show that memory recall happened.
		chatPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
			if cached || onToolEvent == nil {
				return
			}
			onToolEvent("tool_call", map[string]any{"tool": toolName, "args": args})
			onToolEvent("tool_result", map[string]any{"tool": toolName, "result": output})
		}
		if memCtx := o.prefetchMemoryContextWithEvents(ctx, vr.sessionReg, ag.Name, ag.VaultName, userMsg, chatPrefetchCallback); memCtx != "" {
			msgs[0].Content += memCtx
		}

		ctx = SetSessionID(ctx, sessionID)
		schemas, agentGate := applyToolbelt(ag, vr.sessionReg, gate)

		// Auto-inject read-only team tools when the agent is in a channel context.
		// delegate_to_agent is NOT injected — channels use @mention-based delegation
		// so the lead agent writes natural messages like "@Sam, please do X" and the
		// mention parser (CreateFromMentions) spawns the thread automatically.
		if spaceCtx := workforce.GetSpaceContext(ctx); spaceCtx != "" {
			delegationToolNames := []string{"list_team_status", "recall_thread_result"}
			seen := make(map[string]bool, len(schemas))
			for _, s := range schemas {
				seen[s.Function.Name] = true
			}
			for _, name := range delegationToolNames {
				if !seen[name] {
					if t, ok := vr.sessionReg.Get(name); ok {
						schemas = append(schemas, t.Schema())
					}
				}
			}
		}

		// Create isolated session environment for this agent run.
		agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
		if sessErr != nil {
			// Non-fatal: log warning but continue without session isolation.
			slog.Warn("agent session setup failed", "agent", ag.Name, "err", sessErr)
			agentSess = &session.Session{} // empty session
		}
		defer agentSess.Teardown()
		ctx = session.WithEnv(ctx, agentSess.Env)

		agChatBackend, agChatErr := o.backendFor(ag)
		if agChatErr != nil {
			return fmt.Errorf("chat(%s): %w", ag.Name, agChatErr)
		}
		// toolArgsMu guards toolArgsCapture against concurrent writes from parallel
		// tool dispatches. dispatchTools spawns one goroutine per tool call, so
		// OnToolCall/OnToolDone can fire concurrently.
		var toolArgsMu sync.Mutex
		// toolArgsCapture stores args keyed by tool name so OnToolDone can include
		// them in the tool_result event. Last-write-wins when the same tool is called
		// multiple times in one turn (a known limitation).
		// TODO(tool-call-id): key by call ID instead of tool name to fix same-tool collision.
		toolArgsCapture := make(map[string]map[string]any)
		// toolCallIDCapture stores a correlation ID per tool name so that
		// tool_call and tool_result events carry the same id. The frontend
		// uses this id to match results back to the pending call chip.
		toolCallIDCapture := make(map[string]string)
		loopCfg := RunLoopConfig{
			MaxTurns:      50,
			ModelName:     ag.GetModelID(),
			Messages:      msgs,
			Tools:         vr.sessionReg,
			ToolSchemas:   schemas,
			Gate:          agentGate,
			Backend:       agChatBackend,
			OnToken:       onToken,
			OnEvent:          onEvent,
			VaultWarnOnce:    &sync.Once{},
			VaultReconnector: vr.reconnector,
			OnToolCall: func(name string, args map[string]any) {
				callID := fmt.Sprintf("tc-%d-%s", time.Now().UnixNano(), name)
				slog.Info("tool call started", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID)
				toolArgsMu.Lock()
				toolArgsCapture[name] = args
				toolCallIDCapture[name] = callID
				toolArgsMu.Unlock()
				if onToolEvent != nil {
					onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
				} else if onEvent != nil {
					// Emit full tool_call event with id+args so the frontend can show
					// a "running…" chip with context before the result arrives.
					onEvent(backend.StreamEvent{
						Type:    backend.StreamToolCall,
						Payload: map[string]any{"id": callID, "tool": name, "args": args},
					})
				}
			},
			OnToolDone: func(name string, result tools.ToolResult) {
				toolArgsMu.Lock()
				capturedArgs := toolArgsCapture[name]
				callID := toolCallIDCapture[name]
				toolArgsMu.Unlock()
				slog.Info("tool call done", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID, "success", result.Error == "")
				if onToolEvent != nil {
					onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
				} else if onEvent != nil {
					// Emit full tool_result event with matching id so the frontend
					// can attach the result to the correct pending chip.
					onEvent(backend.StreamEvent{
						Type: backend.StreamToolResult,
						Payload: map[string]any{
							"id":      callID,
							"tool":    name,
							"success": result.Error == "",
							"result":  result.Output,
							"args":    capturedArgs,
						},
					})
				}
			},
		}
		// Build a DelegationContext so downstream code (e.g. threadmgr) can
		// trace the delegation lineage through the Go context.
		if GetDelegationContext(ctx) == nil {
			dc := workforce.NewDelegationContext(sessionID, ag.Name, o.maxDelegationDepth())
			ctx = WithDelegationContext(ctx, &dc)
		}

		agentChatStart := time.Now().UnixNano()
		result, loopErr := RunLoop(ctx, loopCfg)
		o.recordLLMLatency(agentChatStart, "agent-chat")
		if loopErr != nil && strings.Contains(loopErr.Error(), "does not support tools") {
			// Model doesn't support function calling — retry without tools.
		} else if loopErr != nil {
			return fmt.Errorf("chat(%s): %w", ag.Name, loopErr)
		} else {
			// Preserve full tool-call/tool-result history so subsequent turns have
			// accurate context (tool names, arguments, results). Matches TaskWithAgent.
			// initialCount = system msg (1) + history snapshot + user msg (1).
			initialCount := 1 + len(history) + 1
			if result.Messages != nil && len(result.Messages) > initialCount {
				sess.appendHistory(result.Messages[initialCount:]...)
			} else {
				sess.appendHistory(
					backend.Message{Role: "user", Content: userMsg},
					backend.Message{Role: "assistant", Content: result.FinalContent},
				)
			}
			o.compactHistory(ctx, sess)
			return nil
		}
	}

	// No tool registry, or model doesn't support tools — direct single-turn completion.
	plainBackend, plainErr := o.backendFor(ag)
	if plainErr != nil {
		return fmt.Errorf("chat(%s): %w", ag.Name, plainErr)
	}
	var buf strings.Builder
	agentChatStart := time.Now().UnixNano()
	_, err := plainBackend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    ag.GetModelID(),
		Messages: msgs,
		OnToken: func(token string) {
			buf.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		},
		OnEvent: onEvent,
	})
	o.recordLLMLatency(agentChatStart, "agent-chat")
	if err != nil {
		return fmt.Errorf("chat(%s): %w", ag.Name, err)
	}

	sess.appendHistory(
		backend.Message{Role: "user", Content: userMsg},
		backend.Message{Role: "assistant", Content: buf.String()},
	)
	o.compactHistory(ctx, sess)
	return nil
}

// WaitForSessionIdle blocks until the session identified by sessionID is no
// longer running (i.e., its exclusive run slot has been released), or until ctx
// is done. Returns true if the session is idle, false if ctx expired first.
// If the session is not found, it is considered idle and true is returned.
func (o *Orchestrator) WaitForSessionIdle(sessionID string, ctx context.Context) bool {
	o.mu.RLock()
	sess, ok := o.sessions[sessionID]
	o.mu.RUnlock()
	if !ok {
		return true
	}
	return sess.WaitForIdle(ctx)
}
