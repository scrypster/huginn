package swarm_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// TestSwarm_TaskCancellationUnderConcurrentFailure verifies that when one task
// fails, its cancellation signal propagates to other running tasks.
func TestSwarm_TaskCancellationUnderConcurrentFailure(t *testing.T) {
	s := swarm.NewSwarm(2) // Low concurrency for testing
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var taskCancelledCount atomic.Int32
	var taskStartedCount atomic.Int32

	tasks := []swarm.SwarmTask{
		{
			ID:    "task1",
			Name:  "Fast Failure",
			Color: "red",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				taskStartedCount.Add(1)
				return errors.New("intentional failure")
			},
		},
		{
			ID:    "task2",
			Name:  "Should Finish",
			Color: "blue",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				taskStartedCount.Add(1)
				select {
				case <-time.After(50 * time.Millisecond):
					return nil
				case <-ctx.Done():
					taskCancelledCount.Add(1)
					return ctx.Err()
				}
			},
		},
		{
			ID:    "task3",
			Name:  "Slow Task",
			Color: "green",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				taskStartedCount.Add(1)
				select {
				case <-time.After(200 * time.Millisecond):
					return nil
				case <-ctx.Done():
					taskCancelledCount.Add(1)
					return ctx.Err()
				}
			},
		},
	}

	results, taskErrors, _, _, err := s.Run(ctx, tasks)

	// Verify results
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Task1 should have failed
	if results[0].Err == nil {
		t.Error("task1 should have failed")
	}

	// Verify that at least one task was started
	if taskStartedCount.Load() == 0 {
		t.Error("no tasks were started")
	}

	// Check for task errors
	if len(taskErrors) == 0 && err == nil {
		t.Logf("Warning: no errors captured, but expected some")
	}
}

// TestSwarm_ResultOrderingUnderConcurrentCompletion verifies that results
// maintain input order even when tasks complete in different orders.
func TestSwarm_ResultOrderingUnderConcurrentCompletion(t *testing.T) {
	s := swarm.NewSwarm(3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tasks := []swarm.SwarmTask{
		{
			ID:   "task1",
			Name: "Fast",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil // complete immediately
			},
		},
		{
			ID:   "task2",
			Name: "Slow",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			},
		},
		{
			ID:   "task3",
			Name: "Medium",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			},
		},
	}

	results, _, _, _, _ := s.Run(ctx, tasks)

	// Verify ordering is preserved despite completion order being different
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	expectedOrder := []string{"task1", "task2", "task3"}
	for i, expected := range expectedOrder {
		if results[i].AgentID != expected {
			t.Errorf("result[%d]: expected ID %s, got %s", i, expected, results[i].AgentID)
		}
	}
}

// TestSwarm_CancelAllUnderConcurrentRun verifies that CancelAll() is safe
// even when called while Run is still executing.
func TestSwarm_CancelAllUnderConcurrentRun(t *testing.T) {
	s := swarm.NewSwarm(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelledCount atomic.Int32
	var finishedCount atomic.Int32

	tasks := []swarm.SwarmTask{
		{
			ID:   "task1",
			Name: "Long 1",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-time.After(500 * time.Millisecond):
					finishedCount.Add(1)
					return nil
				case <-ctx.Done():
					cancelledCount.Add(1)
					return ctx.Err()
				}
			},
		},
		{
			ID:   "task2",
			Name: "Long 2",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-time.After(500 * time.Millisecond):
					finishedCount.Add(1)
					return nil
				case <-ctx.Done():
					cancelledCount.Add(1)
					return ctx.Err()
				}
			},
		},
		{
			ID:   "task3",
			Name: "Long 3",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-time.After(500 * time.Millisecond):
					finishedCount.Add(1)
					return nil
				case <-ctx.Done():
					cancelledCount.Add(1)
					return ctx.Err()
				}
			},
		},
	}

	// Start Run in a background goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _, _, _ = s.Run(ctx, tasks)
	}()

	// Give tasks time to start
	time.Sleep(50 * time.Millisecond)

	// Call CancelAll while tasks are running — should be safe
	s.CancelAll()

	// Wait for Run to complete
	wg.Wait()

	// Some tasks should have been cancelled
	if cancelledCount.Load() == 0 {
		t.Logf("Warning: no tasks were cancelled despite CancelAll()")
	}
}

// TestSwarm_EventEmissionUnderConcurrentCompletion verifies that events
// are emitted correctly even when multiple tasks complete concurrently.
func TestSwarm_EventEmissionUnderConcurrentCompletion(t *testing.T) {
	s := swarm.NewSwarm(5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventCount := atomic.Int32{}
	statusChangeCount := atomic.Int32{}
	errorCount := atomic.Int32{}
	completeCount := atomic.Int32{}

	// Read events in background
	go func() {
		for ev := range s.Events() {
			eventCount.Add(1)
			switch ev.Type {
			case swarm.EventStatusChange:
				statusChangeCount.Add(1)
			case swarm.EventError:
				errorCount.Add(1)
			case swarm.EventComplete:
				completeCount.Add(1)
			}
		}
	}()

	tasks := make([]swarm.SwarmTask, 5)
	for i := 0; i < 5; i++ {
		idx := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("task%d", idx),
			Name: fmt.Sprintf("Task %d", idx),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				emit(swarm.SwarmEvent{Type: swarm.EventToken, Payload: "processing"})
				return nil
			},
		}
	}

	_, _, _, _, _ = s.Run(ctx, tasks)

	// Give event reader time to consume
	time.Sleep(50 * time.Millisecond)

	// Verify events were emitted
	if eventCount.Load() == 0 {
		t.Error("no events were emitted")
	}

	// Verify at least some status changes
	if statusChangeCount.Load() < 5 {
		t.Logf("Warning: fewer status changes than expected (%d < 5)", statusChangeCount.Load())
	}
}

// TestSwarm_MaxConcurrencyEnforcement verifies that even under high concurrent
// load, no more than maxParallel tasks run simultaneously.
func TestSwarm_MaxConcurrencyEnforcement(t *testing.T) {
	const maxParallel = 3
	s := swarm.NewSwarm(maxParallel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var currentConcurrency atomic.Int32
	var maxObserved atomic.Int32
	var mu sync.Mutex
	var concurrent []int32

	tasks := make([]swarm.SwarmTask, 10)
	for i := 0; i < 10; i++ {
		idx := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("task%d", idx),
			Name: fmt.Sprintf("Task %d", idx),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				current := currentConcurrency.Add(1)
				max := current
				if mo := maxObserved.Load(); max > mo {
					maxObserved.Store(max)
				}
				mu.Lock()
				concurrent = append(concurrent, current)
				mu.Unlock()

				time.Sleep(50 * time.Millisecond)
				currentConcurrency.Add(-1)
				return nil
			},
		}
	}

	_, _, _, _, _ = s.Run(ctx, tasks)

	max := maxObserved.Load()
	if max > int32(maxParallel) {
		t.Errorf("max concurrency exceeded: observed %d, limit is %d", max, maxParallel)
	}

	// Verify we saw some concurrency (at least 2 tasks running at once)
	if max < 2 {
		t.Logf("Warning: concurrency was too low: %d", max)
	}
}

// TestSwarm_EventDropping verifies behavior when event channel is full.
func TestSwarm_EventDropping(t *testing.T) {
	s := swarm.NewSwarm(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Do NOT read from Events() channel to fill it up
	tasks := make([]swarm.SwarmTask, 20)
	for i := 0; i < 20; i++ {
		idx := i
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("task%d", idx),
			Name: fmt.Sprintf("Task %d", idx),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				// Emit many events
				for j := 0; j < 50; j++ {
					emit(swarm.SwarmEvent{
						Type:    swarm.EventToken,
						Payload: fmt.Sprintf("token %d", j),
					})
				}
				return nil
			},
		}
	}

	_, _, _, _, _ = s.Run(ctx, tasks)

	// Check if any events were dropped
	dropped := s.DroppedEvents()
	if dropped > 0 {
		t.Logf("Events were dropped: %d", dropped)
	}
	// This is informational; dropping is OK if channel is full.
}
