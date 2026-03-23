package session_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

func TestAppendToThread(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)

	sess := store.New("", "", "")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}

	msg := session.SessionMessage{
		Role:    "assistant",
		Content: "hello from thread",
		Ts:      time.Now(),
	}
	if err := store.AppendToThread(sess.ID, "thread-42", msg); err != nil {
		t.Fatalf("AppendToThread: %v", err)
	}

	// File should exist at the correct path
	threadPath := filepath.Join(dir, sess.ID, "thread-thread-42.jsonl")
	if _, err := os.Stat(threadPath); os.IsNotExist(err) {
		t.Fatalf("expected thread JSONL at %s", threadPath)
	}

	// TailThreadMessages should return the appended message
	msgs, err := store.TailThreadMessages(sess.ID, "thread-42", 10)
	if err != nil {
		t.Fatalf("TailThreadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello from thread" {
		t.Errorf("expected 'hello from thread', got %q", msgs[0].Content)
	}
}

func TestAppendToThreadMultiple(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("", "", "")
	_ = store.SaveManifest(sess)

	for i := 0; i < 5; i++ {
		_ = store.AppendToThread(sess.ID, "t1", session.SessionMessage{
			Role:    "assistant",
			Content: "msg",
		})
	}

	msgs, _ := store.TailThreadMessages(sess.ID, "t1", 3)
	if len(msgs) != 3 {
		t.Errorf("tail(3) of 5 messages: expected 3, got %d", len(msgs))
	}

	all, _ := store.TailThreadMessages(sess.ID, "t1", 100)
	if len(all) != 5 {
		t.Errorf("tail(100) of 5 messages: expected 5, got %d", len(all))
	}
}

func TestTailThreadMessagesNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("", "", "")
	_ = store.SaveManifest(sess)

	// Should return nil, nil for a thread that has no JSONL file yet
	msgs, err := store.TailThreadMessages(sess.ID, "no-such-thread", 10)
	if err != nil {
		t.Fatalf("expected no error for non-existent thread: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil for non-existent thread, got %v", msgs)
	}
}

func TestListThreadIDs(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("", "", "")
	_ = store.SaveManifest(sess)

	_ = store.AppendToThread(sess.ID, "t1", session.SessionMessage{Role: "user", Content: "a"})
	_ = store.AppendToThread(sess.ID, "t2", session.SessionMessage{Role: "user", Content: "b"})
	_ = store.AppendToThread(sess.ID, "t2", session.SessionMessage{Role: "assistant", Content: "c"})

	ids, err := store.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 thread IDs, got %d: %v", len(ids), ids)
	}
	// Both t1 and t2 should be present
	found := make(map[string]bool)
	for _, id := range ids {
		found[id] = true
	}
	if !found["t1"] || !found["t2"] {
		t.Errorf("expected t1 and t2 in list, got %v", ids)
	}
}

func TestListThreadIDsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("", "", "")
	_ = store.SaveManifest(sess)

	ids, err := store.ListThreadIDs(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

func TestAppendToThreadAssignsID(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("", "", "")
	_ = store.SaveManifest(sess)

	msg := session.SessionMessage{Role: "user", Content: "test"}
	_ = store.AppendToThread(sess.ID, "t1", msg)

	msgs, _ := store.TailThreadMessages(sess.ID, "t1", 10)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	if msgs[0].ID == "" {
		t.Error("expected AppendToThread to assign a message ID")
	}
}
