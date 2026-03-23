package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
)

// newTestServerForRelay creates a full Server backed by a real connections.Manager
// so that StoreExternalToken can be called successfully.
func newTestServerForRelay(t *testing.T) *Server {
	t.Helper()
	connStore, err := connections.NewStore(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	connMgr := connections.NewManager(connStore, connections.NewMemoryStore(), "http://localhost/oauth/callback")

	b := &stubBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	cfg := *config.Default()
	srv := New(cfg, orch, session.NewStore(t.TempDir()), testToken, t.TempDir(), connMgr, connStore, nil)
	return srv
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHandleOAuthRelayFromCloud_ValidJWT_StoresToken(t *testing.T) {
	relayKey := make([]byte, 32)
	for i := range relayKey {
		relayKey[i] = byte(i + 1)
	}
	relayKeyB64 := base64.RawURLEncoding.EncodeToString(relayKey)

	const flowID = "test-flow-aaa111"
	srv := newTestServerForRelay(t)
	srv.storeRelayKey(flowID, relayKeyB64)

	relayJWT := makeRelayJWT(t, relayKey, "github", "access_tok_abc", "refresh_tok", 0)

	body := fmt.Sprintf(`{"relay_jwt":%q,"flow_id":%q}`, relayJWT, flowID)
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleOAuthRelayFromCloud(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if !resp["ok"] {
		t.Fatal("expected {ok:true}")
	}

	// Relay key must be consumed — a second claim should fail.
	_, ok := srv.claimRelayKey(flowID)
	if ok {
		t.Fatal("relay key should have been consumed after successful relay")
	}
}

func TestHandleOAuthRelayFromCloud_MissingRelayJWT_Returns400(t *testing.T) {
	srv := newTestServerForRelay(t)
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.handleOAuthRelayFromCloud(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOAuthRelayFromCloud_NoPendingFlow_Returns400(t *testing.T) {
	srv := newTestServerForRelay(t)

	// Store relay_key under a different flow ID; send a mismatched flow_id.
	relayKey := make([]byte, 32)
	relayKeyB64 := base64.RawURLEncoding.EncodeToString(relayKey)
	srv.storeRelayKey("flow-google-111", relayKeyB64)

	relayJWT := makeRelayJWT(t, relayKey, "github", "tok", "", 0)
	body := fmt.Sprintf(`{"relay_jwt":%q,"flow_id":%q}`, relayJWT, "flow-github-999")
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOAuthRelayFromCloud(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleOAuthRelayFromCloud_WrongKey_Returns401(t *testing.T) {
	correctKey := make([]byte, 32)
	for i := range correctKey {
		correctKey[i] = byte(i + 1)
	}
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(i + 7)
	}

	const flowID = "flow-wrongkey-test"
	srv := newTestServerForRelay(t)
	// Store the WRONG key under the flow ID.
	srv.storeRelayKey(flowID, base64.RawURLEncoding.EncodeToString(wrongKey))

	// Sign with the CORRECT key.
	relayJWT := makeRelayJWT(t, correctKey, "github", "tok", "", 0)
	body := fmt.Sprintf(`{"relay_jwt":%q,"flow_id":%q}`, relayJWT, flowID)
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOAuthRelayFromCloud(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleOAuthRelayFromCloud_ConnMgrNil_Returns503(t *testing.T) {
	srv := &Server{
		relayKeys: make(map[string]string),
		connMgr:   nil, // intentionally nil
	}
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(`{"relay_jwt":"anything"}`))
	w := httptest.NewRecorder()
	srv.handleOAuthRelayFromCloud(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleOAuthRelayFromCloud_ExpiredJWT_Returns401(t *testing.T) {
	relayKey := make([]byte, 32)
	relayKeyB64 := base64.RawURLEncoding.EncodeToString(relayKey)
	const flowID = "flow-expired-test"
	srv := newTestServerForRelay(t)
	srv.storeRelayKey(flowID, relayKeyB64)

	// Build an expired JWT.
	claims := jwtlib.MapClaims{
		"provider":     "github",
		"access_token": "tok",
		"iat":          time.Now().Add(-10 * time.Minute).Unix(),
		"exp":          time.Now().Add(-5 * time.Minute).Unix(), // expired
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, _ := tok.SignedString(relayKey)

	body := fmt.Sprintf(`{"relay_jwt":%q,"flow_id":%q}`, signed, flowID)
	req := httptest.NewRequest("POST", "/api/v1/connections/oauth/relay", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleOAuthRelayFromCloud(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired JWT, got %d", w.Code)
	}
}

func TestHandleOAuthRelayFromCloud_RouteRegistered(t *testing.T) {
	relayKey := make([]byte, 32)
	for i := range relayKey {
		relayKey[i] = byte(i + 1)
	}
	relayKeyB64 := base64.RawURLEncoding.EncodeToString(relayKey)

	const flowID = "flow-route-test-aaa"
	srv := newTestServerForRelay(t)
	srv.storeRelayKey(flowID, relayKeyB64)

	relayJWT := makeRelayJWT(t, relayKey, "github", "access_tok_route_test", "", 0)
	body := fmt.Sprintf(`{"relay_jwt":%q,"flow_id":%q}`, relayJWT, flowID)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/connections/oauth/relay", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
