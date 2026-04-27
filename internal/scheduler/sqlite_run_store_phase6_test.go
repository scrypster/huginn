package scheduler_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

// TestSQLiteRunStore_Phase6_RoundTrip persists a run that carries
// TriggerInputs and a WorkflowSnapshot and reads it back to verify the
// JSON-typed columns survive the round trip.
func TestSQLiteRunStore_Phase6_RoundTrip(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	snap := &scheduler.Workflow{
		ID:   "wf-snap",
		Name: "snap",
		Steps: []scheduler.WorkflowStep{
			{Position: 1, Name: "a", Agent: "x", Prompt: "p"},
		},
	}
	run := &scheduler.WorkflowRun{
		ID:               "01HZRP00000000000000000001",
		WorkflowID:       "wf-snap",
		Status:           scheduler.WorkflowRunStatusComplete,
		StartedAt:        time.Now().UTC().Truncate(time.Second),
		TriggerInputs:    map[string]string{"alpha": "1", "beta": "two"},
		WorkflowSnapshot: snap,
	}
	if err := store.Append(run.WorkflowID, run); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := store.Get(run.WorkflowID, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.TriggerInputs["alpha"] != "1" || got.TriggerInputs["beta"] != "two" {
		t.Errorf("TriggerInputs lost on round trip: %#v", got.TriggerInputs)
	}
	if got.WorkflowSnapshot == nil || got.WorkflowSnapshot.ID != "wf-snap" {
		t.Errorf("WorkflowSnapshot lost on round trip: %#v", got.WorkflowSnapshot)
	}
	if len(got.WorkflowSnapshot.Steps) != 1 || got.WorkflowSnapshot.Steps[0].Name != "a" {
		t.Errorf("snapshot steps lost: %#v", got.WorkflowSnapshot.Steps)
	}
}

// TestSQLiteRunStore_Phase6_NilFields verifies a run without trigger inputs
// or snapshot still round-trips as nil/empty (not a zero-value snapshot).
func TestSQLiteRunStore_Phase6_NilFields(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	run := &scheduler.WorkflowRun{
		ID:         "01HZRP00000000000000000002",
		WorkflowID: "wf-empty",
		Status:     scheduler.WorkflowRunStatusComplete,
		StartedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := store.Append(run.WorkflowID, run); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := store.Get(run.WorkflowID, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.TriggerInputs != nil {
		t.Errorf("expected nil TriggerInputs, got %#v", got.TriggerInputs)
	}
	if got.WorkflowSnapshot != nil {
		t.Errorf("expected nil WorkflowSnapshot, got %#v", got.WorkflowSnapshot)
	}
}
