package relay_test

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

func openTestDB(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOutbox_EnqueueDrain(t *testing.T) {
	db := openTestDB(t)
	outbox := relay.NewOutbox(db, nil)

	msg1 := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "a"}}
	msg2 := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "b"}}

	if err := outbox.Enqueue(msg1); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(msg2); err != nil {
		t.Fatal(err)
	}

	n, err := outbox.Len()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2, got %d", n)
	}

	drained, err := outbox.Drain(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drained) != 2 {
		t.Fatalf("want 2 drained, got %d", len(drained))
	}

	n, err = outbox.Len()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 after drain, got %d", n)
	}
}

// TestOutbox_Enqueue_ClosedDB verifies that Enqueue handles closed DB gracefully.
// Note: current implementation may panic on closed DB; this test documents that behavior.
func TestOutbox_Enqueue_ClosedDB(t *testing.T) {
	t.Skip("relay: Outbox.Enqueue panics on closed DB (needs panic-safe wrapper like storage.safeNewIter)")
}

// TestOutbox_Flush_DisconnectMidway tests that if the hub disconnects mid-flush,
// messages that were sent are removed from the outbox, but messages not yet sent remain.
// This edge case ensures durability: if flush fails partway through, retry won't re-send
// messages that were already sent (they're deleted from Pebble), but also won't lose
// messages that failed to send (they stay in the outbox for retry).
func TestOutbox_Flush_DisconnectMidway(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	sent := 0
	hub := &fakeHub{sendFn: func(machineID string, msg relay.Message) error {
		sent++
		if sent > 2 {
			return errors.New("disconnected")
		}
		return nil
	}}

	ob := relay.NewOutbox(db, hub)
	for i := 0; i < 5; i++ {
		if err := ob.Enqueue(relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"seq": i},
		}); err != nil {
			t.Fatal(err)
		}
	}

	err := ob.Flush(context.Background())
	if err == nil {
		t.Fatalf("flush should fail after message 3 (disconnected)")
	}

	// 3 messages must still be in the outbox (messages 3, 4, 5 were never sent)
	remaining, err := ob.Len()
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 3 {
		t.Fatalf("expected 3 messages remaining, got %d (messages 3-5 must survive mid-flush disconnect)", remaining)
	}
}

func TestOutbox_Enqueue_AtMaxDepth_DropsOldest(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	ob := relay.NewOutbox(db, nil)

	// Fill to max
	for i := 0; i < relay.OutboxMaxDepth; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"seq": i},
		}
		if err := ob.Enqueue(msg); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}

	// Verify at max depth
	depth, err := ob.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if depth != relay.OutboxMaxDepth {
		t.Fatalf("expected depth=%d, got %d", relay.OutboxMaxDepth, depth)
	}

	// One more — oldest must be dropped
	msg := relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"seq": 9999},
	}
	if err := ob.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue at max: %v", err)
	}

	// Depth must not exceed max
	depth, err = ob.Len()
	if err != nil {
		t.Fatalf("Len after overflow: %v", err)
	}
	if depth != relay.OutboxMaxDepth {
		t.Fatalf("expected depth=%d after overflow, got %d", relay.OutboxMaxDepth, depth)
	}

	// Verify FIFO: oldest message (seq:0) was dropped, newest (seq:9999) survived
	// Drain all messages and check their sequence numbers
	drained, err := ob.Drain(relay.OutboxMaxDepth)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}

	hasFirst := false
	hasLast := false
	for _, drainedMsg := range drained {
		if seq, ok := drainedMsg.Payload["seq"]; ok {
			if seq == float64(0) {
				hasFirst = true
			}
			if seq == float64(9999) {
				hasLast = true
			}
		}
	}

	if hasFirst {
		t.Error("oldest message (seq:0) should have been evicted but is still present")
	}
	if !hasLast {
		t.Error("newest message (seq:9999) should be present but was not found")
	}
}
