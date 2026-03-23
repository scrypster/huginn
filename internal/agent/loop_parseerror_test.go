package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// parseErrorResponse returns a ChatResponse that has ParseErrors but no valid
// tool calls — simulating malformed JSON in SSE tool blocks.
func parseErrorResponse(count int) *backend.ChatResponse {
	errs := make([]string, count)
	for i := range errs {
		errs[i] = "malformed JSON in tool call"
	}
	return &backend.ChatResponse{
		Content:     "",
		DoneReason:  "tool_calls",
		ParseErrors: errs,
	}
}

// TestRunLoop_ParseErrors_InjectsFeedback verifies that when a turn has ParseErrors,
// a "[system]" user message is injected before the next backend call so the model
// knows its tool calls were dropped.
func TestRunLoop_ParseErrors_InjectsFeedback(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			parseErrorResponse(2), // turn 1: 2 parse errors, no valid tool calls
			stopResponse("done"),  // turn 2: model stops after receiving feedback
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call some tools"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// Verify the second backend call includes the [system] feedback message.
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) < 2 {
		t.Fatalf("expected 2 backend calls, got %d", len(mb.lastRequests))
	}
	msgs := mb.lastRequests[1].Messages
	found := false
	for _, msg := range msgs {
		if msg.Role == "user" && strings.Contains(msg.Content, "[system]") && strings.Contains(msg.Content, "malformed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected [system] feedback message in second backend call; got messages: %v", msgs)
	}
}

// TestRunLoop_ParseErrors_CircuitBreaker verifies that 3 consecutive turns with
// ParseErrors cause the loop to return an error rather than burning tokens
// indefinitely.
func TestRunLoop_ParseErrors_CircuitBreaker(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			parseErrorResponse(1),
			parseErrorResponse(1),
			parseErrorResponse(1),
			stopResponse("should not reach here"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call tools"}},
	})
	if err == nil {
		t.Fatal("expected circuit-breaker error, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "malformed") && !strings.Contains(errStr, "consecutive") {
		t.Errorf("error message does not describe the parse failure: %v", err)
	}
	if result.StopReason != "parse_error_limit" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "parse_error_limit")
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
}

// TestRunLoop_ParseErrors_ResetOnSuccess verifies that a successful turn (no
// ParseErrors) resets the consecutive failure counter so the circuit-breaker
// does not trip even when parse errors appear again after a clean turn.
func TestRunLoop_ParseErrors_ResetOnSuccess(t *testing.T) {
	tool := &mockTool{name: "mytool", result: tools.ToolResult{Output: "ok"}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			parseErrorResponse(1),               // turn 1: parse error (consecutive=1)
			parseErrorResponse(1),               // turn 2: parse error (consecutive=2)
			toolCallResponse("mytool", "call-1"), // turn 3: valid tool call → resets to 0
			parseErrorResponse(1),               // turn 4: parse error (consecutive=1, no trip)
			stopResponse("all good"),            // turn 5: clean stop
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "do stuff"}},
	})
	if err != nil {
		t.Fatalf("circuit-breaker should not have tripped; got error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}
