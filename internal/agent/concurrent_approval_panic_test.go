package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestConcurrentApprovalPanic_NoDeadlock verifies that an OnBeforeWrite panic
// combined with an independent concurrent tool does not cause a deadlock.
// This extends hardening_round13_test.go's panic test by adding a concurrent
// non-write tool that must also complete.
func TestConcurrentApprovalPanic_NoDeadlock(t *testing.T) {
	t.Parallel()

	writeTool := &writeFileTool{}
	readTool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "read-ok"}}

	var panicCount atomic.Int32

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					// write_file: will trigger OnBeforeWrite panic
					{ID: "w1", Function: backend.ToolCallFunction{
						Name: "write_file",
						Arguments: map[string]any{
							"file_path": "/tmp/huginn_cap_test.go",
							"content":   "panic content",
						},
					}},
					// read_file: independent, should not be blocked by panic
					{ID: "r1", Function: backend.ToolCallFunction{
						Name:      "read_file",
						Arguments: map[string]any{},
					}},
				},
			},
			stopResponse("handled"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, _ := RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool, readTool),
		Messages: []backend.Message{{Role: "user", Content: "write and read"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			panicCount.Add(1)
			panic("intentional OnBeforeWrite panic for concurrent test")
		},
	})

	if ctx.Err() != nil {
		t.Fatal("RunLoop timed out (3s) — deadlock detected: OnBeforeWrite panic blocked concurrent tool")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Both tools should have result messages
	var toolMsgCount int
	for _, msg := range result.Messages {
		if msg.Role == "tool" {
			toolMsgCount++
		}
	}
	if toolMsgCount != 2 {
		t.Errorf("expected 2 tool result messages, got %d", toolMsgCount)
	}

	// read_file should have executed successfully
	readFound := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "r1" && msg.Content == "read-ok" {
			readFound = true
			break
		}
	}
	if !readFound {
		t.Error("read_file result not found — concurrent tool was blocked by panic")
	}

	// write_file should have a panic/error message
	writeError := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "w1" &&
			(strings.Contains(msg.Content, "panic") || strings.Contains(msg.Content, "error")) {
			writeError = true
			break
		}
	}
	if !writeError {
		t.Error("expected panic/error message for write_file tool result")
	}
}

// TestConcurrentApprovalPanic_TwoWritesPanic verifies that two concurrent
// write_file calls where the first panics in OnBeforeWrite does not deadlock
// the second one waiting on writeMu.
func TestConcurrentApprovalPanic_TwoWritesPanic(t *testing.T) {
	t.Parallel()

	writeTool := &writeFileTool{}
	var callOrder atomic.Int32

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{
						Name: "write_file",
						Arguments: map[string]any{
							"file_path": "/tmp/huginn_cap_a.go",
							"content":   "a",
						},
					}},
					{ID: "w2", Function: backend.ToolCallFunction{
						Name: "write_file",
						Arguments: map[string]any{
							"file_path": "/tmp/huginn_cap_b.go",
							"content":   "b",
						},
					}},
				},
			},
			stopResponse("done"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, _ := RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool),
		Messages: []backend.Message{{Role: "user", Content: "write two"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			n := callOrder.Add(1)
			if n == 1 {
				// First call panics
				time.Sleep(5 * time.Millisecond) // let second goroutine reach writeMu
				panic("first write panic")
			}
			return true // second call succeeds
		},
	})

	if ctx.Err() != nil {
		t.Fatal("RunLoop timed out — writeMu deadlock from first panic")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have 2 tool messages total
	var toolCount int
	for _, msg := range result.Messages {
		if msg.Role == "tool" {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Errorf("expected 2 tool messages, got %d", toolCount)
	}
}

// TestConcurrentApprovalPanic_SlowApprovalNoPanic verifies that a slow (but
// non-panicking) OnBeforeWrite callback still allows concurrent non-write
// tools to complete.
func TestConcurrentApprovalPanic_SlowApprovalNoPanic(t *testing.T) {
	t.Parallel()

	writeTool := &writeFileTool{}
	readTool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "fast-read"}}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "w1", Function: backend.ToolCallFunction{
						Name: "write_file",
						Arguments: map[string]any{
							"file_path": "/tmp/huginn_slow.go",
							"content":   "slow",
						},
					}},
					{ID: "r1", Function: backend.ToolCallFunction{
						Name:      "read_file",
						Arguments: map[string]any{},
					}},
				},
			},
			stopResponse("done"),
		},
	}

	start := time.Now()
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    newRegistryWith(writeTool, readTool),
		Messages: []backend.Message{{Role: "user", Content: "write and read"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			time.Sleep(100 * time.Millisecond) // slow but not panicking
			return true
		},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want 'stop'", result.StopReason)
	}

	// read_file should not have been blocked by the slow write approval
	readFound := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "r1" && msg.Content == "fast-read" {
			readFound = true
			break
		}
	}
	if !readFound {
		t.Error("read_file result not found")
	}

	// Should complete in reasonable time (not much longer than the 100ms sleep)
	if elapsed > 2*time.Second {
		t.Errorf("elapsed %v, expected much less — possible blocking issue", elapsed)
	}
}
