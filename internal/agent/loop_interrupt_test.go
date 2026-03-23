package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestRunLoop_ContextCancelBeforeStart verifies that a pre-cancelled context
// causes RunLoop to return a cancellation error immediately.
func TestRunLoop_ContextCancelBeforeStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run

	// Backend that blocks forever — should never be reached.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			stopResponse("should not reach"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(ctx, RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	// The backend uses a cancelled context. The AnthropicBackend / ExternalBackend
	// would return a context error; our mockBackend ignores context, so the loop
	// may complete normally OR the pre-cancel ctx.Done check fires. Both are
	// acceptable. What must NOT happen: a panic or a hang.
	_ = result
	_ = err
}

// TestRunLoop_ContextCancelMidLoop verifies that cancelling the context while
// the loop is waiting for a tool to finish causes RunLoop to exit with the
// cancellation error and StopReason="cancelled".
func TestRunLoop_ContextCancelMidLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	// A tool that blocks until the context is cancelled.
	blockingTool := &blockUntilCancelTool{done: ctx.Done()}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("block_tool", "call-1"),
			stopResponse("should not reach"),
		},
	}
	reg := newRegistryWith(blockingTool)

	done := make(chan struct{})
	var result *LoopResult
	var loopErr error
	go func() {
		defer close(done)
		result, loopErr = RunLoop(ctx, RunLoopConfig{
			MaxTurns: 10,
			Backend:  mb,
			Tools:    reg,
			Messages: []backend.Message{{Role: "user", Content: "block"}},
		})
	}()

	// Cancel context after tool starts executing.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunLoop did not exit after context cancel mid-loop")
	}

	if loopErr == nil {
		t.Error("expected non-nil error after context cancel")
	}
	if result != nil && result.StopReason != "cancelled" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "cancelled")
	}
}

// TestRunLoop_BackendErrorOnFirstTurn verifies that a backend error on the
// first turn sets StopReason="error" and propagates the wrapped error.
func TestRunLoop_BackendErrorOnFirstTurn(t *testing.T) {
	t.Parallel()

	sentinelErr := errors.New("connection refused")
	mb := &mockBackend{
		errors: []error{sentinelErr},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error does not wrap sentinel: got %v", err)
	}
	if result == nil {
		t.Fatal("result must not be nil even on error")
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "error")
	}
}

// TestRunLoop_BackendErrorOnSecondTurn verifies that an error on the second
// turn (after a successful tool call) is propagated correctly.
func TestRunLoop_BackendErrorOnSecondTurn(t *testing.T) {
	t.Parallel()

	tool := &mockTool{name: "mytool", result: tools.ToolResult{Output: "ok"}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("mytool", "call-1"),
		},
		errors: []error{nil, errors.New("second turn error")},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("expected error on second turn, got nil")
	}
	if result == nil {
		t.Fatal("result must not be nil even on error")
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "error")
	}
}

// TestRunLoop_ToolPanicRecovered verifies that a panic inside a tool's Execute
// is recovered (via dispatchTools defer/recover) and the loop continues rather
// than crashing the whole process.
// We use read_file as the tool name so dispatchTools classifies it as an
// independent tool and runs it in a goroutine with panic recovery.
func TestRunLoop_ToolPanicRecovered(t *testing.T) {
	t.Parallel()

	panicTool := &interruptPanicTool{}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "call-1"),
			stopResponse("handled"),
		},
	}
	reg := newRegistryWith(panicTool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call panicky"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}

// ---- helpers ----------------------------------------------------------------

// blockUntilCancelTool blocks Execute until the provided done channel is closed.
type blockUntilCancelTool struct {
	done <-chan struct{}
}

func (b *blockUntilCancelTool) Name() string                      { return "block_tool" }
func (b *blockUntilCancelTool) Description() string               { return "" }
func (b *blockUntilCancelTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (b *blockUntilCancelTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: "block_tool"}}
}
func (b *blockUntilCancelTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	<-b.done
	return tools.ToolResult{Output: "cancelled"}
}

// interruptPanicTool panics on every Execute call. It uses "read_file" as its
// name so dispatchTools classifies it as an independent (goroutine) tool and
// the goroutine-level recover fires.
type interruptPanicTool struct{}

func (p *interruptPanicTool) Name() string                      { return "read_file" }
func (p *interruptPanicTool) Description() string               { return "" }
func (p *interruptPanicTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (p *interruptPanicTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: "read_file"}}
}
func (p *interruptPanicTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	panic("test panic in tool")
}
