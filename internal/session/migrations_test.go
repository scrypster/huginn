package session_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func TestMigrationsRegistered(t *testing.T) {
	migs := session.Migrations()
	if len(migs) != 11 {
		t.Fatalf("expected 11 migrations, got %d", len(migs))
	}
}

func TestMemoryReplicationQueueTableCreated(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Create minimal stub tables that migrations depend on
	stubs := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, space_id TEXT, title TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE messages (
			id TEXT NOT NULL PRIMARY KEY,
			container_type TEXT NOT NULL DEFAULT 'session',
			container_id TEXT NOT NULL DEFAULT '',
			tenant_id TEXT NOT NULL DEFAULT '',
			seq INTEGER NOT NULL DEFAULT 0,
			ts TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'assistant',
			content TEXT NOT NULL DEFAULT '',
			agent TEXT NOT NULL DEFAULT '',
			tool_name TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			tool_calls_json TEXT,
			type TEXT NOT NULL DEFAULT '',
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0.0,
			model TEXT NOT NULL DEFAULT '',
			parent_message_id TEXT,
			triggering_message_id TEXT,
			thread_reply_count INTEGER NOT NULL DEFAULT 0,
			thread_last_reply_at TEXT
		)`,
		`CREATE TABLE threads (id TEXT PRIMARY KEY)`,
		`PRAGMA foreign_keys = OFF`,
	}
	for _, ddl := range stubs {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("stub: %v", err)
		}
	}

	// Run all registered migrations in order
	migs := session.Migrations()
	for _, m := range migs {
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin tx for %s: %v", m.Name, err)
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("migrate %s: %v", m.Name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit %s: %v", m.Name, err)
		}
	}

	// Verify memory_replication_queue (V2 schema) has the expected columns
	rows, err := db.Query(`PRAGMA table_info(memory_replication_queue)`)
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		// PRAGMA table_info: cid INTEGER, name TEXT, type TEXT, notnull INTEGER, dflt_value TEXT, pk INTEGER
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("memory_replication_queue table not created")
	}
	required := []string{"id", "target_vault", "source_agent", "space_id", "concept_key", "payload"}
	for _, r := range required {
		found := false
		for _, c := range cols {
			if c == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("column %q missing from memory_replication_queue; got: %v", r, cols)
		}
	}
}

func TestMessagesTypeMigration_AllowsThreadEvent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Old schema (pre-fix): type check excludes thread_event.
	_, err = db.Exec(`
		CREATE TABLE messages (
			id TEXT NOT NULL PRIMARY KEY,
			container_type TEXT NOT NULL,
			container_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT '',
			seq INTEGER NOT NULL,
			ts TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			agent TEXT NOT NULL DEFAULT '',
			tool_name TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			tool_calls_json TEXT,
			type TEXT NOT NULL DEFAULT '' CHECK (type IN ('', 'cost')),
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0.0,
			model TEXT NOT NULL DEFAULT '',
			parent_message_id TEXT,
			triggering_message_id TEXT,
			thread_reply_count INTEGER NOT NULL DEFAULT 0,
			thread_last_reply_at TEXT,
			UNIQUE (container_id, seq)
		)`)
	if err != nil {
		t.Fatalf("create old messages table: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO messages (
			id, container_type, container_id, seq, ts, role, content, type
		) VALUES ('m1', 'session', 'sess-1', 1, '2026-01-01T00:00:00Z', 'assistant', 'hello', '')
	`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	var migrationFound bool
	for _, m := range session.Migrations() {
		if m.Name != "messages_type_thread_event_v1" {
			continue
		}
		migrationFound = true
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin migration tx: %v", err)
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("run migration: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration: %v", err)
		}
		break
	}
	if !migrationFound {
		t.Fatal("messages_type_thread_event_v1 migration not registered")
	}

	if _, err := db.Exec(`
		INSERT INTO messages (
			id, container_type, container_id, seq, ts, role, content, type,
			tool_name, tool_call_id
		) VALUES (
			'm2', 'session', 'sess-1', 2, '2026-01-01T00:00:01Z', 'assistant',
			'lifecycle event', 'thread_event', 'thread_done', 'thr-1'
		)
	`); err != nil {
		t.Fatalf("insert thread_event row after migration: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows after migration, got %d", count)
	}
}

func TestConnectionsRefreshErrorMigration_AddsColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE connections (
			id TEXT NOT NULL PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'oauth',
			account_label TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			scopes TEXT NOT NULL DEFAULT '[]',
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			expires_at TEXT
		)`)
	if err != nil {
		t.Fatalf("create old connections table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO connections(id, provider) VALUES ('c1', 'github')`); err != nil {
		t.Fatalf("seed connections: %v", err)
	}

	var migrationFound bool
	for _, m := range session.Migrations() {
		if m.Name != "connections_refresh_error_columns_v1" {
			continue
		}
		migrationFound = true
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin migration tx: %v", err)
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("run migration: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration: %v", err)
		}
		break
	}
	if !migrationFound {
		t.Fatal("connections_refresh_error_columns_v1 migration not registered")
	}

	// Ensure new columns are present and writable.
	if _, err := db.Exec(`
		UPDATE connections
		SET refresh_failed_at = '2026-01-01T00:00:00Z',
		    last_refresh_error = 'token expired'
		WHERE id = 'c1'`); err != nil {
		t.Fatalf("update new columns: %v", err)
	}

	var failedAt, lastErr string
	if err := db.QueryRow(`
		SELECT refresh_failed_at, last_refresh_error
		FROM connections WHERE id = 'c1'`).Scan(&failedAt, &lastErr); err != nil {
		t.Fatalf("select migrated columns: %v", err)
	}
	if failedAt == "" {
		t.Fatal("expected refresh_failed_at to be set")
	}
	if lastErr != "token expired" {
		t.Fatalf("last_refresh_error = %q, want %q", lastErr, "token expired")
	}
}

// TestApplySchemaThenMigrateOnLegacyDatabase reproduces the real startup
// sequence (ApplySchema, then Migrate) against a pre-bridge sessions table —
// i.e. what every existing Huginn install has on disk. This catches a bug
// that fresh-database tests structurally cannot: ApplySchema's
// `CREATE TABLE IF NOT EXISTS sessions` is a no-op against an existing
// table, so if the base schema also tried to create the
// uq_sessions_external index there, ApplySchema would fail with
// "no such column: external_kind" before migrateSessionsExternalColumnsV1
// ever got a chance to ALTER TABLE the columns in. The index must only be
// created by the migration, after the ALTER TABLE ADD COLUMN statements.
func TestApplySchemaThenMigrateOnLegacyDatabase(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// A pre-bridge sessions table: no external_kind, no external_id.
	// This is what every existing install has on disk.
	if _, err := db.Write().Exec(`
		CREATE TABLE sessions (
			id              TEXT NOT NULL PRIMARY KEY,
			tenant_id       TEXT NOT NULL DEFAULT '',
			title           TEXT NOT NULL DEFAULT '',
			model           TEXT NOT NULL DEFAULT '',
			agent           TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL DEFAULT '',
			updated_at      TEXT NOT NULL DEFAULT '',
			message_count   INTEGER NOT NULL DEFAULT 0,
			last_message_id TEXT NOT NULL DEFAULT '',
			workspace_root  TEXT NOT NULL DEFAULT '',
			workspace_name  TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'active',
			version         INTEGER NOT NULL DEFAULT 1,
			summary         TEXT,
			summary_through TEXT,
			source          TEXT NOT NULL DEFAULT '',
			routine_id      TEXT NOT NULL DEFAULT '',
			run_id          TEXT NOT NULL DEFAULT '',
			space_id        TEXT DEFAULT NULL
		)`); err != nil {
		t.Fatalf("create legacy sessions table: %v", err)
	}

	// This is the exact startup sequence: ApplySchema, then Migrate.
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema on a legacy database failed — this breaks every existing install: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("Migrate on a legacy database: %v", err)
	}

	// The migration must have added the columns...
	var n int
	if err := db.Read().QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name IN ('external_kind','external_id')`,
	).Scan(&n); err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if n != 2 {
		t.Errorf("legacy sessions table has %d of the 2 new columns after migration", n)
	}

	// ...and created the index.
	if err := db.Read().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='uq_sessions_external'`,
	).Scan(&n); err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	if n != 1 {
		t.Error("uq_sessions_external index missing after migration on a legacy database")
	}

	// Re-running startup must stay clean (idempotency).
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("second ApplySchema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

// TestMigration_RollbackOnFailure verifies that migrations are rolled back if they fail.
func TestMigration_RollbackOnFailure(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	// Apply the initial schema (this creates _migrations table)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Define a failing migration that tries to create a table twice
	failingMigration := session.Migrations()
	failingMigration = append(failingMigration, sqlitedb.Migration{
		Name: "test_migration_failure",
		Up: func(tx *sql.Tx) error {
			// Create a table
			if _, err := tx.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			// Try to create the same table again — this will fail
			if _, err := tx.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			return nil
		},
	})

	// First, apply the non-failing migrations
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("initial Migrate: %v", err)
	}

	// Record the migration count before the failing migration
	var countBefore int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&countBefore)
	if err != nil {
		t.Fatalf("query migrations before: %v", err)
	}

	// Try to apply the failing migration
	failingMigs := failingMigration[len(session.Migrations()):]
	err = db.Migrate(failingMigs)
	if err == nil {
		t.Fatal("expected migration to fail, but it succeeded")
	}
	t.Logf("migration failed as expected: %v", err)

	// Verify that the migration was NOT recorded (rolled back)
	var countAfter int
	errAfter := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&countAfter)
	if errAfter != nil {
		t.Fatalf("query migrations after: %v", errAfter)
	}

	if countAfter != countBefore {
		t.Errorf("migration count changed despite rollback: before %d, after %d", countBefore, countAfter)
	}

	// Verify that test_table was NOT created (rolled back)
	var tableExists int
	err = db.Read().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table'`,
	).Scan(&tableExists)
	if err != nil {
		t.Fatalf("query table existence: %v", err)
	}
	if tableExists > 0 {
		t.Error("test_table should not exist after failed migration (rollback)")
	}
}

// TestMigrations_Idempotent verifies that calling Migrations() and Migrate()
// twice produces no errors, and that each registered migration is recorded
// exactly once (re-running is a no-op, not a duplicate insert).
//
// Note: prior to the Claude Code bridge, Migrations() returned nil because
// all schema was squashed into ApplySchema's base DDL. It now also registers
// rolling migrations (sessions_external_columns_v1, claude_ingest_v1) so that
// databases created before the bridge gain the new columns/table — ApplySchema
// alone can't do this since CREATE TABLE IF NOT EXISTS won't add columns to
// an existing table. openSessTestDB already runs Migrations() once; this test
// re-runs them twice more to prove idempotency.
func TestMigrations_Idempotent(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	// The _migrations table should have exactly one row per registered
	// migration — re-running Migrate() must not insert duplicates.
	var count int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&count); err != nil {
		t.Fatalf("query migrations count: %v", err)
	}
	if want := len(session.Migrations()); count != want {
		t.Errorf("expected %d migration rows (one per registered migration, no duplicates), got %d", want, count)
	}
}
