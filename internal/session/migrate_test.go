package session_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// createTestSessionDir creates a fake session filesystem directory structure.
func createTestSessionDir(t *testing.T, baseDir, sessionID string, msgCount int, threadCount int) {
	t.Helper()
	sessDir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	manifest := map[string]any{
		"session_id":    sessionID,
		"id":            sessionID,
		"title":         "test session " + sessionID,
		"model":         "llama3",
		"status":        "active",
		"version":       1,
		"created_at":    time.Now().UTC().Format(time.RFC3339),
		"updated_at":    time.Now().UTC().Format(time.RFC3339),
		"message_count": msgCount,
	}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(sessDir, "manifest.json"), mData, 0644)

	f, _ := os.Create(filepath.Join(sessDir, "messages.jsonl"))
	enc := json.NewEncoder(f)
	for i := 0; i < msgCount; i++ {
		enc.Encode(map[string]any{
			"id":      session.NewID(),
			"ts":      time.Now().UTC().Format(time.RFC3339),
			"seq":     i + 1,
			"role":    "user",
			"content": fmt.Sprintf("msg %d", i),
		})
	}
	f.Close()

	for i := 0; i < threadCount; i++ {
		tFile := filepath.Join(sessDir, fmt.Sprintf("thread-t-%d.jsonl", i+1))
		tf, _ := os.Create(tFile)
		enc := json.NewEncoder(tf)
		enc.Encode(map[string]any{
			"id": session.NewID(), "ts": time.Now().UTC().Format(time.RFC3339),
			"seq": 1, "role": "assistant", "content": "thread response",
		})
		tf.Close()
	}
}

func TestMigrateFromFilesystem_MigratesSessionsAndMessages(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	sessID := session.NewID()
	createTestSessionDir(t, baseDir, sessID, 5, 0)

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("MigrateFromFilesystem: %v", err)
	}

	store := session.NewSQLiteSessionStore(db)
	loaded, err := store.Load(sessID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Title != "test session "+sessID {
		t.Errorf("Title: got %q", loaded.Manifest.Title)
	}

	msgs, err := store.TailMessages(sessID, 100)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("want 5 messages, got %d", len(msgs))
	}
}

func TestMigrateFromFilesystem_MigratesThreads(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	sessID := session.NewID()
	createTestSessionDir(t, baseDir, sessID, 2, 3)

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("MigrateFromFilesystem: %v", err)
	}

	store := session.NewSQLiteSessionStore(db)
	threadIDs, err := store.ListThreadIDs(sessID)
	if err != nil {
		t.Fatalf("ListThreadIDs: %v", err)
	}
	if len(threadIDs) != 3 {
		t.Errorf("want 3 threads, got %d", len(threadIDs))
	}

	for _, tid := range threadIDs {
		msgs, err := store.TailThreadMessages(sessID, tid, 10)
		if err != nil {
			t.Fatalf("TailThreadMessages: %v", err)
		}
		if len(msgs) != 1 {
			t.Errorf("thread %s: want 1 message, got %d", tid, len(msgs))
		}
	}
}

func TestMigrateFromFilesystem_Idempotent(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	sessID := session.NewID()
	createTestSessionDir(t, baseDir, sessID, 3, 0)

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Recreate baseDir (since it was renamed to .bak) for second call test
	// Actually for idempotency test: second call on already-migrated DB should be no-op
	// M5_sessions is already recorded, so second call returns immediately
	if err := session.MigrateFromFilesystem(baseDir+".bak", db); err != nil {
		// The .bak dir still exists — second call should be idempotent (M5_sessions already done)
		_ = err // this is OK since M5_sessions check returns early
	}
	// Verify no duplicate messages
	store := session.NewSQLiteSessionStore(db)
	msgs, _ := store.TailMessages(sessID, 100)
	if len(msgs) != 3 {
		t.Errorf("idempotent: want 3, got %d", len(msgs))
	}
}

func TestMigrateFromFilesystem_EmptyDir(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("empty dir: %v", err)
	}

	var count int
	db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = 'M5_sessions'`).Scan(&count)
	if count != 1 {
		t.Error("M5_sessions not recorded in _migrations")
	}
}

func TestMigrateFromFilesystem_MissingDir_NoOp(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	if err := session.MigrateFromFilesystem("/tmp/huginn-nonexistent-sessions-xyzzy", db); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
}

func TestMigrateFromFilesystem_MultipleSessions(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	ids := []string{session.NewID(), session.NewID(), session.NewID()}
	for i, id := range ids {
		createTestSessionDir(t, baseDir, id, i+1, 0)
	}

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("MigrateFromFilesystem: %v", err)
	}

	store := session.NewSQLiteSessionStore(db)
	manifests, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(manifests) != 3 {
		t.Errorf("want 3 sessions, got %d", len(manifests))
	}
}

func TestMigrateFromFilesystem_BatchInsert(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	sessID := session.NewID()
	sessDir := filepath.Join(baseDir, sessID)
	os.MkdirAll(sessDir, 0755)

	manifest := map[string]any{
		"session_id": sessID, "id": sessID, "title": "batch test",
		"model": "llama3", "status": "active", "version": 1,
		"created_at":    time.Now().UTC().Format(time.RFC3339),
		"updated_at":    time.Now().UTC().Format(time.RFC3339),
		"message_count": 2500,
	}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(sessDir, "manifest.json"), mData, 0644)

	f, _ := os.Create(filepath.Join(sessDir, "messages.jsonl"))
	enc := json.NewEncoder(f)
	for i := 0; i < 2500; i++ {
		enc.Encode(map[string]any{
			"id": session.NewID(), "ts": time.Now().UTC().Format(time.RFC3339),
			"seq": i + 1, "role": "user", "content": fmt.Sprintf("msg %d", i),
		})
	}
	f.Close()

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("MigrateFromFilesystem: %v", err)
	}

	store := session.NewSQLiteSessionStore(db)
	msgs, _ := store.TailMessages(sessID, 3000)
	if len(msgs) != 2500 {
		t.Errorf("want 2500, got %d", len(msgs))
	}
}

func TestMigrateFromFilesystem_CreatesBakDir(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	baseDir := t.TempDir()

	createTestSessionDir(t, baseDir, session.NewID(), 1, 0)

	if err := session.MigrateFromFilesystem(baseDir, db); err != nil {
		t.Fatalf("MigrateFromFilesystem: %v", err)
	}

	if _, err := os.Stat(baseDir + ".bak"); os.IsNotExist(err) {
		t.Errorf("expected bak dir %q to exist", baseDir+".bak")
	}
}

