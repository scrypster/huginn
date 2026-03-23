package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// ── handleSessionActiveState (GET /api/v1/sessions/{id}/active-state) ─────────

func TestHandleSessionActiveState_r2_EmptySessionID(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//active-state", nil)
	// PathValue("id") returns "" when not set.
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSessionActiveState_r2_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/does-not-exist/active-state", nil)
	req.SetPathValue("id", "does-not-exist")
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSessionActiveState_r2_ValidSession_NoDB(t *testing.T) {
	srv := testServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("r2-active-state", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result sessionActiveState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != sess.ID {
		t.Errorf("session_id: expected %q, got %q", sess.ID, result.SessionID)
	}
	// Without DB, seq defaults to 0.
	if result.LastSeq != 0 {
		t.Errorf("expected last_seq=0 (no DB), got %d", result.LastSeq)
	}
	// ActiveThreads should be an initialised empty slice, not nil.
	if result.ActiveThreads == nil {
		t.Error("expected non-nil active_threads slice")
	}
	if len(result.ActiveThreads) != 0 {
		t.Errorf("expected 0 active threads for empty session, got %d", len(result.ActiveThreads))
	}
	// InFlightTasks must be non-nil.
	if result.InFlightTasks == nil {
		t.Error("expected non-nil in_flight_tasks slice")
	}
}

func TestHandleSessionActiveState_r2_ValidSession_WithSQLiteDB(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("r2-active-state-db", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = sqliteStore

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result sessionActiveState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != sess.ID {
		t.Errorf("session_id: expected %q, got %q", sess.ID, result.SessionID)
	}
	// Fresh session has no messages.
	if result.LastSeq != 0 {
		t.Errorf("expected last_seq=0 for empty session, got %d", result.LastSeq)
	}
	if len(result.ActiveThreads) != 0 {
		t.Errorf("expected 0 active threads, got %d", len(result.ActiveThreads))
	}
}

func TestHandleSessionActiveState_r2_ResponseShape(t *testing.T) {
	// Verify the JSON response always contains all required top-level keys.
	srv := testServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("shape-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"session_id", "active_threads", "last_seq", "in_flight_tasks"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q; got: %v", key, raw)
		}
	}
}

func TestHandleSessionActiveState_r2_ContentTypeIsJSON(t *testing.T) {
	srv := testServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("ct-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header to be set")
	}
}

// ── handleActiveState (not registered in routes, tested directly) ────────────

func TestHandleActiveState_r2_BasicResponse(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/active-state", nil)
	w := httptest.NewRecorder()
	srv.handleActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result ActiveStateResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// threads_running defaults to 0 when tm is nil.
	if result.ThreadsRunning != 0 {
		t.Errorf("expected threads_running=0 with nil tm, got %d", result.ThreadsRunning)
	}
	// last_activity_at should be set.
	if result.LastActivityAt.IsZero() {
		t.Error("expected last_activity_at to be set")
	}
}

func TestHandleActiveState_r2_ActiveSessionFromConfig(t *testing.T) {
	srv := testServer(t)
	// Set active session ID in config (separate from active agent).
	srv.cfg.ActiveSessionID = "my-session"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/active-state", nil)
	w := httptest.NewRecorder()
	srv.handleActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result ActiveStateResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ActiveSessionID != "my-session" {
		t.Errorf("expected active_session_id=my-session, got %q", result.ActiveSessionID)
	}
}

func TestHandleActiveState_r2_ResponseShape(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/active-state", nil)
	w := httptest.NewRecorder()
	srv.handleActiveState(w, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"active_session_id", "active_agent_id", "last_activity_at", "threads_running"} {
		if _, ok := raw[key]; !ok {
			// active_session_id and active_agent_id are omitempty — allow them to be absent
			// only when the value is empty.
			if key == "active_session_id" || key == "active_agent_id" {
				continue
			}
			t.Errorf("response missing key %q", key)
		}
	}
}

// ── handleRestoreActiveState (not registered, tested directly) ────────────────

func TestHandleRestoreActiveState_r2_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreActiveState_r2_EmptyBody(t *testing.T) {
	srv := testServer(t)
	// Empty body (no session_id or agent_id) — should succeed with defaults.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty restore, got %d: %s", w.Code, w.Body.String())
	}
	var result ActiveStateResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ActiveSessionID != "" {
		t.Errorf("expected empty session ID, got %q", result.ActiveSessionID)
	}
}

func TestHandleRestoreActiveState_r2_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	body := `{"session_id":"nonexistent-session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreActiveState_r2_ValidSession(t *testing.T) {
	srv := testServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("restore-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store

	body := `{"session_id":"` + sess.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result ActiveStateResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ActiveSessionID != sess.ID {
		t.Errorf("expected active_session_id=%q, got %q", sess.ID, result.ActiveSessionID)
	}
}

func TestHandleRestoreActiveState_r2_AgentNotFound_NoRegistry(t *testing.T) {
	srv := testServer(t)
	// Agent registry is empty in the stub orchestrator — agent lookup will return false.
	body := `{"agent_id":"nonexistent-agent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)
	// Agent not in registry — should return 404.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreActiveState_r2_ResponseShape(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// last_activity_at and threads_running must always be present.
	for _, key := range []string{"last_activity_at", "threads_running"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing required key %q", key)
		}
	}
}

// ── End-to-end via registered routes ─────────────────────────────────────────

func TestActiveStateEndToEnd_SessionActiveState_Unauthorized(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/sessions/some-id/active-state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestActiveStateEndToEnd_SessionActiveState_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sessions/nonexistent/active-state", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestActiveStateEndToEnd_SessionActiveState_FoundSession(t *testing.T) {
	srv, ts := newTestServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("e2e-active-state", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result sessionActiveState
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != sess.ID {
		t.Errorf("expected session_id=%q, got %q", sess.ID, result.SessionID)
	}
}

func TestActiveStateEndToEnd_GetIsRegistered_OtherMethodsNotRouted(t *testing.T) {
	// DELETE to active-state falls through to the SPA catch-all (returns 404
	// from FileServer). The SPA handles all unmatched paths.
	// This test documents the actual mux behavior when a method is not registered.
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/sessions/some-id/active-state", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// The SPA catch-all returns 404 for unregistered method+path combinations.
	// This is expected behaviour given the catch-all "/".
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404 or 405 for unregistered method, got %d", resp.StatusCode)
	}
}
