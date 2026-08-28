package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// defaultToolConcurrency is the maximum number of independent tool calls that
// may execute in parallel. Matches swarm.defaultMaxConcurrency for consistency.
// Override per-run via RunLoopConfig.MaxToolParallelism.
const defaultToolConcurrency = 16

// dispatchedResult holds the result of a single tool execution from dispatchTools.
type dispatchedResult struct {
	index   int
	tc      backend.ToolCall
	content string
}

// RunLoopConfig configures a single agentic loop run.
type RunLoopConfig struct {
	MaxTurns           int
	ModelName          string // model identifier sent to backend
	Messages           []backend.Message
	Tools              *tools.Registry
	ToolSchemas        []backend.Tool
	Gate               *permissions.Gate
	Backend            backend.Backend
	OnToken            func(string)
	OnEvent            func(backend.StreamEvent) // richer streaming; nil = use OnToken
	OnToolCall         func(callID string, name string, args map[string]any)
	OnToolDone         func(callID string, name string, result tools.ToolResult)
	OnPermissionDenied func(name string)
	// OnBeforeWrite is called before any write_file or edit_file tool executes.
	// Receives path, old content (nil for new files), new content.
	// Return true to allow, false to skip. Nil = auto-approve.
	OnBeforeWrite func(path string, oldContent, newContent []byte) bool
	// ToolCallTimeout is the per-tool execution deadline. When <= 0 the default
	// of 5 minutes is used. The timeout is only applied when the caller's
	// context has no tighter deadline already set (additive, not clamping).
	ToolCallTimeout time.Duration
	// CorrelationID is an optional opaque string propagated through logs for
	// distributed tracing. When set it is attached to all structured log lines
	// emitted during this loop run.
	CorrelationID string

	// MaxToolParallelism caps concurrent independent tool execution.
	// 0 or negative uses defaultToolConcurrency (16).
	MaxToolParallelism int

	// VaultWarnOnce gates a single StreamWarning emission when a vault tool
	// fails due to a connection error mid-session. Pass a *sync.Once shared
	// across all RunLoop calls in the same logical session so the warning fires
	// at most once per session. Nil disables the warning (tool error still returned).
	// Only used when VaultReconnector is nil (backward compat for call sites without reconnect).
	VaultWarnOnce *sync.Once

	// VaultReconnector enables automatic mid-session vault reconnect on connection loss.
	// When set, reconnect is attempted before degradation, and the warn gate is managed
	// by VaultReconnector.EmitWarnOnce (VaultWarnOnce is ignored).
	// Nil = warn-once + degrade for the rest of the session (original behavior).
	VaultReconnector *VaultReconnector

	// MemoryMode / MemoryVault opt this loop into post-turn harness persist.
	// Both empty = skip (loop used without a connected vault).
	MemoryMode    string
	MemoryVault   string
	MemoryAgent   string
	MemoryUserMsg string
	MemorySession string
	MemoryHome    string // ~/.huginn; MD fallback when Muninn is off

	// AgentName and SessionID identify the run for permission-prompt routing.
	// Propagated onto every permissions.PermissionRequest built by
	// executeSingle so a serve-mode promptFunc bridge can target the right
	// WS session and agent. Optional — empty for callers that don't track
	// this (e.g. some tests).
	AgentName string
	SessionID string
}

// LoopResult is the final state after the loop ends.
type LoopResult struct {
	FinalContent     string
	TurnCount        int
	StopReason       string            // "stop", "max_turns", "error", "no_tools"
	Messages         []backend.Message // full message history
	PromptTokens     int               // cumulative prompt tokens across all turns
	CompletionTokens int               // cumulative completion tokens across all turns
	MemoryReceipt    bool              // model or harness wrote this turn
	HoldClose        bool              // immersive fail-closed: do not emit assistant row
}

// executeSingle executes a single tool call and returns the result.
// writeMu ensures OnBeforeWrite callbacks are serialized.
// A deferred recover catches panics from any code path (serial or concurrent),
// logs them with a full stack trace, and returns a tool error result.
func (cfg *RunLoopConfig) executeSingle(ctx context.Context, idx int, tc backend.ToolCall, writeMu *sync.Mutex) (result dispatchedResult) {
	// Resolve toolName and callID before the panic defer so the defer
	// can reference them safely.
	// tc.ID is the LLM-provider-assigned call ID (e.g. "call_abc123" from OpenAI).
	// Fall back to a positional ID when the provider omits it (some Ollama models).
	toolName := tc.Function.Name
	argsMap := tc.Function.Arguments
	callID := tc.ID
	if callID == "" {
		callID = fmt.Sprintf("tc-%d-%d-%s", time.Now().UnixNano(), idx, toolName)
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool: panic in executeSingle",
				"tool", toolName,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			result = dispatchedResult{
				index:   idx,
				tc:      tc,
				content: fmt.Sprintf("error: tool %s panicked: %v", toolName, r),
			}
			// Fire OnToolDone so callers can clean up in-flight state (e.g. remove
			// the entry from their capture map). Without this, a panic would leave
			// an orphaned map entry for the lifetime of the turn.
			if cfg.OnToolDone != nil {
				cfg.OnToolDone(callID, toolName, tools.ToolResult{
					Output:  fmt.Sprintf("error: tool %s panicked: %v", toolName, r),
					IsError: true,
					Error:   fmt.Sprintf("tool %s panicked: %v", toolName, r),
				})
			}
		}
	}()

	makeResult := func(content string) dispatchedResult {
		return dispatchedResult{index: idx, tc: tc, content: content}
	}

	if toolName == "" {
		return makeResult("error: tool call has empty function name")
	}

	tool, ok := cfg.Tools.Get(toolName)
	if !ok {
		return makeResult(fmt.Sprintf("I don't have %s.", toolName))
	}

	// Runtime enforcement: verify the tool was included in the schemas sent
	// to the model. A tool may exist in the registry but not be permitted
	// for this agent's toolbelt.
	if !cfg.toolSchemaAllows(toolName) {
		return makeResult(fmt.Sprintf("I don't have %s.", toolName))
	}

	// Memory write gate: a question-shaped turn must be answered from recall,
	// never by inventing and storing a new "fact". Live repro (2026-08-27,
	// Winston DM): asked "what's our production database called?", the model
	// skipped recall, invented "PostgreSQL", and wrote it to the vault as a
	// human-confidence-1 fact. Intercept muninn write-tool calls here — before
	// they ever reach Muninn — whenever the turn's user message reads as a
	// question. Statement-shaped turns pass through untouched.
	if isMuninnWriteTool(toolName) && isQuestionShaped(cfg.MemoryUserMsg) {
		blockOutput := "This is a question, not a new fact — I did not store anything. " +
			"Call muninn_recall with the user's question as context and answer from what you find. " +
			"Never store your own guess or inference as if it were a fact the user told you."
		if cfg.OnToolCall != nil {
			cfg.OnToolCall(callID, toolName, argsMap)
		}
		blockResult := tools.ToolResult{Output: blockOutput}
		if cfg.OnToolDone != nil {
			cfg.OnToolDone(callID, toolName, blockResult)
		}
		return makeResult(blockOutput)
	}

	if cfg.Gate != nil {
		req := permissions.PermissionRequest{
			ToolName:  toolName,
			Level:     tool.Permission(),
			Args:      argsMap,
			Provider:  cfg.Tools.ProviderFor(toolName),
			AgentName: cfg.AgentName,
			SessionID: cfg.SessionID,
		}
		checkResult := cfg.Gate.CheckDetailedCtx(ctx, req)
		if !checkResult.Allowed {
			denyOutput := "error: permission denied"
			if checkResult.Reason != "" {
				denyOutput += ": " + checkResult.Reason
			}
			denyResult := tools.ToolResult{
				Output:  denyOutput,
				Error:   "permission denied",
				IsError: true,
				Metadata: map[string]any{
					"permission_denied": true,
					"reason_code":       checkResult.ReasonCode,
					"reason":            checkResult.Reason,
				},
			}
			// Surface denied tool attempts through normal tool-event callbacks so
			// UIs/auditors can render and record the blocked action consistently.
			if cfg.OnToolCall != nil {
				cfg.OnToolCall(callID, toolName, argsMap)
			}
			if cfg.OnToolDone != nil {
				cfg.OnToolDone(callID, toolName, denyResult)
			}
			if cfg.OnPermissionDenied != nil {
				cfg.OnPermissionDenied(toolName)
			}
			return makeResult(denyOutput)
		}
	}

	if (toolName == "write_file" || toolName == "edit_file") && cfg.OnBeforeWrite != nil {
		path, oldContent, newContent := previewWrite(toolName, argsMap)
		writeMu.Lock()
		allowed, writeCallbackPanic := safeOnBeforeWrite(cfg.OnBeforeWrite, path, oldContent, newContent)
		writeMu.Unlock()
		if writeCallbackPanic != nil {
			return makeResult(fmt.Sprintf("error: write callback panicked: %v", writeCallbackPanic))
		}
		if !allowed {
			return makeResult("error: user rejected this change. Try a different approach.")
		}
	}

	if cfg.OnToolCall != nil {
		cfg.OnToolCall(callID, toolName, argsMap)
	}

	// Apply a per-tool deadline only when the caller has not already set a
	// tighter one. This is purely additive: if the parent context expires
	// sooner, that deadline still wins.
	toolCtx := ctx
	timeout := cfg.ToolCallTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute // enterprise-safe default
	}
	if dl, ok := ctx.Deadline(); !ok || time.Until(dl) > timeout {
		var cancelTool context.CancelFunc
		toolCtx, cancelTool = context.WithTimeout(ctx, timeout)
		defer cancelTool()
	}

	// tryExecuteTool: attempt once; on vault connection error, attempt reconnect and retry.
	toolResult := cfg.tryExecuteTool(ctx, toolCtx, toolName, tool, argsMap)

	// Degradation path: if the vault is still broken after reconnect attempt (or no reconnector),
	// replace the raw transport error with a clear directive for the LLM and emit a one-time warning.
	if toolResult.IsError && cfg.Tools != nil && cfg.Tools.ProviderFor(toolName) == "muninndb" {
		if isVaultConnectionError(fmt.Errorf("%s", toolResult.Error)) {
			slog.Warn("agent: vault tool failed with connection error — degrading",
				"tool", toolName, "err", toolResult.Error)
			toolResult.Error = "Memory vault connection lost. This tool is temporarily unavailable. Continue the task without memory access."
			if cfg.OnEvent != nil {
				warnFn := func() {
					cfg.OnEvent(backend.StreamEvent{
						Type:    backend.StreamWarning,
						Content: "Memory vault reconnect failed. Memory tools are unavailable for the rest of this session.",
					})
				}
				if cfg.VaultReconnector != nil {
					cfg.VaultReconnector.EmitWarnOnce(warnFn)
				} else if cfg.VaultWarnOnce != nil {
					cfg.VaultWarnOnce.Do(warnFn)
				}
			}
		}
	}

	if cfg.OnToolDone != nil {
		cfg.OnToolDone(callID, toolName, toolResult)
	}

	content := toolResult.Output
	if toolResult.IsError && toolResult.Error != "" {
		content = "error: " + toolResult.Error
	} else if content == "" && toolResult.Error != "" {
		content = toolResult.Error
	}
	// Mechanical rewrite at the tool-result boundary: keyring / API-key
	// dumps never reach the 14b. Teammate deny, no secrets.
	content = RewriteCredentialToolResult(content)

	// Truncate large tool outputs to avoid overflowing the model's context window.
	const maxToolOutputBytes = 100 * 1024 // 100 KB
	if len(content) > maxToolOutputBytes {
		content = content[:maxToolOutputBytes] + "\n... [truncated: output exceeded 100 KB]"
	}

	return makeResult(content)
}

// tryExecuteTool executes the tool once. On vault connection error, it attempts a
// mid-session reconnect (if VaultReconnector is configured) and retries exactly once
// with the freshly registered adapter. Falls through to the degradation path on failure.
// Uses the session ctx (not per-tool toolCtx) for the reconnect so the ToolCallTimeout
// does not abort the reconnect handshake.
func (cfg *RunLoopConfig) tryExecuteTool(ctx, toolCtx context.Context, toolName string, tool tools.Tool, args map[string]any) tools.ToolResult {
	result := tool.Execute(toolCtx, args)
	if !result.IsError || cfg.VaultReconnector == nil {
		return result
	}
	if cfg.Tools.ProviderFor(toolName) != "muninndb" {
		return result
	}
	if !isVaultConnectionError(fmt.Errorf("%s", result.Error)) {
		return result
	}

	slog.Warn("agent: vault connection lost, attempting mid-session reconnect", "tool", toolName)

	if !cfg.VaultReconnector.TryReconnect(ctx) {
		// Lost TryLock race — a concurrent goroutine is reconnecting or has already reconnected.
		// Probe the registry: if reconnect completed, retry with the fresh adapter.
		if freshTool, ok := cfg.Tools.Get(toolName); ok {
			if retryResult := freshTool.Execute(toolCtx, args); !retryResult.IsError ||
				!isVaultConnectionError(fmt.Errorf("%s", retryResult.Error)) {
				return retryResult // peer reconnect succeeded
			}
		}
		return result // reconnect still in progress or failed — caller degrades
	}

	freshTool, ok := cfg.Tools.Get(toolName)
	if !ok {
		slog.Warn("agent: vault reconnected but tool not found post-reconnect", "tool", toolName)
		return result
	}
	slog.Info("agent: vault reconnected mid-session, retrying tool", "tool", toolName)
	return freshTool.Execute(toolCtx, args)
}

// dispatchTools executes tool calls, running independent ones concurrently and
// serial ones sequentially, while maintaining order in the result slice.
func (cfg *RunLoopConfig) dispatchTools(ctx context.Context, calls []backend.ToolCall) []dispatchedResult {
	results := make([]dispatchedResult, len(calls))
	var wg sync.WaitGroup
	var writeMu sync.Mutex

	// Partition calls into independent and serial
	var serialIdxs []int
	independentIdxs := make([]int, 0, len(calls))
	for i, tc := range calls {
		name := tc.Function.Name
		if isIndependentTool(name, tc.Function.Arguments, calls) {
			independentIdxs = append(independentIdxs, i)
		} else {
			serialIdxs = append(serialIdxs, i)
		}
	}

	// Semaphore limits concurrent independent tool execution to MaxToolParallelism
	// goroutines (default 16). This bounds goroutine fan-out when the model emits
	// many parallel tool calls and prevents resource exhaustion.
	toolCap := cfg.MaxToolParallelism
	if toolCap <= 0 {
		toolCap = defaultToolConcurrency
	}
	sem := make(chan struct{}, toolCap)

	// Launch concurrent tasks for independent tools.
	// Panic recovery is handled inside executeSingle (single recovery point).
	for _, i := range independentIdxs {
		sem <- struct{}{} // acquire (blocks if at cap)
		wg.Add(1)
		go func(idx int, tc backend.ToolCall) {
			defer func() { <-sem }() // release
			defer wg.Done()
			results[idx] = cfg.executeSingle(ctx, idx, tc, &writeMu)
		}(i, calls[i])
	}

	// Wait for all concurrent tasks to finish
	wg.Wait()

	// Execute serial tools sequentially
	for _, i := range serialIdxs {
		results[i] = cfg.executeSingle(ctx, i, calls[i], &writeMu)
	}

	return results
}

// isIndependentTool classifies whether a tool can be executed in parallel.
// Serial tools (bash, git writes, MCP, conflicting writes) must run sequentially.
// Independent tools (reads, distinct write paths) can run concurrently.
func isIndependentTool(toolName string, args map[string]any, allCalls []backend.ToolCall) bool {
	switch toolName {
	case "bash":
		// bash is never independent; multiple bash calls should run serially
		return false
	case "git_commit", "git_stash":
		// git write operations must be serial
		return false
	case "read_file", "grep", "list_dir", "search_files",
		"web_search", "fetch_url",
		"git_status", "git_log", "git_blame", "git_diff", "git_branch":
		// read-only and git read tools are always independent
		return true
	case "write_file", "edit_file":
		// write_file and edit_file are independent only if they touch different files
		path, _ := args["file_path"].(string)
		if path == "" {
			return false // no path = can't dedup, be safe
		}
		// Serial if any other call in the batch targets the same path
		count := 0
		for _, tc := range allCalls {
			if tc.Function.Name == "write_file" || tc.Function.Name == "edit_file" {
				if p, _ := tc.Function.Arguments["file_path"].(string); p == path {
					count++
					if count > 1 {
						return false // same path appears multiple times
					}
				}
			}
		}
		return true // different paths, safe to parallelize
	default:
		if strings.HasPrefix(toolName, "mcp_") {
			// MCP tools are always serial (state-dependent)
			return false
		}
		// Unknown tools default to serial (safe default)
		return false
	}
}

// RunLoop runs the agentic tool-calling loop.
// It calls the model, executes any tool_calls, feeds results back, and repeats
// until the model stops calling tools or MaxTurns is reached.
func RunLoop(ctx context.Context, cfg RunLoopConfig) (result *LoopResult, err error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 50
	}
	messages := make([]backend.Message, len(cfg.Messages))
	copy(messages, cfg.Messages)

	result = &LoopResult{}
	if early := imageAskStop(messages, cfg.ToolSchemas); early != "" {
		if cfg.OnToken != nil {
			cfg.OnToken(early)
		}
		messages = append(messages, backend.Message{Role: "assistant", Content: early})
		result.FinalContent = early
		result.StopReason = "stop"
		result.Messages = messages
		return result, nil
	}
	defer func() {
		if result != nil && err == nil {
			applyLoopMemoryGate(ctx, cfg, result)
		}
	}()

	var consecutiveParseFailures int
	// Auto-wait: a lead that delegates and then stops without wait_for_threads
	// abandons its spawned threads (and their results). Track one successful
	// delegate_to_agent and inject a single wait_for_threads barrier so the
	// model gets the specialists' results and can answer from them.
	var delegated, waitedForThreads, autoWaited bool
	// toolsRan flips once any tool (including the synthetic auto-wait) has
	// executed; later assistant content then gets the after-tools filter.
	var toolsRan bool
	// specialistResult flips once a wait_for_threads (model-called or the
	// synthetic auto-wait) came back with a finished thread. The next model
	// turn is then speech only and terminal: no promotion, tool calls
	// dropped, residual stripped, run stops. Small leads otherwise re-run
	// the playbook (recall / delegate again) against a task that is done.
	var specialistResult bool
	var workDone bool
	// unfinishedPlanNudges counts continuation nudges injected for
	// looksLikeUnfinishedPlan exits (see below). Bounded by
	// maxUnfinishedPlanContinuations; never reset once incremented.
	var unfinishedPlanNudges int
	var lastWorkSpeech string
	var speechHinted bool
	var contentBeforeAutoWait string
	var denied atomic.Bool
	origDenied := cfg.OnPermissionDenied
	cfg.OnPermissionDenied = func(name string) {
		denied.Store(true)
		if origDenied != nil {
			origDenied(name)
		}
	}

	// tokenGate is recreated each turn (see below) but declared here so it
	// survives the loop's exit: the max-turns fallthrough returns after the
	// for-loop body, and still needs the last turn's gate to flush whatever
	// it held back.
	var tokenGate *backend.ContentToolCallTokenGate
	// flushFinal guarantees the authoritative visible content for this run
	// reaches cfg.OnToken exactly once before RunLoop returns. The gate
	// tracks what it already forwarded live (g.emitted) and Finish only
	// emits the un-streamed suffix, so a turn that already streamed never
	// gets double-painted; a turn the gate held back in full (it looked like
	// a tool-call candidate that never resolved) finally gets flushed.
	flushFinal := func(visible string) {
		if tokenGate != nil {
			tokenGate.Finish(visible)
			return
		}
		if cfg.OnToken != nil && visible != "" {
			cfg.OnToken(visible)
		}
	}

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		result.TurnCount = turn + 1

		// Gate OnToken so backends/mocks that stream raw JSON-in-content never
		// paint harness JSON. parseSSE already Finish-es leftover prose; do
		// not Finish again after ChatCompletion — a second suffix flush
		// after StreamDone forks leftover (`PONG` → `ONG`) into a new
		// anonymous timeline row.
		tokenGate = backend.NewContentToolCallTokenGate(cfg.OnToken, nil)
		tokenGate.SetGrantedTools(cfg.ToolSchemas)
		onToken := cfg.OnToken
		if tokenGate != nil {
			onToken = tokenGate.OnToken
		}
		speechOnly := specialistResult || workDone
		if speechOnly && !speechHinted {
			speechHinted = true
			hint := "[system] Specialists already finished. Speak to the human: say what they reported, then finish any leftover question. No tools. No playbook. No templates."
			if workDone && !specialistResult {
				// 14b specialists otherwise re-fire the same bash until max turns.
				hint = "[system] The tool already ran. Tell the human the result in one short sentence. No more tools."
			}
			messages = append(messages, backend.Message{Role: "user", Content: hint})
		}
		turnCtx := ctx
		var cutter *speechTurnCutter
		if speechOnly {
			// Speech-only turn: tokens are buffered, not streamed — the
			// residual filter needs the whole message, and the token gate
			// only holds a leading JSON prefix. If the model opens with
			// tool JSON it has nothing to say: cut decoding for this
			// request only (the model stays loaded; just the stream ends).
			var cancel context.CancelFunc
			turnCtx, cancel = context.WithCancel(ctx)
			defer cancel()
			cutter = newSpeechTurnCutter(cancel)
			onToken = cutter.OnToken
		}
		chatResult, err := cfg.Backend.ChatCompletion(turnCtx, backend.ChatRequest{
			Model:    cfg.ModelName,
			Messages: messages,
			Tools:    cfg.ToolSchemas,
			OnToken:  onToken,
			OnEvent:  cfg.OnEvent,
		})
		if cutter != nil && cutter.Cut() {
			// Our own cut, not the caller's cancellation.
			if chatResult == nil {
				chatResult = &backend.ChatResponse{DoneReason: "stop"}
			}
			chatResult.Content = cutter.Raw()
			chatResult.ToolCalls = nil
			err = nil
		}
		if err != nil {
			result.StopReason = "error"
			return result, fmt.Errorf("turn %d: %w", turn+1, err)
		}
		if chatResult != nil {
			result.PromptTokens += chatResult.PromptTokens
			result.CompletionTokens += chatResult.CompletionTokens
		}

		// Guard: nil response without an error is treated as an error condition.
		if chatResult == nil {
			result.StopReason = "error"
			result.Messages = messages
			return result, fmt.Errorf("turn %d: backend returned nil response without error", turn+1)
		}

		// Local Qwen/Ollama models sometimes put a lone function-call JSON
		// object in content instead of structured tool_calls — including
		// follow-ups like {"name":"wait_for_threads"} with no arguments.
		// Promote on every turn so the loop keeps running instead of
		// treating that JSON as the final answer.
		if speechOnly {
			if n := len(chatResult.ToolCalls); n > 0 {
				names := make([]string, 0, n)
				for _, tc := range chatResult.ToolCalls {
					names = append(names, tc.Function.Name)
				}
				slog.Warn("agent loop: dropped tool calls after specialist result (speech-only turn)",
					"tools", names, "turn", turn+1)
				chatResult.ToolCalls = nil
			}
			chatResult.Content = backend.VisibleAssistantContentAfterTools(chatResult.Content, lastHumanUserText(messages))
			if chatResult.Content == "" {
				// Nothing sayable came back: keep the last real prose so the
				// run does not end on a blank line.
				chatResult.Content = result.FinalContent
				if chatResult.Content == "" {
					chatResult.Content = contentBeforeAutoWait
				}
				if chatResult.Content == "" {
					chatResult.Content = lastWorkSpeech
				}
			}
			chatResult.Content = applyTeammateSpeech(messages, cfg.ToolSchemas, chatResult.Content)
			// Emit the sayable remainder once (the turn was buffered by the
			// cutter, not the token gate — cutter.OnToken never forwards
			// downstream, so flushFinal's suffix math starts from "" and
			// this is a plain one-shot emit regardless of whether decoding
			// was cut on leading tool JSON).
			flushFinal(chatResult.Content)
			messages = append(messages, backend.Message{Role: "assistant", Content: chatResult.Content})
			result.FinalContent = chatResult.Content
			result.StopReason = "stop"
			result.Messages = messages
			return result, nil
		}
		backend.PromoteContentToolCalls(chatResult)
		// qwen2.5-coder also writes a "playbook": fenced tool JSON mixed with
		// glue prose. Promote embedded invocations of granted tools so they
		// execute in order; unknown names stay inert.
		backend.PromoteGrantedContentToolCalls(chatResult, cfg.ToolSchemas)
		backend.RevealContentToolCalls(chatResult)
		if denied.Load() || toolsRan {
			// After a deny the model often dumps another tool JSON into
			// content (not always leading). After tools ran, small models
			// also echo wait placeholders, playbook glue, and the tool
			// result as JSON next to the answer. None of that is speech.
			chatResult.Content = backend.VisibleAssistantContentAfterTools(chatResult.Content, lastHumanUserText(messages))
		}

		// Append assistant response to history
		assistantMsg := backend.Message{
			Role:      "assistant",
			Content:   chatResult.Content,
			ToolCalls: chatResult.ToolCalls,
		}
		messages = append(messages, assistantMsg)
		result.FinalContent = chatResult.Content

		// Surface parse errors to the model so it can retry with valid JSON.
		// After 3 consecutive failures, stop to avoid unlimited token burn.
		if len(chatResult.ParseErrors) > 0 {
			consecutiveParseFailures++
			slog.Warn("agent loop: SSE tool calls were dropped",
				"count", len(chatResult.ParseErrors),
				"consecutive", consecutiveParseFailures,
				"turn", turn+1)
			if consecutiveParseFailures >= 3 {
				result.StopReason = "parse_error_limit"
				result.Messages = messages
				return result, fmt.Errorf("agent loop: %d consecutive turns had malformed tool calls; stopping to avoid token burn", consecutiveParseFailures)
			}
			messages = append(messages, backend.Message{
				Role:    "user",
				Content: fmt.Sprintf("[system] %d tool call(s) were malformed and could not be executed. Please retry with valid JSON arguments.", len(chatResult.ParseErrors)),
			})
			continue
		}
		consecutiveParseFailures = 0

		// If no tool calls, the loop ends
		if len(chatResult.ToolCalls) == 0 {
			if delegated && !waitedForThreads && !autoWaited && cfg.canAutoWait() {
				// The model stopped after delegating without collecting
				// results. Run one synthetic wait_for_threads (empty args =
				// all active threads in the session) and give the model a
				// final turn to answer from the specialists' summaries.
				autoWaited = true
				toolsRan = true
				contentBeforeAutoWait = result.FinalContent
				wc := backend.ToolCall{
					ID: "auto_wait_1",
					Function: backend.ToolCallFunction{
						Name:      "wait_for_threads",
						Arguments: map[string]any{},
					},
				}
				messages = append(messages, backend.Message{
					Role:      "assistant",
					ToolCalls: []backend.ToolCall{wc},
				})
				var waitAnswer string
				for _, dr := range cfg.dispatchTools(ctx, []backend.ToolCall{wc}) {
					if waitReturnedSpecialistResult(dr.content) {
						specialistResult = true
						if backend.WaitHasSpecialistAnswer(dr.content) {
							waitAnswer = dr.content
						}
					}
					messages = append(messages, backend.Message{
						Role:       "tool",
						ToolName:   dr.tc.Function.Name,
						ToolCallID: dr.tc.ID,
						Content:    dr.content,
					})
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					result.StopReason = "cancelled"
					result.Messages = messages
					return result, fmt.Errorf("run loop cancelled: %w", ctxErr)
				}
				if waitAnswer != "" {
					return stopTurnPersist(result, messages, cfg, result.FinalContent, waitAnswer, lastHumanUserText(messages))
				}
				continue
			}
			if autoWaited && result.FinalContent == "" {
				// The post-wait turn produced nothing; keep the pre-wait prose
				// rather than ending with an empty answer.
				result.FinalContent = contentBeforeAutoWait
			}
			// Bounded continuation: a model with tools available, and at
			// least one tool already run this loop, sometimes narrates the
			// next step ("I will read X, then fix it") instead of calling
			// the tool, and the no-tool-calls exit above would otherwise end
			// the turn half-done. Nudge it to act instead of describing,
			// up to maxUnfinishedPlanContinuations times; on the last one,
			// fall through to the normal terminal path below — an honest
			// partial answer beats a spin loop.
			if toolsRan && len(cfg.ToolSchemas) > 0 &&
				unfinishedPlanNudges < maxUnfinishedPlanContinuations &&
				looksLikeUnfinishedPlan(result.FinalContent) {
				unfinishedPlanNudges++
				messages = append(messages, backend.Message{
					Role:    "user",
					Content: "[system] Continue: execute the next step now by calling the tool — do not describe it.",
				})
				if cfg.OnEvent != nil {
					cfg.OnEvent(backend.StreamEvent{Type: backend.StreamStatus, Content: "continuing"})
				}
				continue
			}
			if speech := applyTeammateSpeech(messages, cfg.ToolSchemas, result.FinalContent); speech != result.FinalContent {
				result.FinalContent = speech
				if n := len(messages); n > 0 && messages[n-1].Role == "assistant" {
					messages[n-1].Content = speech
				}
			}
			flushFinal(result.FinalContent)
			result.StopReason = "stop"
			result.Messages = messages
			return result, nil
		}

		// Execute tool calls — independent ones in parallel, serial ones after
		dispatched := cfg.dispatchTools(ctx, chatResult.ToolCalls)
		toolsRan = true
		var wallDeny, waitAnswer string
		for _, dr := range dispatched {
			switch dr.tc.Function.Name {
			case "delegate_to_agent":
				if !strings.HasPrefix(dr.content, "error:") {
					delegated = true
				} else if backend.IsCompanyWallDeny(dr.content) {
					wallDeny = dr.content
				}
			case "consult_agent":
				if backend.IsCompanyWallDeny(dr.content) {
					wallDeny = dr.content
				}
			case "wait_for_threads":
				waitedForThreads = true
				if waitReturnedSpecialistResult(dr.content) {
					specialistResult = true
					if backend.WaitHasSpecialistAnswer(dr.content) {
						waitAnswer = dr.content
					}
				}
			case "recall_thread_result", "list_team_status":
				// A2A glue — not specialist work of its own.
			default:
				if isTerminalWorkTool(dr.tc.Function.Name) &&
					!strings.HasPrefix(dr.content, "error:") &&
					strings.TrimSpace(dr.content) != "" {
					workDone = true
					lastWorkSpeech = dr.content
				}
			}
			messages = append(messages, backend.Message{
				Role:       "tool",
				ToolName:   dr.tc.Function.Name,
				ToolCallID: dr.tc.ID,
				Content:    dr.content,
			})
		}

		// Check whether the caller's context was cancelled or deadline exceeded
		// while tools were executing. If so, stop the loop immediately.
		if ctxErr := ctx.Err(); ctxErr != nil {
			result.StopReason = "cancelled"
			result.Messages = messages
			return result, fmt.Errorf("run loop cancelled: %w", ctxErr)
		}

		ask := lastHumanUserText(messages)
		if wallDeny != "" && !userAskedSam(ask) {
			if askMentionsDeniedAgent(ask, wallDeny) {
				return stopTurnPersist(result, messages, cfg, result.FinalContent, wallDeny, ask)
			}
			// The human never named the refused agent — they asked for
			// something else entirely and the model delegated on its own.
			// "X isn't in this company." is teammate-to-teammate glue, not
			// an answer; speaking it verbatim reads as a non-sequitur.
			// Retry once with delegation withheld so the model answers the
			// human directly; fall back to an honest rewrite if that retry
			// itself produces nothing sayable.
			return wallDenyAnswerDirectly(ctx, cfg, result, messages, ask)
		}
		if waitAnswer != "" {
			return stopTurnPersist(result, messages, cfg, result.FinalContent, waitAnswer, ask)
		}
	}

	flushFinal(result.FinalContent)
	result.StopReason = "max_turns"
	result.Messages = messages
	return result, nil
}

// isTerminalWorkTool is a specialist command that already did the job. After
// one success, 14b otherwise re-fires the same bash until max turns and never
// speaks. Plan/read/write stay multi-turn.
func isTerminalWorkTool(name string) bool {
	switch name {
	case "bash", "shell", "exec":
		return true
	default:
		return false
	}
}

// maxUnfinishedPlanContinuations bounds how many times RunLoop nudges a model
// that ended a tool-less turn with future-tense narration ("I will read X,
// then fix it") instead of calling a tool. Declared as a var so tests can
// lower it to exercise the cap quickly.
var maxUnfinishedPlanContinuations = 3

// unfinishedPlanFutureRE matches a future-tense commitment to do *work*: an
// intent phrase ("I'll", "I will", "let me", "I'm going to", "I plan to")
// followed, within a few filler words, by a verb that names actual tool work
// (read, check, run, edit, ...). The work-verb requirement is load-bearing —
// the intent phrase alone also appears in ordinary polite closers ("Let me
// know if you need anything else.", "I'll be here if you need me.", "Next, I
// will be available for questions.", "I plan to keep the vault updated"),
// none of which have a next step to execute. Each false nudge costs a full
// extra model turn (~30s on 14b), so the check errs toward not firing.
var unfinishedPlanFutureRE = regexp.MustCompile(`(?i)\b(?:i'll|i will|i'm going to|i am going to|i plan to|let me)\s+(?:(?:now|then|go|ahead|and|first|next|also|just|quickly|immediately|start|begin|by|proceed|to|try|attempt)\s+){0,3}(?:read|check|open|run|inspect|look at|examin|verify|test|edit|fix|update|write|creat|add|remov|delet|apply|patch|modif|search|grep|find|list|scan|fetch|quer|call|use|execut|implement|review|analyz|analys|investigat|continu|gather|collect|compil|build|install|configur|deploy|delegat|consult|recall|remember|save|store)`)

// unfinishedPlanCompletedRE matches completed-result markers. Their presence
// anywhere in the answer means the turn already reported a finished result,
// even if it also mentions further steps — err toward not nudging rather
// than double-nudging a genuinely finished answer.
var unfinishedPlanCompletedRE = regexp.MustCompile("(?i)\\b(done|passes|passing|fixed|changed|complete[d]?|resolved)\\b|```|diff --git")

// looksLikeUnfinishedPlan reports whether text reads as a model narrating
// future work ("I will read the file, then fix the bug") rather than
// reporting a completed result. It is deliberately conservative: any
// completed-result marker anywhere in the text disqualifies a match, and a
// future-tense commitment must appear in the last sentence(s) — an early
// aside ("First let me note: ...") that ends with a real result should not
// match.
func looksLikeUnfinishedPlan(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if unfinishedPlanCompletedRE.MatchString(t) {
		return false
	}
	// n=3 rather than 2: upstream residual-speech cleanup sometimes inserts a
	// stray space after a filename's dot (e.g. "mathutil.go" -> "mathutil.
	// go"), which the crude sentence splitter below reads as an extra
	// sentence boundary. 3 keeps the check anchored near the end of the
	// answer without being fooled by that artifact.
	return unfinishedPlanFutureRE.MatchString(lastSentences(t, 3))
}

// unfinishedPlanSentenceSplitRE splits text into rough sentences/lines for
// lastSentences. Not a full sentence tokenizer — good enough to isolate the
// tail of a short narration turn.
var unfinishedPlanSentenceSplitRE = regexp.MustCompile(`(?:[.!?]+\s+|\n+)`)

// lastSentences returns the last n non-empty sentences/lines of text, joined
// back with ". ". Used to look only at how a turn ends, not whether it
// mentions future work anywhere (an aside earlier in a finished answer
// should not count).
func lastSentences(text string, n int) string {
	parts := unfinishedPlanSentenceSplitRE.Split(text, -1)
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	if n > len(nonEmpty) {
		n = len(nonEmpty)
	}
	return strings.Join(nonEmpty[len(nonEmpty)-n:], ". ")
}

// waitReturnedSpecialistResult reports whether a wait_for_threads tool result
// carries at least one finished thread (threadmgr renders "## Finished
// threads (n)"). A timeout with only "Still running" threads, an error, or
// "No matching threads" does not count — the lead may keep waiting.
func waitReturnedSpecialistResult(content string) bool {
	if strings.HasPrefix(content, "error:") {
		return false
	}
	return strings.Contains(content, "## Finished threads")
}

// userAskedSam is the only case where a company-wall deny may continue:
// the human named Sam, so a re-delegate is their ask, not glue.
func userAskedSam(ask string) bool {
	return strings.Contains(strings.ToLower(ask), "ask sam")
}

// askMentionsDeniedAgent reports whether the human's own ask named the
// agent a delegate/consult call was just refused for (e.g. they asked
// "Ask Steve for the hostname" and delegation to Steve was denied). When
// true, stating the wall line is an honest, on-topic answer. When false,
// the human asked for something unrelated and the refusal is glue from a
// delegation they never requested.
func askMentionsDeniedAgent(ask, wallDeny string) bool {
	name := backend.DeniedAgentName(wallDeny)
	if name == "" {
		return false
	}
	return strings.Contains(strings.ToLower(ask), strings.ToLower(name))
}

// wallDenyAnswerDirectly handles a company-wall deny for an agent the
// human's ask never named: one extra completion with delegate_to_agent and
// consult_agent withheld, so the model must answer the human directly
// instead of narrating the refusal. If that retry itself produces nothing
// sayable (empty, another tool call, or another refusal), falls back to an
// honest rewrite rather than ever speaking "X isn't in this company."
// verbatim as the answer to an unrelated ask.
func wallDenyAnswerDirectly(ctx context.Context, cfg RunLoopConfig, result *LoopResult, messages []backend.Message, ask string) (*LoopResult, error) {
	final := ""
	if cfg.Backend != nil {
		var noDelegate []backend.Tool
		for _, t := range cfg.ToolSchemas {
			if t.Function.Name == "delegate_to_agent" || t.Function.Name == "consult_agent" {
				continue
			}
			noDelegate = append(noDelegate, t)
		}
		retryMessages := append(append([]backend.Message{}, messages...), backend.Message{
			Role:    "user",
			Content: "[system] That teammate isn't available for this. Answer directly yourself — no delegating, no tools.",
		})
		if chatResult, err := cfg.Backend.ChatCompletion(ctx, backend.ChatRequest{
			Model:    cfg.ModelName,
			Messages: retryMessages,
			Tools:    noDelegate,
		}); err == nil && chatResult != nil {
			content := strings.TrimSpace(chatResult.Content)
			if content != "" && len(chatResult.ToolCalls) == 0 && !backend.IsCompanyWallDeny(content) {
				final = content
			}
		}
	}
	if final == "" {
		final = fmt.Sprintf("Couldn't hand that off — %s needs a teammate I don't have here.", strings.TrimSpace(ask))
	}
	messages = append(messages, backend.Message{Role: "assistant", Content: final})
	if cfg.OnToken != nil {
		cfg.OnToken(final)
	}
	result.FinalContent = final
	result.StopReason = "stop"
	result.Messages = messages
	return result, nil
}

// stopTurnPersist ends the run without another model completion and persists
// the teammate wall / wait answer so leftover glue is never generated.
func stopTurnPersist(result *LoopResult, messages []backend.Message, cfg RunLoopConfig, speech, blob, ask string) (*LoopResult, error) {
	final := backend.PersistStopTurn(speech, blob, ask)
	if final == "" {
		final = strings.TrimSpace(speech)
	}
	if final != "" {
		messages = append(messages, backend.Message{Role: "assistant", Content: final})
		if cfg.OnToken != nil {
			cfg.OnToken(final)
		}
	}
	result.FinalContent = final
	result.StopReason = "stop"
	result.Messages = messages
	return result, nil
}

// speechTurnCutter buffers the raw token stream of a speech-only turn. If
// the first non-blank character is '{' the model is re-typing a tool object
// instead of speaking; cancel the request so no more tokens are decoded.
// Nothing is forwarded live — the loop emits the stripped remainder once.
type speechTurnCutter struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	raw     strings.Builder
	decided bool
	cut     bool
}

func newSpeechTurnCutter(cancel context.CancelFunc) *speechTurnCutter {
	return &speechTurnCutter{cancel: cancel}
}

func (c *speechTurnCutter) OnToken(tok string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cut {
		return
	}
	c.raw.WriteString(tok)
	if c.decided {
		return
	}
	if lead := strings.TrimLeft(c.raw.String(), " \t\r\n"); lead != "" {
		c.decided = true
		if lead[0] == '{' {
			c.cut = true
			c.cancel()
		}
	}
}

func (c *speechTurnCutter) Cut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cut
}

func (c *speechTurnCutter) Raw() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw.String()
}

// canAutoWait reports whether a synthetic wait_for_threads barrier can run:
// the tool must be registered and permitted for this agent's toolbelt.
func (cfg *RunLoopConfig) canAutoWait() bool {
	if cfg.Tools == nil || !cfg.toolSchemaAllows("wait_for_threads") {
		return false
	}
	_, ok := cfg.Tools.Get("wait_for_threads")
	return ok
}

// toolSchemaAllows returns true if toolName appears in cfg.ToolSchemas.
// If ToolSchemas is empty (no restriction), all tools are allowed.
func (cfg *RunLoopConfig) toolSchemaAllows(name string) bool {
	if len(cfg.ToolSchemas) == 0 {
		return true
	}
	for _, s := range cfg.ToolSchemas {
		if s.Function.Name == name {
			return true
		}
	}
	return false
}

// safeOnBeforeWrite calls fn inside a recover so that a panicking callback
// does not propagate and leave writeMu locked. Returns (false, panicValue) on
// panic, or (fn's return value, nil) on normal return.
func safeOnBeforeWrite(fn func(string, []byte, []byte) bool, path string, old, new []byte) (allowed bool, panicVal any) {
	defer func() { panicVal = recover() }()
	allowed = fn(path, old, new)
	return
}

// previewWrite extracts path, oldContent, newContent from write_file or edit_file args.
func previewWrite(toolName string, args map[string]any) (path string, oldContent, newContent []byte) {
	switch toolName {
	case "write_file":
		if p, ok := args["file_path"].(string); ok {
			path = p
		}
		if content, ok := args["content"].(string); ok {
			newContent = []byte(content)
		}
		if path != "" {
			oldContent, _ = os.ReadFile(path)
		}

	case "edit_file":
		if p, ok := args["file_path"].(string); ok {
			path = p
		}
		if path != "" {
			oldContent, _ = os.ReadFile(path)
		}
		if len(oldContent) > 0 {
			oldStr, _ := args["old_string"].(string)
			newStr, _ := args["new_string"].(string)
			replaceAll, _ := args["replace_all"].(bool)
			content := string(oldContent)
			if replaceAll {
				content = strings.ReplaceAll(content, oldStr, newStr)
			} else {
				content = strings.Replace(content, oldStr, newStr, 1)
			}
			newContent = []byte(content)
		}
	}

	return
}
