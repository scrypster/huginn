package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidateWorkflowTimeout_ClampsBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   int
		want int
	}{
		{name: "negative maps to zero", in: -5, want: 0},
		{name: "zero preserved", in: 0, want: 0},
		{name: "in range preserved", in: 15, want: 15},
		{name: "above max clamped", in: maxWorkflowTimeoutMinutes + 99, want: maxWorkflowTimeoutMinutes},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidateWorkflowTimeout(tc.in); got != tc.want {
				t.Fatalf("ValidateWorkflowTimeout(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateCronSchedule_Validation(t *testing.T) {
	t.Parallel()
	if err := ValidateCronSchedule(""); err != nil {
		t.Fatalf("empty schedule should be accepted: %v", err)
	}
	if err := ValidateCronSchedule("0 9 * * 1-5"); err != nil {
		t.Fatalf("valid schedule should pass: %v", err)
	}
	if err := ValidateCronSchedule("*/5 * * * * *"); err != nil {
		t.Fatalf("valid second-precision schedule should pass: %v", err)
	}
	if err := ValidateCronSchedule("not-a-cron"); err == nil {
		t.Fatal("invalid schedule should fail")
	}
}

func TestScheduler_SettersWireDependencies(t *testing.T) {
	t.Parallel()
	s := New()
	store := NewWorkflowRunStore(t.TempDir())
	s.SetWorkflowRunStore(store)
	q := &DeliveryQueue{}
	s.SetDeliveryQueue(q)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workflowRunStore != store {
		t.Fatal("workflow run store setter did not apply")
	}
	if s.deliveryQueue != q {
		t.Fatal("delivery queue setter did not apply")
	}
}

func TestScheduler_LoadWorkflows_SkipsInvalidSchedules(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	dir := t.TempDir()

	writeWorkflowYAML(t, dir+"/good.yaml", map[string]any{
		"id": "wf-good", "name": "Good", "enabled": true, "schedule": "0 9 * * *",
	})
	writeWorkflowYAML(t, dir+"/bad.yaml", map[string]any{
		"id": "wf-bad", "name": "Bad", "enabled": true, "schedule": "invalid-cron",
	})

	if err := s.LoadWorkflows(dir); err != nil {
		t.Fatalf("LoadWorkflows: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.workflowEntries) != 1 {
		t.Fatalf("registered entries = %d, want 1", len(s.workflowEntries))
	}
	if _, ok := s.workflowEntries["wf-good"]; !ok {
		t.Fatal("expected wf-good to be registered")
	}
	if _, ok := s.workflowEntries["wf-bad"]; ok {
		t.Fatal("wf-bad should not be registered")
	}
}

func TestScheduler_LoadWorkflows_SkipsSubWorkflowCycles(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	dir := t.TempDir()

	writeWorkflowYAML(t, dir+"/wf-a.yaml", map[string]any{
		"id": "wf-a", "name": "A", "enabled": true, "schedule": "0 9 * * *",
		"steps": []map[string]any{
			{"name": "call-b", "position": 0, "sub_workflow": "wf-b"},
		},
	})
	writeWorkflowYAML(t, dir+"/wf-b.yaml", map[string]any{
		"id": "wf-b", "name": "B", "enabled": true, "schedule": "0 10 * * *",
		"steps": []map[string]any{
			{"name": "call-a", "position": 0, "sub_workflow": "wf-a"},
		},
	})
	writeWorkflowYAML(t, dir+"/wf-leaf.yaml", map[string]any{
		"id": "wf-leaf", "name": "Leaf", "enabled": true, "schedule": "0 11 * * *",
		"steps": []map[string]any{
			{"name": "ok", "position": 0, "prompt": "leaf"},
		},
	})

	if err := s.LoadWorkflows(dir); err != nil {
		t.Fatalf("LoadWorkflows: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflowEntries["wf-a"]; ok {
		t.Fatal("wf-a should be skipped because it participates in a sub_workflow cycle")
	}
	if _, ok := s.workflowEntries["wf-b"]; ok {
		t.Fatal("wf-b should be skipped because it participates in a sub_workflow cycle")
	}
	if _, ok := s.workflowEntries["wf-leaf"]; !ok {
		t.Fatal("wf-leaf should still be registered")
	}
}

func TestScheduler_TriggerWorkflowWithInputs_ForwardsInputs(t *testing.T) {
	s := New()
	gotInputs := make(chan map[string]string, 1)
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		gotInputs <- InitialInputs(ctx)
		return nil
	})

	wf := &Workflow{ID: "wf-inputs", Name: "Inputs", Enabled: true}
	if err := s.TriggerWorkflowWithInputs(context.Background(), wf, map[string]string{"seed": "42"}); err != nil {
		t.Fatalf("TriggerWorkflowWithInputs: %v", err)
	}

	select {
	case got := <-gotInputs:
		if got["seed"] != "42" {
			t.Fatalf("InitialInputs(seed) = %q, want 42", got["seed"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for workflow runner")
	}
}

func TestScheduler_RunWorkflowSyncWithInputs_ReturnsPersistedRun(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	s := New()
	s.SetWorkflowRunStore(store)
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		if InitialInputs(ctx)["from"] != "api" {
			t.Fatalf("expected inputs to be forwarded to sync runner")
		}
		// Use the pre-generated run ID injected by RunWorkflowSyncWithInputs so
		// the caller can look the run up by ID after the runner returns.
		id := pregenRunIDFromContext(ctx)
		if id == "" {
			id = "run-sync-1"
		}
		run := &WorkflowRun{
			ID:         id,
			WorkflowID: w.ID,
			StartedAt:  time.Now().UTC(),
			Status:     WorkflowRunStatusComplete,
		}
		return store.Append(w.ID, run)
	})

	wf := &Workflow{ID: "wf-sync", Name: "Sync WF"}
	run, err := s.RunWorkflowSyncWithInputs(context.Background(), wf, map[string]string{"from": "api"})
	if err != nil {
		t.Fatalf("RunWorkflowSyncWithInputs: %v", err)
	}
	if run == nil {
		t.Fatalf("unexpected run: %+v", run)
	}
}

func TestScheduler_RunWorkflowSyncWithInputs_NoStoreReturnsNilRun(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	wf := &Workflow{ID: "wf-sync-nostore", Name: "Sync no-store"}

	run, err := s.RunWorkflowSyncWithInputs(context.Background(), wf, map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("RunWorkflowSyncWithInputs: %v", err)
	}
	if run != nil {
		t.Fatalf("expected nil run when store is not set, got %+v", run)
	}
}

func TestScheduler_RunWorkflowSyncWithInputs_PropagatesRunnerError(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		return errors.New("runner failed")
	})

	_, err := s.RunWorkflowSyncWithInputs(context.Background(), &Workflow{ID: "wf-sync-fail"}, nil)
	if err == nil {
		t.Fatal("expected runner error from RunWorkflowSyncWithInputs")
	}
}

func TestScheduler_CancelWorkflow_UserCancellationCause(t *testing.T) {
	s := New()
	started := make(chan struct{})
	causeCh := make(chan error, 1)
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		close(started)
		<-ctx.Done()
		causeCh <- context.Cause(ctx)
		return nil
	})

	wf := &Workflow{ID: "wf-cancel", Name: "Cancelable", Enabled: true}
	if err := s.TriggerWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("TriggerWorkflow: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("workflow runner did not start")
	}

	if !s.CancelWorkflow(wf.ID) {
		t.Fatal("CancelWorkflow should return true while workflow is running")
	}
	select {
	case cause := <-causeCh:
		if !errors.Is(cause, errUserCancelled) {
			t.Fatalf("context cause = %v, want errUserCancelled", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not observe cancellation")
	}

	// The cancel handle should be cleared after run exit.
	time.Sleep(50 * time.Millisecond)
	if s.CancelWorkflow(wf.ID) {
		t.Fatal("second CancelWorkflow should return false after cleanup")
	}
}

func TestScheduler_CancelWorkflow_NotRunningReturnsFalse(t *testing.T) {
	t.Parallel()
	s := New()
	if s.CancelWorkflow("missing") {
		t.Fatal("CancelWorkflow should return false for non-running workflow")
	}
}
