package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestScheduler_PanicRecovery_CronJob verifies that a panic inside the workflow runner
// is caught by the recover() wrapper and does not crash the scheduler goroutine.
// After the panic, subsequent cron firings must still work.
func TestScheduler_PanicRecovery_CronJob(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sched.Stop(ctx)
	}()

	var calls atomic.Int64
	fired := make(chan struct{}, 3)
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		n := calls.Add(1)
		fired <- struct{}{}
		if n == 1 {
			panic("deliberate panic in workflow runner")
		}
		return nil
	})

	wf := &Workflow{
		ID:       "panic-test",
		Name:     "Panic Test",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Wait for at least 2 firings: the first panics, the second should still work.
	timeout := time.After(3 * time.Second)
	seen := 0
	for seen < 2 {
		select {
		case <-fired:
			seen++
		case <-timeout:
			t.Fatalf("expected 2 workflow firings within 3s (panic recovery allows continuation), got %d", seen)
		}
	}
}

// TestScheduler_PanicRecovery_TriggerWorkflow verifies that a panic in a manually
// triggered workflow is recovered and the workflowRunning flag is cleaned up.
func TestScheduler_PanicRecovery_TriggerWorkflow(t *testing.T) {
	sched := New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		panic("trigger panic")
	})

	wf := &Workflow{ID: "trigger-panic", Name: "Trigger Panic", Enabled: true}
	if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("TriggerWorkflow: %v", err)
	}

	// Give the goroutine time to run and recover.
	time.Sleep(100 * time.Millisecond)

	// After recovery, workflowRunning must be cleared — a second trigger must succeed.
	if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
		t.Errorf("second TriggerWorkflow after panic recovery should succeed, got: %v", err)
	}
	// Let it settle.
	time.Sleep(50 * time.Millisecond)
}

// TestScheduler_ShutdownUnblocksSemaphore verifies that when the semaphore is
// full, TriggerWorkflow returns ErrConcurrencyLimitReached synchronously and
// Stop() completes without hanging on blocked goroutines.
func TestScheduler_ShutdownUnblocksSemaphore(t *testing.T) {
	sched := New()

	// Fill all semaphore slots with long-running workflows that block until done.
	release := make(chan struct{})
	var started atomic.Int64
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		started.Add(1)
		<-release // block until test releases them
		return nil
	})

	// Start maxConcurrentWorkflows workflows to saturate the semaphore.
	for i := range maxConcurrentWorkflows {
		wf := &Workflow{ID: "blocking-" + string(rune('a'+i)), Name: "Blocker", Enabled: true}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			t.Fatalf("TriggerWorkflow[%d]: %v", i, err)
		}
	}

	// Wait for all slots to be taken.
	deadline := time.After(2 * time.Second)
	for started.Load() < int64(maxConcurrentWorkflows) {
		select {
		case <-deadline:
			t.Fatalf("semaphore slots not all taken within 2s; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// With a full semaphore, TriggerWorkflow must now return ErrConcurrencyLimitReached
	// synchronously rather than blocking a goroutine — this prevents shutdown hangs.
	extra := &Workflow{ID: "extra", Name: "Extra", Enabled: true}
	err := sched.TriggerWorkflow(context.Background(), extra)
	if err == nil {
		t.Fatal("expected ErrConcurrencyLimitReached when semaphore is full, got nil")
	}
	if !errors.Is(err, ErrConcurrencyLimitReached) {
		t.Fatalf("expected ErrConcurrencyLimitReached, got: %v", err)
	}

	// Stop the scheduler — must return quickly because no goroutine is blocked
	// waiting on the semaphore (we returned the error synchronously above).
	stopDone := make(chan struct{})
	go func() {
		close(release) // release the running workflows
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sched.Stop(ctx)
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// passed — Stop completed without hanging
	case <-time.After(5 * time.Second):
		t.Error("Stop() took too long — semaphore likely blocked shutdown")
	}
}

// TestScheduler_AlreadyRunning_TriggerWorkflow verifies that triggering a workflow
// that is already running returns ErrWorkflowAlreadyRunning.
func TestScheduler_AlreadyRunning_TriggerWorkflow(t *testing.T) {
	sched := New()

	started := make(chan struct{})
	release := make(chan struct{})
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		close(started)
		<-release
		return nil
	})

	wf := &Workflow{ID: "concurrent", Name: "Concurrent", Enabled: true}
	if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	<-started

	err := sched.TriggerWorkflow(context.Background(), wf)
	if err == nil {
		t.Fatal("expected ErrWorkflowAlreadyRunning, got nil")
	}
	if !isAlreadyRunning(err) {
		t.Errorf("expected ErrWorkflowAlreadyRunning, got: %v", err)
	}
	close(release)
}

// TestScheduler_ConcurrentTriggers_MaxConcurrency verifies the semaphore
// bounds concurrent execution at maxConcurrentWorkflows.
func TestScheduler_ConcurrentTriggers_MaxConcurrency(t *testing.T) {
	sched := New()

	var concurrent atomic.Int64
	var maxSeen atomic.Int64
	var mu sync.Mutex
	release := make(chan struct{})

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		n := concurrent.Add(1)
		mu.Lock()
		if n > maxSeen.Load() {
			maxSeen.Store(n)
		}
		mu.Unlock()
		<-release
		concurrent.Add(-1)
		return nil
	})

	// Trigger maxConcurrentWorkflows+2 distinct workflows.
	total := maxConcurrentWorkflows + 2
	for i := range total {
		wf := &Workflow{ID: "wf-conc-" + string(rune('A'+i)), Name: "Concurrent", Enabled: true}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			// Some may fail if already running — just skip.
			continue
		}
	}

	// Give goroutines time to start and acquire semaphore slots.
	time.Sleep(100 * time.Millisecond)

	if got := maxSeen.Load(); got > int64(maxConcurrentWorkflows) {
		t.Errorf("concurrent executions exceeded max: got %d, want ≤ %d", got, maxConcurrentWorkflows)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
}

func isAlreadyRunning(err error) bool {
	return err != nil && err.Error() != "" &&
		len(err.Error()) > 0 &&
		containsStr(err.Error(), "already running")
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr2(s, sub))
}

func containsStr2(s, sub string) bool {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
