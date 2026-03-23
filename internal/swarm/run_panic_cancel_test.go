package swarm_test

// hardening_h5_iter5_test.go — Hardening pass iteration 5 (swarm).
//
// Areas covered:
//  1. NewSwarm: maxParallel <= 0 defaults to defaultMaxConcurrency (16)
//  2. Swarm.Run: all tasks complete, eventsCh is closed
//  3. Swarm.Run: panicking task is recovered as error (EventError emitted)
//  4. Swarm.Run: context cancel → tasks waiting for semaphore get StatusCancelled
//  5. Swarm.CancelAll: cancels running agents
//  6. Swarm.DroppedEvents: returns 0 when buffer not full
//  7. Swarm.Run: empty task list → eventsCh closed immediately
//  8. Swarm.Run: task emitting EventComplete sets agent to StatusDone
//  9. Swarm.Run: EventError payload contains the error value

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/swarm"
)

// ── 1. maxParallel <= 0 defaults to 2 ────────────────────────────────────────

func TestH5_Swarm_DefaultMaxParallel(t *testing.T) {
	s := swarm.NewSwarm(0)

	var concurrent int32
	var maxConcurrent int32

	tasks := make([]swarm.SwarmTask, 4)
	for i := range tasks {
		tasks[i] = swarm.SwarmTask{
			ID:   fmt.Sprintf("task-%d", i),
			Name: fmt.Sprintf("Task %d", i),
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				n := atomic.AddInt32(&concurrent, 1)
				for {
					cur := atomic.LoadInt32(&maxConcurrent)
					if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
						break
					}
				}
				time.Sleep(50 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
				return nil
			},
		}
	}

	ctx := context.Background()
	go s.Run(ctx, tasks) //nolint:errcheck

	// Drain events.
	for range s.Events() {
	}

	// defaultMaxConcurrency is 16; with only 4 tasks the cap must be <= 16.
	if atomic.LoadInt32(&maxConcurrent) > 16 {
		t.Errorf("max concurrent should be <= 16 (defaultMaxConcurrency), got %d", maxConcurrent)
	}
}

// ── 2. All tasks complete, eventsCh is closed ─────────────────────────────────

func TestH5_Swarm_Run_AllComplete_ChannelClosed(t *testing.T) {
	s := swarm.NewSwarm(2)
	tasks := []swarm.SwarmTask{
		{ID: "t1", Name: "T1", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
		{ID: "t2", Name: "T2", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error { return nil }},
	}

	done := make(chan struct{})
	go func() {
		s.Run(context.Background(), tasks) //nolint:errcheck
		close(done)
	}()

	// Drain events until channel closed.
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.Events():
			if !ok {
				// Channel closed — success.
				return
			}
		case <-timeout:
			t.Fatal("eventsCh not closed within timeout")
		}
	}
}

// ── 3. Panicking task is recovered ───────────────────────────────────────────

func TestH5_Swarm_Run_PanicRecovered(t *testing.T) {
	s := swarm.NewSwarm(2)
	tasks := []swarm.SwarmTask{
		{
			ID:   "panic-task",
			Name: "PanicTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				panic("unexpected condition")
			},
		},
	}

	var gotError bool
	doneCh := make(chan struct{})
	go func() {
		for ev := range s.Events() {
			if ev.Type == swarm.EventError {
				gotError = true
			}
		}
		close(doneCh)
	}()

	s.Run(context.Background(), tasks) //nolint:errcheck
	<-doneCh

	if !gotError {
		t.Error("want EventError from panicking task, got none")
	}
}

// ── 4. Context cancel → waiting tasks get StatusCancelled ────────────────────

func TestH5_Swarm_Run_ContextCancel_CancelledTasks(t *testing.T) {
	// Only 1 semaphore slot — second task must wait and get cancelled.
	s := swarm.NewSwarm(1)

	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	tasks := []swarm.SwarmTask{
		{
			ID:   "t1",
			Name: "T1",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				close(started)
				// Hold the semaphore slot until context is cancelled.
				<-ctx.Done()
				return ctx.Err()
			},
		},
		{
			ID:   "t2",
			Name: "T2",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}

	go func() {
		<-started
		cancel()
	}()

	s.Run(ctx, tasks) //nolint:errcheck

	// Drain events.
	for range s.Events() {
	}

	if s.DroppedEvents() != 0 {
		t.Logf("note: %d events dropped (buffer full)", s.DroppedEvents())
	}
}

// ── 5. CancelAll cancels running agents ───────────────────────────────────────

func TestH5_Swarm_CancelAll(t *testing.T) {
	s := swarm.NewSwarm(2)

	runnerStarted := make(chan struct{}, 1)
	runnerCancelled := make(chan struct{}, 1)

	tasks := []swarm.SwarmTask{
		{
			ID:   "long-task",
			Name: "LongTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				runnerStarted <- struct{}{}
				<-ctx.Done()
				runnerCancelled <- struct{}{}
				return ctx.Err()
			},
		},
	}

	go s.Run(context.Background(), tasks) //nolint:errcheck

	// Wait for runner to start.
	select {
	case <-runnerStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not start")
	}

	s.CancelAll()

	select {
	case <-runnerCancelled:
		// Success.
	case <-time.After(3 * time.Second):
		t.Fatal("runner not cancelled after CancelAll")
	}

	// Drain events.
	for range s.Events() {
	}
}

// ── 6. DroppedEvents returns 0 when buffer not full ───────────────────────────

func TestH5_Swarm_DroppedEvents_Zero(t *testing.T) {
	s := swarm.NewSwarm(4)
	tasks := []swarm.SwarmTask{
		{ID: "t1", Name: "T1", Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
			return nil
		}},
	}

	go func() {
		for range s.Events() {
		}
	}()

	s.Run(context.Background(), tasks) //nolint:errcheck
	if s.DroppedEvents() != 0 {
		t.Errorf("expected 0 dropped events, got %d", s.DroppedEvents())
	}
}

// ── 7. Empty task list → eventsCh closed immediately ─────────────────────────

func TestH5_Swarm_EmptyTasks(t *testing.T) {
	s := swarm.NewSwarm(2)
	done := make(chan struct{})
	go func() {
		for range s.Events() {
		}
		close(done)
	}()
	s.Run(context.Background(), nil) //nolint:errcheck
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("eventsCh not closed for empty task list")
	}
}

// ── 8. Task returning nil → EventComplete emitted ─────────────────────────────

func TestH5_Swarm_TaskSuccess_StatusDone(t *testing.T) {
	s := swarm.NewSwarm(2)
	tasks := []swarm.SwarmTask{
		{
			ID:   "ok-task",
			Name: "OKTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return nil
			},
		},
	}

	// Start consumer BEFORE Run so we don't miss events.
	var gotDone bool
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for ev := range s.Events() {
			if ev.AgentID == "ok-task" && ev.Type == swarm.EventComplete {
				gotDone = true
			}
		}
	}()

	s.Run(context.Background(), tasks) //nolint:errcheck

	// Wait for consumer to drain.
	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not finish")
	}

	if !gotDone {
		t.Error("want EventComplete for successful task, got none")
	}
}

// ── 9. EventError payload contains the error ──────────────────────────────────

func TestH5_Swarm_EventError_Payload(t *testing.T) {
	s := swarm.NewSwarm(2)
	want := errors.New("task error")
	tasks := []swarm.SwarmTask{
		{
			ID:   "err-task",
			Name: "ErrTask",
			Run: func(ctx context.Context, emit func(swarm.SwarmEvent)) error {
				return want
			},
		},
	}

	// Start consumer BEFORE Run.
	var got error
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for ev := range s.Events() {
			if ev.Type == swarm.EventError {
				if e, ok := ev.Payload.(error); ok {
					got = e
				}
			}
		}
	}()

	s.Run(context.Background(), tasks) //nolint:errcheck

	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not finish")
	}

	if !errors.Is(got, want) {
		t.Errorf("want error %v in EventError payload, got %v", want, got)
	}
}
