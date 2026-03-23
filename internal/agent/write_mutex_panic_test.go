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

// writeFileTool is a harmless stand-in for write_file so that executeSingle
// reaches the OnBeforeWrite callback before calling the real tool.
type writeFileTool struct{}

func (w *writeFileTool) Name() string                      { return "write_file" }
func (w *writeFileTool) Description() string               { return "test write_file" }
func (w *writeFileTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (w *writeFileTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "write_file"}}
}
func (w *writeFileTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: "wrote file"}
}

// TestRunLoop_OnBeforeWritePanic_NoHang verifies that a panic inside the
// OnBeforeWrite callback does not crash or deadlock RunLoop.
// The callback panic must be recovered and writeMu must be released so that
// any concurrent write call can also proceed.
//
// Bug: writeMu.Lock() is called before OnBeforeWrite; if OnBeforeWrite panics,
// writeMu.Unlock() is never reached. The goroutine-level recover (from P0-1)
// catches the panic and calls wg.Done(), but writeMu remains locked. A second
// concurrent write goroutine waiting on writeMu.Lock() will then deadlock,
// preventing wg.Wait() from returning.
func TestRunLoop_OnBeforeWritePanic_MutexReleased(t *testing.T) {
	t.Parallel()

	reg := newRegistryWith(&writeFileTool{})

	// Two write_file calls to DIFFERENT paths — isIndependentTool returns true
	// for each (no path collision), so both are dispatched as concurrent goroutines.
	// Each goroutine will call executeSingle which invokes OnBeforeWrite while
	// holding writeMu. If goroutine 1 panics before releasing writeMu, goroutine 2
	// will block on writeMu.Lock() and never call wg.Done() — deadlock.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-write-1",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "/tmp/huginn_p02_a.txt",
								"content":   "hello",
							},
						},
					},
					{
						ID: "tc-write-2",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "/tmp/huginn_p02_b.txt",
								"content":   "world",
							},
						},
					},
				},
			},
			stopResponse("done"),
		},
	}

	var callCount atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, _ := RunLoop(ctx, RunLoopConfig{
		MaxTurns:  5,
		ModelName: "test-model",
		Messages:  []backend.Message{{Role: "user", Content: "write two files"}},
		Tools:     reg,
		Backend:   mb,
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			n := callCount.Add(1)
			if n == 1 {
				// First goroutine to acquire writeMu: sleep briefly so the
				// second goroutine reaches writeMu.Lock() and blocks, then panic.
				// Without the fix, writeMu stays locked → second goroutine hangs.
				time.Sleep(5 * time.Millisecond)
				panic("intentional OnBeforeWrite panic (P0-2 test)")
			}
			// Second call: allow the write (should only be reached if writeMu was released).
			return true
		},
	})

	// If the context fired, RunLoop deadlocked — the mutex was not released.
	if ctx.Err() != nil {
		t.Fatal("RunLoop timed out (3s) — OnBeforeWrite panic left writeMu locked, causing deadlock")
	}

	if result == nil {
		t.Fatal("expected non-nil LoopResult")
	}

	// Verify we got TWO tool result messages (both goroutines must have completed).
	toolMsgCount := 0
	for _, m := range result.Messages {
		if m.Role == "tool" {
			toolMsgCount++
		}
	}
	if toolMsgCount != 2 {
		t.Errorf("expected 2 tool result messages (both writes finished), got %d; messages: %v", toolMsgCount, result.Messages)
	}

	// The panicking write should produce an error result.
	foundError := false
	for _, m := range result.Messages {
		if m.Role == "tool" && (strings.Contains(m.Content, "error") ||
			strings.Contains(m.Content, "rejected") ||
			strings.Contains(m.Content, "panic")) {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Errorf("expected at least one error/rejection tool message; messages: %v", result.Messages)
	}
}
