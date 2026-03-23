package swarm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarm_PerTaskTimeout verifies that a task with a Timeout is cancelled
// when it exceeds the deadline. The swarm treats context-cancellation errors
// as StatusCancelled (not StatusError), so the task won't appear in taskErrors.
// Instead we verify the agent's status is Cancelled and the task was interrupted.
func TestSwarm_PerTaskTimeout(t *testing.T) {
	cancelled := make(chan struct{})
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
			ID:      "slow",
			Name:    "SlowTask",
			Timeout: 50 * time.Millisecond,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-ctx.Done():
					close(cancelled)
					return ctx.Err()
				case <-time.After(5 * time.Second):
					return nil
				}
			},
		},
		{
			ID:   "fast",
			Name: "FastTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}

	_, _, _, _, _ = s.Run(context.Background(), tasks)
	<-done

	// The slow task should have been cancelled by its per-task timeout.
	select {
	case <-cancelled:
		// ok
	default:
		t.Fatal("expected slow task to be cancelled by timeout")
	}

	// Verify we got a StatusCancelled event for the slow task.
	foundCancelled := false
	for _, ev := range events {
		if ev.AgentID == "slow" && ev.Type == swarm.EventStatusChange {
			if st, ok := ev.Payload.(swarm.AgentStatus); ok && st == swarm.StatusCancelled {
				foundCancelled = true
			}
		}
	}
	if !foundCancelled {
		t.Error("expected StatusCancelled event for slow task")
	}
}

// TestSwarm_TaskErrorStructured verifies that Run returns structured TaskError
// values with the correct AgentID and AgentName.
func TestSwarm_TaskErrorStructured(t *testing.T) {
	s := swarm.NewSwarm(4)
	go func() { for range s.Events() {} }()

	sentinel := fmt.Errorf("sentinel error")
	tasks := []swarm.SwarmTask{
		{
			ID:   "fail-1",
			Name: "Failer",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return sentinel
			},
		},
		{
			ID:   "ok-1",
			Name: "OKTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}

	_, taskErrors, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected combined error")
	}
	if len(taskErrors) != 1 {
		t.Fatalf("expected 1 TaskError, got %d", len(taskErrors))
	}
	te := taskErrors[0]
	if te.AgentID != "fail-1" {
		t.Errorf("expected AgentID='fail-1', got %q", te.AgentID)
	}
	if te.AgentName != "Failer" {
		t.Errorf("expected AgentName='Failer', got %q", te.AgentName)
	}
	if !errors.Is(te.Err, sentinel) {
		t.Errorf("expected sentinel error, got %v", te.Err)
	}
	// Verify Error() string format.
	errStr := te.Error()
	if errStr == "" {
		t.Error("TaskError.Error() returned empty string")
	}
}

// TestSwarm_ZeroTimeoutNoEffect verifies that Timeout=0 means no per-task timeout.
func TestSwarm_ZeroTimeoutNoEffect(t *testing.T) {
	s := swarm.NewSwarm(1)
	go func() { for range s.Events() {} }()

	tasks := []swarm.SwarmTask{
		{
			ID:      "nodeadline",
			Name:    "NoDeadline",
			Timeout: 0,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				// Verify no deadline was set on context.
				if _, ok := ctx.Deadline(); ok {
					return fmt.Errorf("unexpected deadline on context")
				}
				return nil
			},
		},
	}

	_, taskErrors, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(taskErrors) != 0 {
		t.Errorf("expected 0 task errors, got %d", len(taskErrors))
	}
}
