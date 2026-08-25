package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// A backend that reports tools it already ran itself.
type executedToolsBackend struct{ calls int }

func (b *executedToolsBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	return &backend.ChatResponse{
		Content:    "I read the file.",
		DoneReason: "stop",
		ExecutedTools: []backend.ExecutedTool{{
			Call: backend.ToolCall{
				ID:       "tu1",
				Function: backend.ToolCallFunction{Name: "Read", Arguments: map[string]any{"file_path": "/tmp/x"}},
			},
			Result: "package main",
		}},
	}, nil
}
func (b *executedToolsBackend) Health(_ context.Context) error   { return nil }
func (b *executedToolsBackend) Shutdown(_ context.Context) error { return nil }
func (b *executedToolsBackend) ContextWindow() int               { return 200000 }

// TestExecutedToolsArePersistedButNotDispatched verifies that ChatResponse.ExecutedTools
// are written into the message history (assistant tool_call + tool result) but are
// NEVER dispatched through the tool registry. Dispatch is observed the same way
// TestRunLoop_ToolCallExecuted observes it: via a mockTool's callCount. If the loop
// wrongly dispatched based on ExecutedTools, the registered "Read" mockTool's
// callCount would be nonzero.
func TestExecutedToolsArePersistedButNotDispatched(t *testing.T) {
	readTool := &mockTool{
		name:   "Read",
		result: tools.ToolResult{Output: "should never run"},
	}
	reg := newRegistryWith(readTool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  &executedToolsBackend{},
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "read the file"}},
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if readTool.callCount != 0 {
		t.Fatalf("dispatched %d tool calls; ExecutedTools must NEVER be dispatched — they already ran", readTool.callCount)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop — the loop must terminate, not iterate", result.StopReason)
	}

	var sawAssistantWithCall, sawToolResult bool
	for _, m := range result.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "tu1" {
			sawAssistantWithCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "tu1" && m.Content == "package main" {
			sawToolResult = true
		}
	}
	if !sawAssistantWithCall {
		t.Error("no assistant message carrying the executed tool call — it was not persisted into history")
	}
	if !sawToolResult {
		t.Error("no role=tool message carrying the result — Huginn's history is missing the tool output")
	}
}

// mixedFieldsBackend returns a response carrying BOTH a real ToolCalls entry
// and an ExecutedTools entry on its first call, then a terminal stop response
// on the second call (after the real tool call has been dispatched and the
// loop continues for another turn).
type mixedFieldsBackend struct{ calls int }

func (b *mixedFieldsBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		return &backend.ChatResponse{
			Content:    "",
			DoneReason: "tool_calls",
			ToolCalls: []backend.ToolCall{{
				ID:       "real-1",
				Function: backend.ToolCallFunction{Name: "mytool", Arguments: map[string]any{}},
			}},
			ExecutedTools: []backend.ExecutedTool{{
				Call: backend.ToolCall{
					ID:       "tu1",
					Function: backend.ToolCallFunction{Name: "Read", Arguments: map[string]any{"file_path": "/tmp/x"}},
				},
				Result: "package main",
			}},
		}, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (b *mixedFieldsBackend) Health(_ context.Context) error   { return nil }
func (b *mixedFieldsBackend) Shutdown(_ context.Context) error { return nil }
func (b *mixedFieldsBackend) ContextWindow() int               { return 200000 }

// TestToolCallsWinWhenBothFieldsAreSet verifies that when a response carries
// BOTH a real tool call and an already-executed one, the real call still
// dispatches (the loop's behavior is unaffected), while the ExecutedTools
// entry is ignored entirely rather than appended as a tool result answering
// a call no assistant message declared.
func TestToolCallsWinWhenBothFieldsAreSet(t *testing.T) {
	realTool := &mockTool{
		name:   "mytool",
		result: tools.ToolResult{Output: "real tool ran"},
	}
	reg := newRegistryWith(realTool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  &mixedFieldsBackend{},
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "do stuff"}},
	})
	if err != nil {
		t.Fatalf("loop: %v", err)
	}

	if realTool.callCount != 1 {
		t.Fatalf("realTool.callCount = %d, want 1 — the real ToolCalls entry must still dispatch", realTool.callCount)
	}

	for _, m := range result.Messages {
		if m.Role == "tool" && m.ToolCallID == "tu1" {
			t.Fatalf("found orphaned role=tool message for ToolCallID %q — the ExecutedTools entry must be ignored when ToolCalls is also present", m.ToolCallID)
		}
	}

	var sawAssistantWithRealCall bool
	for _, m := range result.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "real-1" {
			sawAssistantWithRealCall = true
		} else if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			t.Errorf("assistant message ToolCalls = %+v, want only the real call [real-1]", m.ToolCalls)
		}
	}
	if !sawAssistantWithRealCall {
		t.Error("no assistant message carrying only the real tool call")
	}
}
