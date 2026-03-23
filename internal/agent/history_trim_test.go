package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// makeMsg is a test helper that creates a backend.Message.
func makeMsg(role, content string) backend.Message {
	return backend.Message{Role: role, Content: content}
}

// TestHistoryTrimPreservesSystemMessage verifies that when the history exceeds
// maxSessionHistory, the system message at index 0 is always preserved and the
// total length is clamped to maxSessionHistory.
func TestHistoryTrimPreservesSystemMessage(t *testing.T) {
	sess := newSession("test-trim-system")

	// Prime with a system message.
	sess.appendHistory(makeMsg("system", "you are a helpful assistant"))

	// Add enough user/assistant pairs to exceed the cap.
	for i := 0; i < maxSessionHistory; i++ {
		sess.appendHistory(
			makeMsg("user", "question"),
			makeMsg("assistant", "answer"),
		)
	}

	h := sess.snapshotHistory()
	if len(h) != maxSessionHistory {
		t.Fatalf("expected history clamped to %d, got %d", maxSessionHistory, len(h))
	}
	if h[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %q", h[0].Role)
	}
}

// TestHistoryTrimNoSystemMessage verifies that when there is no system message,
// the oldest messages are simply dropped to maintain the cap.
func TestHistoryTrimNoSystemMessage(t *testing.T) {
	sess := newSession("test-trim-no-system")

	// Add enough messages to exceed the cap (no system message).
	for i := 0; i < maxSessionHistory+20; i++ {
		sess.appendHistory(makeMsg("user", "msg"))
	}

	h := sess.snapshotHistory()
	if len(h) != maxSessionHistory {
		t.Fatalf("expected history clamped to %d, got %d", maxSessionHistory, len(h))
	}
}

// TestHistoryTrimBelowCap verifies that when the history is below the cap,
// no trimming occurs and all messages are retained.
func TestHistoryTrimBelowCap(t *testing.T) {
	sess := newSession("test-no-trim")

	sess.appendHistory(makeMsg("system", "system prompt"))
	for i := 0; i < 10; i++ {
		sess.appendHistory(makeMsg("user", "q"), makeMsg("assistant", "a"))
	}

	h := sess.snapshotHistory()
	// 1 system + 20 (10 user + 10 assistant) = 21
	if len(h) != 21 {
		t.Fatalf("expected 21 messages, got %d", len(h))
	}
}

// TestHistoryTrimPreservesRecentMessages verifies that after trimming, the retained
// messages are the most recent ones (not the oldest non-system ones).
func TestHistoryTrimPreservesRecentMessages(t *testing.T) {
	sess := newSession("test-trim-recency")

	sess.appendHistory(makeMsg("system", "system prompt"))

	// Add "old" messages that should be evicted.
	for i := 0; i < maxSessionHistory; i++ {
		sess.appendHistory(makeMsg("user", "old"))
	}

	// Add one more batch: these are the "newest" and should survive.
	sess.appendHistory(makeMsg("user", "newest"))

	h := sess.snapshotHistory()
	if h[0].Role != "system" {
		t.Fatalf("first message must be system, got %q", h[0].Role)
	}
	if h[len(h)-1].Content != "newest" {
		t.Fatalf("last message should be newest, got %q", h[len(h)-1].Content)
	}
}
