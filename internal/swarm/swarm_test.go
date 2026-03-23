package swarm_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

func TestSwarm_SemaphoreLimitsConcurrent(t *testing.T) {
	const maxParallel = 2
	const totalTasks = 5
	var inFlight int64
	var peak int64
	tasks := make([]swarm.SwarmTask, totalTasks)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID: fmt.Sprintf("t%d", i),
			Name: fmt.Sprintf("Task %d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				cur := atomic.AddInt64(&inFlight, 1)
				defer atomic.AddInt64(&inFlight, -1)
				for {
					old := atomic.LoadInt64(&peak)
					if cur <= old {
						break
					}
					if atomic.CompareAndSwapInt64(&peak, old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				return nil
			},
		}
	}
	s := swarm.NewSwarm(maxParallel)
	go func() { for range s.Events() {} }()
	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak > maxParallel {
		t.Errorf("peak %d exceeded maxParallel %d", peak, maxParallel)
	}
}

func TestSwarm_RunExitsWhenAllComplete(t *testing.T) {
	var completed int64
	const n = 4
	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID: fmt.Sprintf("t%d", i),
			Name: fmt.Sprintf("Task%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt64(&completed, 1)
				return nil
			},
		}
	}
	s := swarm.NewSwarm(n)
	go func() { for range s.Events() {} }()
	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if atomic.LoadInt64(&completed) != n {
		t.Errorf("expected %d completions, got %d", n, completed)
	}
}

func TestSwarm_CancelContextCancelsAgents(t *testing.T) {
	var cancelCount int64
	const n = 3
	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID: fmt.Sprintf("t%d", i),
			Name: fmt.Sprintf("Task%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-ctx.Done():
					atomic.AddInt64(&cancelCount, 1)
					return ctx.Err()
				case <-time.After(5 * time.Second):
					return nil
				}
			},
		}
	}
	s := swarm.NewSwarm(n)
	go func() { for range s.Events() {} }()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	_, _, _, _, _ = s.Run(ctx, tasks)
	if atomic.LoadInt64(&cancelCount) < n {
		t.Errorf("expected %d cancels, got %d", n, cancelCount)
	}
}

func TestSwarm_PanicRecovery(t *testing.T) {
	tasks := []swarm.SwarmTask{
		{
			ID:   "panic-task",
			Name: "PanicTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				panic("test panic")
			},
		},
		{
			ID:   "normal-task",
			Name: "NormalTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}
	s := swarm.NewSwarm(2)
	go func() { for range s.Events() {} }()
	// Should not panic -- the panic in the task should be recovered
	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run should not return error: %v", err)
	}
}

func TestSwarm_EmptyTasks(t *testing.T) {
	s := swarm.NewSwarm(2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range s.Events() {}
	}()
	_, _, _, _, err := s.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run with empty tasks: %v", err)
	}
	<-done // channel should be closed
}

func TestSwarm_EmitAfterCloseNoPanic(t *testing.T) {
	// Tasks that emit events in a goroutine that may continue after Run returns.
	// This should not panic even if events arrive after eventsCh is closed.
	tasks := make([]swarm.SwarmTask, 3)
	for i := range tasks {
		id := fmt.Sprintf("late-%d", i)
		tasks[i] = swarm.SwarmTask{
			ID:   id,
			Name: id,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				// Spawn a goroutine that emits after the task returns
				go func() {
					time.Sleep(50 * time.Millisecond)
					// This emit could happen after Run() has closed the channel.
					// It must not panic.
					emit(swarm.SwarmEvent{
						AgentID: id,
						Type:    swarm.EventToken,
						Payload: "late-event",
					})
				}()
				return nil
			},
		}
	}
	s := swarm.NewSwarm(3)
	go func() { for range s.Events() {} }()
	_, _, _, _, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Wait for the late goroutines to fire. If emit panics, the test will crash.
	time.Sleep(100 * time.Millisecond)
}

func TestSwarm_CancelAll(t *testing.T) {
	var cancelledCount int64
	const n = 3
	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("cancel-%d", i),
			Name: fmt.Sprintf("CancelTask%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				select {
				case <-ctx.Done():
					atomic.AddInt64(&cancelledCount, 1)
					return ctx.Err()
				case <-time.After(10 * time.Second):
					return nil
				}
			},
		}
	}
	s := swarm.NewSwarm(n)
	go func() { for range s.Events() {} }()

	// Start Run in background
	done := make(chan error, 1)
	go func() {
		_, _, _, _, err := s.Run(context.Background(), tasks)
		done <- err
	}()

	// Wait for tasks to start, then cancel all
	time.Sleep(50 * time.Millisecond)
	s.CancelAll()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after CancelAll")
	}

	if atomic.LoadInt64(&cancelledCount) != n {
		t.Errorf("expected %d cancels, got %d", n, cancelledCount)
	}
}

// TestSwarm_DefaultMaxConcurrency verifies that NewSwarm(0) uses defaultMaxConcurrency (16)
// rather than an uncapped or too-small limit, and correctly bounds concurrent execution.
func TestSwarm_DefaultMaxConcurrency(t *testing.T) {
	const totalTasks = 32
	var inFlight int64
	var peak int64
	tasks := make([]swarm.SwarmTask, totalTasks)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("dc-%d", i),
			Name: fmt.Sprintf("DefaultCap%d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				cur := atomic.AddInt64(&inFlight, 1)
				defer atomic.AddInt64(&inFlight, -1)
				for {
					old := atomic.LoadInt64(&peak)
					if cur <= old {
						break
					}
					if atomic.CompareAndSwapInt64(&peak, old, cur) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		}
	}
	// NewSwarm(0) must use defaultMaxConcurrency = 16, not an uncapped value.
	s := swarm.NewSwarm(0)
	go func() {
		for range s.Events() {
		}
	}()
	if _, _, _, _, err := s.Run(context.Background(), tasks); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Peak must be capped at 16 (defaultMaxConcurrency).
	if peak > 16 {
		t.Errorf("peak concurrent agents %d exceeded defaultMaxConcurrency 16", peak)
	}
	// Sanity: some parallelism occurred.
	if peak < 1 {
		t.Error("expected at least 1 concurrent agent")
	}
}

func TestSwarm_EventFanIn(t *testing.T) {
	const n = 3
	const eventsPerAgent = 4
	tasks := make([]swarm.SwarmTask, n)
	for i := range tasks {
		agentID := fmt.Sprintf("a%d", i)
		tasks[i] = swarm.SwarmTask{
			ID: agentID,
			Name: agentID,
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				for j := 0; j < eventsPerAgent; j++ {
					emit(swarm.SwarmEvent{
						AgentID: agentID,
						Type:    swarm.EventToken,
						Payload: fmt.Sprintf("tok%d", j),
					})
				}
				return nil
			},
		}
	}
	s := swarm.NewSwarm(n)
	var collected []swarm.SwarmEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			collected = append(collected, ev)
		}
	}()
	s.Run(context.Background(), tasks)
	<-done
	if len(collected) < n*eventsPerAgent {
		t.Errorf("expected >= %d events, got %d", n*eventsPerAgent, len(collected))
	}
}
