package session_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/scrypster/huginn/internal/session"
)

func TestMigrationsRegistered(t *testing.T) {
	migs := session.Migrations()
	if len(migs) != 10 {
		t.Fatalf("expected 10 migrations, got %d", len(migs))
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
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			space_id TEXT,
			title TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			last_message_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
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

func TestMessageCountBackfill_RecoversFrozenSessions(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			last_message_id TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE messages (
			id TEXT NOT NULL PRIMARY KEY,
			container_type TEXT NOT NULL,
			container_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			ts TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, title, created_at, updated_at, message_count, last_message_id)
		VALUES ('sess-1', 'frozen', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 0, '')
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO messages (id, container_type, container_id, seq, ts) VALUES
			('m1', 'session', 'sess-1', 1, '2026-01-01T00:01:00Z'),
			('m2', 'session', 'sess-1', 2, '2026-01-01T00:02:00Z'),
			('t1', 'thread', 'thr-1', 1, '2026-01-01T00:03:00Z')
	`); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	var migrationFound bool
	for _, m := range session.Migrations() {
		if m.Name != "sessions_message_count_backfill_v1" {
			continue
		}
		migrationFound = true
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("backfill: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		break
	}
	if !migrationFound {
		t.Fatal("sessions_message_count_backfill_v1 migration not registered")
	}

	var count int
	var lastID, updatedAt string
	if err := db.QueryRow(`
		SELECT message_count, last_message_id, updated_at FROM sessions WHERE id = 'sess-1'
	`).Scan(&count, &lastID, &updatedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count != 2 {
		t.Fatalf("message_count = %d, want 2 (thread rows must not count)", count)
	}
	if lastID != "m2" {
		t.Fatalf("last_message_id = %q, want m2", lastID)
	}
	if updatedAt != "2026-01-01T00:02:00Z" {
		t.Fatalf("updated_at = %q, want last session message ts", updatedAt)
	}
}
