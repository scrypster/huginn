package agent

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// panicTool is a tool whose Execute() always panics. It registers under the
// name "read_file" so that isIndependentTool() classifies it as an independent
// (concurrent) tool — the goroutine path that had no recover() and would
// deadlock by never calling wg.Done().
type panicTool struct{}

func (p *panicTool) Name() string              { return "read_file" }
func (p *panicTool) Description() string       { return "always panics" }
func (p *panicTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (p *panicTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "read_file"}}
}
func (p *panicTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	panic("intentional test panic")
}

// TestRunLoop_ToolPanic_NoDeadlock verifies that a panicking tool does not
// cause RunLoop to deadlock forever. The goroutine dispatching independent tools
// must have a recover() so that WaitGroup.Done() is always called.
//
// Without the fix, this test hangs until the 3-second context timeout fires and
// reports a deadlock failure.
func TestRunLoop_ToolPanic_NoDeadlock(t *testing.T) {
	// "read_file" is classified by isIndependentTool() as an independent tool,
	// so it gets dispatched in a goroutine via wg.Add(1)/go func()/wg.Wait().
	// A panic in that goroutine (without recover) leaves wg.Done() uncalled
	// and wg.Wait() blocks forever — deadlock.
	toolCallResp := &backend.ChatResponse{
		Content:    "",
		DoneReason: "tool_calls",
		ToolCalls: []backend.ToolCall{
			{
				ID: "call1",
				Function: backend.ToolCallFunction{
					Name:      "read_file",
					Arguments: map[string]any{},
				},
			},
		},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResp,
			// If the panic is recovered and the loop continues, the second call
			// gets a stop response.
			stopResponse("recovered"),
		},
	}

	reg := newRegistryWith(&panicTool{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// RunLoop should return within the timeout — not deadlock.
	done := make(chan error, 1)
	go func() {
		_, err := RunLoop(ctx, RunLoopConfig{
			MaxTurns: 5,
			Backend:  mb,
			Tools:    reg,
			Messages: []backend.Message{{Role: "user", Content: "trigger panic"}},
		})
		done <- err
	}()

	select {
	case err := <-done:
		// RunLoop returned — panic was recovered correctly.
		t.Logf("RunLoop returned without deadlock; err=%v", err)
	case <-ctx.Done():
		t.Fatal("RunLoop deadlocked — did not return within 3 seconds after tool panic")
	}
}
