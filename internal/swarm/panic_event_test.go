package swarm

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSwarm_PanicEmitsEventAgentPanic verifies that when a task goroutine
// panics the swarm recovers and emits an EventAgentPanic event whose payload
// contains both the panic value and a stack trace.
func TestSwarm_PanicEmitsEventAgentPanic(t *testing.T) {
	t.Parallel()

	s := NewSwarmWithConfig(SwarmConfig{MaxParallel: 1, EventBufferSize: 64})

	task := SwarmTask{
		ID:   "panic-task",
		Name: "Panicker",
		Run: func(ctx context.Context, emit func(SwarmEvent)) error {
			panic("intentional test panic")
		},
	}

	// Collect all events in the background before Run closes the channel.
	var panicEvents []SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			if ev.Type == EventAgentPanic {
				panicEvents = append(panicEvents, ev)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Run(ctx, []SwarmTask{task})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event consumer to finish")
	}

	if len(panicEvents) == 0 {
		t.Fatal("expected at least one EventAgentPanic, got none")
	}

	ev := panicEvents[0]
	if ev.AgentID != "panic-task" {
		t.Errorf("expected AgentID=%q, got %q", "panic-task", ev.AgentID)
	}
	if ev.AgentName != "Panicker" {
		t.Errorf("expected AgentName=%q, got %q", "Panicker", ev.AgentName)
	}

	payload, ok := ev.Payload.(string)
	if !ok {
		t.Fatalf("expected Payload to be string, got %T", ev.Payload)
	}
	if !strings.Contains(payload, "intentional test panic") {
		t.Errorf("panic payload missing panic value; got: %q", payload)
	}
	// The payload should contain a stack trace reference.
	if !strings.Contains(payload, "goroutine") && !strings.Contains(payload, "swarm") {
		t.Errorf("panic payload appears to be missing stack trace; got: %q", payload)
	}
}

// TestSwarm_PanicTaskRecoveredSwarmContinues verifies that after a panicking
// task is recovered, other tasks in the same swarm still complete successfully.
func TestSwarm_PanicTaskRecoveredSwarmContinues(t *testing.T) {
	t.Parallel()

	s := NewSwarm(2)

	tasks := []SwarmTask{
		{
			ID:   "panicker",
			Name: "Panicker",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				panic("test panic")
			},
		},
		{
			ID:   "healthy",
			Name: "Healthy",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				emit(SwarmEvent{Type: EventToken, Payload: "ok"})
				return nil
			},
		},
	}

	// Drain events so the buffer doesn't fill.
	go func() {
		for range s.Events() {
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	results, _, _, _, _ := s.Run(ctx, tasks)

	// Healthy task should have completed.
	var foundHealthy bool
	for _, r := range results {
		if r.AgentID == "healthy" && r.Err == nil {
			foundHealthy = true
		}
	}
	if !foundHealthy {
		t.Error("expected healthy task to complete successfully alongside the panicking task")
	}
}

// TestEventAgentPanic_ConstantValue ensures EventAgentPanic is distinct from
// other EventType values (not accidentally equal to 0, 1, etc.).
func TestEventAgentPanic_ConstantValue(t *testing.T) {
	t.Parallel()

	others := []EventType{
		EventToken, EventToolStart, EventToolDone,
		EventStatusChange, EventComplete, EventError, EventSwarmReady,
	}
	for _, ev := range others {
		if ev == EventAgentPanic {
			t.Errorf("EventAgentPanic (%d) collides with another EventType (%d)", EventAgentPanic, ev)
		}
	}
}
