package server

import (
	"context"
	"testing"
	"time"
)

// TestWSClient_SafeSend_NoPanicAfterDisconnect verifies that safeSend does not
// panic after a client disconnects. The channel is never closed directly;
// context cancellation is the sole termination signal. When the context is
// cancelled and the buffered channel has space, safeSend may still deliver the
// message (both outcomes are valid); what must not happen is a panic.
func TestWSClient_SafeSend_NoPanicAfterDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: simulates client already disconnected

	c := &wsClient{
		send:   make(chan WSMessage, 4),
		ctx:    ctx,
		cancel: cancel,
	}

	// Must not panic regardless of whether the message is delivered.
	start := time.Now()
	c.safeSend(WSMessage{Type: "done"})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("safeSend blocked too long (%v) after disconnect", elapsed)
	}
}

// TestWSClient_SafeSend_CancelledContext verifies that safeSend never blocks
// indefinitely when the client context is cancelled (client disconnected), even
// if the channel is full. When both the send channel and ctx.Done() are ready,
// Go picks one randomly, so we only assert that the call returns promptly.
func TestWSClient_SafeSend_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	// Use a zero-capacity channel so the channel case is never immediately ready.
	// This forces the select to take the ctx.Done() branch.
	c := &wsClient{
		send:   make(chan WSMessage, 0),
		ctx:    ctx,
		cancel: cancel,
	}

	start := time.Now()
	ok := c.safeSend(WSMessage{Type: "token", Content: "hello"})
	elapsed := time.Since(start)

	if ok {
		t.Error("expected false for cancelled context with full channel")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("safeSend blocked too long (%v) on cancelled context", elapsed)
	}
}

// TestWSClient_SafeSend_NormalDelivery verifies that safeSend delivers a message
// to the send channel when the context is active and the channel has space.
func TestWSClient_SafeSend_NormalDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &wsClient{
		send:   make(chan WSMessage, 4),
		ctx:    ctx,
		cancel: cancel,
	}

	msg := WSMessage{Type: "text", Content: "hello from Tom"}
	ok := c.safeSend(msg)
	if !ok {
		t.Fatal("expected safeSend to return true for live channel")
	}

	select {
	case received := <-c.send:
		if received.Content != msg.Content {
			t.Errorf("got %q, want %q", received.Content, msg.Content)
		}
	default:
		t.Error("message was not in send channel after safeSend returned true")
	}
}

// TestWSClient_SafeSend_ConcurrentCancelNoPanic verifies that concurrent
// safeSend calls and context cancellation are race-free. This mirrors the
// exact disconnect scenario: chat goroutines racing with wsReadPump's
// unregisterClient call. The send channel is never closed directly;
// context cancellation is the sole termination signal.
func TestWSClient_SafeSend_ConcurrentCancelNoPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	c := &wsClient{
		send:   make(chan WSMessage, 64),
		ctx:    ctx,
		cancel: cancel,
	}

	// Start 8 goroutines hammering safeSend.
	senderDone := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for {
				select {
				case <-senderDone:
					return
				default:
					c.safeSend(WSMessage{Type: "token", Content: "x"})
				}
			}
		}()
	}

	// Drain the channel concurrently (simulates writePump).
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.send:
			}
		}
	}()

	// After a brief delay, simulate client disconnect: cancel context only.
	// The channel is intentionally NOT closed — that's the fix.
	time.Sleep(5 * time.Millisecond)
	cancel()

	// Give goroutines time to detect cancellation, then shut them down.
	time.Sleep(20 * time.Millisecond)
	close(senderDone)
	<-drainDone

	// If we reach here without a panic or race, the test passed.
}
