package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestPlanImplementWrite_MultiTurnWith3Writes simulates a multi-turn agentic
// flow: turn 1 = plan (text only), turn 2 = tool calls for 3 file writes,
// turn 3 = summary. All writes are approved.
func TestPlanImplementWrite_MultiTurnWith3Writes(t *testing.T) {
	t.Parallel()

	var approvedPaths []string
	var mu sync.Mutex

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			// Turn 1: planning response (text only, no tool calls)
			stopResponse("Plan: I will create 3 files: main.go, util.go, test.go"),
		},
	}

	// First run: planning phase
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "create a Go project with 3 files"}},
	})
	if err != nil {
		t.Fatalf("plan phase error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("plan phase StopReason = %q, want 'stop'", result.StopReason)
	}
	if result.TurnCount != 1 {
		t.Errorf("plan phase TurnCount = %d, want 1", result.TurnCount)
	}

	// Second run: implementation phase (continue from plan messages)
	mb2 := &mockBackend{
		responses: []*backend.ChatResponse{
			// Turn 1: 3 write_file calls
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/main.go",
						"content":   "package main\nfunc main() {}",
					}}},
					{ID: "w2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/util.go",
						"content":   "package main\nfunc util() {}",
					}}},
					{ID: "w3", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/test.go",
						"content":   "package main\nfunc test() {}",
					}}},
				},
			},
			// Turn 2: summary
			stopResponse("Created all 3 files successfully."),
		},
	}

	implMessages := append(result.Messages, backend.Message{
		Role:    "user",
		Content: "Proceed with the implementation.",
	})

	result2, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb2,
		Tools:    newRegistryWith(writeTool),
		Messages: implMessages,
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			mu.Lock()
			approvedPaths = append(approvedPaths, path)
			mu.Unlock()
			return true
		},
	})
	if err != nil {
		t.Fatalf("implement phase error: %v", err)
	}
	if result2.StopReason != "stop" {
		t.Errorf("implement phase StopReason = %q, want 'stop'", result2.StopReason)
	}

	// All 3 writes should have been approved
	mu.Lock()
	defer mu.Unlock()
	if len(approvedPaths) != 3 {
		t.Errorf("expected 3 OnBeforeWrite calls, got %d: %v", len(approvedPaths), approvedPaths)
	}

	// All 3 writes should have executed
	if writeTool.callCount != 3 {
		t.Errorf("write_file.callCount = %d, want 3", writeTool.callCount)
	}
}

// TestPlanImplementWrite_PartialApproval simulates 3 file writes where only
// 2 of 3 are approved. The rejected write should appear as an error in history.
func TestPlanImplementWrite_PartialApproval(t *testing.T) {
	t.Parallel()

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/approved1.go",
						"content":   "package main",
					}}},
					{ID: "w2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/rejected.go",
						"content":   "package danger",
					}}},
					{ID: "w3", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/approved2.go",
						"content":   "package util",
					}}},
				},
			},
			stopResponse("2 of 3 files written"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "write 3 files"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			// Reject the "rejected.go" file
			return !strings.Contains(path, "rejected")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// Count approvals and rejections in tool messages
	var approvedCount, rejectedCount int
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "write_file" {
			if strings.Contains(msg.Content, "rejected") {
				rejectedCount++
			} else if msg.Content == "written" {
				approvedCount++
			}
		}
	}
	if approvedCount != 2 {
		t.Errorf("expected 2 approved writes, got %d", approvedCount)
	}
	if rejectedCount != 1 {
		t.Errorf("expected 1 rejected write, got %d", rejectedCount)
	}
}

// TestPlanImplementWrite_ContextCancelDuringImplement verifies that cancelling
// the context during the implement phase (mid-write) stops the loop.
func TestPlanImplementWrite_ContextCancelDuringImplement(t *testing.T) {
	t.Parallel()

	var writeCount atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/a.go",
						"content":   "a",
					}}},
					{ID: "w2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/b.go",
						"content":   "b",
					}}},
				},
			},
			stopResponse("done"),
		},
	}

	_, _ = RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "write files"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			n := writeCount.Add(1)
			if n >= 1 {
				cancel() // Cancel after first write
			}
			return true
		},
	})

	// Context should be cancelled
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled")
	}
}

// TestPlanImplementWrite_EmptyPlanThenImplement verifies the transition from
// a text-only planning turn to an implementation turn with tool calls.
func TestPlanImplementWrite_EmptyPlanThenImplement(t *testing.T) {
	t.Parallel()

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "ok"},
	}
	readTool := &mockTool{
		name:   "read_file",
		result: tools.ToolResult{Output: "existing content"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			// Turn 1: read existing file
			toolCallResponse("read_file", "r1"),
			// Turn 2: write new file
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/huginn_test/new.go",
						"content":   "package new",
					}}},
				},
			},
			// Turn 3: done
			stopResponse("Implementation complete."),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool, readTool),
		Messages: []backend.Message{{Role: "user", Content: "read then write"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			return true
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
	if readTool.callCount != 1 {
		t.Errorf("read_file.callCount = %d, want 1", readTool.callCount)
	}
	if writeTool.callCount != 1 {
		t.Errorf("write_file.callCount = %d, want 1", writeTool.callCount)
	}
}

// TestPlanImplementWrite_AllWritesRejected verifies that when all writes are
// rejected, the loop continues and the model gets rejection messages.
func TestPlanImplementWrite_AllWritesRejected(t *testing.T) {
	t.Parallel()

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/x.go", "content": "x",
					}}},
					{ID: "w2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/y.go", "content": "y",
					}}},
				},
			},
			stopResponse("All writes were rejected."),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "write two files"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			return false // reject all
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// write_file should never have been called
	if writeTool.callCount != 0 {
		t.Errorf("write_file.callCount = %d, want 0", writeTool.callCount)
	}

	// Both tool messages should contain rejection
	var rejectionCount int
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "rejected") {
			rejectionCount++
		}
	}
	if rejectionCount != 2 {
		t.Errorf("expected 2 rejection messages, got %d", rejectionCount)
	}
}

// TestPlanImplementWrite_WriteTimeoutDoesNotHang verifies that writes with a
// context timeout do not cause the loop to hang.
func TestPlanImplementWrite_WriteTimeoutDoesNotHang(t *testing.T) {
	t.Parallel()

	writeTool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "ok"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{
						"file_path": "/tmp/timeout.go", "content": "package timeout",
					}}},
				},
			},
			stopResponse("done"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "write with timeout"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			return true
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}
	if ctx.Err() != nil {
		t.Error("context timed out, loop may have hung")
	}
}
