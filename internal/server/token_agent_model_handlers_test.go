package server

// hardening_iter1_test.go — coverage improvements for server handlers.
// Tests uncovered paths: handleGetToken, handleGetAgent, handleDeleteAgent,
// handleUpdateAgent, handleSetActiveAgent (empty name), handleListAvailableModels,
// handleUpdateConfig, handlePullModel, streamEventToWS, stateString.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/backend"
)

// ─── handleGetToken ───────────────────────────────────────────────────────────

func TestHandleGetToken(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["token"] != testToken {
		t.Errorf("expected token %q, got %q", testToken, body["token"])
	}
}

// ─── handleGetAgent ───────────────────────────────────────────────────────────

func TestHandleGetAgent_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/does-not-exist-xyzzy", nil)
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

// ─── handleDeleteAgent ────────────────────────────────────────────────────────

func TestHandleDeleteAgent_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/agents/does-not-exist-xyzzy", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Not found should return 404 from DeleteAgentDefault
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ─── handleUpdateAgent ────────────────────────────────────────────────────────

func TestHandleUpdateAgent_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/myagent", strings.NewReader("{bad json}"))
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

// ─── handleListAvailableModels — Ollama not reachable ─────────────────────────

func TestHandleListAvailableModels_OllamaUnreachable(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/models/available", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should return 200 with an error field when Ollama is not reachable
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if _, ok := body["error"]; !ok {
		// Ollama might actually be running locally — either a models list or an error is acceptable
		if _, ok := body["models"]; !ok {
			t.Error("expected either 'error' or 'models' key in response")
		}
	}
}

// ─── handleUpdateConfig — valid config ────────────────────────────────────────

func TestHandleUpdateConfig_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader("{not json}"))
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

// ─── handlePullModel ─────────────────────────────────────────────────────────

func TestHandlePullModel_EmptyName(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"name": ""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/models/pull", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for empty name, got %d", resp.StatusCode)
	}
}

func TestHandlePullModel_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/models/pull", strings.NewReader("{bad}"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid JSON in pull model, got %d", resp.StatusCode)
	}
}

func TestHandlePullModel_OllamaUnreachable(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"name": "llama3"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/models/pull", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// When Ollama is not running locally, expect 502.
	// If Ollama IS running locally the pull might succeed or fail differently.
	// Either 502 or 200 is acceptable.
	if resp.StatusCode != 502 && resp.StatusCode != 200 {
		t.Fatalf("expected 502 or 200, got %d", resp.StatusCode)
	}
}

// ─── stateString ──────────────────────────────────────────────────────────────

func TestStateString_AllValues(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "idle"},
		{1, "iterating"},
		{2, "agent_loop"},
		{99, "unknown"},
	}
	for _, c := range cases {
		got := stateString(c.n)
		if got != c.want {
			t.Errorf("stateString(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// ─── streamEventToWS ─────────────────────────────────────────────────────────

func TestStreamEventToWS(t *testing.T) {
	ev := backend.StreamEvent{
		Type:    backend.StreamText,
		Content: "hello world",
	}
	msg := streamEventToWS(ev, "sess-1")
	if msg.SessionID != "sess-1" {
		t.Errorf("expected session_id=sess-1, got %q", msg.SessionID)
	}
	if msg.Content != "hello world" {
		t.Errorf("expected content=hello world, got %q", msg.Content)
	}
	// StreamText is NOT normalised to "token" — the onToken callback handles
	// text tokens. Normalising here would double every word (issue #30).
	if msg.Type != "text" {
		t.Errorf("expected type=%q, got %q", "text", msg.Type)
	}
}

// ─── handleUpdateSession — bad body ───────────────────────────────────────────

func TestHandleUpdateSession_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/v1/sessions/someid", strings.NewReader("{bad}"))
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

// ─── handleGetSession — found ────────────────────────────────────────────────

func TestHandleGetSession_Found(t *testing.T) {
	srv, ts := newTestServer(t)
	// Create a session so GetSession finds it.
	sess, err := srv.orch.NewSession("")
 if err != nil {
 	t.Fatalf("NewSession: %v", err)
 }
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID, nil)
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

// ─── handleCloudCallback — missing code ──────────────────────────────────────

func TestHandleCloudCallback_MissingCode(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/cloud/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing code, got %d", resp.StatusCode)
	}
}

// ─── handleDeleteSession ──────────────────────────────────────────────────────

func TestHandleDeleteSession_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/sessions/nonexistent-session", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 for non-existent session, got %d", resp.StatusCode)
	}
}

func TestHandleDeleteSession_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	// Create and persist a session so Delete can find it.
	sess := srv.store.New("to-delete", "/workspace", "claude-3")
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
		t.Error("expected deleted=true")
	}
	// Verify it's gone.
	if srv.store.Exists(sess.ID) {
		t.Error("session should not exist after delete")
	}
}

// ─── handleListThreads ────────────────────────────────────────────────────────

func TestHandleListThreads_NilTM(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/sess-1/threads", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []any
	json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		t.Error("expected empty array, got nil")
	}
}

// ─── handleLogs — n cap at 1000 ───────────────────────────────────────────────

func TestHandleLogs_LargeCap(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/logs?n=99999", nil)
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

// ─── handleBroadcastWS  ───────────────────────────────────────────────────────

func TestBroadcastWS_NilHub_NoPanic(t *testing.T) {
	srv, _ := newTestServer(t)
	// BroadcastWS should not panic even with an empty hub.
	srv.BroadcastWS(WSMessage{Type: "test"})
}

func TestBroadcastPlanning_NilHub_NoPanic(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.BroadcastPlanning("sess-1", "agent")
}

func TestBroadcastPlanningDone_NilHub_NoPanic(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.BroadcastPlanningDone("sess-1")
}

// ─── handleListProviders ──────────────────────────────────────────────────────

func TestHandleListProviders(t *testing.T) {
	_, ts := newTestServerWithConnections(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/providers", nil)
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

// ─── handleListConnections ────────────────────────────────────────────────────

func TestHandleListConnections(t *testing.T) {
	_, ts := newTestServerWithConnections(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/connections", nil)
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

// ─── WebSocket — thread_cancel and thread_inject with nil tm ─────────────────

func TestWebSocket_ThreadCancel_NilTM(t *testing.T) {
	srv, ts := newTestServer(t)
	// tm is nil by default — thread_cancel should be a no-op
	if srv.tm != nil {
		t.Skip("tm is not nil, test not applicable")
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	msg := WSMessage{Type: "thread_cancel", Payload: map[string]any{"thread_id": "t-1"}}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(1, data); err != nil {
		t.Fatal(err)
	}
	// No response expected — just confirm no panic.
}

// ─── Server.Stop when srv is nil ─────────────────────────────────────────────

func TestServerStop_NilSrv(t *testing.T) {
	srv := &Server{}
	// Stop on a server with nil http.Server should return nil without panicking.
	_ = httptest.NewRecorder() // ensure httptest is used to avoid import error
	_ = http.Request{}
	ctx := t.Context()
	err := srv.Stop(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
