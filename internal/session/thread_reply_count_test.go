package session_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestAppendToThread_UpdatesReplyCount verifies that Append with a
// ParentMessageID increments thread_reply_count on the parent message and that
// TailMessages returns the parent with thread_reply_count > 0 so badges
// survive page refresh via GET /api/v1/containers/{id}/threads.
func TestAppendToThread_UpdatesReplyCount(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	// Create and save a session.
	sess := s.New("Thread Count Test", "/workspace", "claude-sonnet-4-6")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Append a root assistant message (this is the parent for thread replies).
	parentID := session.NewID()
	if err := s.Append(sess, session.SessionMessage{
		ID:      parentID,
		Role:    "assistant",
		Content: "Delegating to Sam for analysis.",
		Agent:   "Tom",
		Ts:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append parent: %v", err)
	}

	// Append a thread reply that references the parent.
	replyID := session.NewID()
	if err := s.Append(sess, session.SessionMessage{
		ID:              replyID,
		Role:            "assistant",
		Content:         "Sam's analysis complete.",
		Agent:           "Sam",
		ParentMessageID: parentID,
		Ts:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append reply: %v", err)
	}

	// TailMessages should return the parent with thread_reply_count = 0 in the
	// message scan (TailMessages doesn't return the count, but the DB update
	// ensures it is persisted). Verify via direct DB query.
	rdb := db.Read()
	if rdb == nil {
		t.Fatal("db.Read() returned nil")
	}
	var replyCount int
	err := rdb.QueryRow(
		`SELECT COALESCE(thread_reply_count, 0) FROM messages WHERE id = ?`, parentID,
	).Scan(&replyCount)
	if err != nil {
		t.Fatalf("query thread_reply_count: %v", err)
	}
	if replyCount != 1 {
		t.Errorf("thread_reply_count: want 1, got %d", replyCount)
	}

	// A second reply should increment to 2.
	if err := s.Append(sess, session.SessionMessage{
		ID:              session.NewID(),
		Role:            "assistant",
		Content:         "Sam follow-up.",
		Agent:           "Sam",
		ParentMessageID: parentID,
		Ts:              time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append second reply: %v", err)
	}

	if err := rdb.QueryRow(
		`SELECT COALESCE(thread_reply_count, 0) FROM messages WHERE id = ?`, parentID,
	).Scan(&replyCount); err != nil {
		t.Fatalf("query thread_reply_count (2): %v", err)
	}
	if replyCount != 2 {
		t.Errorf("thread_reply_count after 2 replies: want 2, got %d", replyCount)
	}

	// Non-reply messages must NOT appear in TailMessages (parent_message_id filter).
	msgs, err := s.TailMessages(sess.ID, 50)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	for _, m := range msgs {
		if m.ID == replyID {
			t.Errorf("TailMessages returned thread reply %s — should be filtered out by parent_message_id", replyID)
		}
	}

	// TailMessages must populate ThreadReplyCount for the parent message.
	var parentMsg *session.SessionMessage
	for i := range msgs {
		if msgs[i].ID == parentID {
			parentMsg = &msgs[i]
			break
		}
	}
	if parentMsg == nil {
		t.Fatal("parent message not found in TailMessages result")
	}
	if parentMsg.ThreadReplyCount != 2 {
		t.Errorf("TailMessages ThreadReplyCount for parent: want 2, got %d", parentMsg.ThreadReplyCount)
	}
}

// TestGetThreadReplyCounts_NonZeroWhenRepliesExist verifies that
// GetThreadReplyCounts returns a non-zero count for messages that have
// thread replies, confirming the method works end-to-end via the SQLite store.
func TestGetThreadReplyCounts_NonZeroWhenRepliesExist(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("Reply Count Test", "/workspace", "claude-sonnet-4-6")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Root message.
	parentID := session.NewID()
	if err := s.Append(sess, session.SessionMessage{
		ID:      parentID,
		Role:    "user",
		Content: "root message",
		Ts:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append parent: %v", err)
	}

	// Two thread replies.
	for i := 0; i < 2; i++ {
		if err := s.Append(sess, session.SessionMessage{
			ID:              session.NewID(),
			Role:            "assistant",
			Content:         "reply",
			ParentMessageID: parentID,
			Ts:              time.Now().UTC(),
		}); err != nil {
			t.Fatalf("Append reply %d: %v", i, err)
		}
	}

	counts, err := s.GetThreadReplyCounts(sess.ID)
	if err != nil {
		t.Fatalf("GetThreadReplyCounts: %v", err)
	}
	if counts[parentID] != 2 {
		t.Errorf("GetThreadReplyCounts[parentID] = %d, want 2", counts[parentID])
	}
}

// TestGetThreadReplyCounts_EmptyWhenNoReplies verifies that
// GetThreadReplyCounts returns an empty map when no thread replies exist.
func TestGetThreadReplyCounts_EmptyWhenNoReplies(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("No Replies Test", "/workspace", "claude-sonnet-4-6")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if err := s.Append(sess, session.SessionMessage{
		ID:      session.NewID(),
		Role:    "user",
		Content: "lonely message",
		Ts:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	counts, err := s.GetThreadReplyCounts(sess.ID)
	if err != nil {
		t.Fatalf("GetThreadReplyCounts: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("GetThreadReplyCounts: want empty map, got %v", counts)
	}
}
