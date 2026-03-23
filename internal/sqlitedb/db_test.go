package sqlitedb_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func TestOpen_CreatesDatabase(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if db == nil {
		t.Fatal("Open returned nil DB")
	}
}

func TestOpen_WriteRead(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	_, err := db.Write().Exec(`CREATE TABLE IF NOT EXISTS _test (id TEXT PRIMARY KEY)`)
	if err != nil {
		t.Fatalf("Write().Exec: %v", err)
	}
	_, err = db.Write().Exec(`INSERT INTO _test VALUES ('hello')`)
	if err != nil {
		t.Fatalf("Write().Exec INSERT: %v", err)
	}

	var id string
	err = db.Read().QueryRow(`SELECT id FROM _test`).Scan(&id)
	if err != nil {
		t.Fatalf("Read().QueryRow: %v", err)
	}
	if id != "hello" {
		t.Fatalf("got %q, want %q", id, "hello")
	}
}

func TestOpen_ForeignKeysEnforced(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	db.Write().Exec(`CREATE TABLE parent (id TEXT PRIMARY KEY)`)
	db.Write().Exec(`CREATE TABLE child (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES parent(id))`)

	_, err := db.Write().Exec(`INSERT INTO child VALUES ('c1', 'nonexistent')`)
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

func TestClose_Idempotent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestApplySchema_Idempotent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("first ApplySchema: %v", err)
	}

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("second ApplySchema: %v", err)
	}
}

func TestApplySchema_TablesExist(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	want := []string{
		"sessions", "teams", "team_members", "threads", "thread_deps",
		"messages", "routines", "routine_runs", "relay_cursors",
		"connections", "notifications", "workflow_runs",
		"agent_summaries", "agent_delegations", "_migrations",
	}
	for _, table := range want {
		var name string
		err := db.Read().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrate_RunsOnce(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	ran := 0
	migrations := []sqlitedb.Migration{
		{
			Name: "test_migration_001",
			Up: func(tx *sql.Tx) error {
				ran++
				return nil
			},
		},
	}

	if err := db.Migrate(migrations); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(migrations); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	if ran != 1 {
		t.Fatalf("migration ran %d times, want 1", ran)
	}
}

func TestMigrate_RecordsCompletion(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	migrations := []sqlitedb.Migration{
		{Name: "test_record_001", Up: func(tx *sql.Tx) error { return nil }},
	}
	if err := db.Migrate(migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var count int
	db.Read().QueryRow(
		`SELECT COUNT(*) FROM _migrations WHERE name = 'test_record_001'`,
	).Scan(&count)
	if count != 1 {
		t.Fatalf("_migrations count = %d, want 1", count)
	}
}

func TestMigrate_RollsBackOnError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	migrations := []sqlitedb.Migration{
		{
			Name: "test_fail_001",
			Up: func(tx *sql.Tx) error {
				tx.Exec(`CREATE TABLE _rollback_test (id TEXT)`)
				return fmt.Errorf("intentional failure")
			},
		},
	}
	err := db.Migrate(migrations)
	if err == nil {
		t.Fatal("expected error from failing migration, got nil")
	}

	var name string
	scanErr := db.Read().QueryRow(
		`SELECT name FROM sqlite_master WHERE name = '_rollback_test'`,
	).Scan(&name)
	if scanErr == nil {
		t.Fatal("table _rollback_test exists after rollback — transaction not rolled back")
	}

	var count int
	db.Read().QueryRow(
		`SELECT COUNT(*) FROM _migrations WHERE name = 'test_fail_001'`,
	).Scan(&count)
	if count != 0 {
		t.Fatal("failed migration recorded in _migrations — should not be")
	}
}

func TestSchemaSyncWithDocs(t *testing.T) {
	// Ensures the embedded schema copy stays in sync with docs/schema.
	// Run from repo root: go test ./internal/sqlitedb/...
	embedded, err := os.ReadFile("schema/huginn-sqlite-schema.sql")
	if err != nil {
		t.Fatalf("read embedded copy: %v", err)
	}
	canonical, err := os.ReadFile("../../docs/schema/huginn-sqlite-schema.sql")
	if err != nil {
		t.Fatalf("read canonical docs schema: %v", err)
	}
	if string(embedded) != string(canonical) {
		t.Fatal("internal/sqlitedb/schema/huginn-sqlite-schema.sql is out of sync with docs/schema/huginn-sqlite-schema.sql — run: cp docs/schema/huginn-sqlite-schema.sql internal/sqlitedb/schema/")
	}
}

// openTestDB opens an isolated DB in t.TempDir() and registers t.Cleanup.
func openTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
