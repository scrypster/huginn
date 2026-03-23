package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/session"
)

// makeMachineRelayJWT creates a signed relay JWT using the machine-ID + jwtSecret key derivation.
func makeMachineRelayJWT(t *testing.T, machineID, jwtSecret, provider, accessToken, accountLabel string) string {
	t.Helper()
	key := relayTokenSigningKey(machineID, jwtSecret)
	claims := jwt.MapClaims{
		"provider":      provider,
		"access_token":  accessToken,
		"refresh_token": "",
		"account_label": accountLabel,
		"iat":           time.Now().Unix(),
		"exp":           time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign relay JWT: %v", err)
	}
	return s
}

// makeMachineJWT creates a signed machine JWT with the given machineID.
func makeMachineJWT(t *testing.T, machineID, secret string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"machine_id": machineID,
		"scopes":     []string{"oauth", "relay"},
		"iat":        time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign machine JWT: %v", err)
	}
	return s
}

// newTestServerWithRelay creates a test server with relay and connections configured.
func newTestServerWithRelay(t *testing.T, tokenStorer relay.TokenStorer, jwtSecret string) (*Server, *httptest.Server) {
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
	srv.openBrowserFn = func(_ string) error { return nil }
	srv.SetRelayConfig(tokenStorer, jwtSecret)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

// --- Tests ---

func TestOAuthRelay_Success(t *testing.T) {
	machineID := "test-machine-123"
	jwtSecret := "test-secret-abc"
	provider := "github"
	accessToken := "gho_16C7e42F292c6912E7710c838347Ae178B4a"
	accountLabel := "mjbonanno"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)
	relayJWT := makeMachineRelayJWT(t, machineID, jwtSecret, provider, accessToken, accountLabel)

	tokenStorer := &relay.MemoryTokenStore{}
	tokenStorer.Save(machineJWT)

	srv, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)
	_ = srv

	q := url.Values{}
	q.Set("token", relayJWT)
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html, got %q", ct)
	}
}

func TestOAuthRelay_ErrorParam(t *testing.T) {
	tokenStorer := &relay.MemoryTokenStore{}
	_, ts := newTestServerWithRelay(t, tokenStorer, "secret")

	q := url.Values{}
	q.Set("error", "access_denied")
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_MissingToken(t *testing.T) {
	tokenStorer := &relay.MemoryTokenStore{}
	_, ts := newTestServerWithRelay(t, tokenStorer, "secret")

	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_RelayNotConfigured(t *testing.T) {
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
	// Don't call SetRelayConfig

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	q := url.Values{}
	q.Set("token", "dummy-token")
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_MachineNotRegistered(t *testing.T) {
	tokenStorer := &relay.MemoryTokenStore{}
	// Don't save anything to the token storer
	_, ts := newTestServerWithRelay(t, tokenStorer, "secret")

	q := url.Values{}
	q.Set("token", "dummy-token")
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_InvalidRelayToken(t *testing.T) {
	machineID := "test-machine-123"
	jwtSecret := "test-secret-abc"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)

	tokenStorer := &relay.MemoryTokenStore{}
	tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	q := url.Values{}
	q.Set("token", "invalid.jwt.token")
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_TokenSignedWithWrongKey(t *testing.T) {
	machineID := "test-machine-123"
	jwtSecret := "test-secret-abc"
	wrongSecret := "wrong-secret"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)
	// Create relay JWT signed with wrong secret
	relayJWT := makeMachineRelayJWT(t, machineID, wrongSecret, "github", "gho_xxxx", "user")

	tokenStorer := &relay.MemoryTokenStore{}
	tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	q := url.Values{}
	q.Set("token", relayJWT)
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_MissingRequiredFields(t *testing.T) {
	machineID := "test-machine-123"
	jwtSecret := "test-secret-abc"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)

	tokenStorer := &relay.MemoryTokenStore{}
	tokenStorer.Save(machineJWT)

	_, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	// Create relay JWT with missing access_token
	key := relayTokenSigningKey(machineID, jwtSecret)
	claims := jwt.MapClaims{
		"provider":      "github",
		"account_label": "user",
		"iat":           time.Now().Unix(),
		"exp":           time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	relayJWT, _ := tok.SignedString(key)

	q := url.Values{}
	q.Set("token", relayJWT)
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOAuthRelay_StoredTokenVerification(t *testing.T) {
	machineID := "test-machine-123"
	jwtSecret := "test-secret-abc"
	provider := "github"
	accessToken := "gho_16C7e42F292c6912E7710c838347Ae178B4a"
	accountLabel := "mjbonanno"

	machineJWT := makeMachineJWT(t, machineID, jwtSecret)
	relayJWT := makeMachineRelayJWT(t, machineID, jwtSecret, provider, accessToken, accountLabel)

	tokenStorer := &relay.MemoryTokenStore{}
	tokenStorer.Save(machineJWT)

	srv, ts := newTestServerWithRelay(t, tokenStorer, jwtSecret)

	q := url.Values{}
	q.Set("token", relayJWT)
	req, _ := http.NewRequest("GET", ts.URL+"/oauth/relay?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the connection was stored
	conns, err := srv.connStore.List()
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	conn := conns[0]
	if conn.Provider != connections.ProviderGitHub {
		t.Fatalf("expected provider GitHub, got %v", conn.Provider)
	}
	if conn.AccountLabel != accountLabel {
		t.Fatalf("expected account_label %q, got %q", accountLabel, conn.AccountLabel)
	}
	if conn.Type != connections.ConnectionTypeOAuth {
		t.Fatalf("expected type oauth, got %v", conn.Type)
	}
}

func TestRelayTokenSigningKeyDeterminism(t *testing.T) {
	machineID := "test-machine-id"
	jwtSecret := "test-jwt-secret"

	key1 := relayTokenSigningKey(machineID, jwtSecret)
	key2 := relayTokenSigningKey(machineID, jwtSecret)

	if fmt.Sprintf("%x", key1) != fmt.Sprintf("%x", key2) {
		t.Fatal("relay token signing key should be deterministic")
	}

	// Verify it matches the expected format: SHA-256(machineID + ":" + jwtSecret)
	expectedKey := sha256.Sum256([]byte(machineID + ":" + jwtSecret))
	if fmt.Sprintf("%x", key1) != fmt.Sprintf("%x", expectedKey[:]) {
		t.Fatal("relay token signing key does not match expected format")
	}
}

func TestRelaySuccessHTML_ContainsProvider(t *testing.T) {
	html := relaySuccessHTML("GitHub")
	if !contains(html, "GitHub") {
		t.Fatal("success HTML should contain provider name")
	}
	if !contains(html, "connected successfully") {
		t.Fatal("success HTML should contain 'connected successfully'")
	}
	if !contains(html, "window.close") {
		t.Fatal("success HTML should contain window.close()")
	}
}

func TestRelayErrorHTML_ContainsMessage(t *testing.T) {
	msg := "test error message"
	html := relayErrorHTML(msg)
	if !contains(html, msg) {
		t.Fatal("error HTML should contain error message")
	}
	if !contains(html, "Authorization failed") {
		t.Fatal("error HTML should contain 'Authorization failed'")
	}
}

// Helper function to check if a substring exists in a string
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && s[0:len(substr)] == substr) || findSubstring(s, substr))
}

func findSubstring(haystack, needle string) bool {
	for i := 0; i < len(haystack)-len(needle)+1; i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
