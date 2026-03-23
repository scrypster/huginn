package session_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// testSessionLifecycle exercises the full session lifecycle on any StoreInterface.
func testSessionLifecycle(t *testing.T, store session.StoreInterface) {
	t.Helper()

	// 1. Create a new session.
	sess := store.New("E2E Test", "/workspace/root", "test-model")
	if sess.ID == "" {
		t.Fatal("New() returned session with empty ID")
	}
	if sess.Manifest.Title != "E2E Test" {
		t.Errorf("title: want %q, got %q", "E2E Test", sess.Manifest.Title)
	}

	// Save the manifest so the session is persisted (SQLite requires this before Append).
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest (initial): %v", err)
	}

	// 2. Append 5 messages.
	roles := []string{"user", "assistant", "user", "assistant", "user"}
	for i, role := range roles {
		msg := session.SessionMessage{
			Role:    role,
			Content: roles[i],
		}
		if err := store.Append(sess, msg); err != nil {
			t.Fatalf("Append msg %d: %v", i, err)
		}
	}

	// 3. TailMessages(3) should return exactly 3 messages in order.
	tail, err := store.TailMessages(sess.ID, 3)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(tail) != 3 {
		t.Fatalf("TailMessages(3): want 3, got %d", len(tail))
	}
	// Verify ascending order by checking roles match last 3 appended.
	wantRoles := roles[2:] // ["user", "assistant", "user"]
	for i, msg := range tail {
		if msg.Role != wantRoles[i] {
			t.Errorf("tail[%d].Role: want %q, got %q", i, wantRoles[i], msg.Role)
		}
	}

	// 4. SaveManifest → Load → verify fields match.
	sess.Manifest.Title = "Updated Title"
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest (update): %v", err)
	}
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Title != "Updated Title" {
		t.Errorf("loaded Title: want %q, got %q", "Updated Title", loaded.Manifest.Title)
	}
	if loaded.Manifest.Model != "test-model" {
		t.Errorf("loaded Model: want %q, got %q", "test-model", loaded.Manifest.Model)
	}
	if loaded.Manifest.WorkspaceRoot != "/workspace/root" {
		t.Errorf("loaded WorkspaceRoot: want %q, got %q", "/workspace/root", loaded.Manifest.WorkspaceRoot)
	}

	// 5. Exists(id) → true.
	if !store.Exists(sess.ID) {
		t.Error("Exists: want true, got false")
	}

	// 6. Delete(id).
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 7. Exists(id) → false.
	if store.Exists(sess.ID) {
		t.Error("Exists after Delete: want false, got true")
	}
}

func TestE2E_SessionLifecycle_Filesystem(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	testSessionLifecycle(t, store)
}

func TestE2E_SessionLifecycle_SQLite(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	testSessionLifecycle(t, store)
}
