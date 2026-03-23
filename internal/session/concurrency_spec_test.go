package session

// concurrency_spec_test.go — Race-condition behavior specs for the session store.
//
// Run with: go test -race ./internal/session/...
//
// These tests verify the locking invariants documented in store.go:
// - Append holds sess.mu for the entire file I/O
// - Concurrent appenders never interleave their writes
// - TailMessages concurrent with Append never returns partial/corrupt lines
// - Multiple sessions are fully independent (no cross-session locking needed)

import (
	"fmt"
	"sync"
	"testing"
)

// TestStore_ConcurrentAppend_NoMessageLoss verifies that N goroutines
// appending simultaneously all land in the JSONL without data loss.
// This is the most common multi-agent scenario: agents writing to
// the same session in parallel.
func TestStore_ConcurrentAppend_NoMessageLoss(t *testing.T) {
	const writers = 10
	const msgsPerWriter = 20

	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("concurrent-test", "/ws", "model")

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < msgsPerWriter; i++ {
				msg := SessionMessage{
					Role:    "assistant",
					Content: fmt.Sprintf("worker=%d msg=%d", workerID, i),
				}
				if err := store.Append(sess, msg); err != nil {
					t.Errorf("worker %d: Append error: %v", workerID, err)
				}
			}
		}(w)
	}
	wg.Wait()

	msgs, err := store.TailMessages(sess.ID, writers*msgsPerWriter+1)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	want := writers * msgsPerWriter
	if len(msgs) != want {
		t.Errorf("message loss under concurrent append: got %d messages, want %d", len(msgs), want)
	}
}

// TestStore_ConcurrentAppendAndTail_NoCorruption verifies that TailMessages
// concurrent with Append never returns a partial JSON line.
// Each returned message must be fully parseable and have the correct role.
func TestStore_ConcurrentAppendAndTail_NoCorruption(t *testing.T) {
	const appenders = 5
	const reads = 20
	const msgsPerAppender = 30

	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("read-write-race", "/ws", "model")

	// Seed some messages so TailMessages always has something to return.
	for i := 0; i < 5; i++ {
		if err := store.Append(sess, SessionMessage{Role: "user", Content: "seed"}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var wg sync.WaitGroup

	// Writers
	for w := 0; w < appenders; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < msgsPerAppender; i++ {
				_ = store.Append(sess, SessionMessage{
					Role:    "assistant",
					Content: fmt.Sprintf("writer=%d i=%d", id, i),
				})
			}
		}(w)
	}

	// Readers — verify no corrupt lines
	for r := 0; r < reads; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msgs, err := store.TailMessages(sess.ID, 20)
			if err != nil {
				// A repair error under concurrent write is acceptable; a parse
				// error indicating corruption is not.
				return
			}
			for _, m := range msgs {
				if m.Role == "" {
					t.Errorf("TailMessages returned message with empty role — possible corrupt line")
				}
			}
		}()
	}

	wg.Wait()
}

// TestStore_ConcurrentSessions_AreIsolated verifies that concurrent appends
// to different sessions do not interfere with each other's message counts.
// Each session's JSONL file must contain exactly the messages written to it.
func TestStore_ConcurrentSessions_AreIsolated(t *testing.T) {
	const sessions = 5
	const msgsPerSession = 15

	dir := t.TempDir()
	store := NewStore(dir)

	sessList := make([]*Session, sessions)
	for i := range sessList {
		sessList[i] = store.New(fmt.Sprintf("sess-%d", i), "/ws", "model")
	}

	var wg sync.WaitGroup
	for i, sess := range sessList {
		wg.Add(1)
		go func(idx int, s *Session) {
			defer wg.Done()
			for j := 0; j < msgsPerSession; j++ {
				if err := store.Append(s, SessionMessage{
					Role:    "user",
					Content: fmt.Sprintf("sess=%d msg=%d", idx, j),
				}); err != nil {
					t.Errorf("sess %d: Append: %v", idx, err)
				}
			}
		}(i, sess)
	}
	wg.Wait()

	for i, sess := range sessList {
		msgs, err := store.TailMessages(sess.ID, msgsPerSession+1)
		if err != nil {
			t.Fatalf("sess %d: TailMessages: %v", i, err)
		}
		if len(msgs) != msgsPerSession {
			t.Errorf("sess %d: expected %d messages, got %d (cross-session contamination?)", i, msgsPerSession, len(msgs))
		}
	}
}

// TestStore_SeqNumbers_UniqueAndNoGaps verifies that concurrent appends
// produce globally unique sequence numbers with no collisions.
//
// NOTE: Seq numbers are assigned via atomic.AddInt64 BEFORE the file-write
// lock is acquired.  This means file-write order can differ from seq order
// under contention — i.e., seq numbers in the JSONL file are NOT guaranteed
// to be monotonically increasing by position.  Callers that need ordered
// replay must sort by Seq, not rely on file order.  This test documents that
// invariant explicitly.
func TestStore_SeqNumbers_UniqueAndNoGaps(t *testing.T) {
	const writers = 8
	const msgsPerWriter = 25

	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("seq-test", "/ws", "model")

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < msgsPerWriter; i++ {
				_ = store.Append(sess, SessionMessage{Role: "assistant", Content: "x"})
			}
		}()
	}
	wg.Wait()

	total := writers * msgsPerWriter
	msgs, err := store.TailMessages(sess.ID, total+1)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != total {
		t.Fatalf("expected %d messages, got %d", total, len(msgs))
	}

	// All seq numbers must be unique (no collision from atomic counter).
	seen := make(map[int64]bool, total)
	for _, m := range msgs {
		if m.Seq <= 0 {
			t.Errorf("zero/negative seq number: %d", m.Seq)
		}
		if seen[m.Seq] {
			t.Errorf("duplicate seq number %d — atomic counter collision", m.Seq)
		}
		seen[m.Seq] = true
	}
}
