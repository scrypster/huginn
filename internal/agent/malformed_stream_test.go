package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestRunLoop_EmptyContentNoToolCalls verifies that a backend response with
// empty content and no tool calls ends the loop cleanly with StopReason="stop".
// This is the degenerate case where the model returns nothing — the loop
// should still terminate without an error.
func TestRunLoop_EmptyContentNoToolCalls(t *testing.T) {
	t.Parallel()

	emptyResp := &backend.ChatResponse{
		Content:    "",
		DoneReason: "stop",
		ToolCalls:  nil,
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{emptyResp},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "return nothing"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.FinalContent != "" {
		t.Errorf("FinalContent = %q, want empty", result.FinalContent)
	}
}

// TestRunLoop_ParseErrorsSurfacedToModel verifies that when a ChatResponse
// contains ParseErrors (malformed tool call JSON from the SSE stream), the loop
// sends a corrective user message back to the model and does NOT stop.
func TestRunLoop_ParseErrorsSurfacedToModel(t *testing.T) {
	t.Parallel()

	// First response has parse errors but no tool calls — simulates a malformed
	// SSE tool call that was dropped during streaming.
	malformedResp := &backend.ChatResponse{
		Content:     "I tried to call a tool",
		DoneReason:  "tool_calls",
		ToolCalls:   nil, // dropped due to parse error
		ParseErrors: []string{"tool \"bash\" (id=call_1): malformed args JSON"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			malformedResp,
			stopResponse("I retried successfully"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "run bash"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// Two backend calls: first with parse error, second after corrective message.
	mb.mu.Lock()
	calls := mb.callCount
	mb.mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 backend calls, got %d", calls)
	}
}

// TestRunLoop_ThreeConsecutiveParseErrorsStopsLoop verifies that after 3
// consecutive turns with parse errors, the loop stops with StopReason=
// "parse_error_limit" and returns an error.
func TestRunLoop_ThreeConsecutiveParseErrorsStopsLoop(t *testing.T) {
	t.Parallel()

	malformedResp := &backend.ChatResponse{
		Content:     "",
		DoneReason:  "tool_calls",
		ToolCalls:   nil,
		ParseErrors: []string{"tool \"x\": bad json"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			malformedResp,
			malformedResp,
			malformedResp,
			stopResponse("should not reach"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "bad tool"}},
	})

	if err == nil {
		t.Fatal("expected error after 3 consecutive parse failures, got nil")
	}
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if result.StopReason != "parse_error_limit" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "parse_error_limit")
	}
}

// TestRunLoop_ParseErrorResetOnValidTurn verifies that a valid turn resets
// the consecutive parse-failure counter, so a new parse error after a valid
// turn does not count toward the limit carried over from before.
func TestRunLoop_ParseErrorResetOnValidTurn(t *testing.T) {
	t.Parallel()

	malformedResp := &backend.ChatResponse{
		Content:     "",
		DoneReason:  "tool_calls",
		ToolCalls:   nil,
		ParseErrors: []string{"malformed"},
	}
	validResp := stopResponse("good response")

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			malformedResp,
			malformedResp,
			validResp, // resets counter
			malformedResp,
			malformedResp,
			validResp, // resets counter again
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 20,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "test reset"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}

// TestRunLoop_NilBackendResponseErrors verifies that a nil ChatResponse
// (with no error) from the backend causes an error to be returned.
func TestRunLoop_NilBackendResponseErrors(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{nil}, // nil response, nil error
	}
	reg := newRegistryWith()

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("expected error for nil backend response, got nil")
	}
}
