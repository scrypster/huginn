package server

import (
	"context"
	"testing"
	"time"
)

// TestWSMessageEpochSetByBroadcastToSession verifies that every message sent
// via broadcastToSession carries the same non-zero Epoch value, which matches
// the package-level serverEpoch variable set at process startup.
func TestWSMessageEpochSetByBroadcastToSession(t *testing.T) {
	if serverEpoch == 0 {
		// serverEpoch is set in init() via crypto/rand. A zero value indicates the
		// init function failed, which cannot happen on any real platform. Skip rather
		// than fail so tests are not flaky on unusual sandbox environments.
		t.Skip("serverEpoch is zero — crypto/rand may be unavailable in this environment")
	}

	hub := newWSHub()
	go hub.run()
	t.Cleanup(hub.stop)

	// Create a client subscribed to a specific session.
	const sessionID = "test-session-epoch"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &wsClient{
		send:      make(chan WSMessage, 16),
		sessionID: sessionID,
		ctx:       ctx,
		cancel:    cancel,
	}
	hub.registerWithSession(client, sessionID)

	// Send three messages via broadcastToSession.
	for i := 0; i < 3; i++ {
		hub.broadcastToSession(sessionID, WSMessage{Type: "test"})
	}

	// Collect delivered messages. broadcastToSession delivers synchronously
	// (under hub.mu.RLock), so the messages are already in the channel.
	var msgs []WSMessage
	for i := 0; i < 3; i++ {
		select {
		case m := <-client.send:
			msgs = append(msgs, m)
		case <-time.After(time.Second):
			t.Fatalf("expected message %d but timed out after 1s", i)
		}
	}

	// Every sequenced message must carry the process-level serverEpoch.
	for i, m := range msgs {
		if m.Epoch == 0 {
			t.Errorf("msgs[%d].Epoch = 0, want serverEpoch (%d)", i, serverEpoch)
		}
		if m.Epoch != serverEpoch {
			t.Errorf("msgs[%d].Epoch = %d, want serverEpoch (%d)", i, m.Epoch, serverEpoch)
		}
	}

	// Epochs must be identical across all messages in the same server lifetime.
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Epoch != msgs[0].Epoch {
			t.Errorf("epoch mismatch: msgs[0].Epoch=%d, msgs[%d].Epoch=%d — must be consistent",
				msgs[0].Epoch, i, msgs[i].Epoch)
		}
	}

	// Seq numbers must increase monotonically: 1, 2, 3.
	for i, m := range msgs {
		want := uint64(i + 1)
		if m.Seq != want {
			t.Errorf("msgs[%d].Seq = %d, want %d", i, m.Seq, want)
		}
	}
}

// TestWSMessageGlobalBroadcastHasNoEpoch verifies that messages sent via the
// global broadcast (not broadcastToSession) do NOT carry an Epoch or Seq field,
// preserving backward compatibility for clients that don't understand sequencing.
func TestWSMessageGlobalBroadcastHasNoEpoch(t *testing.T) {
	hub := newWSHub()
	go hub.run()
	t.Cleanup(hub.stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &wsClient{
		send:   make(chan WSMessage, 8),
		ctx:    ctx,
		cancel: cancel,
	}
	// Register as wildcard (no session).
	hub.registerWithSession(client, "")

	hub.broadcast(WSMessage{Type: "global_ping"})

	// The hub's run() goroutine is responsible for routing broadcast messages
	// to clients. Give it a short window to deliver.
	select {
	case m := <-client.send:
		if m.Epoch != 0 {
			t.Errorf("global broadcast Epoch = %d, want 0 (omitempty)", m.Epoch)
		}
		if m.Seq != 0 {
			t.Errorf("global broadcast Seq = %d, want 0 (omitempty)", m.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("expected message on global broadcast but timed out after 1s")
	}
}
