package session_test

// sqlite_seq_regression_test.go — regression tests for SQLiteSessionStore
// sequential message persistence.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// newTestSQLiteStore creates a fresh SQLiteSessionStore backed by a temp DB.
func newTestSQLiteStore(t *testing.T) *session.SQLiteSessionStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return session.NewSQLiteSessionStore(db)
}

// TestSQLiteStore_MultiTurnPersistence verifies that a second Load+Append pair
// correctly uses the next seq number and does not silently drop messages.
//
// Regression: SQLiteSessionStore.Load returned a *Session with seq=0.
// Each Append used atomic.AddInt64(&sess.seq, 1) starting from 0, so turn-2
// produced seq=1 again — a duplicate that INSERT OR IGNORE silently dropped
// due to the UNIQUE (container_id, seq) constraint.
func TestSQLiteStore_MultiTurnPersistence(t *testing.T) {
	t.Parallel()
	s := newTestSQLiteStore(t)

	sess := s.New("test-multi-turn", "/workspace", "test-model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// --- Turn 1 ---
	sess1, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load (turn 1): %v", err)
	}
	now := time.Now().UTC()
	if err := s.Append(sess1, session.SessionMessage{
		ID: session.NewID(), Role: "user", Content: "hello", Ts: now,
	}); err != nil {
		t.Fatalf("Append user turn 1: %v", err)
	}
	if err := s.Append(sess1, session.SessionMessage{
		ID: session.NewID(), Role: "assistant", Content: "Hello! How can I help?", Ts: now,
	}); err != nil {
		t.Fatalf("Append assistant turn 1: %v", err)
	}

	// --- Turn 2 (simulate a second WS chat message: Load again, then Append) ---
	sess2, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load (turn 2): %v", err)
	}
	now2 := time.Now().UTC()
	if err := s.Append(sess2, session.SessionMessage{
		ID: session.NewID(), Role: "user", Content: "what is your name?", Ts: now2,
	}); err != nil {
		t.Fatalf("Append user turn 2: %v", err)
	}
	if err := s.Append(sess2, session.SessionMessage{
		ID: session.NewID(), Role: "assistant", Content: "I am Huginn.", Ts: now2,
	}); err != nil {
		t.Fatalf("Append assistant turn 2: %v", err)
	}

	// All 4 messages must persist.
	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d — turn-2 messages were silently dropped (seq collision)", len(msgs))
	}

	// Verify content in chronological order.
	expected := []struct{ role, content string }{
		{"user", "hello"},
		{"assistant", "Hello! How can I help?"},
		{"user", "what is your name?"},
		{"assistant", "I am Huginn."},
	}
	for i, ex := range expected {
		if msgs[i].Role != ex.role {
			t.Errorf("msgs[%d].Role = %q, want %q", i, msgs[i].Role, ex.role)
		}
		if msgs[i].Content != ex.content {
			t.Errorf("msgs[%d].Content = %q, want %q", i, msgs[i].Content, ex.content)
		}
	}
}

// TestSQLiteStore_LoadInitializesSeqFromDB verifies that Load sets sess.seq
// to the current max seq so that the first Append after a fresh Load uses
// the correct next sequence number.
func TestSQLiteStore_LoadInitializesSeqFromDB(t *testing.T) {
	t.Parallel()
	s := newTestSQLiteStore(t)

	sess := s.New("seq-init-test", "/workspace", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Append two messages via initial session object.
	now := time.Now().UTC()
	_ = s.Append(sess, session.SessionMessage{ID: session.NewID(), Role: "user", Content: "a", Ts: now})
	_ = s.Append(sess, session.SessionMessage{ID: session.NewID(), Role: "assistant", Content: "b", Ts: now})

	// Reload: seq should be initialized to 2 (max from DB), not 0.
	reloaded, err := s.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Append a third message; it must use seq=3 (not seq=1).
	if err := s.Append(reloaded, session.SessionMessage{
		ID: session.NewID(), Role: "user", Content: "third", Ts: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append third message: %v", err)
	}

	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d — third message was dropped (seq not initialized from DB)", len(msgs))
	}
	if msgs[2].Content != "third" {
		t.Errorf("expected msgs[2].Content='third', got %q", msgs[2].Content)
	}
}
