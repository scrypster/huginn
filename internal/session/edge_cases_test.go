package session

import (
	"testing"
	"time"
)

// TestTailMessages_ZeroN verifies that TailMessages(id, 0) returns an empty
// slice (not all messages). Previously, len(all) <= 0 was only true when all
// was empty, so a non-empty JSONL file would return all its messages.
func TestTailMessages_ZeroN(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("edge-case test", "/ws", "model")
	for i := 0; i < 5; i++ {
		if err := store.Append(sess, SessionMessage{
			Role:    "user",
			Content: "msg",
			Ts:      time.Now(),
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	msgs, err := store.TailMessages(sess.ID, 0)
	if err != nil {
		t.Fatalf("TailMessages(0) error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("TailMessages(0): expected 0 messages, got %d", len(msgs))
	}
}

// TestTailMessages_NegativeN verifies that TailMessages with a negative n
// returns an empty slice, consistent with the n=0 behavior.
func TestTailMessages_NegativeN(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("negative-n test", "/ws", "model")
	if err := store.Append(sess, SessionMessage{
		Role:    "user",
		Content: "hello",
		Ts:      time.Now(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	msgs, err := store.TailMessages(sess.ID, -1)
	if err != nil {
		t.Fatalf("TailMessages(-1) error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("TailMessages(-1): expected 0 messages, got %d", len(msgs))
	}
}

// TestTailThreadMessages_ZeroN verifies that TailThreadMessages(sid, tid, 0)
// returns an empty slice rather than all messages.
func TestTailThreadMessages_ZeroN(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := store.New("thread-zero-n", "/ws", "model")
	for i := 0; i < 3; i++ {
		if err := store.AppendToThread(sess.ID, "thread1", SessionMessage{
			Role:    "user",
			Content: "msg",
		}); err != nil {
			t.Fatalf("AppendToThread: %v", err)
		}
	}

	msgs, err := store.TailThreadMessages(sess.ID, "thread1", 0)
	if err != nil {
		t.Fatalf("TailThreadMessages(0) error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("TailThreadMessages(0): expected 0 messages, got %d", len(msgs))
	}
}

// TestReadLastN_ZeroN verifies that ReadLastN(sessionID, 0) returns an empty
// slice rather than all persisted messages.
func TestReadLastN_ZeroN(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a session directory with some persisted messages.
	id := "session-readlastn-zero"
	ps := &PersistentSession{
		ID:        id,
		Title:     "test",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   1,
	}
	if err := store.Create(ps); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := store.AppendMessage(id, &PersistedMessage{
			ID:      NewID(),
			Role:    "user",
			Content: "hello",
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	msgs, err := store.ReadLastN(id, 0)
	if err != nil {
		t.Fatalf("ReadLastN(0) error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("ReadLastN(0): expected 0 messages, got %d", len(msgs))
	}
}
