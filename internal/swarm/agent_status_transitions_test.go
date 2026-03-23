package swarm_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarm_AgentStatusTransitions verifies that an agent progresses through
// the expected StatusQueued → StatusThinking → StatusDone transitions.
func TestSwarm_AgentStatusTransitions(t *testing.T) {
	s := swarm.NewSwarm(2)
	var events []swarm.SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			events = append(events, ev)
		}
	}()

	tasks := []swarm.SwarmTask{
		{
			ID:   "agent1",
			Name: "Agent1",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}

	_, _, _, _, _ = s.Run(context.Background(), tasks)
	<-done

	// Collect status change events for our agent.
	var statuses []swarm.AgentStatus
	for _, ev := range events {
		if ev.AgentID == "agent1" && ev.Type == swarm.EventStatusChange {
			if st, ok := ev.Payload.(swarm.AgentStatus); ok {
				statuses = append(statuses, st)
			}
		}
	}

	// We expect at least StatusThinking and StatusDone.
	hasThinking := false
	hasDone := false
	for _, st := range statuses {
		if st == swarm.StatusThinking {
			hasThinking = true
		}
		if st == swarm.StatusDone {
			hasDone = true
		}
	}
	if !hasThinking {
		t.Error("expected StatusThinking transition event")
	}
	if !hasDone {
		t.Error("expected StatusDone transition event")
	}
}

// TestSwarm_MixedSuccessAndFailure verifies that some tasks succeed while
// others fail, and all agents reach a terminal state.
func TestSwarm_MixedSuccessAndFailure(t *testing.T) {
	const n = 5
	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		i := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("t%d", i),
			Name: fmt.Sprintf("Task%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				if i%2 == 0 {
					return fmt.Errorf("task %d failed", i)
				}
				return nil
			},
		}
	}

	s := swarm.NewSwarm(n)
	var events []swarm.SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			events = append(events, ev)
		}
	}()

	_, _, _, _, _ = s.Run(context.Background(), tasks)
	<-done

	// Verify we received both EventComplete and EventError.
	var completions, errors int
	for _, ev := range events {
		switch ev.Type {
		case swarm.EventComplete:
			completions++
		case swarm.EventError:
			errors++
		}
	}
	// 3 even-indexed tasks (0,2,4) fail; 2 odd-indexed (1,3) succeed.
	if completions < 2 {
		t.Errorf("expected >= 2 completions, got %d", completions)
	}
	if errors < 3 {
		t.Errorf("expected >= 3 errors, got %d", errors)
	}
}

// TestSwarm_EventTimestamps verifies that all emitted events have a non-zero
// At timestamp.
func TestSwarm_EventTimestamps(t *testing.T) {
	s := swarm.NewSwarm(2)
	var events []swarm.SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			events = append(events, ev)
		}
	}()

	tasks := []swarm.SwarmTask{
		{
			ID:   "ts-agent",
			Name: "TSAgent",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "tok"})
				return nil
			},
		},
	}

	_, _, _, _, _ = s.Run(context.Background(), tasks)
	<-done

	for _, ev := range events {
		if ev.At.IsZero() {
			t.Errorf("event type=%d has zero At timestamp", ev.Type)
		}
	}
}

// TestSwarm_AgentNamePropagation verifies that when a task emits an event
// without AgentName, the swarm fills it in correctly.
func TestSwarm_AgentNamePropagation(t *testing.T) {
	s := swarm.NewSwarm(2)
	var tokenEvents []swarm.SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			if ev.Type == swarm.EventToken {
				tokenEvents = append(tokenEvents, ev)
			}
		}
	}()

	tasks := []swarm.SwarmTask{
		{
			ID:   "prop-agent",
			Name: "PropAgent",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				// Emit with only Type set; AgentID and AgentName should be filled.
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "hello"})
				return nil
			},
		},
	}

	_, _, _, _, _ = s.Run(context.Background(), tasks)
	<-done

	if len(tokenEvents) == 0 {
		t.Fatal("expected at least one token event")
	}
	ev := tokenEvents[0]
	if ev.AgentID != "prop-agent" {
		t.Errorf("expected AgentID='prop-agent', got %q", ev.AgentID)
	}
	if ev.AgentName != "PropAgent" {
		t.Errorf("expected AgentName='PropAgent', got %q", ev.AgentName)
	}
}

// TestSwarm_CancelAllBeforeRun verifies that CancelAll is safe to call
// even before Run has started any agents (no agents registered yet).
func TestSwarm_CancelAllBeforeRun(t *testing.T) {
	s := swarm.NewSwarm(2)
	// Should not panic with empty agents map.
	s.CancelAll()
}

// TestSwarm_HighConcurrency stress-tests the swarm with many tasks running
// concurrently. Verifies no deadlock and correct event delivery.
func TestSwarm_HighConcurrency(t *testing.T) {
	const (
		maxParallel = 10
		totalTasks  = 50
	)

	var completed atomic.Int64
	tasks := make([]swarm.SwarmTask, totalTasks)
	for i := range tasks {
		i := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("hc-%d", i),
			Name: fmt.Sprintf("HCTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: fmt.Sprintf("tok%d", i)})
				time.Sleep(time.Millisecond)
				completed.Add(1)
				return nil
			},
		}
	}

	s := swarm.NewSwarm(maxParallel)
	go func() { for range s.Events() {} }()

	start := time.Now()
	_, _, _, _, _ = s.Run(context.Background(), tasks)

	if completed.Load() != totalTasks {
		t.Errorf("expected %d completions, got %d", totalTasks, completed.Load())
	}
	// Sanity: with maxParallel=10 and 1ms sleep, 50 tasks should complete well under 1s.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("high concurrency test took too long: %v", elapsed)
	}
}

// TestSwarm_DroppedEventsAreNonNegative verifies the counter never goes negative.
func TestSwarm_DroppedEventsNonNegative(t *testing.T) {
	s := swarm.NewSwarm(2)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:   "dr-agent",
			Name: "DrAgent",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}
	_, _, _, _, _ = s.Run(context.Background(), tasks)
	if s.DroppedEvents() < 0 {
		t.Errorf("DroppedEvents should be >= 0, got %d", s.DroppedEvents())
	}
}
