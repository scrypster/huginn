package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

const testToken = "test-token-1234567890abcdef1234567890abcdef"

// newTestServer creates a Server backed by a real (but minimal) orchestrator
// using a stub backend. Returns the server and an httptest.Server.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)

	b := &stubBackend{}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	cfg := *config.Default()
	srv := New(cfg, orch, store, testToken, t.TempDir(), nil, nil, nil)
	// Isolate agent loading: use the default (built-in) agent config so tests
	// are not affected by whatever agents the developer has configured locally
	// under ~/.huginn/agents/. This also ensures that "Chris" (the default agent)
	// is always available for workflow validation tests.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}
	// Prevent tests from opening real browser windows during handleCloudConnect.
	srv.openBrowserFn = func(_ string) error { return nil }
	// Isolate config writes: use a temp file so tests never corrupt ~/.huginn/config.json.
	srv.configPath = t.TempDir() + "/config.json"
	// Isolate keychain writes: no-op storer returns the canonical keyring reference format
	// without touching the real macOS Keychain.
	srv.keyStorerFn = func(slot, _ string) (string, error) { return "keyring:huginn:" + slot, nil }

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

// stubBackend satisfies backend.Backend for tests without making real LLM calls.
type stubBackend struct{}

func (s *stubBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	reply := "stub response"
	if req.OnToken != nil {
		req.OnToken(reply)
	}
	return &backend.ChatResponse{Content: reply, DoneReason: "stop"}, nil
}
func (s *stubBackend) Health(_ context.Context) error            { return nil }
func (s *stubBackend) Shutdown(_ context.Context) error          { return nil }
func (s *stubBackend) ContextWindow() int                        { return 4096 }

// --- Tests ---

func TestServerHealth(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestServerUnauthorized(t *testing.T) {
	// /api/v1/health is intentionally public (tray health poll, no token).
	// Use an authenticated endpoint to verify auth is enforced.
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestServerUnauthorized_BadToken(t *testing.T) {
	// /api/v1/health is intentionally public (tray health poll, no token).
	// Use an authenticated endpoint to verify bad tokens are rejected.
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestServerTokenQueryParam(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health?token=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServerListSessions(t *testing.T) {
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
	// Empty store returns empty array
	if body == nil {
		t.Fatal("expected empty array, got nil")
	}
}

func TestServerCreateSession(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["session_id"] == "" {
		t.Fatal("expected non-empty session_id")
	}
}

func TestServerGetSession_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nonexistent", nil)
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

func TestServerGetConfig(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body config.Config
	json.NewDecoder(resp.Body).Decode(&body)
	if body.ReasonerModel == "" {
		t.Fatal("expected reasoner_model in config")
	}
}

func TestServerUpdateConfig_InvalidPort(t *testing.T) {
	_, ts := newTestServer(t)
	// Port 80 is below 1024 and not 0 (dynamic), so validation should fail
	body := `{"web_ui":{"port":80}}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader(body))
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

func TestServerRuntimeStatus(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/runtime/status", nil)
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
	if body["state"] != "idle" {
		t.Fatalf("expected state idle, got %q", body["state"])
	}
}

func TestServerListModels(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["reasoner"] == "" {
		t.Fatal("expected non-empty reasoner model")
	}
}

func TestServerStats(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stats", nil)
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

func TestServerLogs(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/logs?n=10", nil)
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
	if _, ok := body["lines"]; !ok {
		t.Fatal("expected 'lines' key in response")
	}
}

func TestServerListAgents(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents", nil)
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

func TestServerGetActiveAgent_Empty(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/active", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if name, ok := body["name"]; !ok {
		t.Fatal("expected 'name' key in response")
	} else if name != "" {
		t.Errorf("expected empty name (no active agent set), got %q", name)
	}
}

func TestServerSetActiveAgent_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"name": "nonexistent-agent-xyz"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/active", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerStartStop(t *testing.T) {
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	b := &stubBackend{}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	cfg := *config.Default()
	cfg.WebUI.Port = 0 // dynamic

	srv := New(cfg, orch, store, testToken, t.TempDir(), nil, nil, nil)
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("expected non-empty address")
	}

	// Verify the server is actually listening
	resp, err := http.Get("http://" + addr + "/api/v1/health?token=" + testToken)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServerWebSocket_Connect(t *testing.T) {
	_, ts := newTestServer(t)
	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	// Send a ping and expect a pong
	pingMsg := WSMessage{Type: "ping"}
	data, _ := json.Marshal(pingMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatal(err)
	}

	_, respData, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var pong WSMessage
	json.Unmarshal(respData, &pong)
	if pong.Type != "pong" {
		t.Fatalf("expected pong, got %q", pong.Type)
	}
}

func TestServerWebSocket_Unauthorized(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error for unauthorized WebSocket")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestServerWebSocket_BadToken(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=wrong"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error for bad token WebSocket")
	}
	if resp != nil && resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleGetMessages_SessionNotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/nonexistent/messages", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// TailMessages returns nil, nil for a nonexistent session file (empty slice)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		t.Fatal("expected non-nil (empty) array")
	}
}

func TestHandleGetMessages_ReturnsMessages(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a session and append messages to its store
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_ = store.Append(sess, session.SessionMessage{
			Role:    "user",
			Content: "message",
		})
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var msgs []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestHandleGetMessages_LimitParam(t *testing.T) {
	srv, ts := newTestServer(t)

	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_ = store.Append(sess, session.SessionMessage{Role: "user", Content: "msg"})
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var msgs []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (limit), got %d", len(msgs))
	}
}

func TestHandleGetMessages_LimitCapAt500(t *testing.T) {
	srv, ts := newTestServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = store.Append(sess, session.SessionMessage{Role: "user", Content: "msg"})
	}

	// limit=9999 should be capped at 500 internally but still return 5 messages
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages?limit=9999", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var msgs []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
}

func TestHandleGetMessages_DefaultLimit(t *testing.T) {
	srv, ts := newTestServer(t)
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("test", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_ = store.Append(sess, session.SessionMessage{Role: "user", Content: "msg"})
	}

	// No limit param — should default to 50, returning all 10
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/"+sess.ID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var msgs []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&msgs)
	if len(msgs) != 10 {
		t.Fatalf("expected 10 messages (default limit), got %d", len(msgs))
	}
}

func TestHandleListThreads_NilManager(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions/some-session/threads", nil)
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

func TestHandleCost_NilAccumulator_ReturnsZero(t *testing.T) {
	srv, ts := newTestServer(t)
	// ca is nil by default
	_ = srv
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/cost", nil)
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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["session_total_usd"]; !ok {
		t.Error("missing session_total_usd in response")
	}
	if body["session_total_usd"] != 0.0 {
		t.Errorf("expected 0.0, got %v", body["session_total_usd"])
	}
}

func TestHandleCost_ReturnsSessionTotal(t *testing.T) {
	srv, ts := newTestServer(t)
	ca := threadmgr.NewCostAccumulator(0)
	ca.Record("t-1", 1_000_000, 1_000_000, "claude-sonnet-4")
	srv.ca = ca

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/cost", nil)
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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["session_total_usd"]; !ok {
		t.Error("missing session_total_usd in response")
	}
	total, ok := body["session_total_usd"].(float64)
	if !ok {
		t.Fatalf("session_total_usd is not a float64: %T", body["session_total_usd"])
	}
	if total <= 0 {
		t.Errorf("expected positive total, got %f", total)
	}
}
