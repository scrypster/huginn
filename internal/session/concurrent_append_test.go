package session_test

import (
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// TestSQLiteStore_ConcurrentAppend_Ordering verifies that concurrent appends
// from multiple goroutines do not lose messages. It launches 10 goroutines
// each appending 10 messages and checks that all 100 messages are stored.
func TestSQLiteStore_ConcurrentAppend_Ordering(t *testing.T) {
	t.Parallel()

	db := openSessTestDB(t)
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store := session.NewSQLiteSessionStore(db)

	sess := store.New("concurrent-test", "/tmp/ws", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	const goroutines = 10
	const msgsPerGoroutine = 10
	const totalMessages = goroutines * msgsPerGoroutine

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gIdx int) {
			defer wg.Done()
			for m := 0; m < msgsPerGoroutine; m++ {
				msg := session.SessionMessage{
					Role:    "user",
					Content: "msg from goroutine",
				}
				if err := store.Append(sess, msg); err != nil {
					t.Errorf("goroutine %d, msg %d: Append: %v", gIdx, m, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Load all messages and verify count.
	msgs, err := store.TailMessages(sess.ID, totalMessages+10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}

	if len(msgs) != totalMessages {
		t.Errorf("expected %d messages, got %d", totalMessages, len(msgs))
	}

	// Verify all messages have unique IDs.
	seen := make(map[string]struct{}, len(msgs))
	for _, msg := range msgs {
		if msg.ID == "" {
			t.Error("message has empty ID")
			continue
		}
		if _, dup := seen[msg.ID]; dup {
			t.Errorf("duplicate message ID: %s", msg.ID)
		}
		seen[msg.ID] = struct{}{}
	}

	// Verify all seq values are unique (ordering may be non-deterministic
	// across goroutines, but each seq should appear exactly once).
	seqSeen := make(map[int64]struct{}, len(msgs))
	for _, msg := range msgs {
		if _, dup := seqSeen[msg.Seq]; dup {
			t.Errorf("duplicate seq: %d", msg.Seq)
		}
		seqSeen[msg.Seq] = struct{}{}
	}
}
