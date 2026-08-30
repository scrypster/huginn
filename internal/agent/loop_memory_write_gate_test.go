package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestRunLoop_QuestionTurnBlocksModelMuninnRemember reproduces the live
// 2026-08-27 Winston DM defect: asked "what's our production database
// called?", the model answered by inventing a fact and calling
// muninn_remember instead of recalling. The harness must intercept a
// muninn_remember call made during a question-shaped turn (MemoryUserMsg
// ends in '?' / opens with an interrogative) — the underlying tool must
// never execute, and the tool result returned to the model must steer it
// toward recall instead.
func TestRunLoop_QuestionTurnBlocksModelMuninnRemember(t *testing.T) {
	remember := &mockTool{
		name:   "muninn_remember",
		result: tools.ToolResult{Output: "stored"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("muninn_remember", "call-1"),
			stopResponse("Our production database is called PostgreSQL."),
		},
	}
	reg := newRegistryWith(remember)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		Backend:       mb,
		Tools:         reg,
		Messages:      []backend.Message{{Role: "user", Content: "what's our production database called?"}},
		MemoryUserMsg: "what's our production database called?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remember.callCount != 0 {
		t.Fatalf("muninn_remember was executed (callCount=%d), want 0 — write must be intercepted before reaching the tool", remember.callCount)
	}

	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "muninn_remember" {
			found = true
			low := strings.ToLower(msg.Content)
			if !strings.Contains(low, "recall") {
				t.Errorf("blocked tool result should tell the model to recall instead, got: %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatal("expected a muninn_remember tool result message in history")
	}
}

// TestRunLoop_StatementTurnAllowsModelMuninnRemember verifies the write gate
// does not interfere with normal writes: when the user's turn is a plain
// statement (not question-shaped), a model-issued muninn_remember call
// passes through and actually executes.
func TestRunLoop_StatementTurnAllowsModelMuninnRemember(t *testing.T) {
	remember := &mockTool{
		name:   "muninn_remember",
		result: tools.ToolResult{Output: "stored"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("muninn_remember", "call-1"),
			stopResponse("Got it, noted."),
		},
	}
	reg := newRegistryWith(remember)

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		Backend:       mb,
		Tools:         reg,
		Messages:      []backend.Message{{Role: "user", Content: "our production database is named yggdrasil"}},
		MemoryUserMsg: "our production database is named yggdrasil",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remember.callCount != 1 {
		t.Fatalf("muninn_remember was not executed (callCount=%d), want 1 — statement turns must pass through", remember.callCount)
	}
}
