package agent

import (
	"context"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
)

// AgentChat runs a user message through the full agentic loop (tool-calling).
// Falls back to plain Chat if no tool registry is configured or tools not available.
// Callbacks: onToken (each token), onToolCall (before exec), onToolDone (after exec), onPermDenied.
func (o *Orchestrator) AgentChat(
	ctx context.Context,
	userMsg string,
	maxTurns int,
	onToken func(string),
	onToolCall func(callID string, name string, args map[string]any),
	onToolDone func(callID string, name string, result tools.ToolResult),
	onPermDenied func(name string),
	onBeforeWrite func(path string, oldContent, newContent []byte) bool,
	onEvent func(backend.StreamEvent),
) error {
	if err := o.ValidateWiring(); err != nil {
		return err
	}
	o.mu.Lock()
	reg := o.toolRegistry
	gate := o.permGate
	agReg := o.agentReg
	sess := o.defaultSession()
	o.mu.Unlock()
	sess.setState(StateAgentLoop)

	defer sess.setState(StateIdle)

	// If no tool registry, fall back to plain chat.
	if reg == nil {
		return o.Chat(ctx, userMsg, onToken, onEvent)
	}

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())

	globalInstructions := LoadGlobalInstructions()
	// Project instructions (.huginn.md) now flow through ctxText via
	// ContextBuilder.BuildCtx (internal/agent/context.go), which every prompt
	// path shares — so they don't need loading again here.
	projectInstructions := ""

	// Resolve the default agent once — used for vault connection, system prompt, and toolbelt.
	var defaultAgent *agents.Agent
	agentName := ""
	agentVaultName := ""
	agentMemoryMode := ""
	agentVaultDescription := ""
	contextNotesBlock := ""
	if agReg != nil {
		if da := agReg.DefaultAgent(); da != nil {
			defaultAgent = da
			agentName = da.Name
			agentVaultName = da.VaultName
			agentMemoryMode = da.MemoryMode
			agentVaultDescription = da.VaultDescription
			if da.ContextNotesEnabled && o.huginnHome != "" {
				contextNotesBlock = mem.NotesPromptBlock(o.huginnHome, da.Name)
			}
		}
	}

	// Connect to MuninnDB vault for this session — forks the shared registry so
	// vault tools are isolated per session. Always safe to call; degrades gracefully.
	vr := o.connectAgentVault(ctx, defaultAgent, reg)
	defer vr.cancel()

	if vr.warning != "" {
		logVaultUnavailable(agentName, "", vr.warning)
	}

	// Resolve skills fragment: per-agent if assigned, global fallback otherwise.
	agentSkillsFragment := o.skillsFragmentFor(agReg)

	// Use the session-forked registry so vault tools are visible to the prompt builder
	// and toolbelt filter — the shared reg is never mutated.
	systemPrompt := buildAgentSystemPrompt(ctxText, agentSkillsFragment, vr.sessionReg, globalInstructions, projectInstructions, agentName, contextNotesBlock, agentMemoryMode, agentVaultName, agentVaultDescription)

	history := sess.snapshotHistory()

	messages := []backend.Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, backend.Message{Role: "user", Content: userMsg})

	ctx = WithMemoryGate(ctx, agentMemoryMode, GetSessionID(ctx), agentName)
	if memCtx := o.prefetchMemoryContextWithEvents(ctx, vr.sessionReg, agentName, agentVaultName, userMsg, nil); memCtx != "" {
		messages[0].Content += memCtx
	}

	// Check if the model supports tools; fall back to plain chat if not.
	if o.registry != nil && !o.registry.ModelSupportsTools(o.defaultModelName()) {
		return o.Chat(ctx, userMsg, onToken, onEvent)
	}

	// Apply toolbelt restrictions from the default agent using the session-forked registry.
	// Fork also gives this run its own isolated gate so the shared o.permGate is never mutated.
	var schemas []backend.Tool
	runGate := gate // fallback: shared gate with no provider restrictions
	if defaultAgent != nil {
		schemas, runGate = applyToolbelt(defaultAgent, vr.sessionReg, gate)
		// runGate is now a per-run fork (own sweep goroutine) — close it when
		// this run ends or the sweeper leaks. Guarded so the shared fallback
		// gate above is never closed.
		if runGate != nil && runGate != gate {
			forked := runGate
			defer forked.Close()
		}
	}
	if schemas == nil {
		// No agent configured (or no default agent): allow all registered tools.
		schemas = vr.sessionReg.AllSchemas()
	}
	schemas = filterMuninnSchemas(schemas, agentMemoryMode)

	_, agentChatModel, agentChatBackend, agentChatErr := o.resolveDefaultAgent()
	if agentChatErr != nil {
		return agentChatErr
	}
	cfg := RunLoopConfig{
		Hooks:              o.toolHooks(),
		MaxTurns:           maxTurns,
		Messages:           messages,
		Tools:              vr.sessionReg,
		ToolSchemas:        schemas,
		Gate:               runGate,
		Backend:            agentChatBackend,
		ModelName:          agentChatModel,
		OnToken:            onToken,
		OnEvent:            onEvent,
		OnToolCall:         onToolCall,
		OnToolDone:         onToolDone,
		OnPermissionDenied: onPermDenied,
		OnBeforeWrite:      onBeforeWrite,
		VaultReconnector:   vr.reconnector,
		SessionID:          GetSessionID(ctx),
		MetricsWriter:      o.runLoopMetrics(),
		TurnKind:           "agent-loop",
	}
	if defaultAgent != nil {
		cfg.AgentName = defaultAgent.Name
		cfg.MemoryMode = agentMemoryMode
		cfg.MemoryVault = pinMuninnVault(agentVaultName)
		cfg.MemoryAgent = agentName
		cfg.MemoryUserMsg = userMsg
		cfg.MemorySession = GetSessionID(ctx)
		cfg.MemoryHome = o.huginnHome
	}

	loopStart := time.Now().UnixNano()
	loopResult, err := RunLoop(ctx, cfg)
	o.recordLLMLatency(loopStart, "agent-loop")
	if loopResult != nil {
		o.lastUsagePrompt.Store(int64(loopResult.PromptTokens))
		o.lastUsageCompletion.Store(int64(loopResult.CompletionTokens))
	}
	if err != nil {
		return err
	}

	// The loop's Messages slice starts with the messages we passed in (system + history + user).
	// We only want to append the NEW messages from this loop (tool calls, tool results, final assistant).
	initialCount := 1 + len(history) + 1 // system msg + history msgs + user msg
	var newMsgs []backend.Message
	if loopResult.Messages != nil && len(loopResult.Messages) > initialCount {
		newMsgs = loopResult.Messages[initialCount:]
	}
	appendHistoryHonoringGate(sess, userMsg, loopResult.FinalContent, newMsgs, loopResult.HoldClose)
	o.compactHistoryAsync(sess)

	return nil
}
