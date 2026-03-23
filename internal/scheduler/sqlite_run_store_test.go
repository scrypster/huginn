package scheduler_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openRunTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSQLiteRunStore_CompileTimeInterface ensures SQLiteWorkflowRunStore satisfies the interface.
func TestSQLiteRunStore_CompileTimeInterface(t *testing.T) {
	var _ scheduler.WorkflowRunStoreInterface = (*scheduler.SQLiteWorkflowRunStore)(nil)
}

// TestSQLiteRunStore_AppendAndList appends one run and lists it back.
func TestSQLiteRunStore_AppendAndList(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	run := &scheduler.WorkflowRun{
		ID:         "01HZ000000000000000000001",
		WorkflowID: "wf-alpha",
		Status:     scheduler.WorkflowRunStatusComplete,
		Steps:      nil,
		StartedAt:  time.Now().UTC().Truncate(time.Second),
	}

	if err := store.Append(run.WorkflowID, run); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := store.List(run.WorkflowID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 run, got %d", len(got))
	}
	if got[0].ID != run.ID {
		t.Errorf("ID: want %q, got %q", run.ID, got[0].ID)
	}
	if got[0].Status != run.Status {
		t.Errorf("Status: want %q, got %q", run.Status, got[0].Status)
	}
}

// TestSQLiteRunStore_ListEmpty returns an empty slice (not an error) for a workflow with no runs.
func TestSQLiteRunStore_ListEmpty(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	got, err := store.List("nonexistent-workflow", 10)
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty slice, got %d items", len(got))
	}
}

// TestSQLiteRunStore_ListCapped inserts 5 runs and verifies List(wf, 3) returns 3 newest first.
func TestSQLiteRunStore_ListCapped(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	wfID := "wf-capped"
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		run := &scheduler.WorkflowRun{
			ID:         fmt.Sprintf("01HZ0000000000000000%05d", i),
			WorkflowID: wfID,
			Status:     scheduler.WorkflowRunStatusComplete,
			StartedAt:  base.Add(time.Duration(i) * time.Hour),
		}
		if err := store.Append(wfID, run); err != nil {
			t.Fatalf("Append run %d: %v", i, err)
		}
	}

	got, err := store.List(wfID, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 runs, got %d", len(got))
	}

	// Should be newest first: indices 4, 3, 2
	expectedIDs := []string{
		fmt.Sprintf("01HZ0000000000000000%05d", 4),
		fmt.Sprintf("01HZ0000000000000000%05d", 3),
		fmt.Sprintf("01HZ0000000000000000%05d", 2),
	}
	for i, want := range expectedIDs {
		if got[i].ID != want {
			t.Errorf("run[%d]: want ID %q, got %q", i, want, got[i].ID)
		}
	}
}

// TestSQLiteRunStore_WithSteps verifies WorkflowStepResult round-trips via JSON.
func TestSQLiteRunStore_WithSteps(t *testing.T) {
	db := openRunTestDB(t)
	store := scheduler.NewSQLiteWorkflowRunStore(db)

	now := time.Now().UTC().Truncate(time.Second)
	completedAt := now.Add(5 * time.Minute)

	run := &scheduler.WorkflowRun{
		ID:          "01HZ000000000000000000002",
		WorkflowID:  "wf-steps",
		Status:      scheduler.WorkflowRunStatusFailed,
		StartedAt:   now,
		CompletedAt: &completedAt,
		Error:       "step 2 timed out",
		Steps: []scheduler.WorkflowStepResult{
			{
				Position:  1,
				Slug:      "fetch-data",
				RoutineID: "routine-001",
				SessionID: "session-aaa",
				Status:    "complete",
				Error:     "",
			},
			{
				Position:  2,
				Slug:      "process-data",
				RoutineID: "routine-002",
				SessionID: "session-bbb",
				Status:    "failed",
				Error:     "timed out after 30s",
			},
		},
	}

	if err := store.Append(run.WorkflowID, run); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := store.List(run.WorkflowID, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 run, got %d", len(got))
	}

	r := got[0]
	if r.ID != run.ID {
		t.Errorf("ID: want %q, got %q", run.ID, r.ID)
	}
	if r.Status != run.Status {
		t.Errorf("Status: want %q, got %q", run.Status, r.Status)
	}
	if r.Error != run.Error {
		t.Errorf("Error: want %q, got %q", run.Error, r.Error)
	}
	if r.CompletedAt == nil {
		t.Fatal("CompletedAt: want non-nil, got nil")
	}
	if !r.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt: want %v, got %v", completedAt, *r.CompletedAt)
	}
	if len(r.Steps) != 2 {
		t.Fatalf("Steps: want 2, got %d", len(r.Steps))
	}

	s0 := r.Steps[0]
	if s0.Position != 1 {
		t.Errorf("Steps[0].Position: want 1, got %d", s0.Position)
	}
	if s0.Slug != "fetch-data" {
		t.Errorf("Steps[0].Slug: want %q, got %q", "fetch-data", s0.Slug)
	}
	if s0.RoutineID != "routine-001" {
		t.Errorf("Steps[0].RoutineID: want %q, got %q", "routine-001", s0.RoutineID)
	}
	if s0.SessionID != "session-aaa" {
		t.Errorf("Steps[0].SessionID: want %q, got %q", "session-aaa", s0.SessionID)
	}
	if s0.Status != "complete" {
		t.Errorf("Steps[0].Status: want %q, got %q", "complete", s0.Status)
	}

	s1 := r.Steps[1]
	if s1.Position != 2 {
		t.Errorf("Steps[1].Position: want 2, got %d", s1.Position)
	}
	if s1.Error != "timed out after 30s" {
		t.Errorf("Steps[1].Error: want %q, got %q", "timed out after 30s", s1.Error)
	}
}
