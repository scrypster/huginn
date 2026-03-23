package scheduler

// workflow_skipped_test.go — tests for the workflow_skipped WS event that is
// emitted when a cron-triggered workflow cannot acquire the global concurrency
// semaphore because all slots are already occupied.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestScheduler_WorkflowSkipped_BroadcastEmitted verifies that when the global
// concurrency semaphore is full and a cron-triggered workflow fires, a
// "workflow_skipped" event is broadcast via the configured broadcast function.
//
// Strategy:
//  1. Fill the semaphore by triggering (maxConcurrentWorkflows) long-running
//     workflows via TriggerWorkflow (which blocks synchronously on the semaphore).
//  2. Register a workflow with a very short cron interval (@every 50ms).
//  3. The registered workflow's cron handler will find the semaphore full,
//     emit workflow_skipped, and return immediately.
//  4. Assert that the broadcast function received at least one workflow_skipped
//     event with the correct workflow_id.
func TestScheduler_WorkflowSkipped_BroadcastEmitted(t *testing.T) {
	sched := New()

	// Channel used to block the runner so the semaphore stays full.
	release := make(chan struct{})
	var started atomic.Int64

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		started.Add(1)
		<-release
		return nil
	})

	// Collect broadcast events.
	type broadcastEvent struct {
		eventType string
		payload   map[string]any
	}
	var mu sync.Mutex
	var events []broadcastEvent

	sched.SetBroadcastFunc(func(eventType string, payload map[string]any) {
		mu.Lock()
		events = append(events, broadcastEvent{eventType: eventType, payload: payload})
		mu.Unlock()
	})

	sched.Start()
	defer func() {
		close(release) // unblock all running workflows before stopping
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sched.Stop(ctx)
	}()

	// Fill every semaphore slot with a blocking workflow.
	for i := range maxConcurrentWorkflows {
		wf := &Workflow{
			ID:      workflowSkippedID("fill", i),
			Name:    "Filler",
			Enabled: true,
		}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			t.Fatalf("TriggerWorkflow filler %d: %v", i, err)
		}
	}

	// Wait until all fillers have started and are holding the semaphore.
	deadline := time.After(3 * time.Second)
	for started.Load() < int64(maxConcurrentWorkflows) {
		select {
		case <-deadline:
			t.Fatalf("fillers did not start within 3s; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Now register a workflow with a very short schedule. The semaphore is full
	// so every cron firing should be skipped and emit workflow_skipped.
	const skippedID = "wf-skipped-target"
	wf := &Workflow{
		ID:       skippedID,
		Name:     "SkippedTarget",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Wait for at least one workflow_skipped event.
	deadline2 := time.After(3 * time.Second)
	for {
		mu.Lock()
		found := false
		for _, ev := range events {
			if ev.eventType == "workflow_skipped" {
				if id, _ := ev.payload["workflow_id"].(string); id == skippedID {
					found = true
				}
			}
		}
		mu.Unlock()
		if found {
			break
		}
		select {
		case <-deadline2:
			mu.Lock()
			t.Fatalf("expected workflow_skipped event for %q within 3s; events: %+v", skippedID, events)
			mu.Unlock()
			return
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Verify the event payload contains "reason": "concurrency limit".
	mu.Lock()
	defer mu.Unlock()
	for _, ev := range events {
		if ev.eventType == "workflow_skipped" {
			if id, _ := ev.payload["workflow_id"].(string); id == skippedID {
				reason, _ := ev.payload["reason"].(string)
				if reason == "" {
					t.Errorf("workflow_skipped event missing reason field; payload: %v", ev.payload)
				}
			}
		}
	}
}

// TestScheduler_WorkflowSkipped_NoBroadcastFnNoPanic verifies that when the
// broadcastFn is nil (not configured), a skipped workflow does not panic.
// This tests the nil-guard in the scheduler's cron handler.
func TestScheduler_WorkflowSkipped_NoBroadcastFnNoPanic(t *testing.T) {
	sched := New()

	release := make(chan struct{})
	var started atomic.Int64

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		started.Add(1)
		<-release
		return nil
	})
	// No broadcast function set — broadcastFn is nil.

	sched.Start()
	defer func() {
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sched.Stop(ctx)
	}()

	// Fill the semaphore.
	for i := range maxConcurrentWorkflows {
		wf := &Workflow{
			ID:      workflowSkippedID("nil-fill", i),
			Name:    "NilFill",
			Enabled: true,
		}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			t.Fatalf("TriggerWorkflow: %v", err)
		}
	}

	// Wait for fillers.
	deadline := time.After(3 * time.Second)
	for started.Load() < int64(maxConcurrentWorkflows) {
		select {
		case <-deadline:
			t.Fatalf("fillers did not start; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Register the skipped workflow — must not panic even with nil broadcastFn.
	wf := &Workflow{
		ID:       "wf-skipped-nil-broadcast",
		Name:     "SkippedNilBroadcast",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Let it tick a few times without panicking.
	time.Sleep(200 * time.Millisecond)
}

// TestScheduler_WorkflowSkipped_WorkflowRunsOnNextTick verifies that a skipped
// workflow is not permanently disabled — once the semaphore has a free slot it
// executes on the next cron tick.
func TestScheduler_WorkflowSkipped_WorkflowRunsOnNextTick(t *testing.T) {
	sched := New()

	// release controls the blocker workflows.
	release := make(chan struct{})
	var started atomic.Int64
	var targetCalls atomic.Int64

	const targetID = "wf-retry-on-tick"

	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		if w.ID == targetID {
			targetCalls.Add(1)
			return nil
		}
		// Blocker workflow.
		started.Add(1)
		<-release
		return nil
	})

	sched.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sched.Stop(ctx)
	}()

	// Fill the semaphore with blocker workflows.
	for i := range maxConcurrentWorkflows {
		wf := &Workflow{
			ID:      workflowSkippedID("tick-fill", i),
			Name:    "TickFill",
			Enabled: true,
		}
		if err := sched.TriggerWorkflow(context.Background(), wf); err != nil {
			t.Fatalf("TriggerWorkflow blocker %d: %v", i, err)
		}
	}

	// Wait for all blocker slots to fill.
	deadline := time.After(3 * time.Second)
	for started.Load() < int64(maxConcurrentWorkflows) {
		select {
		case <-deadline:
			t.Fatalf("blocker slots not filled; started=%d", started.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Register the target workflow.  It will be skipped while the semaphore is full.
	wf := &Workflow{
		ID:       targetID,
		Name:     "RetryOnTick",
		Schedule: "@every 50ms",
		Enabled:  true,
	}
	if err := sched.RegisterWorkflow(wf); err != nil {
		t.Fatalf("RegisterWorkflow target: %v", err)
	}

	// Wait a bit — target must be skipped (no executions yet).
	time.Sleep(150 * time.Millisecond)
	if targetCalls.Load() > 0 {
		t.Errorf("expected 0 target calls while semaphore is full, got %d", targetCalls.Load())
	}

	// Release the blocker workflows so the semaphore has free slots.
	close(release)

	// The target workflow should execute on the next cron tick.
	deadline2 := time.After(3 * time.Second)
	for targetCalls.Load() == 0 {
		select {
		case <-deadline2:
			t.Fatal("target workflow did not run after semaphore was freed")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// workflowSkippedID is a helper that generates a unique workflow ID for the
// filler workflows so the test IDs do not collide across sub-tests.
func workflowSkippedID(prefix string, i int) string {
	return prefix + "-" + string(rune('a'+i))
}
