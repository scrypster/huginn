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

	// 4. Inject delegation tools when the agent's model supports delegation.
	// TierLow (7b) must not see delegate_to_agent — InferCapabilities sets
	// SupportsDelegation=false. Empty/unknown model IDs stay optimistic.
	// LocalTools:["*"] would otherwise pull them via step 1; strip in that case.
	{
		delegationNames := []string{"delegate_to_agent", "list_team_status", "recall_thread_result", "wait_for_threads"}
		if agents.AgentSupportsDelegation(ag) {
			seenDelegation := make(map[string]bool, len(schemas))
			for _, s := range schemas {
				seenDelegation[s.Function.Name] = true
			}
			for _, dname := range delegationNames {
				if !seenDelegation[dname] {
					if dt, ok := reg.Get(dname); ok {
						schemas = append(schemas, dt.Schema())
					}
				}
			}
		} else {
			deny := make(map[string]bool, len(delegationNames))
			for _, dname := range delegationNames {
				deny[dname] = true
			}
			filtered := schemas[:0]
			for _, s := range schemas {
				if !deny[s.Function.Name] {
					filtered = append(filtered, s)
				}
			}
			schemas = filtered
		}
	}

	// 4b. create_agent is never implied by God Mode or a toolbelt wildcard.
	// Only an explicit local_tools: ["create_agent"] grant may keep the schema.
	{
		namedHire := false
		for _, n := range ag.LocalTools {
			if n == tools.CreateAgentName {
				namedHire = true
				break
			}
		}
		if !namedHire {
			filtered := schemas[:0]
			for _, s := range schemas {
				if s.Function.Name != tools.CreateAgentName {
					filtered = append(filtered, s)
				}
			}
			schemas = filtered
		}
	}

	// 5. Fork the permission gate so each agent run gets isolated provider maps.
	// When gate is nil (no permission gate configured), the forked gate is also nil.
	var agentGate *permissions.Gate
	if gate != nil {
		// Always allow "muninndb" (vault tools) and "builtin" (delegation tools and
		// other builtins) even when the agent has an explicit (or empty) toolbelt.
		// Without this, the gate would reject calls to delegate_to_agent (tagged
		// "builtin") and muninn tools (tagged "muninndb") with "permission denied"
		// once AllowedProviders is a non-nil map. The schemas are already included
		// by steps 3 and 4 above; the gate bypass keeps those calls permitted.
		allowed := agents.AllowedProviders(ag.Toolbelt)
		// Empty map = deny external providers. Wildcard {"*": true} is
		// explicit allow-all and already covers vault/delegation tags.
		if allowed != nil && !allowed["*"] {
			allowed["muninndb"] = true
			allowed["builtin"] = true
		}
		agentGate = gate.Fork(
			agents.WatchedProviders(ag.Toolbelt),
			allowed,
		)
		// Pre-seed persisted "always allow" grants (from a prior
		// "Always allow for <Agent>" decision) so this run doesn't
		// re-prompt for tools the human already approved for this agent.
		agentGate.SeedSessionAllowed(ag.ApprovedTools)
	}

	schemas = filterMuninnSchemas(schemas, ag.MemoryMode)
	return schemas, agentGate
}

// toolGetter is the minimal interface needed to look up a registered tool by
// name. *tools.Registry satisfies this interface.
type toolGetter interface {
	Get(name string) (tools.Tool, bool)
}

// injectDelegationTools appends delegation tool schemas to schemas when the
// context carries a space context. It is a no-op when there is no space context
// or when the tool is already present in schemas. The original slice is never
// mutated if it is unchanged.
func injectDelegationTools(ctx context.Context, schemas []backend.Tool, reg toolGetter, ag *agents.Agent) []backend.Tool {
	if !agents.AgentSupportsDelegation(ag) {
		return schemas
	}
	if workforce.GetSpaceContext(ctx) == "" {
		return schemas
	}
	delegationToolNames := []string{"delegate_to_agent", "list_team_status", "recall_thread_result", "wait_for_threads"}
	seen := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		seen[s.Function.Name] = true
	}
	for _, name := range delegationToolNames {
		if !seen[name] {
			if t, ok := reg.Get(name); ok {
				schemas = append(schemas, t.Schema())
			}
		}
	}
	return schemas
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

// ErrQueueWaitTimeout marks a ChatWithAgent error as "this queued turn could
// not claim its session's exclusive run slot within one wait ceiling" — a
// queuing fact, never a real failure. A predecessor that is still
// legitimately running (a slow local model can take minutes) is expected
// to eventually finish; callers must keep retrying (with errors.Is on this
// sentinel) rather than ever surfacing the wrapping error as assistant
// speech or a persisted chat row.
var ErrQueueWaitTimeout = fmt.Errorf("queue wait timeout")

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
	if budgetTokens > 0 {
		slog.Warn("agent: budgetTokens parameter is not yet enforced", "budget", budgetTokens)
	}

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
	onToolCall func(string, string, map[string]any),
	onToolDone func(string, string, tools.ToolResult),
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

// errToolsNotSupported is returned by runAgentTurn when the underlying RunLoop
// signals that the model does not support function calling. The caller
// (ChatWithAgent) uses this sentinel to fall back to a plain single-turn
// completion without tools.
var errToolsNotSupported = fmt.Errorf("model does not support tools")

// toolResultDisplayText returns the text a UI/onEvent consumer should show for
// a tool call's result. A successful call shows its Output; a failed call
// shows Error (falling back to Output if Error is somehow empty) so a failure
// never renders as a blank "result" — matching what the model itself sees in
// its own next-turn context (loop.go prefixes IsError results with "error: ").
// Without this, a live-captured tool_result wire payload for a failed
// read_file call showed {"result":"","success":false} — the real error text
// ("no such file or directory") was silently dropped, leaving only the model's
// own guess about what went wrong visible anywhere.
func toolResultDisplayText(result tools.ToolResult) string {
	if result.IsError && result.Error != "" {
		return result.Error
	}
	return result.Output
}

// agentTurnOpts captures the parameters that differ between ChatWithAgent and
// TaskWithAgent so that both can delegate their shared core logic to
// runAgentTurn. Fields that are common to every turn (agent, userMsg, session,
// registry, gate) are always required; optional callbacks may be nil.
type agentTurnOpts struct {
	// ag is the agent whose persona, model, and toolbelt are used.
	ag *agents.Agent
	// userMsg is the human turn to process.
	userMsg string
	// systemPromptBase is the fully-assembled system prompt that the caller
	// has already built (persona + any extra fragments). runAgentTurn appends
	// memory-mode instructions and prefetched memory context to this base.
	systemPromptBase string
	// history is a pre-snapshotted slice of prior conversation messages.
	history []backend.Message
	// sess is the session whose history will be updated after the loop.
	sess *Session
	// reg is the tool registry for this turn (never nil when runAgentTurn is called).
	reg *tools.Registry
	// gate is the permission gate (may be nil).
	gate *permissions.Gate
	// maxTurns caps the RunLoop iteration count.
	maxTurns int
	// errorPrefix is used in wrapped error messages ("chat" or "task").
	errorPrefix string
	// latencySlot is the stats slot passed to recordLLMLatency.
	latencySlot string
	// sessionID is propagated into ctx via SetSessionID. May be empty.
	sessionID string
	// prefetchCb is invoked for each pre-fetched memory event. Callers wire
	// this differently: Chat routes through onToolEvent; Task routes through
	// onToolCall/onToolDone. May be nil.
	prefetchCb func(toolName string, args map[string]any, output string, cached bool)
	// onToken, onToolCall, onToolDone, onPermDenied, onEvent are the streaming
	// callbacks passed directly to RunLoopConfig.
	onToken      func(string)
	onToolCall   func(string, string, map[string]any)
	onToolDone   func(string, string, tools.ToolResult)
	onPermDenied func(string)
	onEvent      func(backend.StreamEvent)
	// ctxSetup, when non-nil, is called after the session environment has been
	// injected and before RunLoop is invoked. It allows the caller to perform
	// additional context mutations (e.g. SetSessionID, delegation context).
	ctxSetup func(ctx context.Context) context.Context
	// vaultPrefetch, when non-nil, supplies a precomputed vault connection +
	// memory prefetch (see startVaultPrefetch). Callers start this goroutine
	// BEFORE building systemPromptBase so vault connect + prefetch overlaps
	// with contextBuilder.Build / loadAgentSummaries instead of running after
	// it. When nil, runAgentTurn falls back to computing it inline (serially).
	// Never consulted for trivial asks, which skip vault MCP entirely.
	vaultPrefetch func() vaultPrefetchOutcome
}

// vaultPrefetchOutcome bundles what runAgentTurn's vault-connect + prefetch
// step used to compute serially: the vault connection, ctx with the memory
// gate applied, and the memory-context system-prompt addendum (memory-mode
// instruction + prefetched muninn_where_left_off / muninn_recall output).
type vaultPrefetchOutcome struct {
	vr          vaultResult
	ctx         context.Context
	memAddendum string
}

// startVaultPrefetch runs connectAgentVault followed by
// prefetchMemoryContextWithEvents in a goroutine and returns a function that
// blocks for the result. Call it as early as possible — before building the
// (often slow: repo search, summary loading) system-prompt base — so the two
// independent phases overlap instead of running serially. This is DEFECT B
// phase 2: on a local 14b, contextBuilder.Build/loadAgentSummaries and the
// vault connect (2 retry attempts) + up to 3 sequential 2s-timeout MCP calls
// were previously back-to-back on every non-trivial turn.
func (o *Orchestrator) startVaultPrefetch(
	ctx context.Context,
	ag *agents.Agent,
	reg *tools.Registry,
	sessionID, userMsg string,
	prefetchCb func(toolName string, args map[string]any, output string, cached bool),
) func() vaultPrefetchOutcome {
	resultCh := make(chan vaultPrefetchOutcome, 1)
	go func() {
		vr := o.connectAgentVault(ctx, ag, reg)
		gctx := WithMemoryGate(ctx, ag.MemoryMode, sessionID, ag.Name)
		addendum := ""
		if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
			slog.Info("vault tools available", "agent", ag.Name, "session_id", sessionID, "vault", ag.VaultName)
			addendum = memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
		}
		if memCtx := o.prefetchMemoryContextWithEvents(gctx, vr.sessionReg, ag.Name, ag.VaultName, userMsg, prefetchCb); memCtx != "" {
			addendum += memCtx
		}
		resultCh <- vaultPrefetchOutcome{vr: vr, ctx: gctx, memAddendum: addendum}
	}()
	return func() vaultPrefetchOutcome { return <-resultCh }
}

// runAgentTurn is the shared core of ChatWithAgent (tool-registry path) and
// TaskWithAgent. It:
//  1. Connects the MuninnDB vault (forks the registry; always safe).
//  2. Appends memory-mode instructions and pre-fetched memory context to the
//     system prompt provided by the caller.
//  3. Constructs the message list: [system] + history + [user].
//  4. Applies the agent toolbelt and injects delegation tools.
//  5. Builds and tears down an isolated session environment.
//  6. Resolves the per-agent backend.
//  7. Runs the tool-calling loop via RunLoop.
//  8. Appends new messages to the session history and compacts it.
//
// All caller-specific differences (prompt augmentation, callback wiring,
// error prefix, latency slot, maxTurns) are captured in opts.
func (o *Orchestrator) runAgentTurn(ctx context.Context, opts agentTurnOpts) error {
	ag := opts.ag
	trivial := IsTrivialAsk(opts.userMsg)

	// 1+2. Connect vault and prefetch memory context — forks the shared
	// registry; safe to call even when vault is unconfigured (returns a
	// no-op registry fork with cancel=func(){}). Trivial asks skip vault MCP
	// entirely so 14b never sees muninn / hire tools.
	//
	// When opts.vaultPrefetch is set, the caller already started this work
	// concurrently with building systemPromptBase (contextBuilder.Build /
	// loadAgentSummaries) — we just join it here. Otherwise fall back to
	// computing it inline (serially), preserving old behavior for any caller
	// that doesn't opt in.
	var vr vaultResult
	systemPrompt := opts.systemPromptBase
	if trivial {
		vr = vaultResult{sessionReg: opts.reg, cancel: func() {}}
	} else if opts.vaultPrefetch != nil {
		outcome := opts.vaultPrefetch()
		vr = outcome.vr
		ctx = outcome.ctx
		systemPrompt += outcome.memAddendum
	} else {
		vr = o.connectAgentVault(ctx, ag, opts.reg)
		ctx = WithMemoryGate(ctx, ag.MemoryMode, opts.sessionID, ag.Name)
		if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
			slog.Info("vault tools available", "agent", ag.Name, "session_id", opts.sessionID, "vault", ag.VaultName)
			systemPrompt += memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
		}
		if memCtx := o.prefetchMemoryContextWithEvents(ctx, vr.sessionReg, ag.Name, ag.VaultName, opts.userMsg, opts.prefetchCb); memCtx != "" {
			systemPrompt += memCtx
		}
	}
	defer vr.cancel()

	if vr.warning != "" {
		logVaultUnavailable(ag.Name, opts.sessionID, vr.warning)
	}

	// 3. Build message list: system + history snapshot + user turn.
	messages := make([]backend.Message, 0, 2+len(opts.history))
	messages = append(messages, backend.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, opts.history...)
	messages = append(messages, backend.Message{Role: "user", Content: opts.userMsg})
	if !isTrivialPing(normalizeTrivialAsk(opts.userMsg)) {
		kept := messages[:0]
		for _, m := range messages {
			if m.Role == "assistant" && backend.IsLeftoverPongSpeech(m.Content) {
				continue
			}
			kept = append(kept, m)
		}
		messages = kept
	}

	// Trivial: tools-free completion. Empty ToolSchemas means "all tools
	// allowed" in RunLoop, so we must not enter the tool loop with a nil belt.
	// Last-chance strip keeps wait/delegate/consult off if schemas are rebuilt.
	if trivial {
		if opts.ctxSetup != nil {
			ctx = opts.ctxSetup(ctx)
		}
		return o.completeTrivialAsk(ctx, opts, messages)
	}

	// 4. Resolve tool schemas and permission gate for this agent run.
	schemas, agentGate := applyToolbelt(ag, vr.sessionReg, opts.gate)
	schemas = injectDelegationTools(ctx, schemas, vr.sessionReg, ag)
	if IsHireAsk(opts.userMsg) {
		schemas = stripHireDelegationTools(schemas)
		if len(messages) > 0 && messages[0].Role == "system" {
			messages[0].Content += "\n\nHiring is your job via create_agent. If the human already gave a name and a role, call create_agent now. Do not dump a form. Do not delegate hiring."
		}
	}

	// 5. Create isolated session environment (temp dir, env vars).
	agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
	if sessErr != nil {
		slog.Warn("agent session setup failed", "agent", ag.Name, "err", sessErr)
		agentSess = &session.Session{}
	}
	defer agentSess.Teardown()
	ctx = session.WithEnv(ctx, agentSess.Env)

	// Allow the caller to mutate ctx further (e.g. SetSessionID, delegation ctx).
	if opts.ctxSetup != nil {
		ctx = opts.ctxSetup(ctx)
	}

	// 6. Resolve the per-agent backend.
	b, backendErr := o.backendFor(ag)
	if backendErr != nil {
		return fmt.Errorf("%s(%s): %w", opts.errorPrefix, ag.Name, backendErr)
	}

	// 7. Run the tool-calling loop.
	cfg := RunLoopConfig{
		MaxTurns:           opts.maxTurns,
		ModelName:          ag.GetModelID(),
		Messages:           messages,
		Tools:              vr.sessionReg,
		ToolSchemas:        schemas,
		Gate:               agentGate,
		Backend:            b,
		OnToken:            opts.onToken,
		OnToolCall:         opts.onToolCall,
		OnToolDone:         opts.onToolDone,
		OnPermissionDenied: opts.onPermDenied,
		OnEvent:            opts.onEvent,
		VaultWarnOnce:      &sync.Once{},
		VaultReconnector:   vr.reconnector,
		MemoryMode:         ag.MemoryMode,
		MemoryVault:        pinMuninnVault(ag.VaultName),
		MemoryAgent:        ag.Name,
		MemoryUserMsg:      opts.userMsg,
		MemorySession:      opts.sessionID,
		MemoryHome:         o.huginnHome,
		AgentName:          ag.Name,
		SessionID:          opts.sessionID,
	}

	start := time.Now().UnixNano()
	loopResult, err := RunLoop(ctx, cfg)
	o.recordLLMLatency(start, opts.latencySlot)
	if err != nil {
		if strings.Contains(err.Error(), "does not support tools") {
			// Signal to the caller (ChatWithAgent) that it should fall back to
			// a plain single-turn completion without tool schemas.
			return errToolsNotSupported
		}
		return fmt.Errorf("%s(%s): %w", opts.errorPrefix, ag.Name, err)
	}

	// 8. Persist new messages into session history (after memory gate).
	initialCount := 1 + len(opts.history) + 1 // system + history + user
	var newMsgs []backend.Message
	if loopResult.Messages != nil && len(loopResult.Messages) > initialCount {
		newMsgs = loopResult.Messages[initialCount:]
	}
	appendHistoryHonoringGate(opts.sess, opts.userMsg, loopResult.FinalContent, newMsgs, loopResult.HoldClose)
	o.compactHistoryAsync(opts.sess)
	return nil
}

// completeTrivialAsk is the tools-free hallway path: persona/roster/space/clock
// stay, but 14b never sees wait_for_threads / delegate_to_agent / consult_agent.
func (o *Orchestrator) completeTrivialAsk(ctx context.Context, opts agentTurnOpts, messages []backend.Message) error {
	if opts.onEvent != nil {
		opts.onEvent(backend.StreamEvent{Type: backend.StreamStatus, Content: "thinking"})
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		messages[0].Content += "\n\nAnswer the current user message only. If they asked who is here or how many people are in this channel, name the teammates from the roster. Do not repeat the local clock unless they asked the time. Do not repeat Pong unless they pinged."
		if line := channelMembersLine(opts.userMsg, workforce.GetChannelMembers(ctx)); line != "" {
			messages[0].Content += "\n\n" + line + ". Answer who-is-here / how many people from this list only, not the desk."
		}
		if company, ok := backend.NamedCompanyRosterAsk(opts.userMsg); ok {
			if line := namedCompanyMembersLine(company, workforce.GetCompanyRoster(ctx, company)); line != "" {
				messages[0].Content += "\n\n" + line + ". Answer who-is-in-" + company + " from this list only, not this channel."
			}
		}
	}
	if !backend.IsTimeAsk(opts.userMsg) {
		kept := messages[:0]
		for _, m := range messages {
			if m.Role == "assistant" && backend.IsLeftoverClockSpeech(m.Content) {
				continue
			}
			kept = append(kept, m)
		}
		messages = kept
	}
	if !isTrivialPing(normalizeTrivialAsk(opts.userMsg)) {
		kept := messages[:0]
		for _, m := range messages {
			if m.Role == "assistant" && backend.IsLeftoverPongSpeech(m.Content) {
				continue
			}
			kept = append(kept, m)
		}
		messages = kept
	}
	ag := opts.ag
	if isTrivialPing(normalizeTrivialAsk(opts.userMsg)) {
		const pong = "Pong."
		if opts.onToken != nil {
			opts.onToken(pong)
		}
		appendHistoryHonoringGate(opts.sess, opts.userMsg, pong, nil, false)
		o.compactHistoryAsync(opts.sess)
		return nil
	}
	if speech := backend.TrivialAckSpeech(opts.userMsg); speech != "" {
		if opts.onToken != nil {
			opts.onToken(speech)
		}
		appendHistoryHonoringGate(opts.sess, opts.userMsg, speech, nil, false)
		o.compactHistoryAsync(opts.sess)
		return nil
	}
	if company, ok := backend.NamedCompanyRosterAsk(opts.userMsg); ok {
		names := workforce.GetCompanyRoster(ctx, company)
		if speech := backend.FillNamedCompanyRosterPersist("", opts.userMsg, company, names); speech != "" {
			if opts.onToken != nil {
				opts.onToken(speech)
			}
			appendHistoryHonoringGate(opts.sess, opts.userMsg, speech, nil, false)
			o.compactHistoryAsync(opts.sess)
			return nil
		}
	}
	b, backendErr := o.backendFor(ag)
	if backendErr != nil {
		return fmt.Errorf("%s(%s): %w", opts.errorPrefix, ag.Name, backendErr)
	}
	var buf strings.Builder
	start := time.Now().UnixNano()
	_, err := b.ChatCompletion(ctx, backend.ChatRequest{
		Model:    ag.GetModelID(),
		Messages: messages,
		OnToken: func(token string) {
			buf.WriteString(token)
			if opts.onToken != nil {
				opts.onToken(token)
			}
		},
		OnEvent: opts.onEvent,
	})
	o.recordLLMLatency(start, opts.latencySlot)
	if err != nil {
		return fmt.Errorf("%s(%s): %w", opts.errorPrefix, ag.Name, err)
	}
	appendHistoryHonoringGate(opts.sess, opts.userMsg, buf.String(), nil, false)
	o.compactHistoryAsync(opts.sess)
	return nil
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
	onToolCall func(string, string, map[string]any),
	onToolDone func(string, string, tools.ToolResult),
	onPermDenied func(string),
	onEvent func(backend.StreamEvent),
) error {
	o.mu.RLock()
	reg := o.toolRegistry
	gate := o.permGate
	sess := o.defaultSession()
	agentReg := o.agentReg
	o.mu.RUnlock()
	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	if reg == nil {
		return o.ChatWithAgent(ctx, ag, userMsg, GetSessionID(ctx), onToken, nil, onEvent)
	}

	taskPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
		if cached {
			return
		}
		callID := fmt.Sprintf("prefetch-%s-%d", toolName, time.Now().UnixNano())
		if onToolCall != nil {
			onToolCall(callID, toolName, args)
		}
		if onToolDone != nil {
			onToolDone(callID, toolName, tools.ToolResult{Output: output})
		}
	}

	// Start vault connect + memory prefetch concurrently with
	// contextBuilder.Build / loadAgentSummaries below (DEFECT B phase 2) —
	// these two phases are independent until runAgentTurn joins them.
	// sessionID matches what the agentTurnOpts below carries: "" (TaskWithAgent
	// does not thread a session ID into opts.sessionID).
	var vaultPrefetch func() vaultPrefetchOutcome
	if !IsTrivialAsk(userMsg) {
		vaultPrefetch = o.startVaultPrefetch(ctx, ag, reg, "", userMsg, taskPrefetchCallback)
	}

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())
	recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
	systemPromptBase := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)
	if agentReg != nil {
		roster := agents.BuildRoster(agentReg, o.ModelInfoFn(), ag.Name)
		systemPromptBase = agents.AppendTeamRoster(systemPromptBase, roster, agents.AgentSupportsDelegation(ag))
	}

	history := sess.snapshotHistory()

	return o.runAgentTurn(ctx, agentTurnOpts{
		ag:               ag,
		userMsg:          userMsg,
		systemPromptBase: systemPromptBase,
		history:          history,
		sess:             sess,
		reg:              reg,
		gate:             gate,
		maxTurns:         maxTurns,
		errorPrefix:      "task",
		latencySlot:      "agent-loop",
		prefetchCb:       taskPrefetchCallback,
		onToken:          onToken,
		onToolCall:       onToolCall,
		onToolDone:       onToolDone,
		onPermDenied:     onPermDenied,
		onEvent:          onEvent,
		vaultPrefetch:    vaultPrefetch,
	})
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
	if err := o.ValidateWiring(); err != nil {
		return err
	}
	// Fast path: read lock — handles the common case where the session already exists.
	o.mu.RLock()
	var sess *Session
	if sessionID != "" {
		sess = o.sessions[sessionID]
	} else {
		sess = o.defaultSession()
	}
	reg := o.toolRegistry
	gate := o.permGate
	maxTurns := o.defaultMaxTurns
	memReplicator := o.optionalMemoryReplicatorLocked()
	agentReg := o.agentReg
	o.mu.RUnlock()
	if maxTurns <= 0 {
		maxTurns = 50
	}

	// Slow path: named session not found — create it under write lock (double-check pattern).
	if sess == nil && sessionID != "" {
		o.mu.Lock()
		if s, ok := o.sessions[sessionID]; ok {
			sess = s // created by a concurrent goroutine between RUnlock and Lock
		} else {
			// Session not in memory (e.g. after server restart). Create a fresh
			// in-memory session for this ID rather than falling back to the shared
			// default session, which may contain stale/malformed history that causes
			// HTTP 400 errors from the LLM API (e.g. tool_use blocks with empty input).
			sess = newSession(sessionID)
			o.sessions[sessionID] = sess
		}
		o.mu.Unlock()
	}

	// Guard against concurrent calls on the same session. Only one agentic loop
	// may run at a time per session — concurrent calls would interleave history
	// appends, producing garbled context for future turns.
	// Queue behind the in-flight run (hallway @mentions share one space-chat
	// session). Do not fail-closed with "already running" — that leaked into
	// #Huginn as assistant speech.
	if !sess.beginExclusiveRun(ctx) {
		// Wrap ErrQueueWaitTimeout so callers (runWSChat) can tell "still
		// queued, keep retrying" apart from a real failure with errors.Is
		// instead of string-sniffing — and MUST NEVER persist this as
		// assistant speech: a queued turn is not an error.
		return fmt.Errorf("chat(%s): session %s still busy after queue wait: %w", ag.Name, sessionID, ErrQueueWaitTimeout)
	}
	defer sess.endRun()
	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	if ag.GetModelID() == "" {
		return fmt.Errorf("agent %q has no model configured — open Agent settings to assign a model", ag.Name)
	}

	// Trivial asks (time/clock/date, ping, thanks, who-is-here) skip repo
	// search, memory summaries, and skills so the 14b sees the message in
	// seconds — not after 60s of orchestration. Clock + roster stay.
	trivial := IsTrivialAsk(userMsg)
	if onEvent != nil {
		onEvent(backend.StreamEvent{Type: backend.StreamStatus, Content: "thinking"})
	}
	if isTrivialPing(normalizeTrivialAsk(userMsg)) {
		const pong = "Pong."
		if onToken != nil {
			onToken(pong)
		}
		appendHistoryHonoringGate(sess, userMsg, pong, nil, false)
		o.compactHistoryAsync(sess)
		return nil
	}
	// "say DELTA" / "repeat X" / "echo X": a bare word to speak back, not a
	// task. Answering it via a full agentic turn is how a trivial ask ends
	// up costing 3 LLM calls and minutes on a slow local model — the model
	// sometimes even delegates it. Skip straight to speaking the word.
	if word, ok := TrivialSayEchoWord(userMsg); ok {
		speech := word + "."
		if onToken != nil {
			onToken(speech)
		}
		appendHistoryHonoringGate(sess, userMsg, speech, nil, false)
		o.compactHistoryAsync(sess)
		return nil
	}
	if speech := backend.TrivialAckSpeech(userMsg); speech != "" {
		if onToken != nil {
			onToken(speech)
		}
		appendHistoryHonoringGate(sess, userMsg, speech, nil, false)
		o.compactHistoryAsync(sess)
		return nil
	}
	if company, ok := backend.NamedCompanyRosterAsk(userMsg); ok {
		names := workforce.GetCompanyRoster(ctx, company)
		if speech := backend.FillNamedCompanyRosterPersist("", userMsg, company, names); speech != "" {
			if onToken != nil {
				onToken(speech)
			}
			appendHistoryHonoringGate(sess, userMsg, speech, nil, false)
			o.compactHistoryAsync(sess)
			return nil
		}
	}
	// muninn_recall/muninn_forget are registered session-locally by
	// connectAgentVault (into a forked registry) — never on the shared
	// o.toolRegistry that `reg` points to here. Passing `reg` straight to
	// tryForgetFastPath always missed the tools and silently fell through
	// to a full ~100s LLM turn (Opus vet, 2026-08-28 — the "forget what I
	// told you about the staging database" repro). Connect the vault first,
	// scoped to forget asks only, so every other turn keeps the old
	// zero-connect trivial-ask fast path untouched.
	if isForgetAsk(userMsg) {
		fvr := o.connectAgentVault(ctx, ag, reg)
		handled := o.tryForgetFastPath(ctx, ag, userMsg, sess, fvr.sessionReg, onToken)
		fvr.cancel()
		if handled {
			return nil
		}
	}
	if o.tryNamedHireFastPath(ctx, ag, userMsg, sess, reg, onToken, onEvent) {
		return nil
	}

	// chatPrefetchCallback forwards prefetch-phase tool events (e.g. an
	// automatic muninn_recall) to onToolEvent so the UI shows "agent recalled
	// memory" even for calls made before the visible tool loop starts.
	chatPrefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
		if cached || onToolEvent == nil {
			return
		}
		// "phase": "prefetch" marks these as silent pre-turn calls. Consumers
		// must key off this, NOT the tool name: muninn_recall is also a tool
		// the agent calls deliberately mid-loop, and a name-based test would
		// swallow those real calls (no chip, no persisted record, no
		// permission audit entry).
		onToolEvent("tool_call", map[string]any{"tool": toolName, "args": args, "phase": "prefetch"})
		onToolEvent("tool_result", map[string]any{"tool": toolName, "result": output, "phase": "prefetch"})
	}

	// Start vault connect + memory prefetch concurrently with
	// contextBuilder.Build / loadAgentSummaries below (DEFECT B phase 2) —
	// only worth it on the tool-registry path (reg != nil), which is the only
	// path that consumes vaultPrefetch; the plain-completion fallback below
	// never calls runAgentTurn. Trivial asks never touch vault MCP at all.
	var vaultPrefetch func() vaultPrefetchOutcome
	if reg != nil && !trivial {
		vaultPrefetch = o.startVaultPrefetch(ctx, ag, reg, sessionID, userMsg, chatPrefetchCallback)
	}

	var ctxText string
	var recentSummaries []agents.SessionSummary
	if !trivial {
		ctxText = o.contextBuilder.Build(userMsg, ag.GetModelID())
		recentSummaries = o.loadAgentSummaries(ctx, ag.Name)
	}
	systemPromptBase := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)
	if agentReg != nil {
		roster := agents.BuildRoster(agentReg, o.ModelInfoFn(), ag.Name)
		systemPromptBase = agents.AppendTeamRoster(systemPromptBase, roster, agents.AgentSupportsDelegation(ag))
	}

	// Per-agent skills fragment. Non-default agents (workflow steps, delegated
	// workers) need their assigned skills appended just like the default agent
	// does in mcp_agent_chat.go. Without this they execute with no skills,
	// which is a major parity gap for scheduled workflows.
	if !trivial {
		if skillsFrag := o.SkillsFragmentForAgent(ag); skillsFrag != "" {
			systemPromptBase += "\n\n" + skillsFrag
		}
	}

	// Inject space context (channel/DM metadata) if available.
	if spaceCtx := workforce.GetSpaceContext(ctx); spaceCtx != "" {
		systemPromptBase += "\n\n" + spaceCtx
	}
	// Inject channel-recent summary (channels only, not DMs).
	if recentCtx := workforce.GetChannelRecent(ctx); recentCtx != "" {
		systemPromptBase += "\n\n" + recentCtx
	}

	// Inject per-step pre-authorised connection picks (Phase 1.4). When a
	// workflow step declares `connections: { github: my-personal-gh, ... }`
	// the runner places that map into ctx via WithStepConnections; surface it
	// as a system addendum so the agent uses those account labels in tool calls.
	if connHint := stepConnectionsAddendum(ctx); connHint != "" {
		systemPromptBase += "\n\n" + connHint
	}

	// Machine local clock (America/New_York, labeled ET). One short line.
	// Do not invent a vault — this is the wall clock at inject.
	systemPromptBase = backend.AppendLocalClock(systemPromptBase, time.Now())

	history := sess.snapshotHistory()

	// When a tool registry is configured, delegate to the shared runAgentTurn
	// core. If the model rejects tool schemas (e.g. deepseek-r1 on Ollama),
	// runAgentTurn returns errToolsNotSupported and we fall through to the plain
	// single-turn completion below.
	if reg != nil {
		// toolArgsMu guards toolArgsCapture against concurrent writes from parallel
		// tool dispatches. dispatchTools spawns one goroutine per tool call, so
		// OnToolCall/OnToolDone can fire concurrently.
		var toolArgsMu sync.Mutex
		// toolArgsCapture stores args keyed by callID (the LLM-assigned tool call ID).
		// Entries are deleted in OnToolDone to prevent unbounded growth per turn.
		// Keying by callID (not tool name) fixes the same-tool-twice collision.
		toolArgsCapture := make(map[string]map[string]any)

		chatOnToolCall := func(callID string, name string, args map[string]any) {
			slog.Debug("tool call started", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID)
			toolArgsMu.Lock()
			toolArgsCapture[callID] = args
			toolArgsMu.Unlock()
			if onToolEvent != nil {
				// Carry the real callID: consumers pair tool_call with
				// tool_result by it. Tools run in parallel (dispatchTools),
				// so name-based pairing mis-attributes results between two
				// concurrent same-name calls.
				onToolEvent("tool_call", map[string]any{"id": callID, "tool": name, "args": args})
			} else if onEvent != nil {
				onEvent(backend.StreamEvent{
					Type:    backend.StreamToolCall,
					Payload: map[string]any{"id": callID, "tool": name, "args": args},
				})
			}
		}

		chatOnToolDone := func(callID string, name string, result tools.ToolResult) {
			toolArgsMu.Lock()
			capturedArgs := toolArgsCapture[callID]
			delete(toolArgsCapture, callID)
			toolArgsMu.Unlock()
			permissionDenied := false
			reasonCode := ""
			reason := ""
			if result.Metadata != nil {
				if denied, ok := result.Metadata["permission_denied"].(bool); ok {
					permissionDenied = denied
				}
				if rc, ok := result.Metadata["reason_code"].(string); ok {
					reasonCode = rc
				}
				if rs, ok := result.Metadata["reason"].(string); ok {
					reason = rs
				}
			}
			// Replicate memory writes to other channel members' vaults.
			if memReplicator != nil && isMemoryToolName(name) && !result.IsError {
				if replCtx := workforce.GetReplicationContext(ctx); replCtx != nil {
					memReplicator.Intercept(ctx, name, capturedArgs, result, ag.Name, replCtx)
				}
			}
			slog.Debug("tool call done", "agent", ag.Name, "tool", name, "session_id", sessionID, "call_id", callID, "success", result.Error == "")
			if onToolEvent != nil {
				// id/args/success must match the onEvent branch below: the WS
				// path routes ALL tools through onToolEvent, so anything
				// missing here is lost from the tool chip, the persisted
				// tool-call record, and the permission audit entry.
				payload := map[string]any{
					"id":      callID,
					"tool":    name,
					"result":  toolResultDisplayText(result),
					"args":    capturedArgs,
					"success": result.Error == "",
				}
				if result.Metadata != nil {
					payload["metadata"] = result.Metadata
				}
				if permissionDenied {
					payload["permission_denied"] = true
					payload["reason_code"] = reasonCode
					payload["reason"] = reason
				}
				onToolEvent("tool_result", payload)
			} else if onEvent != nil {
				payload := map[string]any{
					"id":      callID,
					"tool":    name,
					"success": result.Error == "",
					"result":  toolResultDisplayText(result),
					"args":    capturedArgs,
				}
				if result.Metadata != nil {
					payload["metadata"] = result.Metadata
				}
				if permissionDenied {
					payload["permission_denied"] = true
					payload["reason_code"] = reasonCode
					payload["reason"] = reason
				}
				onEvent(backend.StreamEvent{
					Type:    backend.StreamToolResult,
					Payload: payload,
				})
			}
		}

		turnErr := o.runAgentTurn(ctx, agentTurnOpts{
			ag:               ag,
			userMsg:          userMsg,
			systemPromptBase: systemPromptBase,
			history:          history,
			sess:             sess,
			reg:              reg,
			gate:             gate,
			maxTurns:         maxTurns,
			errorPrefix:      "chat",
			latencySlot:      "agent-chat",
			sessionID:        sessionID,
			prefetchCb:       chatPrefetchCallback,
			onToken:          onToken,
			onToolCall:       chatOnToolCall,
			onToolDone:       chatOnToolDone,
			onEvent:          onEvent,
			vaultPrefetch:    vaultPrefetch,
			// ctxSetup wires the session ID into ctx and establishes a delegation
			// context so downstream code (e.g. threadmgr) can trace the lineage.
			ctxSetup: func(c context.Context) context.Context {
				// Space-thread wakes use an ephemeral orch session so leftover
				// speech cannot land as a hallway root. The runner pre-sets the
				// real hallway / space session on ctx for delegate_to_agent —
				// do not overwrite it.
				if GetSessionID(c) == "" && sessionID != "" {
					c = SetSessionID(c, sessionID)
				}
				if threadmgr.GetCallingAgent(c) == "" {
					c = threadmgr.SetCallingAgent(c, ag.Name)
				}
				if GetDelegationContext(c) == nil {
					dcSID := GetSessionID(c)
					if dcSID == "" {
						dcSID = sessionID
					}
					dc := workforce.NewDelegationContext(dcSID, ag.Name, o.maxDelegationDepth())
					c = WithDelegationContext(c, &dc)
				}
				return c
			},
		})
		if turnErr == nil {
			return nil
		}
		if turnErr != errToolsNotSupported {
			return turnErr
		}
		// Model doesn't support function calling — fall through to plain chat.
	}

	// No tool registry, or model doesn't support tools — direct single-turn completion.
	msgs := []backend.Message{{Role: "system", Content: systemPromptBase}}
	msgs = append(msgs, history...)
	msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

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
	o.compactHistoryAsync(sess)
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
