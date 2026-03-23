package session_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openSessTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil { t.Fatalf("sqlitedb.Open: %v", err) }
	if err := db.ApplySchema(); err != nil { t.Fatalf("ApplySchema: %v", err) }
	if err := db.Migrate(session.Migrations()); err != nil { t.Fatalf("Migrate: %v", err) }
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteSession_New_SaveManifest_Load(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("My Session", "/workspace", "qwen3:30b")
	if sess.ID == "" { t.Fatal("New() returned session with empty ID") }
	if sess.Manifest.Title != "My Session" { t.Errorf("Title: want My Session, got %q", sess.Manifest.Title) }

	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := s.Load(sess.ID)
	if err != nil { t.Fatalf("Load: %v", err) }
	if loaded.Manifest.Title != "My Session" {
		t.Errorf("Title: want My Session, got %q", loaded.Manifest.Title)
	}
	if loaded.Manifest.Model != "qwen3:30b" {
		t.Errorf("Model: want qwen3:30b, got %q", loaded.Manifest.Model)
	}
	if loaded.Manifest.WorkspaceRoot != "/workspace" {
		t.Errorf("WorkspaceRoot: want /workspace, got %q", loaded.Manifest.WorkspaceRoot)
	}
}

func TestSQLiteSession_Load_NotFound(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	_, err := s.Load("nonexistent-id")
	if err == nil { t.Fatal("expected error for nonexistent session, got nil") }
}

func TestSQLiteSession_Exists(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("test", "", "")
	if ok := s.Exists(sess.ID); ok { t.Error("Exists should return false before SaveManifest") }
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }
	if ok := s.Exists(sess.ID); !ok { t.Error("Exists should return true after SaveManifest") }
}

func TestSQLiteSession_Delete(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("delete me", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }
	if err := s.Delete(sess.ID); err != nil { t.Fatalf("Delete: %v", err) }
	if ok := s.Exists(sess.ID); ok { t.Error("session should not exist after Delete") }
}

func TestSQLiteSession_List(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	for i, title := range []string{"first", "second", "third"} {
		sess := s.New(title, "", "")
		sess.Manifest.UpdatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest %d: %v", i, err) }
	}

	manifests, err := s.List()
	if err != nil { t.Fatalf("List: %v", err) }
	if len(manifests) != 3 { t.Fatalf("want 3, got %d", len(manifests)) }
	if manifests[0].Title != "third" {
		t.Errorf("want third first (newest), got %q", manifests[0].Title)
	}
}

func TestSQLiteSession_List_Empty(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	manifests, err := s.List()
	if err != nil { t.Fatalf("List empty: %v", err) }
	if len(manifests) != 0 { t.Errorf("want 0, got %d", len(manifests)) }
}

func TestSQLiteSession_LoadOrReconstruct(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("recon", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	loaded, err := s.LoadOrReconstruct(sess.ID)
	if err != nil { t.Fatalf("LoadOrReconstruct: %v", err) }
	if loaded.ID != sess.ID { t.Errorf("ID mismatch") }
}

func TestSQLiteSession_Append_TailMessages(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("msg test", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	for i := 0; i < 5; i++ {
		msg := session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		}
		if err := s.Append(sess, msg); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	msgs, err := s.TailMessages(sess.ID, 3)
	if err != nil { t.Fatalf("TailMessages: %v", err) }
	if len(msgs) != 3 { t.Fatalf("want 3 tail messages, got %d", len(msgs)) }
	// TailMessages returns last N in chronological order.
	if msgs[2].Content != "message 4" {
		t.Errorf("last message: want message 4, got %q", msgs[2].Content)
	}
}

func TestSQLiteSession_TailMessages_Empty(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("empty msgs", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	msgs, err := s.TailMessages(sess.ID, 50)
	if err != nil { t.Fatalf("TailMessages: %v", err) }
	if len(msgs) != 0 { t.Errorf("want 0, got %d", len(msgs)) }
}

func TestSQLiteSession_Append_UpdatesMessageCount(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("count test", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	for i := 0; i < 3; i++ {
		if err := s.Append(sess, session.SessionMessage{Role: "user", Content: "hi"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if sess.Manifest.MessageCount != 3 {
		t.Errorf("MessageCount: want 3, got %d", sess.Manifest.MessageCount)
	}
}

func TestSQLiteSession_Append_PreservesFields(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("fields test", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	msg := session.SessionMessage{
		Role:       "tool",
		Content:    "result",
		Agent:      "my-agent",
		ToolName:   "bash",
		ToolCallID: "tc-1",
		PromptTok:  10,
		CompTok:    20,
		CostUSD:    0.001,
		ModelName:  "qwen3:30b",
	}
	if err := s.Append(sess, msg); err != nil { t.Fatalf("Append: %v", err) }

	msgs, err := s.TailMessages(sess.ID, 10)
	if err != nil { t.Fatalf("TailMessages: %v", err) }
	if len(msgs) != 1 { t.Fatalf("want 1, got %d", len(msgs)) }
	got := msgs[0]
	if got.Agent != "my-agent" { t.Errorf("Agent: want my-agent, got %q", got.Agent) }
	if got.ToolName != "bash" { t.Errorf("ToolName: want bash, got %q", got.ToolName) }
	if got.PromptTok != 10 { t.Errorf("PromptTok: want 10, got %d", got.PromptTok) }
}

func TestSQLiteSession_AppendToThread_TailThreadMessages(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("thread test", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	threadID := session.NewID()
	for i := 0; i < 5; i++ {
		msg := session.SessionMessage{Role: "assistant", Content: fmt.Sprintf("thread msg %d", i)}
		if err := s.AppendToThread(sess.ID, threadID, msg); err != nil {
			t.Fatalf("AppendToThread %d: %v", i, err)
		}
	}

	msgs, err := s.TailThreadMessages(sess.ID, threadID, 3)
	if err != nil { t.Fatalf("TailThreadMessages: %v", err) }
	if len(msgs) != 3 { t.Fatalf("want 3, got %d", len(msgs)) }
}

func TestSQLiteSession_TailThreadMessages_Missing(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("thread missing", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	// Tailing a thread that doesn't exist returns nil, nil (not an error).
	msgs, err := s.TailThreadMessages(sess.ID, "nonexistent-thread", 10)
	if err != nil { t.Fatalf("TailThreadMessages missing: %v", err) }
	if msgs != nil { t.Errorf("want nil, got %v", msgs) }
}

func TestSQLiteSession_ListThreadIDs(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("thread list", "", "")
	if err := s.SaveManifest(sess); err != nil { t.Fatalf("SaveManifest: %v", err) }

	ids := []string{session.NewID(), session.NewID(), session.NewID()}
	for _, tid := range ids {
		if err := s.AppendToThread(sess.ID, tid, session.SessionMessage{Role: "user", Content: "hi"}); err != nil {
			t.Fatalf("AppendToThread: %v", err)
		}
	}

	listed, err := s.ListThreadIDs(sess.ID)
	if err != nil { t.Fatalf("ListThreadIDs: %v", err) }
	if len(listed) != 3 { t.Fatalf("want 3 thread IDs, got %d", len(listed)) }
}

func TestSQLiteSession_Create_LoadManifest(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	id := session.NewID()
	ps := &session.PersistentSession{
		ID:        id,
		Title:     "persist test",
		Model:     "llama3",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
		Status:    "active",
		Version:   1,
	}
	if err := s.Create(ps); err != nil { t.Fatalf("Create: %v", err) }

	loaded, err := s.LoadManifest(id)
	if err != nil { t.Fatalf("LoadManifest: %v", err) }
	if loaded.Title != "persist test" { t.Errorf("Title: want persist test, got %q", loaded.Title) }
}

func TestSQLiteSession_AppendMessage_ReadMessages(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	id := session.NewID()
	ps := &session.PersistentSession{ID: id, Title: "msg test", Status: "active", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.Create(ps); err != nil { t.Fatalf("Create: %v", err) }

	for i := 0; i < 5; i++ {
		msg := &session.PersistedMessage{
			ID:   session.NewID(),
			Ts:   time.Now().UTC().Format(time.RFC3339),
			Seq:  int64(i + 1),
			Role: "user",
			Content: fmt.Sprintf("persisted msg %d", i),
		}
		if err := s.AppendMessage(id, msg); err != nil { t.Fatalf("AppendMessage %d: %v", i, err) }
	}

	msgs, err := s.ReadMessages(id)
	if err != nil { t.Fatalf("ReadMessages: %v", err) }
	if len(msgs) != 5 { t.Fatalf("want 5, got %d", len(msgs)) }

	last, err := s.ReadLastN(id, 2)
	if err != nil { t.Fatalf("ReadLastN: %v", err) }
	if len(last) != 2 { t.Fatalf("want 2, got %d", len(last)) }
}

func TestSQLiteSession_UpdateManifest(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	id := session.NewID()
	ps := &session.PersistentSession{ID: id, Title: "update test", Status: "active", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.Create(ps); err != nil { t.Fatalf("Create: %v", err) }

	if err := s.UpdateManifest(id, func(m *session.PersistentSession) {
		m.Title = "updated title"
		m.Status = "closed"
	}); err != nil { t.Fatalf("UpdateManifest: %v", err) }

	loaded, err := s.LoadManifest(id)
	if err != nil { t.Fatalf("LoadManifest: %v", err) }
	if loaded.Title != "updated title" { t.Errorf("Title: want updated title, got %q", loaded.Title) }
	if loaded.Status != "closed" { t.Errorf("Status: want closed, got %q", loaded.Status) }
}

// TestSQLiteSession_Exists_ScanErrorReturnsFalse verifies that Exists() returns false
// (not panics or silently corrupts state) when the underlying query fails.
// We force a scan error by closing the DB before calling Exists.
func TestSQLiteSession_Exists_ScanErrorReturnsFalse(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	// Verify Exists works normally before closing.
	sess := s.New("scan-err-test", "", "")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if ok := s.Exists(sess.ID); !ok {
		t.Fatal("Exists should return true before DB close")
	}

	// Close the DB to induce a query/scan error on the next call.
	db.Close()

	// Exists must return false (not panic) when the DB is closed.
	got := s.Exists(sess.ID)
	if got {
		t.Error("Exists should return false when the DB query fails (closed DB)")
	}
}

func TestSQLiteSession_RepairJSONL_NoOp(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	id := session.NewID()
	ps := &session.PersistentSession{ID: id, Title: "repair test", Status: "active", Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.Create(ps); err != nil { t.Fatalf("Create: %v", err) }

	count, err := s.RepairJSONL(id)
	if err != nil { t.Fatalf("RepairJSONL: %v", err) }
	_ = count
}
