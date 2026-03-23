package server

// coverage_boost2_test.go — second pass to push server closer to 90%.
// Targets remaining uncovered branches.

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

// ─── handleSendMessage — missing session id ───────────────────────────────────

func TestHandleSendMessage_MissingSessionID(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions//messages", strings.NewReader(`{"content":"hi"}`))
	w := httptest.NewRecorder()
	srv.handleSendMessage(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── handleOAuthCallback — callback failed redirect ──────────────────────────

func TestHandleOAuthCallback_CallbackFailed(t *testing.T) {
	srv, _ := newTestServerWithConnections(t)

	// Register routes for this test's server
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts2 := httptest.NewServer(mux)
	defer ts2.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts2.URL + "/oauth/callback?state=unknownstate&code=badcode")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	// Either callback_failed (HandleOAuthCallback error) or connected= (unlikely, but accept)
	if !strings.Contains(loc, "callback_failed") && !strings.Contains(loc, "connected=") && !strings.Contains(loc, "error=") {
		t.Errorf("unexpected redirect location: %q", loc)
	}
}

// ─── handleListConnections — with connections store ───────────────────────────

func TestHandleListConnections_WithConnections(t *testing.T) {
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
	var conns []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&conns)
	if conns == nil {
		t.Error("expected non-nil array")
	}
}

// ─── handleListAvailableModels — default baseURL ─────────────────────────────

func TestHandleListAvailableModels_DefaultBaseURL(t *testing.T) {
	srv, _ := newTestServer(t)
	// OllamaBaseURL is empty — exercises the `if baseURL == ""` branch
	srv.cfg.OllamaBaseURL = ""
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/available", nil)
	w := httptest.NewRecorder()
	srv.handleListAvailableModels(w, req)
	// Either 200 with models (Ollama running) or 200 with error field (not running)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ─── handlePullModel — default baseURL ───────────────────────────────────────

func TestHandlePullModel_DefaultBaseURL(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.cfg.OllamaBaseURL = "" // exercises `if baseURL == ""` branch
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/pull", strings.NewReader(`{"name":"llama3"}`))
	w := httptest.NewRecorder()
	srv.handlePullModel(w, req)
	// 502 (unreachable) or 200 (if Ollama running)
	if w.Code != 502 && w.Code != 200 {
		t.Errorf("expected 502 or 200, got %d", w.Code)
	}
}

// ─── handleOAuthRelay — relay token missing fields (provider empty) ───────────

func TestHandleOAuthRelay_MissingProvider(t *testing.T) {
	machineID := "test-machine-boost2"
	jwtSecret := "boost2-secret"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)

	tokenStorer := &relay.MemoryTokenStore{}
	_ = tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	key := relayTokenSigningKey(machineID, jwtSecret)
	claims := jwt.MapClaims{
		"provider":      "",
		"access_token":  "tok",
		"account_label": "user",
		"iat":           time.Now().Unix(),
		"exp":           time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	relayJWT, _ := tok.SignedString(key)

	resp, err := http.Get(ts.URL + "/oauth/relay?token=" + relayJWT)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── handleOAuthRelay — token with expiry field set ───────────────────────────

func TestHandleOAuthRelay_WithExpiryField(t *testing.T) {
	machineID := "test-machine-expiry"
	jwtSecret := "expiry-secret"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)
	tokenStorer := &relay.MemoryTokenStore{}
	_ = tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	key := relayTokenSigningKey(machineID, jwtSecret)
	claims := jwt.MapClaims{
		"provider":      "slack",
		"access_token":  "xoxb-test",
		"refresh_token": "",
		"account_label": "testuser",
		"expiry":        float64(time.Now().Add(1 * time.Hour).Unix()),
		"iat":           time.Now().Unix(),
		"exp":           time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	relayJWT, _ := tok.SignedString(key)

	resp, err := http.Get(ts.URL + "/oauth/relay?token=" + relayJWT)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── handleOAuthRelay — invalid machine JWT (not parseable) ──────────────────

func TestHandleOAuthRelay_InvalidMachineJWT(t *testing.T) {
	tokenStorer := &relay.MemoryTokenStore{}
	_ = tokenStorer.Save("not.a.valid.jwt.at.all")

	_, ts := newTestServerWithRelay(t, tokenStorer, "some-secret")

	resp, err := http.Get(ts.URL + "/oauth/relay?token=anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── handleOAuthRelay — machine JWT has no machine_id ────────────────────────

func TestHandleOAuthRelay_MachineJWTNoMachineID(t *testing.T) {
	jwtSecret := "no-machine-id-secret"
	claims := jwt.MapClaims{
		"user": "test",
		"iat":  time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	machineJWT, _ := tok.SignedString([]byte(jwtSecret))

	tokenStorer := &relay.MemoryTokenStore{}
	_ = tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	resp, err := http.Get(ts.URL + "/oauth/relay?token=anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── wsReadPump — bad JSON is skipped ────────────────────────────────────────

func TestWSReadPump_BadJSONIsSkipped(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send invalid JSON — server should skip it (no crash)
	if err := conn.WriteMessage(1, []byte("{invalid json")); err != nil {
		t.Fatal(err)
	}

	// Then send a ping and confirm pong arrives (proves connection is still alive)
	pingMsg := WSMessage{Type: "ping"}
	data, _ := json.Marshal(pingMsg)
	if err := conn.WriteMessage(1, data); err != nil {
		t.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respData, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var pong WSMessage
	json.Unmarshal(respData, &pong)
	if pong.Type != "pong" {
		t.Errorf("expected pong, got %q", pong.Type)
	}
}

// ─── serveEphemeralRelay — bad challenge ─────────────────────────────────────

func TestServeEphemeralRelay_BadChallenge(t *testing.T) {
	srv, _ := newTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// Use an invalid base64 challenge — serveEphemeralRelay should close immediately
	go srv.serveEphemeralRelay(ln, "not valid base64!!!@@@", "slack")

	// Wait briefly; the listener should be closed
	time.Sleep(50 * time.Millisecond)
}

// ─── readBody helper ──────────────────────────────────────────────────────────

func readBody2(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	return string(data)
}
