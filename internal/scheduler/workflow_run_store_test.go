package scheduler

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkflowRunStore_AppendAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkflowRunStore(dir)

	run := &WorkflowRun{
		ID:         "run1",
		WorkflowID: "wf1",
		Status:     WorkflowRunStatusComplete,
		StartedAt:  time.Now().UTC(),
	}
	if err := store.Append("wf1", run); err != nil {
		t.Fatal(err)
	}
	runs, err := store.List("wf1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1, got %d", len(runs))
	}
	if runs[0].ID != "run1" {
		t.Errorf("ID want run1, got %q", runs[0].ID)
	}
}

func TestWorkflowRunStore_ListEmpty(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	runs, err := store.List("nonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Errorf("want 0, got %d", len(runs))
	}
}

func TestWorkflowRunStore_ListCapped(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkflowRunStore(dir)
	for i := 0; i < 5; i++ {
		r := &WorkflowRun{ID: fmt.Sprintf("run%d", i), WorkflowID: "wf2",
			Status: WorkflowRunStatusComplete, StartedAt: time.Now().UTC()}
		_ = store.Append("wf2", r)
	}
	runs, err := store.List("wf2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Errorf("want 3 (capped), got %d", len(runs))
	}
	// newest first: last 3 appended were run2, run3, run4; reversed = run4, run3, run2
	if runs[0].ID != "run4" {
		t.Errorf("want run4 first (newest), got %q", runs[0].ID)
	}
}
