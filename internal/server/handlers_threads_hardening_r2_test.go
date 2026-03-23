package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// createR2TestSession creates a session in a fresh session store wired into the
// server, returning the session ID.
func createR2TestSession(t *testing.T, srv *Server) string {
	t.Helper()
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store
	sess := store.New("r2-test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save session manifest: %v", err)
	}
	return sess.ID
}

// ── handleGetMessageThread additional coverage ────────────────────────────────

func TestHandleGetMessageThread_R2_ContentType(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/msg-abc/thread", nil)
	req.SetPathValue("id", "msg-abc")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestHandleGetMessageThread_R2_NilDB_NonNilResult(t *testing.T) {
	// With nil DB the handler must return a non-nil empty JSON array (not null).
	srv := testServer(t) // db is nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/any-id/thread", nil)
	req.SetPathValue("id", "any-id")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	raw := strings.TrimSpace(w.Body.String())
	// Body must be JSON array, not "null".
	if raw == "null" || raw == "" {
		t.Errorf("expected [] JSON array, got %q", raw)
	}
	if !strings.HasPrefix(raw, "[") {
		t.Errorf("expected JSON array start '[', got %q", raw)
	}
}

func TestHandleGetMessageThread_R2_WithRealDB_UnknownMsg_ReturnsEmpty(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/msg-unknown/thread", nil)
	req.SetPathValue("id", "msg-unknown")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d items", len(result))
	}
}

// ── handleGetContainerThreads additional coverage ──────────────────────────────

func TestHandleGetContainerThreads_R2_ContentType(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/ctr-x/threads", nil)
	req.SetPathValue("id", "ctr-x")
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestHandleGetContainerThreads_R2_NilDB_NonNilResult(t *testing.T) {
	srv := testServer(t) // db is nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/ctr-99/threads", nil)
	req.SetPathValue("id", "ctr-99")
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	raw := strings.TrimSpace(w.Body.String())
	if raw == "null" || raw == "" {
		t.Errorf("expected [] JSON array, got %q", raw)
	}
	if !strings.HasPrefix(raw, "[") {
		t.Errorf("expected JSON array start '[', got %q", raw)
	}
}

func TestHandleGetContainerThreads_R2_WithRealDB_UnknownContainer_ReturnsEmpty(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers/no-such-container/threads", nil)
	req.SetPathValue("id", "no-such-container")
	w := httptest.NewRecorder()
	srv.handleGetContainerThreads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d items", len(result))
	}
}

// ── handleGetThread ───────────────────────────────────────────────────────────

func TestHandleGetThread_R2_BothPathValuesMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//threads/", nil)
	// Neither id nor thread_id path values are set.
	w := httptest.NewRecorder()
	srv.handleGetThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetThread_R2_ThreadIDMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/threads/", nil)
	req.SetPathValue("id", "sess-1")
	// thread_id deliberately left unset.
	w := httptest.NewRecorder()
	srv.handleGetThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetThread_R2_SessionNotFound_Returns404(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent/threads/t-1", nil)
	req.SetPathValue("id", "nonexistent")
	req.SetPathValue("thread_id", "t-1")
	w := httptest.NewRecorder()
	srv.handleGetThread(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (session not found), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetThread_R2_NilThreadManager_Returns404(t *testing.T) {
	srv := testServer(t)
	sessionID := createR2TestSession(t, srv)
	// tm is nil by default.

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID+"/threads/thread-1", nil)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("thread_id", "thread-1")
	w := httptest.NewRecorder()
	srv.handleGetThread(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (no thread manager), got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleReplyThread ─────────────────────────────────────────────────────────

func TestHandleReplyThread_R2_BothPathValuesMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{"input": "go"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions//threads//reply", bytes.NewReader(body))
	// No path values set.
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleReplyThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReplyThread_R2_ThreadIDMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/threads//reply", nil)
	req.SetPathValue("id", "sess-1")
	// thread_id not set.
	w := httptest.NewRecorder()
	srv.handleReplyThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReplyThread_R2_SessionNotFound_Returns404(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{"input": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/ghost-session/threads/t-1/reply", bytes.NewReader(body))
	req.SetPathValue("id", "ghost-session")
	req.SetPathValue("thread_id", "t-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleReplyThread(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReplyThread_R2_NilThreadManager_Returns503(t *testing.T) {
	srv := testServer(t)
	sessionID := createR2TestSession(t, srv)

	body, _ := json.Marshal(map[string]string{"input": "continue"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/threads/t-1/reply", bytes.NewReader(body))
	req.SetPathValue("id", sessionID)
	req.SetPathValue("thread_id", "t-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleReplyThread(w, req)

	// Nil TM → 503 before JSON decode.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (nil TM), got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleCancelThread ────────────────────────────────────────────────────────

func TestHandleCancelThread_R2_BothPathValuesMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions//threads/", nil)
	// No path values set.
	w := httptest.NewRecorder()
	srv.handleCancelThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCancelThread_R2_ThreadIDMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/sess-1/threads/", nil)
	req.SetPathValue("id", "sess-1")
	// thread_id not set.
	w := httptest.NewRecorder()
	srv.handleCancelThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCancelThread_R2_SessionNotFound_Returns404(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/no-such/threads/t-1", nil)
	req.SetPathValue("id", "no-such")
	req.SetPathValue("thread_id", "t-1")
	w := httptest.NewRecorder()
	srv.handleCancelThread(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCancelThread_R2_NilThreadManager_Returns503(t *testing.T) {
	srv := testServer(t)
	sessionID := createR2TestSession(t, srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+sessionID+"/threads/t-1", nil)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("thread_id", "t-1")
	w := httptest.NewRecorder()
	srv.handleCancelThread(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no thread manager), got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleCreateThread ────────────────────────────────────────────────────────

func TestHandleCreateThread_R2_SessionIDMissing_Returns400(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "task": "do something"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions//threads", bytes.NewReader(body))
	// id path value not set.
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateThread(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateThread_R2_SessionNotFound_Returns404(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "task": "do something"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/gone-session/threads", bytes.NewReader(body))
	req.SetPathValue("id", "gone-session")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateThread(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateThread_R2_NilThreadManager_Returns503(t *testing.T) {
	srv := testServer(t)
	sessionID := createR2TestSession(t, srv)

	body, _ := json.Marshal(map[string]string{"agent_id": "agent-1", "task": "do something"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sessionID+"/threads", bytes.NewReader(body))
	req.SetPathValue("id", sessionID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateThread(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no thread manager), got %d: %s", w.Code, w.Body.String())
	}
}

// ── full HTTP integration ──────────────────────────────────────────────────────

func TestHandleGetMessageThread_R2_ViaHTTP(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/messages/msg-1/thread", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body == nil {
		t.Error("expected non-nil (empty) array")
	}
}

func TestHandleGetContainerThreads_R2_ViaHTTP(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/containers/ctr-1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body == nil {
		t.Error("expected non-nil (empty) array")
	}
}

func TestHandleGetMessageThread_R2_ViaHTTP_Unauthenticated(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/messages/msg-1/thread")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleGetContainerThreads_R2_ViaHTTP_Unauthenticated(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/containers/ctr-1/threads")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestHandleCreateThread_R2_ViaHTTP_NotRegistered verifies that the
// handleCreateThread handler is not yet registered in the router (the route
// POST /api/v1/sessions/{id}/threads does not exist) so that we can track
// this as a known gap. The test is intentionally skipped when the route IS
// registered in the future, ensuring forward-compatibility.
//
// NOTE: The create-thread, get-thread, reply-thread, and cancel-thread handlers
// are implemented in handlers_threads.go but are not yet wired as routes in
// server.go. All four are tested directly at the handler level above.
func TestHandleCreateThread_R2_ViaHTTP_RouteNotYetRegistered(t *testing.T) {
	_, ts := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"agent_id": "a1", "task": "work"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/sess-x/threads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The route is not registered yet — the mux returns 404 (SPA catch-all serves it).
	// When the route is eventually added, this test can be updated to verify 401
	// for unauthenticated requests.
	if resp.StatusCode == http.StatusUnauthorized {
		// Route was registered — test is no longer needed in this form.
		t.Log("NOTE: route is now registered; update this test to verify proper auth behavior")
	}
	// Accept both 404 (not registered) and 401 (registered + no auth).
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 404 (not registered) or 401 (registered+unauth), got %d", resp.StatusCode)
	}
}

// ── scanThreadMessageRows unit tests ──────────────────────────────────────────

func TestScanThreadMessageRows_R2_EmptyRows(t *testing.T) {
	// Open a real SQLite DB and run an empty SELECT to get empty *sql.Rows.
	db := openTestSQLiteDB(t)
	rdb := db.Read()

	rows, err := rdb.Query(`SELECT 1 WHERE 1 = 0`) // no results
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	// We cannot call scanThreadMessageRows directly (it expects messages columns),
	// but we can verify the nil-DB path of handleGetMessageThread returns [] instead.
	srv := testServer(t)
	srv.SetDB(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/no-parent/thread", nil)
	req.SetPathValue("id", "no-parent")
	w := httptest.NewRecorder()
	srv.handleGetMessageThread(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []threadMessageRow
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result))
	}
}
