package agent

// loop_parse_limit_test.go — targeted tests for the 3-strike parse-error
// circuit breaker in RunLoop.  The coverage files above already contain
// overlapping tests (malformed_stream_test.go, loop_parseerror_test.go); these
// tests complement them with additional message-content and 2-bad-then-good
// scenarios that were not yet covered.

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestRunLoop_ParseLimit_ThreeStrikesReturnsError verifies the core 3-strike
// contract: three consecutive turns with ParseErrors must cause RunLoop to
// return a non-nil error (not loop forever) and StopReason="parse_error_limit".
func TestRunLoop_ParseLimit_ThreeStrikesReturnsError(t *testing.T) {
	t.Parallel()

	bad := &backend.ChatResponse{
		Content:     "trying to call tool",
		DoneReason:  "tool_calls",
		ParseErrors: []string{"unexpected EOF in tool call JSON"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{bad, bad, bad, stopResponse("never reached")},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 20,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call the tool"}},
	})

	if err == nil {
		t.Fatal("expected error after 3 consecutive parse failures, got nil")
	}
	if result == nil {
		t.Fatal("result must not be nil even when an error is returned")
	}
	if result.StopReason != "parse_error_limit" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "parse_error_limit")
	}
}

// TestRunLoop_ParseLimit_ErrorMessageMeaningful verifies that the error message
// produced by the circuit breaker is descriptive — it must contain either the
// word "consecutive" or "malformed" so operators can diagnose the problem.
func TestRunLoop_ParseLimit_ErrorMessageMeaningful(t *testing.T) {
	t.Parallel()

	bad := &backend.ChatResponse{
		DoneReason:  "tool_calls",
		ParseErrors: []string{"JSON decode error: unexpected token at pos 42"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{bad, bad, bad},
	}
	reg := newRegistryWith()

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "consecutive") && !strings.Contains(msg, "malformed") {
		t.Errorf("error message is not meaningful; got: %q", msg)
	}
}

// TestRunLoop_ParseLimit_TwoBadThenGoodSucceeds verifies that exactly 2
// consecutive parse failures followed by a valid response does NOT trigger the
// circuit breaker — the loop should succeed and return StopReason="stop".
func TestRunLoop_ParseLimit_TwoBadThenGoodSucceeds(t *testing.T) {
	t.Parallel()

	bad := &backend.ChatResponse{
		Content:     "bad",
		DoneReason:  "tool_calls",
		ParseErrors: []string{"malformed"},
	}
	good := stopResponse("all good after retries")

	mb := &mockBackend{
		responses: []*backend.ChatResponse{bad, bad, good},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "try tools"}},
	})
	if err != nil {
		t.Fatalf("2 bad then good must not trip the circuit breaker; got error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}

// TestRunLoop_ParseLimit_TwoBadThenToolCallSucceeds verifies the 2-bad-then-good
// path also works when the good response is a valid tool call (not just a stop).
func TestRunLoop_ParseLimit_TwoBadThenToolCallSucceeds(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "echo", result: tools.ToolResult{Output: "pong"}}

	bad := &backend.ChatResponse{
		Content:     "",
		DoneReason:  "tool_calls",
		ParseErrors: []string{"bad json"},
	}
	goodToolCall := toolCallResponse("echo", "c1")
	finalStop := stopResponse("done")

	mb := &mockBackend{
		responses: []*backend.ChatResponse{bad, bad, goodToolCall, finalStop},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "echo something"}},
	})
	if err != nil {
		t.Fatalf("2 bad then valid tool call must succeed; got error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_ParseLimit_CounterResetBetweenStrikes verifies that the counter
// is reset after a valid turn so the circuit breaker only trips on *consecutive*
// failures — not cumulative ones across the entire session.
func TestRunLoop_ParseLimit_CounterResetBetweenStrikes(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "mytool", result: tools.ToolResult{Output: "ok"}}

	bad := &backend.ChatResponse{
		DoneReason:  "tool_calls",
		ParseErrors: []string{"oops"},
	}
	validTool := toolCallResponse("mytool", "call-x")
	finalStop := stopResponse("end")

	// Sequence: bad, bad, validTool (resets counter), bad, bad, validTool (resets), stop
	// The counter never reaches 3 consecutively so no error should be returned.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			bad, bad, validTool,
			bad, bad, finalStop,
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 20,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "do stuff"}},
	})
	if err != nil {
		t.Fatalf("counter should reset on valid turns; got error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}
