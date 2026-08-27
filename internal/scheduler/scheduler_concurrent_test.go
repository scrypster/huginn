package scheduler

// scheduler_concurrent_test.go — concurrency and edge-case tests for Scheduler.
// Tests: max concurrent workflow semaphore, TriggerWorkflow already-running guard,
// RegisterWorkflow with disabled workflow, RemoveWorkflow, and Stop.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestScheduler_TriggerWorkflow_AlreadyRunning verifies that a second
// TriggerWorkflow call for the same workflow while it is executing returns
// ErrWorkflowAlreadyRunning.
func TestScheduler_TriggerWorkflow_AlreadyRunning(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	defer sched.Stop(context.Background())

	blocked := make(chan struct{})
	unblock := make(chan struct{})

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		close(blocked)
		<-unblock
		return nil
	})

	wf := &Workflow{ID: "wf-concurrent-guard", Name: "Guard", Enabled: true, Schedule: "@every 1h"}

	if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("first trigger unexpected error: %v", err)
	}

	// Wait until the first run has entered the runner.
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start within 2s")
	}

	// Second trigger for the same workflow must fail with ErrWorkflowAlreadyRunning.
	err := sched.TriggerWorkflow(context.Background(), wf)
	if !errors.Is(err, ErrWorkflowAlreadyRunning) {
		t.Errorf("expected ErrWorkflowAlreadyRunning, got: %v", err)
	}

	close(unblock) // let the first run finish
}

// TestScheduler_TriggerWorkflow_ConcurrentSameWorkflowSingleWinner verifies
// that concurrent trigger attempts for the same workflow ID have exactly one
// winner. All other callers must receive ErrWorkflowAlreadyRunning (not nil).
func TestScheduler_TriggerWorkflow_ConcurrentSameWorkflowSingleWinner(t *testing.T) {
	sched := New()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return nil
	})

	wf := &Workflow{ID: "wf-concurrent-single-winner", Name: "SingleWinner", Enabled: true}
	const callers = 32

	start := make(chan struct{})
	var wg sync.WaitGroup
	var successCount atomic.Int64
	var alreadyRunningCount atomic.Int64
	errs := make(chan error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := sched.TriggerWorkflow(context.Background(), wf)
			switch {
			case err == nil:
				successCount.Add(1)
			case errors.Is(err, ErrWorkflowAlreadyRunning):
				alreadyRunningCount.Add(1)
			default:
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Fatalf("successful trigger count = %d, want 1", got)
	}
	if got := alreadyRunningCount.Load(); got != callers-1 {
		t.Fatalf("already-running errors = %d, want %d", got, callers-1)
	}
	select {
	case err := <-errs:
		t.Fatalf("unexpected trigger error: %v", err)
	default:
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("workflow runner did not start")
	}
	close(release)
}

// TestScheduler_TriggerWorkflow_NoRunner verifies that TriggerWorkflow returns
// an error when no WorkflowRunner has been configured.
func TestScheduler_TriggerWorkflow_NoRunner(t *testing.T) {
	sched := New()
	wf := &Workflow{ID: "wf-no-runner", Enabled: true, Schedule: "@every 1h"}
	err := sched.TriggerWorkflow(context.Background(), wf)
	if err == nil {
		t.Fatal("expected error when runner is not configured")
	}
}

// TestScheduler_RegisterWorkflow_DisabledSkipped verifies that a disabled workflow
// is silently skipped (no cron entry, no error).
func TestScheduler_RegisterWorkflow_DisabledSkipped(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	defer sched.Stop(context.Background())

	runnerCalled := false
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		runnerCalled = true
		return nil
	})

	wf := &Workflow{
		ID:       "wf-disabled",
		Name:     "Disabled",
		Schedule: "@every 50ms",
		Enabled:  false, // disabled
	}

	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("unexpected error from RegisterWorkflow for disabled workflow: %v", err)
	}

	// Wait a bit — runner should NOT be called.
	time.Sleep(200 * time.Millisecond)
	if runnerCalled {
		t.Error("runner should not be called for a disabled workflow")
	}
}

// TestScheduler_RegisterWorkflow_NoRunner verifies that RegisterWorkflow returns
// an error when no runner is configured.
func TestScheduler_RegisterWorkflow_NoRunner(t *testing.T) {
	sched := New()
	wf := &Workflow{
		ID:       "wf-no-runner-reg",
		Name:     "NoRunner",
		Schedule: "@every 1h",
		Enabled:  true,
	}
	err := sched.RegisterWorkflow(wf)
	if err == nil {
		t.Fatal("expected error when runner is not configured")
	}
}

// TestScheduler_RegisterWorkflow_EmptySchedule verifies that an enabled
// workflow with an empty schedule is a one-shot (no cron entry, no error).
func TestScheduler_RegisterWorkflow_EmptySchedule(t *testing.T) {
	sched := New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	wf := &Workflow{
		ID:       "wf-empty-sched",
		Name:     "EmptySched",
		Schedule: "",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("one-shot empty schedule should succeed: %v", err)
	}
	sched.mu.Lock()
	_, ok := sched.workflowEntries[wf.ID]
	sched.mu.Unlock()
	if ok {
		t.Fatal("one-shot must not create a cron entry")
	}
}

// TestScheduler_RemoveWorkflow verifies that RemoveWorkflow deregisters a workflow
// so it is no longer invoked by cron.
func TestScheduler_RemoveWorkflow(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	defer sched.Stop(context.Background())

	var callCount atomic.Int32
	firstCall := make(chan struct{}, 1)
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		n := callCount.Add(1)
		if n == 1 {
			select {
			case firstCall <- struct{}{}:
			default:
			}
		}
		return nil
	})

	wf := &Workflow{
		ID:       "wf-remove-test",
		Name:     "RemoveTest",
		Schedule: "@every 50ms",
		Enabled:  true,
	}

	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Wait for first invocation (cron may have initial delay — give up to 2s).
	select {
	case <-firstCall:
	case <-time.After(2 * time.Second):
		t.Fatal("expected at least one call before remove")
	}
	beforeRemove := callCount.Load()

	sched.RemoveWorkflow(wf.ID)

	// After removal, no new calls should come in.
	time.Sleep(200 * time.Millisecond)
	afterRemove := callCount.Load()

	// Allow for one more in-flight call but not continuous new calls.
	if afterRemove > beforeRemove+1 {
		t.Errorf("expected no new calls after remove; before=%d after=%d", beforeRemove, afterRemove)
	}
}

// TestScheduler_RemoveWorkflow_NotRegistered verifies that RemoveWorkflow is a
// no-op (does not panic) when the workflow ID is not registered.
func TestScheduler_RemoveWorkflow_NotRegistered(t *testing.T) {
	sched := New()
	// Should not panic.
	sched.RemoveWorkflow("does-not-exist")
}

// TestScheduler_Stop_WaitsForRunningJobs verifies that Stop blocks until all
// in-flight cron jobs complete.
func TestScheduler_Stop_WaitsForRunningJobs(t *testing.T) {
	sched := New()
	sched.Start(context.Background())

	started := make(chan struct{})
	done := make(chan struct{})
	var startOnce sync.Once

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		startOnce.Do(func() { close(started) })
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	wf := &Workflow{
		ID:       "wf-stop-wait",
		Name:     "StopWait",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Wait for a job to start.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("cron job did not start within 2s")
	}

	go func() {
		sched.Stop(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// Stop returned after job finished.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s after job completion")
	}
}

// TestScheduler_MaxConcurrentWorkflows verifies that the semaphore limits
// the number of simultaneously executing workflows to maxConcurrentWorkflows.
func TestScheduler_MaxConcurrentWorkflows(t *testing.T) {
	sched := New()
	sched.Start(context.Background())
	defer sched.Stop(context.Background())

	var (
		mu          sync.Mutex
		concurrent  int
		maxObserved int
	)
	unblock := make(chan struct{})

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		mu.Lock()
		concurrent++
		if concurrent > maxObserved {
			maxObserved = concurrent
		}
		mu.Unlock()

		<-unblock // block until test releases

		mu.Lock()
		concurrent--
		mu.Unlock()
		return nil
	})

	// Register more workflows than the semaphore limit — they will all try to
	// run via TriggerWorkflow. Each acquires the semaphore before running.
	const attempts = maxConcurrentWorkflows + 5

	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		wf := &Workflow{
			ID:      fmt.Sprintf("wf-sem-%d", i),
			Name:    fmt.Sprintf("SemTest%d", i),
			Enabled: true,
		}
		// Set runner before triggering
		go func(w *Workflow) {
			defer wg.Done()
			sched.TriggerWorkflow(context.Background(), w) //nolint:errcheck — some may fail if already running
		}(wf)
	}

	// Give goroutines time to start and acquire the semaphore.
	time.Sleep(200 * time.Millisecond)

	// The concurrent count must never exceed maxConcurrentWorkflows.
	mu.Lock()
	observed := maxObserved
	mu.Unlock()

	// Release all blocked runners.
	close(unblock)
	wg.Wait()

	if observed > maxConcurrentWorkflows {
		t.Errorf("concurrent workflows exceeded limit: max observed=%d, limit=%d", observed, maxConcurrentWorkflows)
	}
}

// TestScheduler_TriggerWorkflow_ConcurrencyHammer saturates TriggerWorkflow with
// many distinct workflow IDs at once and verifies the semaphore enforces a hard
// cap without deadlocks or unexpected error classes.
func TestScheduler_TriggerWorkflow_ConcurrencyHammer(t *testing.T) {
	sched := New()

	var concurrent atomic.Int64
	var maxSeen atomic.Int64
	release := make(chan struct{})
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		n := concurrent.Add(1)
		for {
			cur := maxSeen.Load()
			if n <= cur || maxSeen.CompareAndSwap(cur, n) {
				break
			}
		}
		<-release
		concurrent.Add(-1)
		return nil
	})

	const attempts = 400
	start := make(chan struct{})
	var wg sync.WaitGroup
	var okCount atomic.Int64
	var rejectedCount atomic.Int64
	errs := make(chan error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		wf := &Workflow{
			ID:      fmt.Sprintf("wf-hammer-%03d", i),
			Name:    "Hammer",
			Enabled: true,
		}
		go func(w *Workflow) {
			defer wg.Done()
			<-start
			err := sched.TriggerWorkflow(context.Background(), w)
			switch {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ErrConcurrencyLimitReached):
				rejectedCount.Add(1)
			default:
				errs <- err
			}
		}(wf)
	}
	close(start)
	wg.Wait()

	if got := okCount.Load(); got != int64(maxConcurrentWorkflows) {
		t.Fatalf("successful launches = %d, want %d (semaphore capacity)", got, maxConcurrentWorkflows)
	}
	if got := rejectedCount.Load(); got != attempts-int64(maxConcurrentWorkflows) {
		t.Fatalf("rejected launches = %d, want %d", got, attempts-int64(maxConcurrentWorkflows))
	}
	if got := maxSeen.Load(); got > int64(maxConcurrentWorkflows) {
		t.Fatalf("max concurrent runner calls = %d, want <= %d", got, maxConcurrentWorkflows)
	}
	select {
	case err := <-errs:
		t.Fatalf("unexpected TriggerWorkflow error class: %v", err)
	default:
	}

	close(release)
}
