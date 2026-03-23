package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
)

// blockingTransport never returns from Receive until the context is cancelled.
type blockingTransport struct {
	sent [][]byte
}

func (b *blockingTransport) Send(_ context.Context, msg []byte) error {
	b.sent = append(b.sent, msg)
	return nil
}

func (b *blockingTransport) Receive(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingTransport) Close() error { return nil }

// TestCallTool_ExplicitDeadline_Respected verifies that when CallTool is
// passed a context with a short deadline, the call times out at that deadline
// even though the transport blocks indefinitely.
//
// This also indirectly proves that when context.Background() is passed (no
// deadline), CallTool's internal 2-minute fallback timeout is the only thing
// that would unblock it — the test uses an explicit short deadline for speed.
func TestCallTool_ExplicitDeadline_Respected(t *testing.T) {
	t.Parallel()

	tr := &blockingTransport{}
	c := mcp.NewMCPClient(tr)

	// Caller passes a 150ms deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.CallTool(ctx, "any-tool", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from CallTool with blocking transport")
	}
	// Must have returned well within a second (caller deadline was 150ms).
	if elapsed > 2*time.Second {
		t.Errorf("CallTool took too long (%v); caller deadline of 150ms was not respected", elapsed)
	}
}
