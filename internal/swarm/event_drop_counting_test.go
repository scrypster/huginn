package swarm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestSwarm_EventDrop_CountsDropped verifies that when the event buffer fills,
// dropped events are counted (not silently discarded) so operators can detect
// consumer lag.
//
// Bug: emit does select{case ch<-e: default:} — a full 512-buffer drops
// events silently with no log or counter. Operators cannot tell whether TUI
// updates were lost.
//
// Fix: add a DroppedEvents atomic counter; increment on every drop;
// expose via DroppedEvents() int64 accessor.
func TestSwarm_EventDrop_CountsDropped(t *testing.T) {
	t.Parallel()

	s := NewSwarm(10)

	// Fill the buffer by emitting more events than the channel capacity.
	// We emit directly via emit to avoid starting a real swarm.
	totalEmitted := 0
	for i := 0; i < 600; i++ { // 512 buffer + 88 overflow
		s.emit(SwarmEvent{Type: EventToken, Payload: "tok"})
		totalEmitted++
	}

	dropped := s.DroppedEvents()
	if dropped == 0 {
		t.Errorf("expected DroppedEvents > 0 after emitting %d events into 512-buffer channel, got 0", totalEmitted)
	}
	t.Logf("emitted %d events, dropped %d (channel capacity %d)", totalEmitted, dropped, cap(s.eventsCh))
}

// TestSwarm_EventDrop_NoDeadlock verifies that a full event buffer with a
// slow consumer does not deadlock the swarm.
func TestSwarm_EventDrop_NoDeadlock(t *testing.T) {
	t.Parallel()

	s := NewSwarm(2)

	// Slow consumer: reads one event per 50ms.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	consumed := atomic.Int64{}
	go func() {
		for {
			select {
			case _, ok := <-s.Events():
				if !ok {
					return
				}
				consumed.Add(1)
				time.Sleep(50 * time.Millisecond)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Fast producer: emit 1000 events; must not deadlock.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			s.emit(SwarmEvent{Type: EventToken, Payload: "tok"})
		}
	}()

	select {
	case <-done:
		// Good — all emissions completed without deadlock.
	case <-time.After(2 * time.Second):
		t.Error("emit deadlocked with slow consumer")
	}
}
