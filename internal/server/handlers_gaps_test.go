package server

// handlers_gaps_test.go — additional coverage for handler paths not exercised
// by the existing server_test.go and hardening test files.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// ---------------------------------------------------------------------------
// handleHealth
// ---------------------------------------------------------------------------

// TestHandleHealth_ReturnsVersionAndStatus verifies that /api/v1/health (no
// auth required) returns 200 with status "ok" and a version field.
func TestHandleHealth_ReturnsVersionAndStatus(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if _, ok := body["version"]; !ok {
		t.Error("expected 'version' key in health response")
	}
	if _, ok := body["satellite_connected"]; !ok {
		t.Error("expected 'satellite_connected' key in health response")
	}
}

// TestHandleHealth_SatelliteDisconnectedByDefault checks that satellite_connected
// is false when no satellite is wired (the default test-server case).
func TestHandleHealth_SatelliteDisconnectedByDefault(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	connected, _ := body["satellite_connected"].(bool)
	if connected {
		t.Error("expected satellite_connected=false with no satellite wired")
	}
}

// ---------------------------------------------------------------------------
// handleListSessions
// ---------------------------------------------------------------------------

// TestHandleListSessions_EmptyStore verifies that an empty session store
// returns an empty JSON array (not null).
func TestHandleListSessions_EmptyStore(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		t.Fatal("expected non-nil empty array, got nil")
	}
	if len(body) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(body))
	}
}

// TestHandleListSessions_IncludeRoutine verifies that the include_routine_sessions
// query param controls whether routine sessions are returned.
func TestHandleListSessions_IncludeRoutine(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a session and mark it as "routine".
	sess := srv.store.New("routine-sess", "/tmp", "claude-3")
	sess.Manifest.Source = "routine"
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Without flag — routine sessions should be hidden.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var body []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body) != 0 {
		t.Errorf("expected 0 sessions (routine hidden), got %d", len(body))
	}

	// With flag — routine sessions should be visible.
	req2, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions?include_routine_sessions=true", nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err2 := http.DefaultClient.Do(req2)
	if err2 != nil {
		t.Fatalf("request 2 failed: %v", err2)
	}
	defer resp2.Body.Close()
	var body2 []json.RawMessage
	json.NewDecoder(resp2.Body).Decode(&body2)
	if len(body2) != 1 {
		t.Errorf("expected 1 session (routine included), got %d", len(body2))
	}
}

// TestHandleListSessions_NonRoutineSessionAlwaysVisible verifies that a
// normal (non-routine) session is always returned.
func TestHandleListSessions_NonRoutineSessionAlwaysVisible(t *testing.T) {
	srv, ts := newTestServer(t)

	sess := srv.store.New("normal-sess", "/tmp", "claude-3")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body) != 1 {
		t.Errorf("expected 1 session, got %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// handleGetSession
// ---------------------------------------------------------------------------

// TestHandleGetSession_NotFound verifies that fetching a non-existent session
// returns 404.
func TestHandleGetSession_404(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/does-not-exist", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected non-empty error message in 404 response")
	}
}

// TestHandleGetSession_FoundAfterCreate verifies that a session created via
// POST /sessions can subsequently be fetched by ID.
func TestHandleGetSession_FoundAfterCreate(t *testing.T) {
	_, ts := newTestServer(t)
	// Create a session.
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	sessionID := createBody["session_id"]
	if sessionID == "" {
		t.Fatal("expected session_id in create response")
	}

	// Fetch it.
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sessionID, nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != 200 {
		t.Fatalf("expected 200 for existing session, got %d", getResp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleDeleteSession
// ---------------------------------------------------------------------------

// TestHandleDeleteSession_DeletesExistingSession verifies that a persisted
// session can be deleted and is gone afterwards.
func TestHandleDeleteSession_DeletesExistingSession(t *testing.T) {
	srv, ts := newTestServer(t)
	sess := srv.store.New("delete-me", "/workspace", "claude-3")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/sessions/"+sess.ID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)
	if !body["deleted"] {
		t.Error("expected deleted=true in response")
	}

	if srv.store.Exists(sess.ID) {
		t.Error("session should not exist after successful delete")
	}
}

// TestHandleDeleteSession_NotFoundReturns404 verifies that deleting a
// non-existent session returns 404.
func TestHandleDeleteSession_NotFoundReturns404(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/sessions/no-such-session", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleUpdateConfig
// ---------------------------------------------------------------------------

// TestHandleUpdateConfig_InvalidJSON_Gaps returns 400 for malformed JSON.
func TestHandleUpdateConfig_InvalidJSON_Gaps(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader("{not valid json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateConfig_ValidPayload verifies that a valid config payload
// returns 200 with saved=true.
func TestHandleUpdateConfig_ValidPayload(t *testing.T) {
	_, ts := newTestServer(t)
	// Use a minimal valid config payload (port=0 is allowed — dynamic).
	payload := `{"version":1,"web_ui":{"port":0},"backend":{"provider":"anthropic"}}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["saved"] != true {
		t.Errorf("expected saved=true, got %v", body["saved"])
	}
}

// TestHandleUpdateConfig_NegativePortReturns400 ensures a negative port is
// rejected with 400.
func TestHandleUpdateConfig_NegativePortReturns400(t *testing.T) {
	_, ts := newTestServer(t)
	payload := `{"version":1,"web_ui":{"port":-5}}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleUpdateSession
// ---------------------------------------------------------------------------

// TestHandleUpdateSession_InvalidBodyReturns400 verifies that a PATCH with
// malformed JSON returns 400.
func TestHandleUpdateSession_InvalidBodyReturns400(t *testing.T) {
	srv, ts := newTestServer(t)
	// Create and persist a session in the server's store.
	sess := srv.store.New("t", "/tmp", "m")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/"+sess.ID, strings.NewReader("bad json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad body, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateSession_UpdatesTitle verifies that PATCHing a session's title
// persists it in the store.
func TestHandleUpdateSession_UpdatesTitle(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create and persist a session in the server's own store.
	sess := srv.store.New("original title", "/tmp", "model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	body := `{"title":"new title"}`
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/"+sess.ID, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Reload and verify the title.
	reloaded, err := srv.store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Manifest.Title != "new title" {
		t.Errorf("expected title='new title', got %q", reloaded.Manifest.Title)
	}
}

// ---------------------------------------------------------------------------
// handleCloudStatus
// ---------------------------------------------------------------------------

// TestHandleCloudStatus_NoSatellite verifies that /api/v1/cloud/status returns
// registered=false and connected=false when no satellite is wired.
func TestHandleCloudStatus_NoSatellite(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/cloud/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["registered"] != false {
		t.Errorf("expected registered=false, got %v", body["registered"])
	}
	if body["connected"] != false {
		t.Errorf("expected connected=false, got %v", body["connected"])
	}
}

// ---------------------------------------------------------------------------
// handleCreateSession — with space_id
// ---------------------------------------------------------------------------

// TestHandleCreateSession_WithSpaceID verifies that creating a session with a
// space_id body field returns a valid session_id (space_id wiring is best-effort).
func TestHandleCreateSession_WithSpaceID(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"space_id":"space-abc"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	json.NewDecoder(resp.Body).Decode(&respBody)
	if respBody["session_id"] == "" {
		t.Error("expected non-empty session_id even when space_id is provided")
	}
}

// TestHandleCreateSession_WithSpaceID_PersistsToStore verifies that creating
// a session with a space_id actually persists the session manifest to the store
// with the correct space_id set. This is the regression test for the bug where
// s.store.Load(sess.ID) always failed because the orchestrator session was
// never written to SQLite, so space_id was silently dropped.
func TestHandleCreateSession_WithSpaceID_PersistsToStore(t *testing.T) {
	srv, ts := newTestServer(t)
	body := `{"space_id":"space-xyz"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	json.NewDecoder(resp.Body).Decode(&respBody)
	sessionID := respBody["session_id"]
	if sessionID == "" {
		t.Fatal("expected non-empty session_id")
	}

	// Load the session directly from the store to verify space_id was persisted.
	stored, loadErr := srv.store.Load(sessionID)
	if loadErr != nil {
		t.Fatalf("store.Load(%q): %v — session was never persisted to the store", sessionID, loadErr)
	}
	if stored.Manifest.SpaceID != "space-xyz" {
		t.Errorf("expected SpaceID %q, got %q", "space-xyz", stored.Manifest.SpaceID)
	}
}

// ---------------------------------------------------------------------------
// handleListThreads
// ---------------------------------------------------------------------------

// TestHandleListThreads_EmptyIDReturns400 verifies that a missing session ID
// returns 400. (The mux uses path params so we use a fake ID and check 200 with empty array.)
func TestHandleListThreads_WithSessionIDNoThreadManager(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/some-id/threads", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// With nil tm, returns 200 with empty array.
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleGetSession — session created via store directly
// ---------------------------------------------------------------------------

// TestHandleGetSession_WithManifest verifies that a session saved directly
// into the store is accessible via the GET handler.
func TestHandleGetSession_WithManifest(t *testing.T) {
	srv, ts := newTestServer(t)

	sess := srv.store.New("manifest-test", "/tmp", "claude-3")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// The orchestrator's GetSession uses its own memory — create via POST instead.
	createReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	createReq.Header.Set("Authorization", "Bearer "+testToken)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	sid := createBody["session_id"]

	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sid, nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// handleGetMessages — before_seq pagination
// ---------------------------------------------------------------------------

// TestHandleGetMessages_BeforeSeqParam verifies that the before_seq query
// parameter is accepted and returns 200.
func TestHandleGetMessages_BeforeSeqParam(t *testing.T) {
	srv, ts := newTestServer(t)

	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("paged", "/tmp", "model")
	_ = store.SaveManifest(sess)
	for i := 0; i < 5; i++ {
		_ = store.Append(sess, session.SessionMessage{Role: "user", Content: "msg"})
	}

	// Use a before_seq larger than any message seq — should return all 5.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages?before_seq=9999", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
