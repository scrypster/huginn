package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
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
	ModelName          string           // model identifier sent to backend
	Messages           []backend.Message
	Tools              *tools.Registry
	ToolSchemas        []backend.Tool
	Gate               *permissions.Gate
	Backend            backend.Backend
	OnToken            func(string)
	OnEvent            func(backend.StreamEvent) // richer streaming; nil = use OnToken
	OnToolCall         func(name string, args map[string]any)
	OnToolDone         func(name string, result tools.ToolResult)
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
}

// LoopResult is the final state after the loop ends.
type LoopResult struct {
	FinalContent     string
	TurnCount        int
	StopReason       string            // "stop", "max_turns", "error", "no_tools"
	Messages         []backend.Message // full message history
	PromptTokens     int               // cumulative prompt tokens across all turns
	CompletionTokens int               // cumulative completion tokens across all turns
}

// executeSingle executes a single tool call and returns the result.
// writeMu ensures OnBeforeWrite callbacks are serialized.
// A deferred recover catches panics from any code path (serial or concurrent),
// logs them with a full stack trace, and returns a tool error result.
func (cfg *RunLoopConfig) executeSingle(ctx context.Context, idx int, tc backend.ToolCall, writeMu *sync.Mutex) (result dispatchedResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("tool: panic in executeSingle",
				"tool", tc.Function.Name,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			result = dispatchedResult{
				index:   idx,
				tc:      tc,
				content: fmt.Sprintf("error: tool %s panicked: %v", tc.Function.Name, r),
			}
		}
	}()

	toolName := tc.Function.Name
	argsMap := tc.Function.Arguments

	makeResult := func(content string) dispatchedResult {
		return dispatchedResult{index: idx, tc: tc, content: content}
	}

	if toolName == "" {
		return makeResult("error: tool call has empty function name")
	}

	tool, ok := cfg.Tools.Get(toolName)
	if !ok {
		return makeResult(fmt.Sprintf("error: unknown tool %q", toolName))
	}

	// Runtime enforcement: verify the tool was included in the schemas sent
	// to the model. A tool may exist in the registry but not be permitted
	// for this agent's toolbelt.
	if !cfg.toolSchemaAllows(toolName) {
		return makeResult(fmt.Sprintf("error: tool %q is not available", toolName))
	}

	if cfg.Gate != nil {
		req := permissions.PermissionRequest{
			ToolName: toolName,
			Level:    tool.Permission(),
			Args:     argsMap,
			Provider: cfg.Tools.ProviderFor(toolName),
		}
		if !cfg.Gate.Check(req) {
			if cfg.OnPermissionDenied != nil {
				cfg.OnPermissionDenied(toolName)
			}
			return makeResult("error: permission denied")
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
		cfg.OnToolCall(toolName, argsMap)
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
		cfg.OnToolDone(toolName, toolResult)
	}

	content := toolResult.Output
	if toolResult.IsError && toolResult.Error != "" {
		content = "error: " + toolResult.Error
	} else if content == "" && toolResult.Error != "" {
		content = toolResult.Error
	}

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
func RunLoop(ctx context.Context, cfg RunLoopConfig) (*LoopResult, error) {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 50
	}
	messages := make([]backend.Message, len(cfg.Messages))
	copy(messages, cfg.Messages)

	result := &LoopResult{}

	var consecutiveParseFailures int

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		result.TurnCount = turn + 1

		chatResult, err := cfg.Backend.ChatCompletion(ctx, backend.ChatRequest{
			Model:    cfg.ModelName,
			Messages: messages,
			Tools:    cfg.ToolSchemas,
			OnToken:  cfg.OnToken,
			OnEvent:  cfg.OnEvent,
		})
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
			result.StopReason = "stop"
			result.Messages = messages
			return result, nil
		}

		// Execute tool calls — independent ones in parallel, serial ones after
		dispatched := cfg.dispatchTools(ctx, chatResult.ToolCalls)
		for _, dr := range dispatched {
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
	}

	result.StopReason = "max_turns"
	result.Messages = messages
	return result, nil
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
