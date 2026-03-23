package server

// ws_hub_drain_test.go — verifies that messages enqueued in broadcastC just
// before stop() are delivered to connected clients (drain-on-shutdown fix).

import (
	"context"
	"testing"
	"time"
)

// TestWSHub_Stop_DrainsBroadcastChannel verifies that messages posted to
// broadcastC immediately before stop() are not silently dropped: the drain
// loop inside stop() must deliver them to all registered clients.
func TestWSHub_Stop_DrainsBroadcastChannel(t *testing.T) {
	h := newWSHub()
	go h.run()

	// Register a client with a large enough buffer so delivery is non-blocking.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &wsClient{
		send:   make(chan WSMessage, 64),
		ctx:    ctx,
		cancel: cancel,
	}
	h.registerWithSession(c, "sess-drain")

	// Enqueue several messages directly into broadcastC before stopping.
	// We bypass h.broadcast() so the messages are definitely in the channel
	// when stop() is called, exercising the drain path.
	const numMsgs = 5
	for i := 0; i < numMsgs; i++ {
		h.broadcastC <- WSMessage{Type: "drain-test"}
	}

	// stop() must drain the pending messages and deliver them to the client.
	h.stop()

	// Count how many drain-test messages arrived on the client's send channel.
	received := 0
	deadline := time.After(1 * time.Second)
	for {
		select {
		case msg := <-c.send:
			if msg.Type == "drain-test" {
				received++
			}
		case <-deadline:
			goto done
		}
	}
done:
	if received != numMsgs {
		t.Errorf("expected %d drained messages delivered to client, got %d", numMsgs, received)
	}
}

// TestWSHub_Stop_Idempotent_WithDrain verifies that calling stop() twice does
// not panic, even with the drain loop in the first call.
func TestWSHub_Stop_Idempotent_WithDrain(t *testing.T) {
	h := newWSHub()
	go h.run()

	h.stop() // first call: drains (empty channel → returns immediately)
	h.stop() // second call: must be a no-op, no panic
}
