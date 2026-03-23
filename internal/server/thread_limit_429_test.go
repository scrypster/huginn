package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestHandleCreateThread_LimitReached_Returns429 verifies that when the active
// thread count for a session has reached the configured maximum,
// handleCreateThread returns HTTP 429 with a structured JSON body:
//
//	{"code":"thread_limit_reached","active_count":<n>,"max_threads":20,"message":"..."}
func TestHandleCreateThread_LimitReached_Returns429(t *testing.T) {
	srv, _ := newTestServer(t)

	// Wire the agent loader so the handler can validate the agent_id.
	const agentName = "TestAgent"
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: agentName},
		}}, nil
	}

	// Create and wire a session so the handler can load it.
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store
	sess := store.New("limit-test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save session manifest: %v", err)
	}

	// Wire a ThreadManager with MaxThreadsPerSession = 1.
	tm := threadmgr.New()
	tm.MaxThreadsPerSession = 1
	srv.tm = tm

	// Pre-create one thread to fill the limit (SessionID must match).
	_, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess.ID,
		AgentID:   agentName,
		Task:      "existing task",
	})
	if err != nil {
		t.Fatalf("pre-create thread: %v", err)
	}
	// Confirm the limit is already hit.
	if tm.ActiveCount(sess.ID) != 1 {
		t.Fatalf("expected 1 active thread before test, got %d", tm.ActiveCount(sess.ID))
	}

	// POST a new thread — this should now return 429.
	body, _ := json.Marshal(map[string]any{
		"agent_id": agentName,
		"task":     "a second task",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/threads",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateThread(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}

	// Verify the structured response fields.
	if resp["code"] != "thread_limit_reached" {
		t.Errorf("code = %q, want thread_limit_reached", resp["code"])
	}
	if _, ok := resp["active_count"]; !ok {
		t.Error("active_count field missing from 429 response")
	}
	if _, ok := resp["max_threads"]; !ok {
		t.Error("max_threads field missing from 429 response")
	}
	if msg, _ := resp["message"].(string); msg == "" {
		t.Error("message field should be non-empty in 429 response")
	}
}
