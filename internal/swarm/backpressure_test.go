package swarm_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// containsString is a small helper used across swarm tests.
func containsString(s, sub string) bool {
	return strings.Contains(s, sub)
}

// TestSwarm_SlowConsumerDoesNotBlockAgents verifies that a slow (or absent)
// event consumer doesn't prevent agents from executing and completing.
// The event channel has a fixed buffer (512); when it fills, events are dropped
// (counted in DroppedEvents) rather than blocking agent goroutines.
func TestSwarm_SlowConsumerDoesNotBlockAgents(t *testing.T) {
	t.Parallel()

	const n = 10
	const eventsPerAgent = 100 // each agent emits many events to saturate the buffer
	var completedCount int64

	tasks := make([]swarm.SwarmTask, n)
	for i := 0; i < n; i++ {
		i := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("flood-%d", i),
			Name: fmt.Sprintf("FloodTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				for j := 0; j < eventsPerAgent; j++ {
					emit(swarm.SwarmEvent{
						Type:    swarm.EventToken,
						Payload: fmt.Sprintf("tok-%d-%d", i, j),
					})
				}
				atomic.AddInt64(&completedCount, 1)
				return nil
			},
		}
	}

	s := swarm.NewSwarm(n)

	// Consumer reads at a throttled pace — simulates a slow TUI renderer.
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for range s.Events() {
			// Slow consumer: introduce small delay per event.
			time.Sleep(100 * time.Microsecond)
		}
	}()

	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-consumerDone

	if atomic.LoadInt64(&completedCount) != n {
		t.Errorf("expected %d agents to complete, got %d", n, completedCount)
	}
	// Dropped events are acceptable — they indicate backpressure was handled
	// gracefully (by dropping) rather than by blocking.
	t.Logf("dropped events: %d", s.DroppedEvents())
}

// TestSwarm_NoGoroutineLeak verifies that after Run returns, the event channel
// is closed and all goroutines spawned by the swarm have exited.
// We drain the channel in a separate goroutine and verify it terminates.
func TestSwarm_NoGoroutineLeak(t *testing.T) {
	t.Parallel()

	const n = 5
	tasks := make([]swarm.SwarmTask, n)
	for i := 0; i < n; i++ {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("leak-%d", i),
			Name: fmt.Sprintf("LeakTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "tok"})
				return nil
			},
		}
	}

	s := swarm.NewSwarm(n)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for range s.Events() {}
	}()

	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// After Run returns, the event channel must be closed.
	// The drain goroutine should therefore exit promptly.
	select {
	case <-drainDone:
		// channel was closed and consumer goroutine exited
	case <-time.After(3 * time.Second):
		t.Fatal("event channel was not closed after Run returned (goroutine leak)")
	}
}

// TestSwarm_EventChannelClosedAfterCancel verifies that even when the context
// is cancelled, the event channel is still closed after Run returns so that
// consumer goroutines can exit.
func TestSwarm_EventChannelClosedAfterCancel(t *testing.T) {
	t.Parallel()

	const n = 3
	tasks := make([]swarm.SwarmTask, n)
	for i := 0; i < n; i++ {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("cclose-%d", i),
			Name: fmt.Sprintf("CCloseTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					return nil
				}
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := swarm.NewSwarm(n)

	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for range s.Events() {}
	}()

	runDone := make(chan error, 1)
	go func() { _, _, _, _, err := s.Run(ctx, tasks); runDone <- err }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-runDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}

	// Event channel must be closed after Run exits.
	select {
	case <-drainDone:
	case <-time.After(3 * time.Second):
		t.Fatal("event channel not closed after cancelled Run")
	}
}
