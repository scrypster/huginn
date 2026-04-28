package session_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/scrypster/huginn/internal/session"
)

func TestMigrationsRegistered(t *testing.T) {
	migs := session.Migrations()
	if len(migs) != 7 {
		t.Fatalf("expected 7 migrations, got %d", len(migs))
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
		`CREATE TABLE messages (id TEXT PRIMARY KEY, ts TEXT)`,
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
