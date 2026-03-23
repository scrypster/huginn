package relay_test

// outbox_extended_test.go — additional Outbox tests.
// Covers: concurrent enqueue, flush with nil hub, flush cancelled context,
// Drain with limit, Len on empty outbox, and token type roundtrip.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// capturingHub collects sent messages for assertions in outbox_extended tests.
// (Cannot reuse fakeHub from relay_test.go because that type lacks a Sent() method.)
type capturingHub struct {
	mu     sync.Mutex
	sent   []relay.Message
	sendFn func(machineID string, msg relay.Message) error
}

func (h *capturingHub) Send(machineID string, msg relay.Message) error {
	if h.sendFn != nil {
		return h.sendFn(machineID, msg)
	}
	h.mu.Lock()
	h.sent = append(h.sent, msg)
	h.mu.Unlock()
	return nil
}

func (h *capturingHub) Close(_ string) {}

func (h *capturingHub) Sent() []relay.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]relay.Message, len(h.sent))
	copy(out, h.sent)
	return out
}

func openExtTestDB(t *testing.T) *storage.Store {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestOutbox_ConcurrentEnqueue verifies that concurrent Enqueue calls do not
// race and that all messages are stored (up to the queue capacity).
func TestOutbox_ConcurrentEnqueue(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = ob.Enqueue(relay.Message{
				Type:    relay.MsgToken,
				Payload: map[string]any{"seq": idx},
			})
		}(i)
	}
	wg.Wait()

	// All enqueues must succeed (queue is nowhere near full).
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d enqueue error: %v", i, err)
		}
	}

	n, err := ob.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != goroutines {
		t.Errorf("expected %d messages, got %d", goroutines, n)
	}
}

// TestOutbox_Flush_NilHub returns an error when the hub is nil.
func TestOutbox_Flush_NilHub(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	if err := ob.Enqueue(relay.Message{Type: relay.MsgDone}); err != nil {
		t.Fatal(err)
	}

	err := ob.Flush(context.Background())
	if err == nil {
		t.Fatal("expected error from Flush with nil hub, got nil")
	}
}

// TestOutbox_Flush_CancelledContext verifies that a cancelled context causes
// Flush to return a context error rather than sending messages.
func TestOutbox_Flush_CancelledContext(t *testing.T) {
	db := openExtTestDB(t)
	hub := &capturingHub{}
	ob := relay.NewOutbox(db, hub)

	for i := 0; i < 5; i++ {
		if err := ob.Enqueue(relay.Message{Type: relay.MsgToken, Payload: map[string]any{"i": i}}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := ob.Flush(ctx)
	if err == nil {
		t.Fatal("expected error on pre-cancelled Flush context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

// TestOutbox_Drain_WithLimit verifies that Drain(n) returns at most n messages
// and leaves the rest in the queue.
func TestOutbox_Drain_WithLimit(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	for i := 0; i < 10; i++ {
		if err := ob.Enqueue(relay.Message{Type: relay.MsgToken, Payload: map[string]any{"seq": i}}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	drained, err := ob.Drain(3)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != 3 {
		t.Errorf("expected 3 drained, got %d", len(drained))
	}

	remaining, err := ob.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if remaining != 7 {
		t.Errorf("expected 7 remaining, got %d", remaining)
	}
}

// TestOutbox_Drain_Empty verifies that Drain on an empty outbox returns an empty
// slice and no error.
func TestOutbox_Drain_Empty(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	drained, err := ob.Drain(10)
	if err != nil {
		t.Fatalf("Drain on empty: %v", err)
	}
	if len(drained) != 0 {
		t.Errorf("expected 0 messages from empty outbox, got %d", len(drained))
	}
}

// TestOutbox_Len_Empty verifies that Len on a fresh outbox returns 0.
func TestOutbox_Len_Empty(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	n, err := ob.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 for empty outbox, got %d", n)
	}
}

// TestOutbox_Flush_SendsAllMessages verifies that a successful Flush sends all
// queued messages to the hub and empties the outbox.
func TestOutbox_Flush_SendsAllMessages(t *testing.T) {
	db := openExtTestDB(t)
	hub := &capturingHub{}
	ob := relay.NewOutbox(db, hub)

	for i := 0; i < 5; i++ {
		if err := ob.Enqueue(relay.Message{Type: relay.MsgToken, Payload: map[string]any{"i": i}}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	if err := ob.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if len(hub.Sent()) != 5 {
		t.Errorf("expected 5 messages sent, got %d", len(hub.Sent()))
	}

	n, err := ob.Len()
	if err != nil {
		t.Fatalf("Len after flush: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 messages remaining after flush, got %d", n)
	}
}

// TestOutbox_FIFOOrdering verifies that Drain returns messages in insertion order.
func TestOutbox_FIFOOrdering(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	types := []relay.MessageType{relay.MsgToken, relay.MsgDone, relay.MsgWarning}
	for _, mt := range types {
		if err := ob.Enqueue(relay.Message{Type: mt}); err != nil {
			t.Fatalf("Enqueue %s: %v", mt, err)
		}
	}

	drained, err := ob.Drain(len(types))
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(drained) != len(types) {
		t.Fatalf("expected %d messages, got %d", len(types), len(drained))
	}
	for i, msg := range drained {
		if msg.Type != types[i] {
			t.Errorf("position %d: expected type %q, got %q", i, types[i], msg.Type)
		}
	}
}

// TestOutbox_EnqueueAfterDrain verifies that after draining, new enqueues
// work correctly (sequence counter does not break after drain).
func TestOutbox_EnqueueAfterDrain(t *testing.T) {
	db := openExtTestDB(t)
	ob := relay.NewOutbox(db, nil)

	if err := ob.Enqueue(relay.Message{Type: relay.MsgToken}); err != nil {
		t.Fatal(err)
	}
	if _, err := ob.Drain(10); err != nil {
		t.Fatal(err)
	}

	// Enqueue new message after drain
	if err := ob.Enqueue(relay.Message{Type: relay.MsgDone}); err != nil {
		t.Fatalf("Enqueue after drain: %v", err)
	}

	n, err := ob.Len()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 message after drain+enqueue, got %d", n)
	}
}
