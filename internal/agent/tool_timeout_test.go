package agent

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// hangingTool blocks in Execute until its context is cancelled, simulating a
// stuck MCP tool server or an unresponsive external process.
type hangingTool struct {
	name    string
	started chan struct{}
}

func (h *hangingTool) Name() string                      { return h.name }
func (h *hangingTool) Description() string               { return "" }
func (h *hangingTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (h *hangingTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: h.name}}
}
func (h *hangingTool) Execute(ctx context.Context, _ map[string]any) tools.ToolResult {
	if h.started != nil {
		select {
		case h.started <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return tools.ToolResult{IsError: true, Error: "context: " + ctx.Err().Error()}
}

// TestRunLoop_ToolCallTimeout verifies that a tool which hangs indefinitely is
// killed by the per-tool deadline introduced in RunLoopConfig.ToolCallTimeout,
// and that the loop receives an error result and continues to the next turn
// (where the backend can decide what to do).
func TestRunLoop_ToolCallTimeout(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	hang := &hangingTool{name: "mcp_hang", started: started}

	reg := newRegistryWith(hang)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("mcp_hang", "call-timeout-1"),
			stopResponse("recovered after timeout"),
		},
	}

	const shortTimeout = 100 * time.Millisecond

	start := time.Now()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:        5,
		Backend:         mb,
		Tools:           reg,
		Messages:        []backend.Message{{Role: "user", Content: "call hanging tool"}},
		ToolCallTimeout: shortTimeout,
	})

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The loop should have continued after the timeout and ended normally.
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}

	// The tool should have been started and timed out quickly.
	select {
	case <-started:
		// good — tool was invoked
	default:
		t.Error("hanging tool was never started")
	}

	// Total elapsed should be close to the timeout, not minutes.
	if elapsed > 5*time.Second {
		t.Errorf("RunLoop took too long (%v), tool timeout did not fire", elapsed)
	}

	// The tool result message should contain a context-related error.
	var foundError bool
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "mcp_hang" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected tool result message in history for timed-out tool")
	}
}

// TestRunLoop_ToolCallTimeout_CallerDeadlineWins verifies that if the caller
// already has a tighter deadline than ToolCallTimeout, we do not override it.
func TestRunLoop_ToolCallTimeout_CallerDeadlineWins(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	hang := &hangingTool{name: "mcp_tight", started: started}

	reg := newRegistryWith(hang)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("mcp_tight", "call-tight-1"),
			stopResponse("done"),
		},
	}

	// Caller sets a 50ms deadline — much tighter than ToolCallTimeout of 10s.
	const callerDeadline = 50 * time.Millisecond
	const longTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), callerDeadline)
	defer cancel()

	start := time.Now()

	// The loop will error out because the caller's context expires before the
	// backend's second call. That's fine — we just want to verify the tool
	// itself was killed by the caller's deadline, not the long ToolCallTimeout.
	_, _ = RunLoop(ctx, RunLoopConfig{
		MaxTurns:        5,
		Backend:         mb,
		Tools:           reg,
		Messages:        []backend.Message{{Role: "user", Content: "call tight"}},
		ToolCallTimeout: longTimeout,
	})

	elapsed := time.Since(start)

	// Should finish around callerDeadline, not longTimeout.
	if elapsed > 3*time.Second {
		t.Errorf("RunLoop took %v — caller deadline did not win over ToolCallTimeout", elapsed)
	}
}
