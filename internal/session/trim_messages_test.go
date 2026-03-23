package session

// Iteration 2 hardening tests for the session package.
// Covers weaknesses in trimMessagesIfNeeded and thread operations.

import (
	"fmt"
	"testing"
)

// Hardening: trimMessagesIfNeeded actually trims when count exceeds cap.
// Weakness: the function runs in a background goroutine and its code paths
// were not directly tested; encode failures could leave temp files on disk.
func TestStore_TrimMessagesIfNeeded_ActuallyTrims(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	store.MaxMessagesPerSession = 10 // small cap for testing

	sess := store.New("trim test", "/ws", "model")

	// Append messages up to 2× the cap to trigger a trim.
	for i := 0; i < 20; i++ {
		msg := SessionMessage{Role: "user", Content: fmt.Sprintf("message %d", i)}
		if err := store.Append(sess, msg); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Force an explicit trim (trimMessagesIfNeeded normally runs async; call sync here).
	store.trimMessagesIfNeeded(sess.ID)

	// After trim, at most `cap` messages should be on disk.
	msgs, err := store.TailMessages(sess.ID, 100)
	if err != nil {
		t.Fatalf("TailMessages after trim: %v", err)
	}
	if len(msgs) > store.MaxMessagesPerSession {
		t.Errorf("want at most %d messages after trim, got %d", store.MaxMessagesPerSession, len(msgs))
	}
	// Messages should be in FIFO order (oldest removed, newest kept).
	if len(msgs) > 0 && msgs[len(msgs)-1].Content != "message 19" {
		t.Errorf("want last message to be 'message 19' (newest), got %q", msgs[len(msgs)-1].Content)
	}
}

// Hardening: trimMessagesIfNeeded with invalid session ID logs but doesn't panic.
func TestStore_TrimMessagesIfNeeded_InvalidID_NoOp(t *testing.T) {
	store := NewStore(t.TempDir())
	// Should not panic even with non-existent / bad session
	store.trimMessagesIfNeeded("nonexistent-session-xyz")
}

// Hardening: AppendToThread rejects invalid sessionID.
// Weakness: without validation, path-traversal IDs could escape baseDir.
func TestStore_AppendToThread_InvalidSessionID_Rejected(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.AppendToThread("../evil", "threadID", SessionMessage{Role: "user", Content: "x"})
	if err == nil {
		t.Error("want error for path-traversal sessionID, got nil")
	}
}

// Hardening: AppendToThread rejects invalid threadID.
func TestStore_AppendToThread_InvalidThreadID_Rejected(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("validate thread", "/ws", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	err := store.AppendToThread(sess.ID, "../../hack", SessionMessage{Role: "user", Content: "x"})
	if err == nil {
		t.Error("want error for path-traversal threadID, got nil")
	}
}

// Hardening: ListThreadIDs returns empty slice (not nil) when session has no threads.
func TestStore_ListThreadIDs_NoThreads_EmptySlice(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("no threads", "/ws", "model")
	_ = store.SaveManifest(sess)

	ids, err := store.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ids == nil {
		t.Error("want empty slice, got nil")
	}
	if len(ids) != 0 {
		t.Errorf("want 0 thread IDs, got %d", len(ids))
	}
}

// Hardening: TailThreadMessages returns nil, nil for non-existent thread (not an error).
func TestStore_TailThreadMessages_NonExistentThread_NilNil(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := store.New("no threads", "/ws", "model")
	_ = store.SaveManifest(sess)

	msgs, err := store.TailThreadMessages(sess.ID, "ghost-thread", 100)
	if err != nil {
		t.Errorf("want nil error for non-existent thread, got %v", err)
	}
	if msgs != nil {
		t.Errorf("want nil messages for non-existent thread, got %v", msgs)
	}
}
