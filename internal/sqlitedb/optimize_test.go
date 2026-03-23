package sqlitedb_test

import (
	"testing"
)

// TestPRAGMAOptimize_EmptyDB verifies that "PRAGMA optimize" completes without
// error on a freshly opened database that has no user tables.
func TestPRAGMAOptimize_EmptyDB(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.RunOptimize(); err != nil {
		t.Fatalf("RunOptimize on empty db: %v", err)
	}
}

// TestPRAGMAOptimize_PopulatedDB verifies that "PRAGMA optimize" completes
// without error on a database with schema tables and some rows.
func TestPRAGMAOptimize_PopulatedDB(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Apply the full schema so there are real tables with indexes.
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Insert a handful of rows so the planner has something to analyze.
	for i := 0; i < 5; i++ {
		db.Write().Exec(
			`INSERT OR IGNORE INTO sessions
				(id, title, model, agent, created_at, updated_at,
				 message_count, last_message_id, workspace_root, workspace_name,
				 status, version, source, routine_id, run_id)
			 VALUES (?, ?, '', '', datetime('now'), datetime('now'), 0, '', '', '', 'active', 1, '', '', '')`,
			"test-session-optimize-"+string(rune('a'+i)),
			"Test Session",
		)
	}

	if err := db.RunOptimize(); err != nil {
		t.Fatalf("RunOptimize on populated db: %v", err)
	}
}

// TestApplySchema_RunsOptimize verifies that ApplySchema does not return an
// error (PRAGMA optimize is called internally but failures are non-fatal).
func TestApplySchema_RunsOptimize(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	// ApplySchema internally calls PRAGMA optimize; this must not fail.
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema (with embedded optimize): %v", err)
	}
	// Calling RunOptimize explicitly after ApplySchema must also succeed.
	if err := db.RunOptimize(); err != nil {
		t.Fatalf("RunOptimize after ApplySchema: %v", err)
	}
}
