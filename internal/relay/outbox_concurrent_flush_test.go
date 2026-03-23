package relay_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// TestOutbox_ConcurrentFlushOrdering verifies that all messages are delivered
// when Flush is called from multiple concurrent goroutines. Ordering is not
// asserted because Flush has no cross-goroutine serialization — each goroutine
// reads and delivers the outbox independently, so interleaving is expected.
func TestOutbox_ConcurrentFlushOrdering(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var deliveredMu sync.Mutex
	delivered := make(map[int]bool)

	hub := &fakeHub{
		sendFn: func(machineID string, msg relay.Message) error {
			if seq, ok := msg.Payload["seq"]; ok {
				deliveredMu.Lock()
				delivered[int(seq.(float64))] = true
				deliveredMu.Unlock()
			}
			time.Sleep(1 * time.Millisecond)
			return nil
		},
	}

	outbox := relay.NewOutbox(db, hub)

	// Enqueue 20 messages
	for i := 0; i < 20; i++ {
		msg := relay.Message{
			Type:      relay.MsgToken,
			MachineID: "test",
			Payload:   map[string]any{"seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	// Spawn multiple goroutines calling Flush concurrently
	var wg sync.WaitGroup
	ctx := context.Background()
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = outbox.Flush(ctx)
		}()
	}
	wg.Wait()

	// Give a moment for any in-flight messages
	time.Sleep(50 * time.Millisecond)

	// All 20 unique seq values must have been delivered at least once.
	deliveredMu.Lock()
	defer deliveredMu.Unlock()

	if len(delivered) == 0 {
		t.Error("no messages were delivered despite concurrent flush attempts")
	}
	for i := 0; i < 20; i++ {
		if !delivered[i] {
			t.Errorf("seq %d was never delivered", i)
		}
	}
}

// TestOutbox_EnqueueUnderConcurrentFlush verifies that messages enqueued while
// Flush is running are either delivered in a subsequent flush or remain in the outbox.
func TestOutbox_EnqueueUnderConcurrentFlush(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	var deliveryCount atomic.Int32
	var flushing atomic.Bool

	hub := &fakeHub{
		sendFn: func(machineID string, msg relay.Message) error {
			flushing.Store(true)
			defer flushing.Store(false)
			time.Sleep(10 * time.Millisecond) // Simulate slow delivery
			deliveryCount.Add(1)
			return nil
		},
	}

	outbox := relay.NewOutbox(db, hub)

	// Enqueue initial batch
	for i := 0; i < 10; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"batch": 1, "seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("enqueue batch 1 %d: %v", i, err)
		}
	}

	// Start a flush in background
	var wg sync.WaitGroup
	ctx := context.Background()
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = outbox.Flush(ctx)
	}()

	// While flushing, enqueue more messages
	time.Sleep(5 * time.Millisecond)
	for i := 0; i < 5; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"batch": 2, "seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("enqueue batch 2 %d: %v", i, err)
		}
	}

	wg.Wait()

	// Wait for any pending deliveries
	time.Sleep(100 * time.Millisecond)

	// Flush again to get remaining messages
	if err := outbox.Flush(ctx); err != nil {
		// Some errors are expected (hub already received all)
		_ = err
	}

	// Total delivered + remaining should equal 15
	delivered := deliveryCount.Load()
	remaining, err := outbox.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	total := int32(delivered) + int32(remaining)
	if total != 15 {
		t.Errorf("expected 15 total messages (delivered+remaining), got %d (delivered=%d, remaining=%d)", total, delivered, remaining)
	}
}

// TestOutbox_FlushAndDrainRace verifies that Flush and Drain don't panic or corrupt
// the outbox when called concurrently. This is a stress test for race conditions.
func TestOutbox_FlushAndDrainRace(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	hub := &fakeHub{
		sendFn: func(machineID string, msg relay.Message) error {
			time.Sleep(1 * time.Millisecond)
			return nil
		},
	}

	outbox := relay.NewOutbox(db, hub)

	// Enqueue 30 messages
	for i := 0; i < 30; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	// Spawn goroutines calling Drain and Flush concurrently
	// The test goal is to verify no panics/corruption, not exact counts
	var wg sync.WaitGroup
	ctx := context.Background()
	var panicOccurred bool
	var panicMutex sync.Mutex

	// Flushers
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicMutex.Lock()
					panicOccurred = true
					panicMutex.Unlock()
					t.Logf("Flush panicked: %v", r)
				}
			}()
			_ = outbox.Flush(ctx)
		}()
	}

	// Draners
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicMutex.Lock()
					panicOccurred = true
					panicMutex.Unlock()
					t.Logf("Drain panicked: %v", r)
				}
			}()
			_, _ = outbox.Drain(10)
		}()
	}

	wg.Wait()

	if panicOccurred {
		t.Error("concurrent Flush/Drain caused panic")
	}

	// The main invariant: verify we can still interact with the outbox
	// without panics or corruption
	final, err := outbox.Len()
	if err != nil {
		t.Errorf("Len() failed: %v", err)
	}
	if final < 0 {
		t.Errorf("negative length: %d", final)
	}

	// Should be able to drain remaining without error
	_, err = outbox.Drain(100)
	if err != nil {
		t.Errorf("final drain failed: %v", err)
	}
}

// TestOutbox_LenUnderConcurrentModification verifies that Len() returns reasonable values
// when the outbox is being modified concurrently.
func TestOutbox_LenUnderConcurrentModification(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	outbox := relay.NewOutbox(db, nil)

	// Spawn goroutines: some enqueue, some read length
	var wg sync.WaitGroup
	var lenValues []int
	var lenMu sync.Mutex

	// Enqueuers
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				msg := relay.Message{
					Type:    relay.MsgToken,
					Payload: map[string]any{"goroutine": id, "seq": i},
				}
				if err := outbox.Enqueue(msg); err != nil {
					t.Errorf("enqueue g%d s%d: %v", id, i, err)
				}
			}
		}(g)
	}

	// Len readers
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				n, err := outbox.Len()
				if err != nil {
					t.Errorf("len: %v", err)
				}
				lenMu.Lock()
				lenValues = append(lenValues, n)
				lenMu.Unlock()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	lenMu.Lock()
	defer lenMu.Unlock()

	// Check that all reported lengths are <= 30 (total enqueued)
	for _, n := range lenValues {
		if n > 30 {
			t.Errorf("len reported %d, but max possible is 30", n)
		}
		if n < 0 {
			t.Errorf("len reported negative: %d", n)
		}
	}
}

// TestOutbox_EnqueueFlushDrainInterleavedSequence tests a realistic scenario:
// messages are enqueued, flushed, drained, and re-enqueued in an interleaved fashion.
func TestOutbox_EnqueueFlushDrainInterleavedSequence(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	hub := &fakeHub{
		sendFn: func(machineID string, msg relay.Message) error {
			return nil
		},
	}

	outbox := relay.NewOutbox(db, hub)

	// Phase 1: Enqueue 20, flush
	for i := 0; i < 20; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"phase": 1, "seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("phase 1 enqueue: %v", err)
		}
	}
	if err := outbox.Flush(context.Background()); err != nil {
		t.Fatalf("phase 1 flush: %v", err)
	}

	// Verify empty
	if n, _ := outbox.Len(); n != 0 {
		t.Errorf("after phase 1 flush, expected 0, got %d", n)
	}

	// Phase 2: Enqueue 10, drain 5
	for i := 0; i < 10; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"phase": 2, "seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("phase 2 enqueue: %v", err)
		}
	}
	drained, err := outbox.Drain(5)
	if err != nil {
		t.Fatalf("phase 2 drain: %v", err)
	}
	if len(drained) != 5 {
		t.Errorf("phase 2 drain: expected 5, got %d", len(drained))
	}

	// Verify 5 remaining
	if n, _ := outbox.Len(); n != 5 {
		t.Errorf("after phase 2 drain, expected 5, got %d", n)
	}

	// Phase 3: Enqueue 15 more, flush remaining 20
	for i := 0; i < 15; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"phase": 3, "seq": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("phase 3 enqueue: %v", err)
		}
	}
	if err := outbox.Flush(context.Background()); err != nil {
		t.Fatalf("phase 3 flush: %v", err)
	}

	// Verify empty
	if n, _ := outbox.Len(); n != 0 {
		t.Errorf("after phase 3 flush, expected 0, got %d", n)
	}
}

// Note: fakeHub is defined in relay_test.go, so we don't redefine it here.
