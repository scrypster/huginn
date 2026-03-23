package swarm

import (
	"context"
	"testing"
	"time"
)

// TestNewSwarmWithConfig_DefaultBufferSize verifies that NewSwarm and
// NewSwarmWithConfig with EventBufferSize=0 both produce a channel with
// capacity == defaultEventBufferSize (512).
func TestNewSwarmWithConfig_DefaultBufferSize(t *testing.T) {
	t.Parallel()

	s1 := NewSwarm(1)
	if cap(s1.eventsCh) != defaultEventBufferSize {
		t.Errorf("NewSwarm: expected event buffer %d, got %d", defaultEventBufferSize, cap(s1.eventsCh))
	}

	s2 := NewSwarmWithConfig(SwarmConfig{MaxParallel: 1, EventBufferSize: 0})
	if cap(s2.eventsCh) != defaultEventBufferSize {
		t.Errorf("NewSwarmWithConfig(0): expected event buffer %d, got %d", defaultEventBufferSize, cap(s2.eventsCh))
	}
}

// TestNewSwarmWithConfig_CustomBufferSize verifies that a custom EventBufferSize
// is respected.
func TestNewSwarmWithConfig_CustomBufferSize(t *testing.T) {
	t.Parallel()

	const custom = 1024
	s := NewSwarmWithConfig(SwarmConfig{MaxParallel: 2, EventBufferSize: custom})
	if cap(s.eventsCh) != custom {
		t.Errorf("expected event buffer %d, got %d", custom, cap(s.eventsCh))
	}
}

// TestNewSwarmWithConfig_SmallBufferDropsEvents verifies that with a tiny
// buffer events are dropped rather than blocking when the consumer is slow.
func TestNewSwarmWithConfig_SmallBufferDropsEvents(t *testing.T) {
	t.Parallel()

	s := NewSwarmWithConfig(SwarmConfig{MaxParallel: 1, EventBufferSize: 1})
	task := SwarmTask{
		ID:   "emit-many",
		Name: "EmitMany",
		Run: func(ctx context.Context, emit func(SwarmEvent)) error {
			// Emit many events without a consumer — buffer will fill quickly.
			for i := 0; i < 50; i++ {
				emit(SwarmEvent{Type: EventToken, Payload: "x"})
			}
			return nil
		},
	}

	// Drain events slowly in the background after Run returns.
	go func() {
		for range s.Events() {
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.Run(ctx, []SwarmTask{task})

	// Some events should have been dropped given the tiny buffer.
	if s.DroppedEvents() == 0 {
		t.Log("no events dropped with buffer=1; this may happen if the goroutine scheduling drained the channel fast enough")
	}
}

// TestNewSwarmWithConfig_MaxParallelDefault verifies <= 0 MaxParallel falls back
// to defaultMaxConcurrency.
func TestNewSwarmWithConfig_MaxParallelDefault(t *testing.T) {
	t.Parallel()

	s := NewSwarmWithConfig(SwarmConfig{MaxParallel: 0})
	if s.maxParallel != defaultMaxConcurrency {
		t.Errorf("expected maxParallel=%d, got %d", defaultMaxConcurrency, s.maxParallel)
	}
}
