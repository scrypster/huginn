package agent

import (
	"context"
	"fmt"
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

	o.mu.Lock()
	wsRoot := o.workspaceRoot
	o.mu.Unlock()
	globalInstructions := LoadGlobalInstructions()
	projectInstructions := LoadProjectInstructions(wsRoot)

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

	// Surface MCP setup failures as a visible warning in the chat stream.
	if vr.warning != "" && onEvent != nil {
		onEvent(backend.StreamEvent{
			Type:    backend.StreamWarning,
			Content: fmt.Sprintf("\u26a0\ufe0f Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
		})
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
	}
	if schemas == nil {
		// No agent configured (or no default agent): allow all registered tools.
		schemas = vr.sessionReg.AllSchemas()
	}

	_, agentChatModel, agentChatBackend, agentChatErr := o.resolveDefaultAgent()
	if agentChatErr != nil {
		return agentChatErr
	}
	cfg := RunLoopConfig{
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
	if loopResult.Messages != nil && len(loopResult.Messages) > initialCount {
		newMsgs := loopResult.Messages[initialCount:]
		sess.appendHistory(newMsgs...)
	} else {
		// Fallback: at minimum record user + final response.
		sess.appendHistory(
			backend.Message{Role: "user", Content: userMsg},
			backend.Message{Role: "assistant", Content: loopResult.FinalContent},
		)
	}
	o.compactHistory(ctx, sess)

	return nil
}
