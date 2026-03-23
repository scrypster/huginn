package swarm_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarm_OneAgentErrors_OthersComplete verifies that when one agent returns
// a non-context error the other agents still complete successfully and Run
// returns a non-nil error that references the failing task.
//
// Note: the swarm only collects errors from tasks whose final status is
// StatusError (i.e. tasks that returned a non-context error with a live
// context). Context-cancelled tasks get StatusCancelled and are not included.
func TestSwarm_OneAgentErrors_OthersComplete(t *testing.T) {
	t.Parallel()

	var successCount int64
	tasks := []swarm.SwarmTask{
		{
			ID:   "err-task",
			Name: "ErrorTask",
			// Return a non-context error so the task gets StatusError (not Cancelled).
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return errors.New("intentional error")
			},
		},
		{
			ID:   "ok-task-1",
			Name: "OKTask1",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				atomic.AddInt64(&successCount, 1)
				return nil
			},
		},
		{
			ID:   "ok-task-2",
			Name: "OKTask2",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				atomic.AddInt64(&successCount, 1)
				return nil
			},
		},
	}

	s := swarm.NewSwarm(3)
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); for range s.Events() {} }()
	t.Cleanup(func() { <-drainDone })

	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("Run should return error when a task returns a non-context error")
	}
	if !containsString(err.Error(), "err-task") {
		t.Errorf("error should reference the failing task, got: %v", err)
	}
	if atomic.LoadInt64(&successCount) != 2 {
		t.Errorf("expected 2 successful tasks, got %d", successCount)
	}
}

// TestSwarm_AllAgentsError verifies that when all agents return non-context
// errors, Run returns a combined (multi-error) error.
func TestSwarm_AllAgentsError(t *testing.T) {
	t.Parallel()

	const n = 3
	tasks := make([]swarm.SwarmTask, n)
	for i := 0; i < n; i++ {
		i := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("err-%d", i),
			Name: fmt.Sprintf("ErrTask%d", i),
			// Non-context errors → StatusError → collected by Run.
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return fmt.Errorf("task %d failed", i)
			},
		}
	}

	s := swarm.NewSwarm(n)
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); for range s.Events() {} }()
	t.Cleanup(func() { <-drainDone })

	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err == nil {
		t.Fatal("Run should return error when all tasks return non-context errors")
	}
}

// TestSwarm_ContextCancelStopsAllAgents verifies that cancelling the top-level
// context causes all running agents to stop promptly.
func TestSwarm_ContextCancelStopsAllAgents(t *testing.T) {
	t.Parallel()

	const n = 4
	started := make(chan struct{}, n)
	tasks := make([]swarm.SwarmTask, n)
	for i := 0; i < n; i++ {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("cancel-%d", i),
			Name: fmt.Sprintf("CancelTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(30 * time.Second):
					return nil
				}
			},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := swarm.NewSwarm(n)
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); for range s.Events() {} }()
	t.Cleanup(func() { <-drainDone })

	runDone := make(chan error, 1)
	go func() {
		_, _, _, _, err := s.Run(ctx, tasks); runDone <- err
	}()

	// Wait for tasks to start, then cancel.
	timeout := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-started:
		case <-timeout:
			t.Fatal("tasks did not start within timeout")
		}
	}
	cancel()

	select {
	case <-runDone:
		// Run returned; agents should have stopped.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}
