package threadmgr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/logger"
	"github.com/scrypster/huginn/internal/session"
)

const maxTurns = 50

// HelpResolveTimeout is the maximum time we will wait for a HelpResolver to
// return an answer. If the resolver hangs (e.g. network partition, dead LLM
// backend) beyond this deadline the thread is cancelled and a warning is
// logged. 30 minutes is intentionally generous — human users can be slow.
const HelpResolveTimeout = 30 * time.Minute

// BroadcastFn is a function that sends a typed payload to all WebSocket clients
// subscribed to the given sessionID.
type BroadcastFn func(sessionID, msgType string, payload map[string]any)

// DagFn is called after a thread completes (done or error) to trigger DAG evaluation.
type DagFn func()

// loopResultKind indicates why runOnce exited.
type loopResultKind int

const (
	loopDone    loopResultKind = iota // thread completed (finish, error, max turns, context cancel)
	loopBlocked                       // ErrHelp — waiting for user input
)

type loopResult struct {
	kind    loopResultKind
	helpMsg string // only set when kind == loopBlocked
}

// SpawnThread launches a goroutine that drives the LLM tool-calling loop for
// the given thread. It returns immediately; the goroutine runs independently.
//
// Parameters:
//   - ctx: parent context; cancellation propagates into the LLM calls
//   - threadID: the thread to run (must exist in the manager)
//   - store: session store for persisting thread messages
//   - sess: active session (for session ID and model)
//   - reg: agent registry to resolve the thread's AgentID
//   - b: LLM backend
//   - broadcast: function to push events to WebSocket clients
//   - ca: shared cost accumulator
//   - dagFn: called on completion; may be nil
func (tm *ThreadManager) SpawnThread(
	ctx context.Context,
	threadID string,
	store session.StoreInterface,
	sess *session.Session,
	reg *agents.AgentRegistry,
	b backend.Backend,
	broadcast BroadcastFn,
	ca *CostAccumulator,
	dagFn DagFn,
) {
	logger.Info("SpawnThread: enter", "thread_id", threadID, "ctx_err", ctx.Err())

	threadCtx, cancel := context.WithCancel(ctx)

	if !tm.Start(threadID, threadCtx, cancel) {
		// Another goroutine already won the race and transitioned this thread
		// out of StatusQueued. Discard the context we just created and bail.
		logger.Warn("SpawnThread: Start() returned false — thread not in StatusQueued",
			"thread_id", threadID)
		cancel()
		return
	}
	logger.Info("SpawnThread: started successfully", "thread_id", threadID)

	agentID := ""
	task := ""
	parentMessageID := ""
	var threadTimeout time.Duration
	if t, ok := tm.Get(threadID); ok {
		agentID = t.AgentID
		task = t.Task
		parentMessageID = t.ParentMessageID
		threadTimeout = t.Timeout
	}

	// Apply per-thread deadline when Timeout is set.
	if threadTimeout > 0 {
		var timeoutCancel context.CancelFunc
		threadCtx, timeoutCancel = context.WithTimeout(threadCtx, threadTimeout)
		// Chain cancels: timeoutCancel is released when the goroutine exits via
		// the deferred cancel() below (which closes threadCtx's parent).
		_ = timeoutCancel // goroutine captures threadCtx; parent cancel cleans up
	}

	// Snapshot emitter reference under read lock for goroutine safety.
	tm.mu.RLock()
	emitter := tm.emitter
	tm.mu.RUnlock()

	// Snapshot space_id once — immutable after session creation so safe to read outside goroutine.
	spaceID := sess.SpaceID()

	// Emit "spawned" lifecycle event before the goroutine starts.
	emitter.Emit(ThreadEvent{
		Event:     "spawned",
		ThreadID:  threadID,
		AgentID:   agentID,
		Task:      task,
		SpaceID:   spaceID,
		SessionID: sess.ID,
	})

	threadStartedPayload := map[string]any{
		"thread_id": threadID,
		"agent_id":  agentID,
		"task":      task,
	}
	if parentMessageID != "" {
		threadStartedPayload["parent_message_id"] = parentMessageID
	}
	broadcast(sess.ID, "thread_started", threadStartedPayload)

	// Emit "started" lifecycle event (goroutine is about to launch).
	emitter.Emit(ThreadEvent{
		Event:     "started",
		ThreadID:  threadID,
		AgentID:   agentID,
		Task:      task,
		SpaceID:   spaceID,
		SessionID: sess.ID,
	})

	go func() {
		defer cancel() // always release context when goroutine exits

		// Snapshot resolver references under read lock to avoid data races.
		tm.mu.RLock()
		helpResolver := tm.helpResolver
		completionNotifier := tm.completionNotifier
		resolveBackend := tm.backendFor
		preparer := tm.runtimePreparer
		tm.mu.RUnlock()

		// Build per-agent runtime (toolbelt + MuninnDB vault + memory_mode
		// prompt). Do this once per thread so we connect the vault and fork
		// the gate exactly once, then re-use across help-block resume cycles.
		// On preparer error or nil runtime we fall back to the legacy global
		// toolRegistry/toolExecutor path — vault outages must not kill
		// delegation, and tests can opt out by leaving the preparer unset.
		var runtime *AgentRuntime
		if preparer != nil {
			rt, prepErr := preparer(threadCtx, agentID)
			if prepErr != nil {
				slog.Warn("threadmgr: AgentRuntimePreparer failed; falling back to global toolset",
					"thread_id", threadID, "agent", agentID, "err", prepErr)
			} else if rt != nil {
				runtime = rt
				defer func() {
					if runtime != nil && runtime.Cleanup != nil {
						runtime.Cleanup()
					}
				}()
			}
		}

		// Resolve the correct backend for this agent's provider. The raw `b`
		// passed to SpawnThread is the fallback (often Ollama on localhost).
		// Agents that specify a provider (e.g. "anthropic") need the resolver
		// to get the right backend; without this Sam would talk to Ollama.
		agentBackend := b
		if resolveBackend != nil && reg != nil {
			if ag, found := reg.ByName(agentID); found && ag.Provider != "" {
				if resolved, err := resolveBackend(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID()); err == nil {
					agentBackend = resolved
					logger.Info("SpawnThread: resolved agent-specific backend",
						"thread_id", threadID, "agent", agentID, "provider", ag.Provider)
				} else {
					logger.Warn("SpawnThread: backend resolution failed, using fallback",
						"thread_id", threadID, "agent", agentID, "provider", ag.Provider, "err", err)
				}
			} else {
				logger.Info("SpawnThread: using default backend (no provider set)",
					"thread_id", threadID, "agent", agentID)
			}
		} else {
			logger.Info("SpawnThread: no resolveBackend or reg, using default backend",
				"thread_id", threadID, "agent", agentID)
		}

		// emitter is already snapshotted above (before the goroutine launch).

		var injectedInput string
		var tokenCounter int64
		for {
			result := tm.runOnce(threadCtx, threadID, agentID, injectedInput, sess, store, reg, agentBackend, broadcast, ca, dagFn, helpResolver, completionNotifier, emitter, &tokenCounter, runtime)
			if result.kind == loopDone {
				return
			}
			// loopBlocked — wait for user input
			input, ok := tm.waitForInputOnce(threadCtx, threadID)
			if !ok {
				// context cancelled
				tm.Cancel(threadID)
				return
			}
			// Append user input to thread history for context building
			if err := store.AppendToThread(sess.ID, threadID, session.SessionMessage{
				Role:    "user",
				Content: input,
			}); err != nil {
				slog.Warn("threadmgr: failed to append to thread", "thread_id", threadID, "err", err)
			}
			injectedInput = input
		}
	}()
}

// runOnce runs the LLM tool loop for one session (up to maxTurns). It uses
// defer/recover to catch ErrFinish, ErrHelp, and unexpected panics.
// Returns a loopResult indicating whether the thread is done or blocked.
func (tm *ThreadManager) runOnce(
	ctx context.Context,
	threadID string,
	agentID string,
	injectedInput string,
	sess *session.Session,
	store session.StoreInterface,
	reg *agents.AgentRegistry,
	b backend.Backend,
	broadcast BroadcastFn,
	ca *CostAccumulator,
	dagFn DagFn,
	helpResolver HelpResolver,
	completionNotifier *CompletionNotifier,
	emitter *EventEmitter,
	tokenCounter *int64,
	runtime *AgentRuntime,
) (result loopResult) {
	result = loopResult{kind: loopDone} // default: done

	startTime := time.Now()
	elapsed := func() int { return int(time.Since(startTime).Milliseconds()) }

	// history holds the in-memory conversation for this thread's loop.
	var history []backend.Message

	// Panic recovery: catches ErrFinish, ErrHelp, and unexpected panics.
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case *ErrFinish:
				tm.Complete(threadID, v.Summary)
				broadcast(sess.ID, "thread_done", map[string]any{
					"thread_id":  threadID,
					"status":     v.Summary.Status,
					"summary":    v.Summary.Summary,
					"elapsed_ms": elapsed(),
				})
				emitter.Emit(ThreadEvent{
					Event:     "completed",
					ThreadID:  threadID,
					AgentID:   agentID,
					SpaceID:   sess.SpaceID(),
					SessionID: sess.ID,
					Text:      v.Summary.Summary,
				})
				if dagFn != nil {
					dagFn()
				}
				if completionNotifier != nil {
					s := v.Summary
					go completionNotifier.Notify(ctx, sess.ID, threadID, agentID, &s)
				}
				result = loopResult{kind: loopDone}

			case *ErrHelp:
				tm.setBlocked(threadID, v.Message)
				if helpResolver != nil {
					go func(msg string) {
						// Bound the resolver call so a hung network or dead LLM
						// backend cannot block this goroutine forever. If the
						// deadline fires we cancel the thread and warn loudly.
						resolveCtx, resolveCancel := context.WithTimeout(ctx, HelpResolveTimeout)
						defer resolveCancel()

						answer, err := helpResolver.Resolve(resolveCtx, sess.ID, threadID, agentID, msg)
						if err != nil {
							if resolveCtx.Err() != nil {
								// Resolver timed out — cancel the blocked thread.
								slog.Warn("threadmgr: help resolver timed out",
									"thread_id", threadID,
									"timeout", HelpResolveTimeout)
								tm.Cancel(threadID)
								return
							}
							// Fallback: broadcast thread_help for human input.
							broadcast(sess.ID, "thread_help", map[string]any{
								"thread_id": threadID,
								"message":   msg,
							})
							return
						}
						// Feed the answer into the thread's InputCh.
						tm.mu.RLock()
						liveThread, ok := tm.threads[threadID]
						tm.mu.RUnlock()
						if ok && liveThread.InputCh != nil {
							select {
							case liveThread.InputCh <- answer:
							case <-ctx.Done():
							}
						}
					}(v.Message)
				} else {
					broadcast(sess.ID, "thread_help", map[string]any{
						"thread_id": threadID,
						"message":   v.Message,
					})
				}
				result = loopResult{kind: loopBlocked, helpMsg: v.Message}

			default:
				// Unexpected panic — auto-summary with error status.
				msg := fmt.Sprintf("unexpected panic: %v", r)
				summary := summariseFromHistory(history, msg, "error")
				tm.Complete(threadID, summary)
				broadcast(sess.ID, "thread_done", map[string]any{
					"thread_id":  threadID,
					"status":     "error",
					"summary":    msg,
					"elapsed_ms": elapsed(),
				})
				emitter.Emit(ThreadEvent{
					Event:     "error",
					ThreadID:  threadID,
					AgentID:   agentID,
					SpaceID:   sess.SpaceID(),
					SessionID: sess.ID,
					Text:      msg,
				})
				if dagFn != nil {
					dagFn()
				}
				result = loopResult{kind: loopDone}
			}
		}
	}()

	logger.Info("runOnce: enter", "thread_id", threadID, "agent", agentID, "ctx_err", ctx.Err())

	thread := tm.getThread(threadID)
	if thread == nil {
		logger.Warn("runOnce: thread not found, exiting", "thread_id", threadID)
		return
	}

	// Snapshot tool registry and executor under read lock for goroutine safety.
	tm.mu.RLock()
	toolReg := tm.toolRegistry
	toolExec := tm.toolExecutor
	tm.mu.RUnlock()

	tt := &ThreadTools{}
	tools := []backend.Tool{
		tt.FinishSchema(),
		tt.RequestHelpSchema(),
	}

	// Per-agent runtime path: schemas come from the orchestrator-built
	// runtime (toolbelt + MuninnDB vault + skills/connections). When unset
	// or empty, fall back to local_tools resolved against the global
	// toolRegistry — preserves behaviour for tests and memory-disabled
	// agents that the runtime preparer chooses not to populate.
	switch {
	case runtime != nil && len(runtime.Schemas) > 0:
		tools = append(tools, runtime.Schemas...)
	case toolReg != nil && reg != nil:
		if ag, found := reg.ByName(agentID); found {
			switch {
			case len(ag.LocalTools) == 1 && ag.LocalTools[0] == "*":
				tools = append(tools, toolReg.AllBuiltinSchemas()...)
			case len(ag.LocalTools) > 0:
				tools = append(tools, toolReg.SchemasByNames(ag.LocalTools)...)
			}
		}
	}

	// Build initial context messages.
	history = buildContext(thread, store, tm, reg)

	// If the runtime supplies a system prompt addendum (memory_mode +
	// memory_block), append it to the persona system message produced by
	// buildContext. buildContext always emits a leading system message, but
	// guard against future changes by prepending a fresh system message
	// when the head role differs.
	if runtime != nil && runtime.ExtraSystem != "" {
		if len(history) > 0 && history[0].Role == "system" {
			history[0].Content += runtime.ExtraSystem
		} else {
			history = append([]backend.Message{{Role: "system", Content: runtime.ExtraSystem}}, history...)
		}
	}
	logger.Info("runOnce: context built", "thread_id", threadID, "history_len", len(history))
	// Log roles for debugging the "must end with user message" constraint.
	if len(history) > 0 {
		lastRole := history[len(history)-1].Role
		lastContentLen := len(history[len(history)-1].Content)
		logger.Info("runOnce: context last message",
			"thread_id", threadID, "last_role", lastRole, "last_content_len", lastContentLen)
	}

	// If resuming after help, inject the user input into history.
	if injectedInput != "" {
		history = append(history, backend.Message{
			Role:    "user",
			Content: injectedInput,
		})
	}

	// Determine which model to use.
	modelID := tm.resolveModelID(threadID, reg, sess)
	logger.Info("runOnce: resolved model", "thread_id", threadID, "model", modelID)

	for turn := 0; turn < maxTurns; turn++ {
		// Check for context cancellation before each LLM call.
		select {
		case <-ctx.Done():
			tm.mu.Lock()
			t, ok := tm.threads[threadID]
			if ok && t.Status != StatusDone && t.Status != StatusBlocked {
				t.Status = StatusCancelled
				t.CompletedAt = time.Now()
			}
			tm.mu.Unlock()
			return loopResult{kind: loopDone}
		default:
		}

		// Check budget.
		if err := ca.CheckBudget(); err != nil {
			summary := summariseFromHistory(history, "budget exceeded: "+err.Error(), "error")
			tm.Complete(threadID, summary)
			broadcast(sess.ID, "thread_done", map[string]any{
				"thread_id":  threadID,
				"status":     "error",
				"summary":    "budget exceeded",
				"elapsed_ms": elapsed(),
			})
			if dagFn != nil {
				dagFn()
			}
			return loopResult{kind: loopDone}
		}

		// Broadcast thinking status at the start of each turn.
		broadcast(sess.ID, "thread_status", map[string]any{
			"thread_id": threadID,
			"status":    "thinking",
		})

		// Build the chat request, streaming tokens via broadcast.
		req := backend.ChatRequest{
			Model:    modelID,
			Messages: history,
			Tools:    tools,
			OnToken: func(tok string) {
				if tok == "" {
					return
				}
				broadcast(sess.ID, "thread_token", map[string]any{
					"thread_id": threadID,
					"token":     tok,
				})
				// Emit token event sampled 1-in-5 to avoid flooding the emitter.
				if tokenCounter != nil {
					if n := atomic.AddInt64(tokenCounter, 1); n%5 == 0 {
						emitter.Emit(ThreadEvent{
							Event:     "token",
							ThreadID:  threadID,
							AgentID:   agentID,
							SpaceID:   sess.SpaceID(),
							SessionID: sess.ID,
							Text:      tok,
						})
					}
				}
			},
		}

		var resp *backend.ChatResponse
		var err error

		logger.Info("runOnce: calling ChatCompletion",
			"thread_id", threadID, "model", modelID, "turn", turn, "history_len", len(history))

		// Single retry on error.
		for attempt := 0; attempt < 2; attempt++ {
			resp, err = b.ChatCompletion(ctx, req)
			if err == nil {
				break
			}
			logger.Warn("runOnce: ChatCompletion error",
				"thread_id", threadID, "turn", turn, "attempt", attempt, "err", err)
			if attempt == 0 {
				// Wait 2s before retry, but respect ctx cancellation.
				select {
				case <-ctx.Done():
					tm.mu.Lock()
					t, ok := tm.threads[threadID]
					if ok && t.Status != StatusDone && t.Status != StatusBlocked {
						t.Status = StatusCancelled
						t.CompletedAt = time.Now()
					}
					tm.mu.Unlock()
					return loopResult{kind: loopDone}
				case <-time.After(2 * time.Second):
				}
			}
		}

		if err == nil {
			logger.Info("runOnce: ChatCompletion succeeded",
				"thread_id", threadID, "turn", turn, "content_len", len(resp.Content),
				"tool_calls", len(resp.ToolCalls), "done_reason", resp.DoneReason)
		}

		if err != nil {
			// Context cancelled or permanent error.
			if ctx.Err() != nil {
				tm.mu.Lock()
				t, ok := tm.threads[threadID]
				if ok && t.Status != StatusDone && t.Status != StatusBlocked {
					t.Status = StatusCancelled
					t.CompletedAt = time.Now()
				}
				tm.mu.Unlock()
				return loopResult{kind: loopDone}
			}
			// Permanent LLM error — complete thread with error status.
			autoSummary := FinishSummary{Summary: "LLM API error: " + err.Error(), Status: "error"}
			tm.Complete(threadID, autoSummary)
			broadcast(sess.ID, "thread_done", map[string]any{
				"thread_id":  threadID,
				"status":     "error",
				"summary":    autoSummary.Summary,
				"elapsed_ms": elapsed(),
			})
			if dagFn != nil {
				dagFn()
			}
			return loopResult{kind: loopDone}
		}

		// Track tokens.
		tm.mu.Lock()
		if t, ok := tm.threads[threadID]; ok {
			t.TokensUsed += resp.PromptTokens + resp.CompletionTokens
		}
		tm.mu.Unlock()
		ca.Record(threadID, resp.PromptTokens, resp.CompletionTokens, modelID)

		// Check per-thread token budget (0 = unlimited).
		if err := checkTokenBudget(tm, threadID); err != nil {
			summary := summariseFromHistory(history, "token budget exhausted", "completed-with-timeout")
			tm.Complete(threadID, summary)
			broadcast(sess.ID, "thread_done", map[string]any{
				"thread_id":  threadID,
				"status":     "completed-with-timeout",
				"summary":    "token budget exhausted",
				"elapsed_ms": elapsed(),
			})
			if dagFn != nil {
				dagFn()
			}
			return loopResult{kind: loopDone}
		}

		// Persist cost record to thread JSONL.
		if err := store.AppendToThread(sess.ID, threadID, session.SessionMessage{
			Role:      "cost",
			Type:      "cost",
			PromptTok: resp.PromptTokens,
			CompTok:   resp.CompletionTokens,
			ModelName: modelID,
		}); err != nil {
			slog.Warn("threadmgr: failed to append to thread", "thread_id", threadID, "err", err)
		}

		// Append assistant message to history.
		assistantMsg := backend.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		history = append(history, assistantMsg)

		// Persist the assistant message.
		if err := store.AppendToThread(sess.ID, threadID, session.SessionMessage{
			Role:    "assistant",
			Content: resp.Content,
			Agent:   agentID,
		}); err != nil {
			slog.Warn("threadmgr: failed to append to thread", "thread_id", threadID, "err", err)
		}

		// If no tool calls, and done reason is "stop" or "length", we're done.
		if len(resp.ToolCalls) == 0 {
			logger.Info("runOnce: no tool calls, completing thread",
				"thread_id", threadID, "done_reason", resp.DoneReason,
				"content_preview", clipResult(resp.Content, 120))
			if resp.DoneReason == "length" {
				summary := summariseFromHistory(history, "context length exceeded", "completed-with-timeout")
				tm.Complete(threadID, summary)
				broadcast(sess.ID, "thread_done", map[string]any{
					"thread_id":  threadID,
					"status":     "completed-with-timeout",
					"summary":    clipResult(resp.Content, 200),
					"elapsed_ms": elapsed(),
				})
				if dagFn != nil {
					dagFn()
				}
				if completionNotifier != nil {
					go completionNotifier.Notify(ctx, sess.ID, threadID, agentID, &summary)
				}
				return loopResult{kind: loopDone}
			}
			// stop — no tools called, treat as implicit finish (completed-with-timeout per spec)
			summary := FinishSummary{
				Summary: clipResult(resp.Content, 500),
				Status:  "completed-with-timeout",
			}
			tm.Complete(threadID, summary)
			broadcast(sess.ID, "thread_done", map[string]any{
				"thread_id":  threadID,
				"status":     "completed-with-timeout",
				"summary":    clipResult(resp.Content, 200),
				"elapsed_ms": elapsed(),
			})
			if dagFn != nil {
				dagFn()
			}
			if completionNotifier != nil {
				go completionNotifier.Notify(ctx, sess.ID, threadID, agentID, &summary)
			}
			return loopResult{kind: loopDone}
		}

		// Update status to tooling while processing tool calls.
		tm.mu.Lock()
		if t, ok := tm.threads[threadID]; ok && t.Status != StatusCancelled {
			t.Status = StatusTooling
		}
		tm.mu.Unlock()

		// Broadcast tooling status.
		broadcast(sess.ID, "thread_status", map[string]any{
			"thread_id": threadID,
			"status":    "tooling",
		})

		// Process each tool call.
		for _, tc := range resp.ToolCalls {
			// Broadcast tool call event before dispatching.
			broadcast(sess.ID, "thread_tool_call", map[string]any{
				"thread_id": threadID,
				"tool":      tc.Function.Name,
				"args":      tc.Function.Arguments,
			})

			switch tc.Function.Name {
			case "finish":
				tt.Finish(tc.Function.Arguments)
				// Finish() panics — execution stops here via defer/recover.
			case "request_help":
				tt.RequestHelp(tc.Function.Arguments)
				// RequestHelp() panics — execution stops here via defer/recover.
			default:
				// Dispatch to the per-agent runtime executor when available
				// (gate-wrapped against the agent's session-local registry,
				// includes MuninnDB and toolbelt providers). Otherwise fall
				// back to the global gate-wrapped executor for the legacy
				// path. When neither is set, return "unknown tool" so unit
				// tests without server wiring still complete deterministically.
				var resultContent string
				switch {
				case runtime != nil && runtime.ExecuteTool != nil:
					toolCtx := SetCallingAgent(ctx, agentID)
					result, execErr := runtime.ExecuteTool(toolCtx, tc.Function.Name, tc.Function.Arguments)
					if execErr != nil {
						resultContent = fmt.Sprintf("tool error: %v", execErr)
					} else {
						resultContent = result
					}
				case toolExec != nil:
					toolCtx := SetCallingAgent(ctx, agentID)
					result, execErr := toolExec(toolCtx, tc.Function.Name, tc.Function.Arguments)
					if execErr != nil {
						resultContent = fmt.Sprintf("tool error: %v", execErr)
					} else {
						resultContent = result
					}
				default:
					resultContent = fmt.Sprintf("unknown tool: %s", tc.Function.Name)
				}
				// Full content goes into LLM history; clipped for persistent store.
				history = append(history, backend.Message{
					Role:       "tool",
					Content:    resultContent,
					ToolName:   tc.Function.Name,
					ToolCallID: tc.ID,
				})
				if err := store.AppendToThread(sess.ID, threadID, session.SessionMessage{
					Role:       "tool",
					Content:    clipResult(resultContent, 200),
					ToolName:   tc.Function.Name,
					ToolCallID: tc.ID,
				}); err != nil {
					slog.Warn("threadmgr: failed to append to thread", "thread_id", threadID, "err", err)
				}
				// Broadcast tool done event after non-panic tool completes.
				broadcast(sess.ID, "thread_tool_done", map[string]any{
					"thread_id":      threadID,
					"tool":           tc.Function.Name,
					"result_summary": clipResult(resultContent, 120),
				})
			}
		}

		// Back to thinking.
		tm.mu.Lock()
		if t, ok := tm.threads[threadID]; ok && t.Status == StatusTooling {
			t.Status = StatusThinking
		}
		tm.mu.Unlock()
	}

	// Exceeded maxTurns.
	summary := summariseFromHistory(history, "max turns reached", "completed-with-timeout")
	tm.Complete(threadID, summary)
	broadcast(sess.ID, "thread_done", map[string]any{
		"thread_id":  threadID,
		"status":     "completed-with-timeout",
		"summary":    "max turns reached",
		"elapsed_ms": elapsed(),
	})
	if dagFn != nil {
		dagFn()
	}
	if completionNotifier != nil {
		s := summary
		go completionNotifier.Notify(ctx, sess.ID, threadID, agentID, &s)
	}
	return loopResult{kind: loopDone}
}

// waitForInputOnce blocks until the thread receives user input on its InputCh
// or ctx is cancelled. Returns (input, true) if input received, ("", false) if
// context cancelled.
func (tm *ThreadManager) waitForInputOnce(ctx context.Context, threadID string) (string, bool) {
	tm.mu.RLock()
	liveThread, ok := tm.threads[threadID]
	tm.mu.RUnlock()
	if !ok {
		return "", false
	}
	inputCh := liveThread.InputCh

	select {
	case <-ctx.Done():
		return "", false
	case input := <-inputCh:
		// Transition back to thinking.
		tm.mu.Lock()
		if t, found := tm.threads[threadID]; found && t.Status == StatusBlocked {
			t.Status = StatusThinking
		}
		tm.mu.Unlock()
		return input, true
	}
}

// setBlocked sets the thread status to blocked in the internal map and fires
// the OnStatusChange hooks. The mu lock is released before firing hooks to
// prevent deadlocks if a hook re-enters the manager.
func (tm *ThreadManager) setBlocked(threadID, helpMessage string) {
	tm.mu.Lock()
	t, ok := tm.threads[threadID]
	if !ok {
		tm.mu.Unlock()
		return
	}
	t.Status = StatusBlocked
	_ = helpMessage // message is broadcast by caller
	tm.mu.Unlock()
	tm.fireStatusChange(threadID, StatusBlocked)
}

// getThread returns a live (pointer) reference to the thread for internal use.
// Callers must not mutate the returned value outside the manager's lock.
func (tm *ThreadManager) getThread(threadID string) *Thread {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.threads[threadID]
}

// resolveModelID determines the model ID to use for the given thread by
// consulting the agent registry. Falls back to the session model.
func (tm *ThreadManager) resolveModelID(threadID string, reg *agents.AgentRegistry, sess *session.Session) string {
	tm.mu.RLock()
	t, ok := tm.threads[threadID]
	agentID := ""
	if ok {
		agentID = t.AgentID
	}
	tm.mu.RUnlock()

	if reg != nil && agentID != "" {
		if ag, found := reg.ByName(agentID); found {
			if mid := ag.GetModelID(); mid != "" {
				return mid
			}
		}
	}
	return sess.Manifest.Model
}

// summariseFromHistory generates a FinishSummary from the last few assistant
// messages in the history. Used for auto-summary on timeout or error.
func summariseFromHistory(history []backend.Message, reason, status string) FinishSummary {
	var parts []string
	count := 0
	for i := len(history) - 1; i >= 0 && count < 3; i-- {
		if history[i].Role == "assistant" && history[i].Content != "" {
			parts = append([]string{clipResult(history[i].Content, 200)}, parts...)
			count++
		}
	}
	summary := reason
	if len(parts) > 0 {
		summary = reason + ": " + strings.Join(parts, " | ")
	}
	return FinishSummary{
		Summary: clipResult(summary, 500),
		Status:  status,
	}
}

// clipResult truncates a string to maxLen characters, appending "..." if clipped.
func clipResult(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
