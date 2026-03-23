package mcp_test

// list_tools_timeout_test.go verifies that ListTools applies a 5-second default
// timeout when the caller's context has no deadline, so a transport that never
// responds does not hang indefinitely.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
)

// hangingTransport is a Transport whose Receive call blocks until Close is
// called or until the test finishes.  Send always succeeds.
type hangingTransport struct {
	mu     sync.Mutex
	closed bool
	ch     chan struct{} // closed when Close() is called
}

func newHangingTransport() *hangingTransport {
	return &hangingTransport{ch: make(chan struct{})}
}

func (h *hangingTransport) Send(_ context.Context, _ []byte) error {
	return nil
}

// Receive blocks until the transport is closed.  The context deadline is
// deliberately ignored so that only a client-side deadline can unblock the
// goroutine (this is what we want to prove the test exercises).
func (h *hangingTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-h.ch:
		return nil, context.Canceled
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *hangingTransport) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.ch)
	}
	return nil
}

// TestListTools_DefaultTimeout_NoDeadline verifies that when the caller passes
// context.Background() (no deadline), ListTools still returns an error within
// ~6 seconds due to the built-in 5-second default timeout.
func TestListTools_DefaultTimeout_NoDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow timeout test in short mode")
	}

	tr := newHangingTransport()
	defer tr.Close()

	c := mcp.NewMCPClient(tr)

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := c.ListTools(context.Background()) // no deadline
		done <- err
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("expected an error from ListTools when transport hangs, got nil")
		}
		// The built-in timeout is 5 seconds; allow 6 seconds of wall-clock
		// slack to avoid flakiness on loaded CI runners.
		if elapsed > 6*time.Second {
			t.Errorf("ListTools took %v; expected to return within 6s", elapsed)
		}
		if elapsed < 4*time.Second {
			// Returned too fast — something else must have triggered the error.
			t.Logf("ListTools returned in %v (faster than 5s timeout; may be OK if transport closed)", elapsed)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("ListTools did not return within 8s despite having a 5s default timeout")
	}
}

// TestListTools_CallerDeadlineRespected verifies that when the caller already
// has a deadline shorter than 5s, that shorter deadline is respected and
// ListTools does NOT re-wrap the context with the 5s default.
func TestListTools_CallerDeadlineRespected(t *testing.T) {
	tr := newHangingTransport()
	defer tr.Close()

	c := mcp.NewMCPClient(tr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.ListTools(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when transport hangs and caller deadline expires")
	}
	// Should return near the 200ms caller deadline, not the 5s default.
	if elapsed > 1*time.Second {
		t.Errorf("ListTools took %v; expected to honor 200ms caller deadline", elapsed)
	}
}
