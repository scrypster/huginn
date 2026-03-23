package session_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestStore_MaxMessagesPerSession_DefaultIsSet verifies the default cap is initialized.
func TestStore_MaxMessagesPerSession_DefaultIsSet(t *testing.T) {
	store := session.NewStore(t.TempDir())
	if store.MaxMessagesPerSession <= 0 {
		t.Errorf("expected positive MaxMessagesPerSession default, got %d", store.MaxMessagesPerSession)
	}
	if store.MaxMessagesPerSession != session.DefaultMaxMessagesPerSession {
		t.Errorf("expected %d, got %d", session.DefaultMaxMessagesPerSession, store.MaxMessagesPerSession)
	}
}

// TestStore_MaxMessagesPerSession_TrimTriggersAtCap verifies that the JSONL file
// is trimmed when message count exceeds the cap.
// We use a very small cap to avoid writing thousands of messages in tests.
func TestStore_MaxMessagesPerSession_TrimTriggersAtCap(t *testing.T) {
	store := session.NewStore(t.TempDir())
	store.MaxMessagesPerSession = 50 // small cap for test speed

	sess := store.New("test-session", "/tmp", "claude-haiku-4")

	// Write 100 messages — twice the cap.
	for i := 0; i < 100; i++ {
		msg := session.SessionMessage{
			Role:    "user",
			Content: "message content",
			Ts:      time.Now().UTC(),
		}
		if err := store.Append(sess, msg); err != nil {
			t.Fatalf("Append failed at message %d: %v", i, err)
		}
	}

	// Tail should return the last min(100, MaxMessagesPerSession) messages.
	// The trim is async — give it a moment.
	time.Sleep(50 * time.Millisecond)

	msgs, err := store.TailMessages(sess.ID, session.DefaultMaxMessagesPerSession)
	if err != nil {
		t.Fatalf("TailMessages failed: %v", err)
	}
	// Should have at most MaxMessagesPerSession messages remaining.
	if len(msgs) > store.MaxMessagesPerSession {
		t.Errorf("expected at most %d messages after trim, got %d", store.MaxMessagesPerSession, len(msgs))
	}
}

// TestDefaultMaxMessagesPerSession_IsPositive verifies the constant is positive.
func TestDefaultMaxMessagesPerSession_IsPositive(t *testing.T) {
	if session.DefaultMaxMessagesPerSession <= 0 {
		t.Errorf("DefaultMaxMessagesPerSession must be positive, got %d", session.DefaultMaxMessagesPerSession)
	}
}

// TestStore_MaxMessagesPerSession_CanBeCustomized verifies custom cap can be set.
func TestStore_MaxMessagesPerSession_CanBeCustomized(t *testing.T) {
	store := session.NewStore(t.TempDir())
	store.MaxMessagesPerSession = 500
	if store.MaxMessagesPerSession != 500 {
		t.Errorf("expected MaxMessagesPerSession=500, got %d", store.MaxMessagesPerSession)
	}
}
