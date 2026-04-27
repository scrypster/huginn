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
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
)

// ChatForSession is the session-scoped version of Chat. It looks up the session
// by ID rather than using the default session. Used for multi-session concurrent access.
// Unlike Chat, it uses the per-session lock (sess.mu) during the LLM call rather than
// the global orchestrator lock (o.mu), allowing two sessions to call ChatForSession
// concurrently without blocking each other.
func (o *Orchestrator) ChatForSession(ctx context.Context, sessionID string, userMsg string, onToken func(string), onEvent func(backend.StreamEvent)) error {
	o.mu.RLock()
	sess, ok := o.sessions[sessionID]
	o.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())

	o.mu.RLock()
	agReg := o.agentReg
	huginnHome := o.huginnHome
	o.mu.RUnlock()

	agentName := ""
	notesBlock := ""
	if agReg != nil {
		if defaultAgent := agReg.DefaultAgent(); defaultAgent != nil {
			agentName = defaultAgent.Name
			if defaultAgent.ContextNotesEnabled && huginnHome != "" {
				notesBlock = mem.NotesPromptBlock(huginnHome, defaultAgent.Name)
			}
		}
	}

	namePrefix := ""
	if agentName != "" {
		namePrefix = "Your name is " + agentName + ".\n\n"
	}
	notesInfix := ""
	if notesBlock != "" {
		notesInfix = notesBlock + "\n\n"
	}
	systemPrompt := namePrefix + notesInfix + "You are Huginn, a helpful AI coding assistant. " +
		"Use markdown formatting — tables, bold, code blocks, lists — when it improves readability.\n\n" + ctxText

	history := sess.snapshotHistory()
	msgs := []backend.Message{{Role: "system", Content: systemPrompt}}
	msgs = append(msgs, history...)
	msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

	_, resolvedModel, resolvedBackend, resolveErr := o.resolveDefaultAgent()
	if resolveErr != nil {
		return resolveErr
	}

	var buf strings.Builder
	chatStart := time.Now().UnixNano()
	resp, err := resolvedBackend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    resolvedModel,
		Messages: msgs,
		OnToken: func(token string) {
			buf.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		},
		OnEvent: onEvent,
	})
	o.recordLLMLatency(chatStart, "coder")
	if resp != nil {
		o.lastUsagePrompt.Store(int64(resp.PromptTokens))
		o.lastUsageCompletion.Store(int64(resp.CompletionTokens))
	}
	if err != nil {
		return err
	}

	sess.appendHistory(
		backend.Message{Role: "user", Content: userMsg},
		backend.Message{Role: "assistant", Content: buf.String()},
	)
	return nil
}

// ChatForSessionWithAgent handles a cloud relay chat turn using the full agentic stack.
// It looks up the session by ID (preserving relay session history), resolves the default
// agent, connects the vault (graceful degradation on failure), and runs the full RunLoop
// with per-agent model selection and tool calling. Tool events are forwarded via onToolEvent.
//
// Falls back to plain ChatCompletion when:
//   - no default agent is configured (no agentReg or no default agent)
//   - no tool registry is available
//   - the model reports it does not support tools
func (o *Orchestrator) ChatForSessionWithAgent(ctx context.Context, sessionID, userMsg string,
	onToken func(string),
	onToolEvent func(eventType string, payload map[string]any),
	onEvent func(backend.StreamEvent)) error {

	o.mu.RLock()
	sess, ok := o.sessions[sessionID]
	reg := o.toolRegistry
	gate := o.permGate
	o.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// resolveDefaultAgent handles nil agentReg gracefully: returns nil ag + default backend.
	// backendFor(nil) falls through to o.backend, so chatBackend is always valid here.
	ag, _, chatBackend, err := o.resolveDefaultAgent()
	if err != nil {
		return fmt.Errorf("chat-session: backend unavailable: %w", err)
	}

	sess.setState(StateAgentLoop)
	defer sess.setState(StateIdle)

	modelID := ""
	if ag != nil {
		modelID = ag.GetModelID()
	}
	ctxText := o.contextBuilder.Build(userMsg, modelID)

	history := sess.snapshotHistory()
	// Pre-call guard: truncate history to prevent context window overflow.
	// compactHistory still runs post-call for steady-state summarization.
	const maxRelayHistory = 40
	if len(history) > maxRelayHistory {
		history = history[len(history)-maxRelayHistory:]
	}

	// Full agentic path: only when both an agent and a tool registry are available.
	if ag != nil && reg != nil {
		recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
		systemPrompt := agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)

		msgs := []backend.Message{{Role: "system", Content: systemPrompt}}
		msgs = append(msgs, history...)
		msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

		vr := o.connectAgentVault(ctx, ag, reg)
		defer vr.cancel()
		if vr.warning != "" && onEvent != nil {
			onEvent(backend.StreamEvent{
				Type:    backend.StreamWarning,
				Content: fmt.Sprintf("\u26a0\ufe0f Memory vault unavailable: %s. Memory features are disabled for this session.", vr.warning),
			})
		}
		if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
			msgs[0].Content += memoryModeInstruction(ag.MemoryMode, ag.VaultName, ag.VaultDescription)
		}
		// Pre-fetch memory orientation and inject into system prompt. Surface
		// synthetic tool_call/tool_result events so the UI can display
		// "agent recalled memory" — but skip duplicate events for cache hits
		// to avoid flooding the timeline on every turn.
		prefetchCallback := func(toolName string, args map[string]any, output string, cached bool) {
			if cached || onToolEvent == nil {
				return
			}
			onToolEvent("tool_call", map[string]any{"tool": toolName, "args": args})
			onToolEvent("tool_result", map[string]any{"tool": toolName, "result": output})
		}
		if memCtx := o.prefetchMemoryContextWithEvents(ctx, vr.sessionReg, ag.Name, ag.VaultName, userMsg, prefetchCallback); memCtx != "" {
			msgs[0].Content += memCtx
		}

		ctx = SetSessionID(ctx, sessionID)
		schemas, agentGate := applyToolbelt(ag, vr.sessionReg, gate)

		agentSess, sessErr := session.BuildAndSetup(agentToolbelt(ag))
		if sessErr != nil {
			slog.Warn("agent session setup failed", "agent", ag.Name, "err", sessErr)
			agentSess = &session.Session{}
		}
		defer agentSess.Teardown()
		ctx = session.WithEnv(ctx, agentSess.Env)

		loopCfg := RunLoopConfig{
			MaxTurns:      50,
			ModelName:     ag.GetModelID(),
			Messages:      msgs,
			Tools:         vr.sessionReg,
			ToolSchemas:   schemas,
			Gate:          agentGate,
			Backend:       chatBackend,
			OnToken:       onToken,
			OnEvent:          onEvent,
			VaultWarnOnce:    &sync.Once{},
			VaultReconnector: vr.reconnector,
			OnToolCall: func(callID string, name string, args map[string]any) {
				if onToolEvent != nil {
					onToolEvent("tool_call", map[string]any{"tool": name, "args": args})
				}
			},
			OnToolDone: func(callID string, name string, result tools.ToolResult) {
				if onToolEvent != nil {
					onToolEvent("tool_result", map[string]any{"tool": name, "result": result.Output})
				}
			},
		}
		t0 := time.Now().UnixNano()
		res, loopErr := RunLoop(ctx, loopCfg)
		o.recordLLMLatency(t0, "agent-chat")
		if loopErr != nil && !strings.Contains(loopErr.Error(), "does not support tools") {
			return fmt.Errorf("chat-session(%s): %w", ag.Name, loopErr)
		}
		if loopErr == nil {
			// Preserve full tool-call/tool-result history so subsequent turns
			// have accurate context. initialCount = system msg (1) + history + user msg (1).
			initialCount := 1 + len(history) + 1
			if res.Messages != nil && len(res.Messages) > initialCount {
				sess.appendHistory(res.Messages[initialCount:]...)
			} else {
				sess.appendHistory(
					backend.Message{Role: "user", Content: userMsg},
					backend.Message{Role: "assistant", Content: res.FinalContent},
				)
			}
			o.compactHistory(ctx, sess)
			return nil
		}
		// Fall through: model doesn't support tools — plain completion below.
	}

	// Plain completion: no agent/registry, or tools-unsupported model.
	var systemPrompt string
	if ag != nil {
		recentSummaries := o.loadAgentSummaries(ctx, ag.Name)
		systemPrompt = agents.BuildPersonaPromptWithMemory(ag, ctxText, recentSummaries)
	} else {
		systemPrompt = "You are Huginn, a helpful AI coding assistant. " +
			"Use markdown formatting when it improves readability.\n\n" + ctxText
	}
	msgs := []backend.Message{{Role: "system", Content: systemPrompt}}
	msgs = append(msgs, history...)
	msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

	var buf strings.Builder
	t0 := time.Now().UnixNano()
	_, err = chatBackend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    modelID,
		Messages: msgs,
		OnToken: func(tok string) {
			buf.WriteString(tok)
			if onToken != nil {
				onToken(tok)
			}
		},
		OnEvent: onEvent,
	})
	o.recordLLMLatency(t0, "agent-chat")
	if err != nil {
		return fmt.Errorf("chat-session: %w", err)
	}
	sess.appendHistory(
		backend.Message{Role: "user", Content: userMsg},
		backend.Message{Role: "assistant", Content: buf.String()},
	)
	o.compactHistory(ctx, sess)
	return nil
}

// compactHistory attempts to compact the conversation history based on configured strategy.
// sess must be the session whose history was just updated (not necessarily the default session).
func (o *Orchestrator) compactHistory(ctx context.Context, sess *Session) {
	o.mu.RLock()
	// Read fields directly — do NOT call defaultModelName() here; it also acquires
	// o.mu.RLock and will deadlock if a writer is pending (recursive read-lock).
	agReg := o.agentReg
	dm := o.defaultModel
	comp := o.compactor
	fallbackCompactBackend := o.backend
	o.mu.RUnlock()

	// Mirror defaultModelName() logic without holding the lock.
	modelName := dm
	if agReg != nil {
		if ag := agReg.DefaultAgent(); ag != nil {
			if id := ag.GetModelID(); id != "" {
				modelName = id
			}
		}
	}

	if comp == nil {
		return
	}

	_, _, compactBackend, _ := o.resolveDefaultAgent()
	if compactBackend == nil {
		compactBackend = fallbackCompactBackend
	}
	snapshot := sess.snapshotHistory()
	newHistory, wasCompacted, _ := comp.MaybeCompact(ctx, snapshot, compactBackend, modelName)
	if wasCompacted {
		sess.replaceHistory(newHistory)
		o.sc.Record("agent.compaction_triggered", 1)
	}
}

// Chat sends a direct message to the coder model without planning.
// onEvent is called for each StreamEvent (richer streaming; supersedes onToken if provided).
func (o *Orchestrator) Chat(ctx context.Context, userMsg string, onToken func(string), onEvent func(backend.StreamEvent)) error {
	o.mu.RLock()
	sess := o.defaultSession()
	o.mu.RUnlock()
	sess.setState(StateAgentLoop)

	defer sess.setState(StateIdle)

	ctxText := o.contextBuilder.Build(userMsg, o.defaultModelName())
	systemPrompt := "You are Huginn, a helpful AI coding assistant. " +
		"Use markdown formatting — tables, bold, code blocks, lists — when it improves readability.\n\n" + ctxText

	history := sess.snapshotHistory()

	msgs := []backend.Message{
		{Role: "system", Content: systemPrompt},
	}
	msgs = append(msgs, history...)
	msgs = append(msgs, backend.Message{Role: "user", Content: userMsg})

	_, chatModel, chatBackend, chatResolveErr := o.resolveDefaultAgent()
	if chatResolveErr != nil {
		return chatResolveErr
	}
	var buf strings.Builder
	chatStart := time.Now().UnixNano()
	resp, err := chatBackend.ChatCompletion(ctx, backend.ChatRequest{
		Model:    chatModel,
		Messages: msgs,
		OnToken: func(token string) {
			buf.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		},
		OnEvent: onEvent,
	})
	o.recordLLMLatency(chatStart, "coder")
	if resp != nil {
		o.lastUsagePrompt.Store(int64(resp.PromptTokens))
		o.lastUsageCompletion.Store(int64(resp.CompletionTokens))
	}
	if err != nil {
		return err
	}

	sess.appendHistory(
		backend.Message{Role: "user", Content: userMsg},
		backend.Message{Role: "assistant", Content: buf.String()},
	)
	o.compactHistory(ctx, sess)
	return nil
}

// BatchChatResult holds the output of a single task in BatchChat.
type BatchChatResult struct {
	Task   string // original task text
	Output string // full model response
	Err    error
}

// BatchChat runs multiple independent tasks concurrently, each in a fresh context
// (system prompt + task only, no shared history). Results are returned in task order.
// This is used by the /parallel TUI command.
func (o *Orchestrator) BatchChat(ctx context.Context, tasks []string) []BatchChatResult {
	if len(tasks) == 0 {
		return nil
	}

	o.mu.RLock()
	model := o.defaultModelName()
	o.mu.RUnlock()

	o.mu.RLock()
	fallbackBatchBackend := o.backend
	o.mu.RUnlock()
	_, _, batchBackend, batchErr := o.resolveDefaultAgent()
	if batchErr != nil {
		batchBackend = fallbackBatchBackend
	}
	results := make([]BatchChatResult, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t string) {
			defer wg.Done()
			results[idx].Task = t
			ctxText := o.contextBuilder.Build(t, model)
			systemPrompt := "You are Huginn, a helpful AI coding assistant. " +
				"Use markdown formatting when it improves readability.\n\n" + ctxText
			msgs := []backend.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: t},
			}
			var buf strings.Builder
			_, err := batchBackend.ChatCompletion(ctx, backend.ChatRequest{
				Model:    model,
				Messages: msgs,
				OnToken:  func(tok string) { buf.WriteString(tok) },
			})
			if err != nil {
				results[idx].Err = err
				return
			}
			results[idx].Output = buf.String()
		}(i, task)
	}
	wg.Wait()
	return results
}
