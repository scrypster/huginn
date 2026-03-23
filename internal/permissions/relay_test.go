package permissions

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestGate_RelayResponseDelivery verifies that a registered relay channel
// receives the approved value and DeliverRelayResponse returns true.
func TestGate_RelayResponseDelivery(t *testing.T) {
	g := NewGate(false, nil)
	ch := make(chan bool, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.RegisterRelayResponse(ctx, "req-1", ch)

	ok := g.DeliverRelayResponse("req-1", true)
	if !ok {
		t.Error("expected DeliverRelayResponse to return true for known requestID")
	}

	select {
	case approved := <-ch:
		if !approved {
			t.Error("expected approved=true")
		}
	default:
		t.Error("channel should have received the approved value")
	}
}

// TestGate_RelayResponseDeny verifies that a denied response is delivered correctly.
func TestGate_RelayResponseDeny(t *testing.T) {
	g := NewGate(false, nil)
	ch := make(chan bool, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.RegisterRelayResponse(ctx, "req-deny", ch)

	ok := g.DeliverRelayResponse("req-deny", false)
	if !ok {
		t.Error("expected DeliverRelayResponse to return true for known requestID")
	}

	select {
	case approved := <-ch:
		if approved {
			t.Error("expected approved=false")
		}
	default:
		t.Error("channel should have received the denied value")
	}
}

// TestGate_RelayResponseUnknownID verifies that DeliverRelayResponse returns
// false and does not panic for an unregistered requestID.
func TestGate_RelayResponseUnknownID(t *testing.T) {
	g := NewGate(false, nil)
	ok := g.DeliverRelayResponse("unknown-id", true)
	if ok {
		t.Error("expected DeliverRelayResponse to return false for unknown requestID")
	}
}

// TestGate_RelayResponseConsumedOnce verifies that a response channel is
// removed after the first delivery (no double-deliver).
func TestGate_RelayResponseConsumedOnce(t *testing.T) {
	g := NewGate(false, nil)
	ch := make(chan bool, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.RegisterRelayResponse(ctx, "req-once", ch)

	// First delivery — should succeed.
	if !g.DeliverRelayResponse("req-once", true) {
		t.Error("first DeliverRelayResponse should return true")
	}
	// Second delivery with the same ID — should return false (already consumed).
	if g.DeliverRelayResponse("req-once", true) {
		t.Error("second DeliverRelayResponse should return false (already consumed)")
	}
}

// TestGate_RelayResponseContextCancel verifies that context cancellation
// auto-denies the pending request and removes it from the map.
func TestGate_RelayResponseContextCancel(t *testing.T) {
	t.Parallel()
	g := NewGate(false, nil)
	ch := make(chan bool, 1)
	ctx, cancel := context.WithCancel(context.Background())

	g.RegisterRelayResponse(ctx, "req-cancel", ch)
	cancel() // trigger auto-deny

	select {
	case approved := <-ch:
		if approved {
			t.Error("expected false (deny) on context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for auto-deny after context cancel")
	}

	// Entry must be gone — a late DeliverRelayResponse should return false.
	if g.DeliverRelayResponse("req-cancel", true) {
		t.Error("expected false for already-cleaned-up requestID")
	}
}

// TestGate_RelayResponseTimeout verifies that a deadline-exceeded context
// causes the pending entry to be denied and cleaned up.
func TestGate_RelayResponseTimeout(t *testing.T) {
	t.Parallel()
	g := NewGate(false, nil)
	ch := make(chan bool, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	g.RegisterRelayResponse(ctx, "req-timeout", ch)

	select {
	case approved := <-ch:
		if approved {
			t.Error("expected false (deny) on timeout")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for auto-deny after deadline")
	}
}

// TestGate_RelayResponseConcurrent verifies that concurrent register/deliver
// calls do not race.
func TestGate_RelayResponseConcurrent(t *testing.T) {
	g := NewGate(false, nil)
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := 0; i < n; i++ {
		id := "req-" + string(rune('A'+i%26)) + string(rune('0'+i%10))
		ch := make(chan bool, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func(reqID string, c chan bool) {
			defer wg.Done()
			g.RegisterRelayResponse(ctx, reqID, c)
		}(id, ch)

		go func(reqID string) {
			defer wg.Done()
			// Delivery may or may not find the ID depending on scheduling;
			// either outcome is valid — we just must not race.
			g.DeliverRelayResponse(reqID, true)
		}(id)
	}

	wg.Wait()
}

// TestNewRelayRequestID verifies that generated IDs are non-empty and unique.
func TestNewRelayRequestID(t *testing.T) {
	const n = 100
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id, err := NewRelayRequestID()
		if err != nil {
			t.Fatalf("NewRelayRequestID: %v", err)
		}
		if id == "" {
			t.Fatal("NewRelayRequestID returned empty string")
		}
		if seen[id] {
			t.Fatalf("NewRelayRequestID returned duplicate: %s", id)
		}
		seen[id] = true
	}
}
