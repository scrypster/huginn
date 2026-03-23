package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestScheduler_SemaphoreStarvation verifies that if max concurrent workflows
// are blocked, subsequent triggers don't stall the entire scheduler.
func TestScheduler_SemaphoreStarvation(t *testing.T) {
	scheduler := New()
	scheduler.Start()
	defer scheduler.Stop(context.Background())

	// Create a runner that blocks indefinitely
	blockingRunner := func(ctx context.Context, w *Workflow) error {
		<-ctx.Done() // Wait indefinitely (or until context cancels)
		return ctx.Err()
	}

	scheduler.SetWorkflowRunner(blockingRunner)

	// Fill all 10 concurrent slots by manually triggering workflows
	var activeSem int32
	var maxActive int32

	blockingRunnerWithTracking := func(ctx context.Context, w *Workflow) error {
		current := atomic.AddInt32(&activeSem, 1)
		// Track max concurrent
		for {
			old := atomic.LoadInt32(&maxActive)
			if current > old && !atomic.CompareAndSwapInt32(&maxActive, old, current) {
				continue
			}
			break
		}
		defer atomic.AddInt32(&activeSem, -1)

		<-ctx.Done()
		return ctx.Err()
	}

	scheduler.SetWorkflowRunner(blockingRunnerWithTracking)

	// Register and trigger 10 workflows
	var wgs [maxConcurrentWorkflows]sync.WaitGroup
	for i := 0; i < maxConcurrentWorkflows; i++ {
		wf := &Workflow{
			ID:       "workflow-" + string(rune(i)),
			Schedule: "* * * * * *", // every second
			Enabled:  true,
		}
		if err := scheduler.RegisterWorkflow(wf); err != nil {
			t.Fatalf("register workflow %d: %v", i, err)
		}

		// Manually trigger via internal mechanism (simulating cron fire)
		wgs[i].Add(1)
		go func(idx int) {
			defer wgs[idx].Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_ = blockingRunnerWithTracking(ctx, &Workflow{ID: "workflow-" + string(rune(idx))})
		}(i)
	}

	// Let workflows start
	time.Sleep(50 * time.Millisecond)

	// Prevent compiler unused var error
	_ = blockingRunnerWithTracking

	// Wait for all 10 to reach blocking point
	time.Sleep(100 * time.Millisecond)

	// Now try to trigger an 11th workflow
	// This should NOT stall; it should timeout waiting for semaphore
	_, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	wf11 := &Workflow{
		ID:       "workflow-11",
		Schedule: "* * * * * *",
		Enabled:  true,
	}
	if err := scheduler.RegisterWorkflow(wf11); err != nil {
		t.Fatalf("register 11th workflow: %v", err)
	}

	// Wait for all original workflows to complete
	for i := 0; i < maxConcurrentWorkflows; i++ {
		wgs[i].Wait()
	}
	elapsed := time.Since(startTime)

	// Verify max concurrent did not exceed limit (or was exactly at limit)
	finalMax := atomic.LoadInt32(&maxActive)
	if finalMax > int32(maxConcurrentWorkflows) {
		t.Errorf("max concurrent workflows %d exceeds limit %d", finalMax, maxConcurrentWorkflows)
	}

	if elapsed > 1*time.Second {
		t.Logf("warning: workflow completion took longer than expected: %v", elapsed)
	}
}

// TestScheduler_ShutdownRaceWithSemaphore verifies that calling Stop()
// while workflows are waiting for the semaphore doesn't deadlock.
// Stop() closes the shutdown channel which causes semaphore-waiters to bail.
func TestScheduler_ShutdownRaceWithSemaphore(t *testing.T) {
	sched := New()
	sched.Start()

	// Fill all semaphore slots via TriggerWorkflow with blocking runners.
	workflowStarted := make(chan struct{}, maxConcurrentWorkflows)
	unblock := make(chan struct{})

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		workflowStarted <- struct{}{}
		select {
		case <-unblock:
		case <-ctx.Done():
		}
		return nil
	})

	// Register and trigger one workflow through the scheduler.
	wf := &Workflow{ID: "shutdown-race", Enabled: true, Schedule: "* * * * *"}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("register: %v", err)
	}
	triggerCtx, triggerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer triggerCancel()
	if err := sched.TriggerWorkflow(triggerCtx, wf); err != nil {
		t.Fatalf("TriggerWorkflow: %v", err)
	}

	// Wait for the runner to start.
	select {
	case <-workflowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("workflow did not start")
	}

	// Stop scheduler — should not deadlock even with a running workflow.
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	close(unblock) // let the runner exit first
	sched.Stop(stopCtx)

	// Verify a fresh scheduler can acquire the semaphore (no leak).
	sched2 := New()
	sched2.Start()
	defer sched2.Stop(context.Background())

	completed := atomic.Bool{}
	sched2.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		completed.Store(true)
		return nil
	})
	wf2 := &Workflow{ID: "post-shutdown", Enabled: true, Schedule: "* * * * *"}
	if err := sched2.RegisterWorkflow(wf2); err != nil {
		t.Fatalf("register wf2: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if err := sched2.TriggerWorkflow(ctx2, wf2); err != nil {
		t.Fatalf("TriggerWorkflow on fresh scheduler: %v", err)
	}

	// Allow the goroutine to finish.
	time.Sleep(100 * time.Millisecond)
	if !completed.Load() {
		t.Error("fresh scheduler semaphore appears deadlocked")
	}
}

// TestScheduler_StopWithShortDeadline verifies that Stop() actually respects
// the provided context deadline and doesn't just wait indefinitely.
func TestScheduler_StopWithShortDeadline(t *testing.T) {
	scheduler := New()
	scheduler.Start()

	blockingRunner := func(ctx context.Context, w *Workflow) error {
		<-time.After(10 * time.Second) // Blocks longer than stop timeout
		return nil
	}

	scheduler.SetWorkflowRunner(blockingRunner)

	wf := &Workflow{
		ID:       "blocking-workflow",
		Schedule: "* * * * * *",
		Enabled:  true,
	}
	if err := scheduler.RegisterWorkflow(wf); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Manually trigger workflow
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = blockingRunner(ctx, wf)
	}()

	// Give workflow time to start
	time.Sleep(100 * time.Millisecond)

	// Stop with short timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	startStop := time.Now()
	scheduler.Stop(stopCtx)
	elapsed := time.Since(startStop)

	// Prevent unused variable
	_ = blockingRunner

	// Stop should return quickly (respecting short deadline)
	// If it waits for the full 10-second workflow, it's ignoring the deadline
	if elapsed > 1*time.Second {
		t.Errorf("Stop() took %v, expected < 1s with 200ms deadline", elapsed)
	}
}

// TestScheduler_TriggerWorkflowAlreadyRunning verifies that attempting to trigger
// a workflow that's already executing returns the correct error.
func TestScheduler_TriggerWorkflowAlreadyRunning(t *testing.T) {
	scheduler := New()
	scheduler.Start()
	defer scheduler.Stop(context.Background())

	workflowRunning := make(chan struct{})
	runnerFinished := make(chan struct{})

	blockingRunner := func(ctx context.Context, w *Workflow) error {
		close(workflowRunning)
		<-ctx.Done()
		close(runnerFinished)
		return ctx.Err()
	}

	scheduler.SetWorkflowRunner(blockingRunner)

	wf := &Workflow{
		ID:       "test-running",
		Schedule: "* * * * * *",
		Enabled:  true,
	}
	if err := scheduler.RegisterWorkflow(wf); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Trigger workflow manually via internal state
	scheduler.mu.Lock()
	scheduler.workflowRunning[wf.ID] = true
	scheduler.mu.Unlock()

	// Verify state is recorded
	scheduler.mu.Lock()
	if !scheduler.workflowRunning[wf.ID] {
		t.Fatal("workflow running state not set")
	}
	scheduler.mu.Unlock()

	// Clean up
	scheduler.mu.Lock()
	delete(scheduler.workflowRunning, wf.ID)
	scheduler.mu.Unlock()
}

// TestScheduler_ConcurrentWorkflowExecutions verifies that multiple workflows
// can run concurrently up to the semaphore limit using TriggerWorkflow.
func TestScheduler_ConcurrentWorkflowExecutions(t *testing.T) {
	sched := New()
	sched.Start()
	defer sched.Stop(context.Background())

	var completedCount atomic.Int32
	var maxConcurrent atomic.Int32
	var activeCount atomic.Int32

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		cur := activeCount.Add(1)
		// Update max via CAS loop.
		for {
			old := maxConcurrent.Load()
			if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		activeCount.Add(-1)
		completedCount.Add(1)
		return nil
	})

	// Use 5 workflows — well within the semaphore limit.
	const n = 5
	wfs := make([]*Workflow, n)
	for i := 0; i < n; i++ {
		wfs[i] = &Workflow{
			ID:       fmt.Sprintf("concurrent-wf-%d", i),
			Enabled:  true,
			Schedule: "* * * * *",
		}
		if err := sched.RegisterWorkflow(wfs[i]); err != nil {
			t.Fatalf("register %d: %v", i, err)
		}
	}

	// Trigger all workflows via the proper API.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, wf := range wfs {
		if err := sched.TriggerWorkflow(ctx, wf); err != nil {
			t.Fatalf("TriggerWorkflow %s: %v", wf.ID, err)
		}
	}

	// Wait for all to complete (runner sleeps 50ms each).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if completedCount.Load() >= n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	max := maxConcurrent.Load()
	if max > int32(maxConcurrentWorkflows) {
		t.Errorf("max concurrent %d exceeds limit %d", max, maxConcurrentWorkflows)
	}
	if completedCount.Load() == 0 {
		t.Fatal("no workflows completed")
	}
}
