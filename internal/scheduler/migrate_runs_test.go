package scheduler_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

func writeJSONLRuns(t *testing.T, path string, runs []*scheduler.WorkflowRun) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range runs {
		enc.Encode(r)
	}
}

// TestMigrateFromJSONL_MigratesRuns writes 2 runs to wf-a.jsonl, migrates, and
// verifies List("wf-a", 10) returns exactly 2 runs.
func TestMigrateFromJSONL_MigratesRuns(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()

	now := time.Now().UTC().Truncate(time.Second)
	runs := []*scheduler.WorkflowRun{
		{ID: "01MIGRATE00000000000000001", WorkflowID: "wf-a", Status: scheduler.WorkflowRunStatusComplete, StartedAt: now},
		{ID: "01MIGRATE00000000000000002", WorkflowID: "wf-a", Status: scheduler.WorkflowRunStatusFailed, StartedAt: now.Add(time.Minute)},
	}
	writeJSONLRuns(t, filepath.Join(dir, "wf-a.jsonl"), runs)

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL: %v", err)
	}

	store := scheduler.NewSQLiteWorkflowRunStore(db)
	got, err := store.List("wf-a", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 runs, got %d", len(got))
	}
}

// TestMigrateFromJSONL_RecordsMigration verifies that even with an empty dir,
// _migrations gets a M4_workflow_runs row.
func TestMigrateFromJSONL_RecordsMigration(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL: %v", err)
	}

	var count int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = 'M4_workflow_runs'`).Scan(&count)
	if err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 migration record, got %d", count)
	}
}

// TestMigrateFromJSONL_Idempotent verifies running migration twice results in
// only 1 row in _migrations (idempotency guard).
func TestMigrateFromJSONL_Idempotent(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()

	now := time.Now().UTC().Truncate(time.Second)
	runs := []*scheduler.WorkflowRun{
		{ID: "01IDEMPOTENT000000000001", WorkflowID: "wf-idem", Status: scheduler.WorkflowRunStatusComplete, StartedAt: now},
	}
	writeJSONLRuns(t, filepath.Join(dir, "wf-idem.jsonl"), runs)

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("first MigrateFromJSONL: %v", err)
	}
	// Run again — must not error (the .bak dir won't exist as a JSONL source now,
	// and idempotency check fires first).
	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("second MigrateFromJSONL: %v", err)
	}

	var count int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = 'M4_workflow_runs'`).Scan(&count)
	if err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("want exactly 1 migration record, got %d", count)
	}
}

// TestMigrateFromJSONL_MissingDir_NoOp verifies that a nonexistent baseDir
// is treated as a no-op (no error, migration recorded).
func TestMigrateFromJSONL_MissingDir_NoOp(t *testing.T) {
	db := openRunTestDB(t)
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL on missing dir: %v", err)
	}

	// Migration should still be recorded.
	var count int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = 'M4_workflow_runs'`).Scan(&count)
	if err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 migration record even for missing dir, got %d", count)
	}
}

// TestMigrateFromJSONL_CreatesBakDir verifies that after migration the
// original baseDir has been renamed to baseDir+".bak".
func TestMigrateFromJSONL_CreatesBakDir(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()
	bakDir := dir + ".bak"

	now := time.Now().UTC().Truncate(time.Second)
	runs := []*scheduler.WorkflowRun{
		{ID: "01BAKTEST000000000000001", WorkflowID: "wf-bak", Status: scheduler.WorkflowRunStatusComplete, StartedAt: now},
	}
	writeJSONLRuns(t, filepath.Join(dir, "wf-bak.jsonl"), runs)

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL: %v", err)
	}

	if _, err := os.Stat(bakDir); os.IsNotExist(err) {
		t.Fatalf("expected bak dir %q to exist after migration", bakDir)
	}
}

// TestMigrateFromJSONL_MultipleWorkflows verifies that runs from multiple
// JSONL files are all migrated to their respective workflow IDs.
func TestMigrateFromJSONL_MultipleWorkflows(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()

	now := time.Now().UTC().Truncate(time.Second)

	// 1 run in wf-x.jsonl
	writeJSONLRuns(t, filepath.Join(dir, "wf-x.jsonl"), []*scheduler.WorkflowRun{
		{ID: "01MULTI000000000000000001", WorkflowID: "wf-x", Status: scheduler.WorkflowRunStatusComplete, StartedAt: now},
	})

	// 2 runs in wf-y.jsonl
	writeJSONLRuns(t, filepath.Join(dir, "wf-y.jsonl"), []*scheduler.WorkflowRun{
		{ID: "01MULTI000000000000000002", WorkflowID: "wf-y", Status: scheduler.WorkflowRunStatusComplete, StartedAt: now},
		{ID: "01MULTI000000000000000003", WorkflowID: "wf-y", Status: scheduler.WorkflowRunStatusFailed, StartedAt: now.Add(time.Minute)},
	})

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL: %v", err)
	}

	store := scheduler.NewSQLiteWorkflowRunStore(db)

	gotX, err := store.List("wf-x", 10)
	if err != nil {
		t.Fatalf("List wf-x: %v", err)
	}
	if len(gotX) != 1 {
		t.Errorf("wf-x: want 1 run, got %d", len(gotX))
	}

	gotY, err := store.List("wf-y", 10)
	if err != nil {
		t.Fatalf("List wf-y: %v", err)
	}
	if len(gotY) != 2 {
		t.Errorf("wf-y: want 2 runs, got %d", len(gotY))
	}
}

// TestMigrateFromJSONL_BatchInsert writes 2500 runs and verifies all 2500
// are migrated (exercises the 1000-row batch logic across 3 transactions).
func TestMigrateFromJSONL_BatchInsert(t *testing.T) {
	db := openRunTestDB(t)
	dir := t.TempDir()

	const total = 2500
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	runs := make([]*scheduler.WorkflowRun, total)
	for i := 0; i < total; i++ {
		runs[i] = &scheduler.WorkflowRun{
			ID:         fmt.Sprintf("01BATCH%019d", i),
			WorkflowID: "wf-batch",
			Status:     scheduler.WorkflowRunStatusComplete,
			StartedAt:  base.Add(time.Duration(i) * time.Second),
		}
	}
	writeJSONLRuns(t, filepath.Join(dir, "wf-batch.jsonl"), runs)

	if err := scheduler.MigrateFromJSONL(dir, db); err != nil {
		t.Fatalf("MigrateFromJSONL: %v", err)
	}

	var count int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE workflow_id = 'wf-batch'`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != total {
		t.Fatalf("want %d runs, got %d", total, count)
	}
}
