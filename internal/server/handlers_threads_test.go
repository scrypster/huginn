package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

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
	var result MessageThreadResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected empty array, got %d items", len(result.Messages))
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

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}

	// Create a thread row linking to parent-1, matching the current query
	// pattern: handleGetMessageThread looks for container_type='thread'
	// messages via threads.parent_msg_id.
	threadID := "thread-1"
	_, err := wdb.Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id, created_at, files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'Sam', 'test task', 'done',
		        'parent-1', ?, '[]', '[]', '[]')`,
		threadID, sess.ID, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	// Insert two thread-scoped reply messages (container_type='thread').
	for i, content := range []string{"reply 1", "reply 2"} {
		if err := sqliteStore.AppendToThread(sess.ID, threadID, session.SessionMessage{
			Role:    "assistant",
			Content: content,
			Agent:   "Sam",
		}); err != nil {
			t.Fatalf("append thread reply %d: %v", i, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/parent-1/thread", nil)
	req.SetPathValue("id", "parent-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result MessageThreadResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(result.Messages))
	}
	// Verify ordering by seq ASC.
	if result.Messages[0].Seq >= result.Messages[1].Seq {
		t.Errorf("expected ascending seq order, got %d then %d", result.Messages[0].Seq, result.Messages[1].Seq)
	}
	if result.Messages[0].Content != "reply 1" {
		t.Errorf("expected first reply content 'reply 1', got %q", result.Messages[0].Content)
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
	var result MessageThreadResponse
	json.NewDecoder(w.Body).Decode(&result) //nolint:errcheck
	if len(result.Messages) != 0 {
		t.Errorf("expected empty, got %d", len(result.Messages))
	}
}

// TestGetContainerThreads_ReplyAgentName is the regression test for the badge
// label bug: GET /api/v1/containers/{id}/threads must return the REPLYING agent's
// name (Sam, from the threads table), not the parent message's agent (Tom).
// The SQL LEFT JOINs threads.agent_name to override the message's agent column.
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

	// Parent message by "Tom" (the lead agent) with thread_reply_count = 1.
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

	// Thread row linking to parent-tom, with agent_name = "Sam" (the delegate).
	// This is how the real code creates threads via SaveThread.
	_, err = wdb.Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id, created_at, files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'Sam', 'Analyze data', 'done',
		        'parent-tom', ?, '[]', '[]', '[]')`,
		"thread-sam-1", sess.ID, ts,
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
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
	// Agent field must be Sam (from threads.agent_name), not Tom (the parent message author).
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

func TestHandleGetMessageThread_ResponseShape(t *testing.T) {
	// Server with no DB — exercises the nil-DB fast path.
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/msg-1/thread", nil)
	req.SetPathValue("id", "msg-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Messages        []any    `json:"messages"`
		DelegationChain []string `json:"delegation_chain"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// delegation_chain must be [] not null
	if resp.DelegationChain == nil {
		t.Errorf("delegation_chain is nil, want empty array []")
	}
	if len(resp.DelegationChain) != 0 {
		t.Errorf("expected delegation_chain length 0, got %d", len(resp.DelegationChain))
	}
}

// stubDelegationStore is a test double for session.DelegationStore.
type stubDelegationStore struct {
	listBySession []session.DelegationRecord
}

func (s *stubDelegationStore) InsertDelegation(d session.DelegationRecord) error {
	return nil
}

func (s *stubDelegationStore) GetDelegation(id string) (*session.DelegationRecord, error) {
	return nil, sql.ErrNoRows
}

func (s *stubDelegationStore) FindDelegationByThread(threadID string) (*session.DelegationRecord, error) {
	return nil, sql.ErrNoRows
}

func (s *stubDelegationStore) ListDelegationsBySession(sessionID string, limit, offset int) ([]session.DelegationRecord, error) {
	return s.listBySession, nil
}

func (s *stubDelegationStore) UpdateDelegationStatus(id, status, result string, startedAt, completedAt *time.Time) error {
	return nil
}

func (s *stubDelegationStore) ReconcileOrphanDelegations() error {
	return nil
}

// TestHandleGetMessageThread_IncludesDelegationChain tests that the delegation chain
// is correctly resolved from delegations in the session and returned in the response.
func TestHandleGetMessageThread_IncludesDelegationChain(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	// Wire a stub delegation store with one delegation record.
	stub := &stubDelegationStore{
		listBySession: []session.DelegationRecord{
			{
				ID:        "del-1",
				SessionID: "sess-1",
				ThreadID:  "t-1",
				FromAgent: "Atlas",
				ToAgent:   "Coder",
				Task:      "implement feature",
				Status:    "completed",
				Result:    "success",
				CreatedAt: time.Now().UTC(),
				StartedAt: time.Now().UTC(),
			},
		},
	}
	srv.delegationStore = stub

	// Insert a threads row.
	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := wdb.Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id, created_at, files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'Atlas', 'test task', 'done',
		        'msg-1', ?, '[]', '[]', '[]')`,
		"t-1", "sess-1", ts,
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	// Call the handler.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/msg-1/thread", nil)
	req.SetPathValue("id", "msg-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	// Assert response.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp MessageThreadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ThreadID != "t-1" {
		t.Errorf("ThreadID: expected 't-1', got %q", resp.ThreadID)
	}
	if resp.SessionID != "sess-1" {
		t.Errorf("SessionID: expected 'sess-1', got %q", resp.SessionID)
	}
	if len(resp.DelegationChain) != 1 {
		t.Errorf("DelegationChain length: expected 1, got %d", len(resp.DelegationChain))
	}
	if len(resp.DelegationChain) > 0 && resp.DelegationChain[0] != "Coder" {
		t.Errorf("DelegationChain[0]: expected 'Coder', got %q", resp.DelegationChain[0])
	}
}

func TestGetMessageThread_ReturnsToolCalls(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("tc-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	parentMsg := session.SessionMessage{
		ID:      "parent-tc-1",
		Role:    "user",
		Content: "do work",
		Ts:      time.Now().UTC(),
	}
	if err := sqliteStore.Append(sess, parentMsg); err != nil {
		t.Fatalf("append parent: %v", err)
	}

	wdb := db.Write()
	if wdb == nil {
		t.Fatal("write db is nil")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := wdb.Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id, created_at, files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'Atlas', 'test', 'done',
		        'parent-tc-1', ?, '[]', '[]', '[]')`,
		"t-tc-1", sess.ID, ts,
	)
	if err != nil {
		t.Fatalf("insert thread: %v", err)
	}

	toolCallsJSON := `[{"id":"tc-1","name":"bash","args":{"cmd":"echo hi"},"result":"hi"}]`
	_, err = wdb.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content,
			 agent, tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model,
			 tool_calls_json)
		VALUES (?, 'thread', ?, 1, ?, 'assistant', 'I ran bash', 'Atlas',
		        '', '', '', 0, 0, 0.0, '', ?)`,
		"reply-tc-1", "t-tc-1", ts, toolCallsJSON,
	)
	if err != nil {
		t.Fatalf("insert reply: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/parent-tc-1/thread", nil)
	req.SetPathValue("id", "parent-tc-1")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result MessageThreadResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if len(result.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on message, got %d", len(result.Messages[0].ToolCalls))
	}
	if result.Messages[0].ToolCalls[0].Name != "bash" {
		t.Errorf("expected tool call name 'bash', got %q", result.Messages[0].ToolCalls[0].Name)
	}
}
