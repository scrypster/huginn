package swarm

import (
	"context"
	"testing"
	"time"
)

// TestNewSwarm_ZeroMaxParallel exercises the `maxParallel <= 0 => 2` branch in NewSwarm.
func TestNewSwarm_ZeroMaxParallel(t *testing.T) {
	s := NewSwarm(0)
	if s == nil {
		t.Fatal("NewSwarm(0) returned nil")
	}
	// DroppedEvents should be 0 initially.
	if s.DroppedEvents() != 0 {
		t.Errorf("expected 0 dropped events, got %d", s.DroppedEvents())
	}
}

// TestNewSwarm_NegativeMaxParallel exercises the `maxParallel <= 0` default branch.
func TestNewSwarm_NegativeMaxParallel(t *testing.T) {
	s := NewSwarm(-5)
	if s == nil {
		t.Fatal("NewSwarm(-5) returned nil")
	}
}

// TestRunTask_ContextCancelledBeforeSemaphore exercises the ctx.Done() select
// branch in runTask when the context is already cancelled before a slot is free.
func TestRunTask_ContextCancelledBeforeSemaphore(t *testing.T) {
	// Use maxParallel=1 so semaphore fills up with a long-running task.
	s := NewSwarm(1)

	// Drain all events in background.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range s.eventsCh {
		}
	}()

	// Pre-cancel context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Fill semaphore with a blocker task that we'll never run.
	blocker := make(chan struct{})
	tasks := []SwarmTask{
		{
			ID:   "blocker",
			Name: "Blocker",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				<-blocker // block until we release
				return nil
			},
		},
		{
			ID:   "waiter",
			Name: "Waiter",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				return nil
			},
		},
	}

	// Run in background - with cancelled context, the waiter should be cancelled
	// while trying to acquire the semaphore.
	runDone := make(chan error, 1)
	go func() {
		_, _, _, _, err := s.Run(ctx, tasks); runDone <- err
	}()

	// Give the swarm a moment to start processing.
	time.Sleep(50 * time.Millisecond)

	// Release blocker.
	close(blocker)

	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Error("Run did not complete in time")
	}

	<-done
}

// TestRunTask_EmitWithEmptyAgentID exercises the `ev.AgentID == ""` fill branch
// in the inner emit closure.
func TestRunTask_EmitWithEmptyAgentID(t *testing.T) {
	s := NewSwarm(2)
	events := []SwarmEvent{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.eventsCh {
			events = append(events, ev)
		}
	}()

	tasks := []SwarmTask{
		{
			ID:   "t1",
			Name: "Task1",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				// Emit with empty AgentID and AgentName — should be filled in by the closure.
				emit(SwarmEvent{
					AgentID:   "", // will be filled to "t1"
					AgentName: "", // will be filled to "Task1"
					Type:      EventToken,
					Payload:   "hello",
				})
				return nil
			},
		},
	}

	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-done

	// Verify we got at least one token event with the agent ID filled in.
	var tokenEvent *SwarmEvent
	for i := range events {
		if events[i].Type == EventToken {
			tokenEvent = &events[i]
			break
		}
	}
	if tokenEvent == nil {
		t.Fatal("expected at least one token event")
	}
	if tokenEvent.AgentID != "t1" {
		t.Errorf("expected AgentID='t1', got %q", tokenEvent.AgentID)
	}
	if tokenEvent.AgentName != "Task1" {
		t.Errorf("expected AgentName='Task1', got %q", tokenEvent.AgentName)
	}
}

// TestRunTask_PanicRecovery exercises the recover() path in runTask when a task panics.
func TestRunTask_PanicRecovery(t *testing.T) {
	s := NewSwarm(2)
	go func() { for range s.eventsCh {} }()

	tasks := []SwarmTask{
		{
			ID:   "panicker",
			Name: "Panicker",
			Run: func(ctx context.Context, emit func(SwarmEvent)) error {
				panic("test panic")
			},
		},
	}

	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run should not propagate panic as error: %v", err)
	}

	// The agent should be in error state.
	s.mu.RLock()
	ag := s.agents["panicker"]
	s.mu.RUnlock()
	if ag == nil {
		t.Fatal("expected agent 'panicker' to be tracked")
	}
	ag.mu.Lock()
	status := ag.Status
	ag.mu.Unlock()
	if status != StatusError {
		t.Errorf("expected StatusError after panic, got %v", status)
	}
}

// TestRunTask_ContextCancelledDuringRun exercises the StatusCancelled path when
// context is cancelled while the task is running.
func TestRunTask_ContextCancelledDuringRun(t *testing.T) {
	s := NewSwarm(2)
	go func() { for range s.eventsCh {} }()

	ctx, cancel := context.WithCancel(context.Background())

	tasks := []SwarmTask{
		{
			ID:   "slow",
			Name: "Slow",
			Run: func(rctx context.Context, emit func(SwarmEvent)) error {
				cancel() // cancel while running
				<-rctx.Done()
				return rctx.Err()
			},
		},
	}

	if _, _, _, _, err := s.Run(ctx, tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}

	s.mu.RLock()
	ag := s.agents["slow"]
	s.mu.RUnlock()
	if ag == nil {
		t.Fatal("expected agent 'slow' to be tracked")
	}
	ag.mu.Lock()
	status := ag.Status
	ag.mu.Unlock()
	if status != StatusCancelled {
		t.Errorf("expected StatusCancelled, got %v", status)
	}
}
