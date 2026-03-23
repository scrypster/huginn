// content_limit_test.go tests the 64 KB message content limit enforced by both
// store implementations (filesystem-backed Store and SQLite-backed SQLiteSessionStore).
package session_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// maxTestContent is exactly the 64 KB boundary.
const maxTestContent = 64 * 1024

// --- SQLite store ---

func TestSQLiteStore_Append_AtLimit(t *testing.T) {
	t.Parallel()
	db := openSQLiteDBForContentTest(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("limit-test", "/ws", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	msg := session.SessionMessage{Role: "user", Content: strings.Repeat("a", maxTestContent)}
	if err := s.Append(sess, msg); err != nil {
		t.Fatalf("Append at exactly 64 KB: unexpected error: %v", err)
	}
}

func TestSQLiteStore_Append_OverLimit(t *testing.T) {
	t.Parallel()
	db := openSQLiteDBForContentTest(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("over-limit-test", "/ws", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	msg := session.SessionMessage{Role: "user", Content: strings.Repeat("b", maxTestContent+1)}
	err := s.Append(sess, msg)
	if err == nil {
		t.Fatal("Append 1 byte over 64 KB limit: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected error to mention 'exceeds', got: %v", err)
	}
}

// --- Filesystem store ---

func TestFSStore_Append_AtLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := session.NewStore(filepath.Join(dir, "sessions"))

	sess := s.New("limit-test-fs", "/ws", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	msg := session.SessionMessage{Role: "user", Content: strings.Repeat("x", maxTestContent)}
	if err := s.Append(sess, msg); err != nil {
		t.Fatalf("Append at exactly 64 KB (fs): unexpected error: %v", err)
	}
}

func TestFSStore_Append_OverLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := session.NewStore(filepath.Join(dir, "sessions"))

	sess := s.New("over-limit-test-fs", "/ws", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	msg := session.SessionMessage{Role: "user", Content: strings.Repeat("y", maxTestContent+1)}
	err := s.Append(sess, msg)
	if err == nil {
		t.Fatal("Append 1 byte over 64 KB limit (fs): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected error to mention 'exceeds', got: %v", err)
	}
}

// openSQLiteDBForContentTest opens an isolated SQLite DB with the session schema applied.
func openSQLiteDBForContentTest(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
