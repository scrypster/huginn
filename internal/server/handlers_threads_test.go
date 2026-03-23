package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// insertTestMessage inserts a message row directly into the messages table
// for use in thread handler tests. It mirrors the minimal columns needed.
func insertTestMessage(t *testing.T, db interface {
	Write() interface{ Exec(string, ...any) (interface{}, error) }
}, id, containerID string, seq int64, role, content, agent, parentMessageID string, threadReplyCount int) {
	t.Helper()
	// We use the SQLiteSessionStore helpers via the store interface instead of
	// direct SQL to stay independent of the schema migration state.
	// This helper is intentionally left as a stub — test setup uses AppendToThread.
}

func TestGetMessageThread_Empty(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/nonexistent/thread", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d items", len(result))
	}
}

func TestGetMessageThread_WithReplies(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	// Insert a parent session so foreign-key constraints (if any) are met.
	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("thread-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Insert parent message with ID "parent-1".
	parentMsg := session.SessionMessage{
		ID:      "parent-1",
		Role:    "user",
		Content: "parent message",
		Ts:      time.Now().UTC(),
	}
	if err := sqliteStore.Append(sess, parentMsg); err != nil {
		t.Fatalf("append parent: %v", err)
	}

	// Insert two reply messages with parent_message_id = "parent-1".
	// We use the raw write DB since the StoreInterface doesn't expose
	// parent_message_id directly (that column is added by the workforce migration).
	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}
	for i, content := range []string{"reply 1", "reply 2"} {
		id := "reply-" + string(rune('A'+i))
		_, err := wdb.Exec(`
			INSERT OR IGNORE INTO messages
				(id, container_type, container_id, seq, ts, role, content, agent,
				 tool_name, tool_call_id, type,
				 prompt_tokens, completion_tokens, cost_usd, model,
				 parent_message_id, thread_reply_count)
			VALUES (?, 'session', ?, ?, ?, 'assistant', ?, '', '', '', '', 0, 0, 0.0, '', 'parent-1', 0)`,
			id, sess.ID, int64(10+i), time.Now().UTC().Format(time.RFC3339), content,
		)
		if err != nil {
			t.Fatalf("insert reply %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/parent-1/thread", nil)
	req.SetPathValue("id", "parent-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(result))
	}
	// Verify ordering by seq ASC.
	if result[0].Seq >= result[1].Seq {
		t.Errorf("expected ascending seq order, got %d then %d", result[0].Seq, result[1].Seq)
	}
	if result[0].Content != "reply 1" {
		t.Errorf("expected first reply content 'reply 1', got %q", result[0].Content)
	}
}

func TestGetContainerThreads_NoThreads(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("container-thread-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Append a regular message (no thread_reply_count).
	msg := session.SessionMessage{
		ID:      "msg-no-thread",
		Role:    "user",
		Content: "hello",
		Ts:      time.Now().UTC(),
	}
	if err := sqliteStore.Append(sess, msg); err != nil {
		t.Fatalf("append: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+sess.ID+"/threads", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array for container with no threads, got %d items", len(result))
	}
}

func TestGetMessageThread_NilDB(t *testing.T) {
	srv := testServer(t)
	// db is nil (default in newTestServer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/some-id/thread", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty), got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	json.NewDecoder(w.Body).Decode(&result) //nolint:errcheck
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

// TestGetContainerThreads_ReplyAgentName is the regression test for the badge
// label bug: GET /api/v1/containers/{id}/threads must return the REPLYING agent's
// name (Sam), not the parent message's agent (Tom). Previously the SQL used the
// parent message's agent column, causing the badge to show "Tom" instead of "Sam".
func TestGetContainerThreads_ReplyAgentName(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("reply-agent-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}

	ts := time.Now().UTC().Format(time.RFC3339)

	// Parent message by "Tom" (the lead agent).
	_, err := wdb.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content, agent,
			 tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model,
			 parent_message_id, thread_reply_count)
		VALUES (?, 'session', ?, 1, ?, 'assistant', 'Delegating to Sam.', 'Tom',
		        '', '', '', 0, 0, 0.0, '', '', 1)`,
		"parent-tom", sess.ID, ts,
	)
	if err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	// Thread reply by "Sam" (the delegate agent) with parent_message_id = parent-tom.
	_, err = wdb.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content, agent,
			 tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model,
			 parent_message_id, thread_reply_count)
		VALUES (?, 'session', ?, 2, ?, 'assistant', 'Analysis complete.', 'Sam',
		        '', '', '', 0, 0, 0.0, '', 'parent-tom', 0)`,
		"reply-sam", sess.ID, ts,
	)
	if err != nil {
		t.Fatalf("insert reply: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/"+sess.ID+"/threads", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 thread root, got %d", len(result))
	}
	// Agent field must be Sam (the replying agent), not Tom (the parent message author).
	if result[0].Agent != "Sam" {
		t.Errorf("thread root Agent = %q, want \"Sam\" (the replying delegate agent, not the parent message author)", result[0].Agent)
	}
	if result[0].ID != "parent-tom" {
		t.Errorf("thread root ID = %q, want \"parent-tom\"", result[0].ID)
	}
}

func TestGetContainerThreads_NilDB(t *testing.T) {
	srv := testServer(t)
	// db is nil (default in newTestServer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/some-id/threads", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (empty), got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	json.NewDecoder(w.Body).Decode(&result) //nolint:errcheck
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}
