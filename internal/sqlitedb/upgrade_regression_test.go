package sqlitedb_test

// upgrade_regression_test.go — guards against ApplySchema failures on fresh
// database initialization.
//
// History: the bug (2026-03-17) caused schema.sql to contain CREATE INDEX
// statements for columns that only existed after migrations ran, breaking
// all upgraded databases. Migrations have since been squashed into the base
// schema DDL, so all columns referenced by indexes are now in CREATE TABLE.
//
// These tests verify the squashed schema works correctly on fresh databases.

import (
	"database/sql"
	"testing"
)

// TestApplySchema_SucceedsOnFreshDatabase verifies that ApplySchema completes
// without error on a fresh empty database — all columns referenced by CREATE
// INDEX statements must be present in the corresponding CREATE TABLE blocks.
func TestApplySchema_SucceedsOnFreshDatabase(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema failed on fresh database: %v\n\nschema.sql contains a CREATE INDEX that references a column not in CREATE TABLE.", err)
	}
}

// TestApplySchema_AllThreadingColumnsPresent verifies that after ApplySchema
// on a fresh database, the messages table includes all threading columns that
// were previously added via migrations (now squashed into the base schema).
func TestApplySchema_AllThreadingColumnsPresent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	wantCols := []string{
		"parent_message_id",
		"triggering_message_id",
		"thread_reply_count",
		"thread_last_reply_at",
	}
	rows, err := db.Read().Query(`PRAGMA table_info(messages)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType, notNull, dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name.String] = true
	}
	for _, col := range wantCols {
		if !got[col] {
			t.Errorf("column %q missing from messages table after ApplySchema — add it to CREATE TABLE in schema.sql", col)
		}
	}
}

// TestApplySchema_IdempotentOnFreshDB verifies that calling ApplySchema twice
// on the same fresh database does not fail (all DDL uses IF NOT EXISTS).
func TestApplySchema_IdempotentOnFreshDB(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	for i := range 2 {
		if err := db.ApplySchema(); err != nil {
			t.Fatalf("ApplySchema call %d failed: %v", i+1, err)
		}
	}
}
