package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_StartStop(t *testing.T) {
	s := New()
	s.Start(context.Background())
	// Stop should return without blocking when no jobs are running.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.Stop(ctx)
}

func TestScheduler_RegisterWorkflow_AddsEntry(t *testing.T) {
	s := New()
	s.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Stop(ctx)
	}()

	var runCount atomic.Int32
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		runCount.Add(1)
		return nil
	})

	w := &Workflow{
		ID:       "test-wf-1",
		Enabled:  true,
		Schedule: "@every 1s",
		Steps:    []WorkflowStep{{Name: "s1", Agent: "a", Prompt: "p", Position: 0}},
	}
	if err := s.RegisterWorkflow(w); err != nil {
		t.Fatalf("RegisterWorkflow: %v", err)
	}

	// Verify entry is tracked.
	s.mu.Lock()
	_, ok := s.workflowEntries[w.ID]
	s.mu.Unlock()
	if !ok {
		t.Fatal("expected workflow entry to be registered")
	}
}

func TestScheduler_RegisterWorkflow_EmptyScheduleError(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })

	w := &Workflow{ID: "no-sched", Enabled: true, Schedule: ""}
	if err := s.RegisterWorkflow(w); err == nil {
		t.Fatal("expected error for empty schedule")
	}
}

func TestScheduler_RegisterWorkflow_NoRunnerError(t *testing.T) {
	s := New() // no runner set
	w := &Workflow{ID: "wf", Enabled: true, Schedule: "@every 1s"}
	if err := s.RegisterWorkflow(w); err == nil {
		t.Fatal("expected error when runner is not configured")
	}
}

func TestScheduler_RemoveWorkflow_NoopWhenNotRegistered(t *testing.T) {
	s := New()
	// Must not panic.
	s.RemoveWorkflow("nonexistent-id")
}

func TestScheduler_TriggerWorkflow_RunsRunner(t *testing.T) {
	s := New()
	var ran atomic.Bool
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		ran.Store(true)
		return nil
	})

	w := &Workflow{ID: "trig-wf", Enabled: true}
	if err := s.TriggerWorkflow(context.Background(), w); err != nil {
		t.Fatalf("TriggerWorkflow: %v", err)
	}

	// Wait for background goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ran.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ran.Load() {
		t.Fatal("expected workflow runner to be called")
	}
}

func TestScheduler_TriggerWorkflow_BusyReturnsError(t *testing.T) {
	s := New()
	block := make(chan struct{})
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		<-block // block until test unblocks it
		return nil
	})

	w := &Workflow{ID: "busy-wf", Enabled: true}

	// First trigger should succeed.
	if err := s.TriggerWorkflow(context.Background(), w); err != nil {
		t.Fatalf("first TriggerWorkflow: %v", err)
	}

	// Give the goroutine time to mark workflowRunning[w.ID] = true.
	time.Sleep(20 * time.Millisecond)

	// Second trigger should return ErrWorkflowAlreadyRunning.
	err := s.TriggerWorkflow(context.Background(), w)
	close(block) // unblock first runner
	if err == nil {
		t.Fatal("expected ErrWorkflowAlreadyRunning, got nil")
	}
}

func TestScheduler_TriggerWorkflow_NoRunnerError(t *testing.T) {
	s := New()
	w := &Workflow{ID: "no-runner-wf"}
	if err := s.TriggerWorkflow(context.Background(), w); err == nil {
		t.Fatal("expected error when no runner configured")
	}
}
