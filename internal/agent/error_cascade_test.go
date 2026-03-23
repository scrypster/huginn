package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestErrorCascade_BackendErrorTurn1 verifies that a backend error on the very
// first turn returns StopReason="error" and does not loop further.
func TestErrorCascade_BackendErrorTurn1(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{
		errors: []error{errors.New("connection refused")},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(),
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain 'connection refused': %v", err)
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want 'error'", result.StopReason)
	}
	if result.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", result.TurnCount)
	}
}

// TestErrorCascade_BackendErrorAfterToolCall verifies that a backend error on
// turn 2 (after a successful tool call) stops the loop and preserves history.
func TestErrorCascade_BackendErrorAfterToolCall(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "file content"}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "c1"),
		},
		errors: []error{
			nil, // turn 1 succeeds
			errors.New("server overloaded"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(tool),
		Messages: []backend.Message{{Role: "user", Content: "read file"}},
	})

	if err == nil {
		t.Fatal("expected error on turn 2")
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want 'error'", result.StopReason)
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", result.TurnCount)
	}
}

// TestErrorCascade_PermissionDeniedInBatch verifies that a permission-denied
// tool in a concurrent batch does not block other tools from executing.
func TestErrorCascade_PermissionDeniedInBatch(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "read-ok"}}
	// Give writeTool PermWrite so the Gate denies it (uses existing permWriteTool from coverage_test.go)
	writeTool := &permWriteTool{name: "write_file", result: tools.ToolResult{Output: "wrote-ok"}}

	// Gate with no promptFunc and skipAll=false: denies all PermWrite/PermExec,
	// but allows PermRead (read_file uses PermRead via mockTool).
	gate := permissions.NewGate(false, nil)

	var deniedTools []string
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
					{ID: "c2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/test.txt",
						"content":   "hello",
					}}},
				},
			},
			stopResponse("handled"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(readTool, writeTool),
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "read and write"}},
		OnPermissionDenied: func(name string) {
			deniedTools = append(deniedTools, name)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// read_file should have executed (PermRead is always allowed)
	if readTool.callCount != 1 {
		t.Errorf("read_file.callCount = %d, want 1", readTool.callCount)
	}

	// Verify permission denied callback fired for write_file
	if len(deniedTools) != 1 || deniedTools[0] != "write_file" {
		t.Errorf("expected OnPermissionDenied for write_file, got %v", deniedTools)
	}

	// Verify denial message in history
	foundDenial := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "c2" && strings.Contains(msg.Content, "permission denied") {
			foundDenial = true
			break
		}
	}
	if !foundDenial {
		t.Error("expected 'permission denied' in tool result for write_file")
	}
}

// TestErrorCascade_ParseErrorRecovery verifies that parse errors (malformed tool
// call JSON) are reported back to the model and it can retry successfully.
func TestErrorCascade_ParseErrorRecovery(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			// Turn 1: response with parse errors (malformed tool calls dropped)
			{
				Content:     "I tried to call a tool",
				DoneReason:  "tool_calls",
				ParseErrors: []string{"invalid JSON in tool call arguments"},
				ToolCalls:   nil, // all tool calls were dropped
			},
			// Turn 2: model retries successfully with no tool calls
			stopResponse("recovered from parse error"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(),
		Messages: []backend.Message{{Role: "user", Content: "call tool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}
	if result.FinalContent != "recovered from parse error" {
		t.Errorf("FinalContent = %q, want 'recovered from parse error'", result.FinalContent)
	}
}

// TestErrorCascade_ParseErrorLimitExceeded verifies that 3 consecutive parse
// errors causes the loop to stop with parse_error_limit.
func TestErrorCascade_ParseErrorLimitExceeded(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{DoneReason: "tool_calls", ParseErrors: []string{"bad json 1"}},
			{DoneReason: "tool_calls", ParseErrors: []string{"bad json 2"}},
			{DoneReason: "tool_calls", ParseErrors: []string{"bad json 3"}},
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    newRegistryWith(),
		Messages: []backend.Message{{Role: "user", Content: "keep failing"}},
	})
	if err == nil {
		t.Fatal("expected error for parse_error_limit")
	}
	if result.StopReason != "parse_error_limit" {
		t.Errorf("StopReason = %q, want 'parse_error_limit'", result.StopReason)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("error should mention consecutive failures: %v", err)
	}
}

// TestErrorCascade_ToolPanicRecovery verifies that a tool panic is recovered
// and reported as an error tool message, allowing the loop to continue.
func TestErrorCascade_ToolPanicRecovery(t *testing.T) {
	t.Parallel()

	panicTool := &panickyTool{name: "read_file"}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "c1"),
			stopResponse("recovered from panic"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(panicTool),
		Messages: []backend.Message{{Role: "user", Content: "trigger panic"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// The panic should be captured as an error in tool messages
	foundPanic := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "panic") {
			foundPanic = true
			break
		}
	}
	if !foundPanic {
		t.Error("expected panic error in tool result messages")
	}
}

// TestErrorCascade_MultipleErrorsOneTurn verifies that multiple tool errors in
// the same turn are all reported and the loop continues.
func TestErrorCascade_MultipleErrorsOneTurn(t *testing.T) {
	t.Parallel()

	errToolA := &mockTool{name: "read_file", result: tools.ToolResult{IsError: true, Error: "file not found"}}
	errToolB := &mockTool{name: "grep", result: tools.ToolResult{IsError: true, Error: "pattern invalid"}}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
					{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
				},
			},
			stopResponse("handled both errors"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(errToolA, errToolB),
		Messages: []backend.Message{{Role: "user", Content: "two failing tools"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// Both tool error messages should be in history
	var errorCount int
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.HasPrefix(msg.Content, "error: ") {
			errorCount++
		}
	}
	if errorCount != 2 {
		t.Errorf("expected 2 error tool messages, got %d", errorCount)
	}
}

// TestErrorCascade_ContextCancelMidTool verifies that context cancellation
// during tool execution stops the loop promptly.
func TestErrorCascade_ContextCancelMidTool(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	slowTool := &contextAwareTool{
		name:  "read_file",
		delay: 2 * time.Second,
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "c1"),
			stopResponse("done"),
		},
	}

	start := time.Now()
	_, _ = RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(slowTool),
		Messages: []backend.Message{{Role: "user", Content: "slow read"}},
	})
	elapsed := time.Since(start)

	// Should finish much faster than the 2s tool delay
	if elapsed > 500*time.Millisecond {
		t.Errorf("loop took %v, expected <500ms due to context cancel", elapsed)
	}
}

// panickyTool panics on Execute.
type panickyTool struct {
	name      string
	callCount atomic.Int32
}

func (t *panickyTool) Name() string                      { return t.name }
func (t *panickyTool) Description() string               { return "" }
func (t *panickyTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *panickyTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *panickyTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.callCount.Add(1)
	panic("intentional tool panic")
}
