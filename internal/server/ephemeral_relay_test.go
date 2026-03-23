package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
)

// validRelayChallenge43 is a well-formed 43-char base64url string (SHA-256 of "test-secret").
const validRelayChallenge43 = "n4bQgYhMfWWaL-qgxVrQFaO_TxsrC4Is0V1sFbDwCgg"

// makeRelayJWT signs a relay JWT using the provided 32-byte key.
func makeRelayJWT(t *testing.T, key []byte, provider, accessToken, refreshToken string, expiry int64) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"provider":      provider,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expiry":        float64(expiry),
		"account_label": "testuser",
		"iat":           now.Unix(),
		"exp":           now.Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign relay JWT: %v", err)
	}
	return signed
}

func newTestServerWithConnStore(t *testing.T) *Server {
	t.Helper()
	connStore, err := connections.NewStore(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	connMgr := connections.NewManager(connStore, connections.NewMemoryStore(), "http://localhost/oauth/callback")
	t.Cleanup(connMgr.Close)

	b := &stubBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	cfg := *config.Default()
	srv := New(cfg, orch, session.NewStore(t.TempDir()), testToken, t.TempDir(), connMgr, connStore, nil)
	return srv
}

// openEphemeralListener starts the serveEphemeralRelay goroutine and returns the listener port.
func openEphemeralListener(t *testing.T, srv *Server, relayChallenge, provider string) (port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	go srv.serveEphemeralRelay(ln, relayChallenge, provider)
	return port
}

// waitForEphemeralServer polls until the ephemeral port is accepting connections or times out.
func waitForEphemeralServer(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ephemeral server on port %d did not come up in time", port)
}

func TestServeEphemeralRelay_ValidJWT(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	key, err := base64.RawURLEncoding.DecodeString(validRelayChallenge43)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	relayJWT := makeRelayJWT(t, key, "slack", "slk_access", "slk_refresh", 0)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?token=%s", port, url.QueryEscape(relayJWT))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "connected successfully") {
		t.Errorf("expected success page, got: %s", string(body))
	}

	// Verify relay_challenge is stored in connection metadata.
	conns, err := srv.connStore.List()
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(conns) == 0 {
		t.Fatal("expected a stored connection, got none")
	}
	conn := conns[0]
	if conn.Provider != "slack" {
		t.Errorf("provider: got %q, want slack", conn.Provider)
	}
	if conn.Metadata["relay_challenge"] != validRelayChallenge43 {
		t.Errorf("relay_challenge in metadata: got %q, want %q", conn.Metadata["relay_challenge"], validRelayChallenge43)
	}
}

func TestServeEphemeralRelay_InvalidJWT(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	// Sign with the wrong key.
	wrongKey := make([]byte, 32)
	claims := jwt.MapClaims{
		"provider":     "slack",
		"access_token": "tok",
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(10 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	badJWT, _ := tok.SignedString(wrongKey)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?token=%s", port, url.QueryEscape(badJWT))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Authorization failed") {
		t.Errorf("expected error page, got: %s", string(body))
	}

	// No connection should be stored.
	conns, _ := srv.connStore.List()
	if len(conns) != 0 {
		t.Errorf("expected no stored connections, got %d", len(conns))
	}
}

func TestServeEphemeralRelay_ProviderMismatch(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge43)
	// JWT claims "slack" but server expects "jira".
	relayJWT := makeRelayJWT(t, key, "slack", "tok", "", 0)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "jira")
	waitForEphemeralServer(t, port)

	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?token=%s", port, url.QueryEscape(relayJWT))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Authorization failed") {
		t.Errorf("expected error page, got: %s", string(body))
	}
}

func TestServeEphemeralRelay_ErrorParam(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	// Simulate an error redirect from the broker.
	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?error=%s",
		port, url.QueryEscape("access_denied"))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Authorization failed") {
		t.Errorf("expected error page, got: %s", string(body))
	}
}

func TestServeEphemeralRelay_MissingToken(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	// Hit the relay endpoint with no token or error param.
	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay", port)
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Authorization failed") {
		t.Errorf("expected error page, got: %s", string(body))
	}
}

func TestServeEphemeralRelay_ServerClosesAfterOneRequest(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge43)
	relayJWT := makeRelayJWT(t, key, "slack", "slk_access", "slk_refresh", 0)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	// First request — valid JWT.
	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?token=%s", port, url.QueryEscape(relayJWT))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	resp.Body.Close()

	// Give the server goroutine a moment to initiate Shutdown.
	time.Sleep(200 * time.Millisecond)

	// Second request should fail — server should have shut down.
	client := &http.Client{Timeout: 500 * time.Millisecond}
	_, err = client.Get(relayURL)
	if err == nil {
		t.Error("expected second request to fail after server shutdown, but it succeeded")
	}
}

func TestServeEphemeralRelay_NilConnMgr(t *testing.T) {
	// Server without a connMgr — relay should return error page but not panic.
	b := &stubBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	cfg := *config.Default()
	srv := New(cfg, orch, session.NewStore(t.TempDir()), testToken, t.TempDir(), nil, nil, nil)

	key, _ := base64.RawURLEncoding.DecodeString(validRelayChallenge43)
	relayJWT := makeRelayJWT(t, key, "slack", "slk_access", "", 0)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?token=%s", port, url.QueryEscape(relayJWT))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	// With no connMgr, StoreExternalTokenWithMeta is skipped — success page shown.
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "connected successfully") {
		t.Errorf("expected success page when connMgr is nil, got: %s", string(body))
	}
}

func TestServeEphemeralRelay_RelayXSSEscaping(t *testing.T) {
	srv := newTestServerWithConnStore(t)

	port := openEphemeralListener(t, srv, validRelayChallenge43, "slack")
	waitForEphemeralServer(t, port)

	// Send an error message with HTML/script injection.
	xssMsg := "<script>alert(1)</script>"
	relayURL := fmt.Sprintf("http://127.0.0.1:%d/oauth/relay?error=%s",
		port, url.QueryEscape(xssMsg))
	resp, err := http.Get(relayURL)
	if err != nil {
		t.Fatalf("GET relay URL: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// The raw script tag must NOT appear verbatim — it must be HTML-escaped.
	if strings.Contains(bodyStr, "<script>alert(1)</script>") {
		t.Error("XSS: raw script tag found in error page — HTML escaping is not working")
	}
	// The escaped version should appear.
	if !strings.Contains(bodyStr, "&lt;script&gt;") {
		t.Errorf("expected HTML-escaped script tag in error page, body: %s", bodyStr)
	}
}

func TestIsKnownProvider(t *testing.T) {
	cases := []struct {
		name  string
		known bool
	}{
		{"google", true},
		{"slack", true},
		{"jira", true},
		{"bitbucket", true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isKnownProvider(tc.name)
			if got != tc.known {
				t.Errorf("isKnownProvider(%q) = %v, want %v", tc.name, got, tc.known)
			}
		})
	}
}

// TestStartOAuthViaBroker_RelayChallengeDerivedCorrectly verifies that the relay_challenge
// sent to the broker is exactly 43 base64url characters (SHA-256 of a 32-byte secret).
func TestStartOAuthViaBroker_RelayChallengeDerivedCorrectly(t *testing.T) {
	srv, _ := newTestServerWithConnections(t)
	srv.connProviders[connections.ProviderSlack] = &stubProvider{name: connections.ProviderSlack}

	mock := &mockBrokerClient{authURL: "https://slack.com/oauth/v2/authorize?state=test"}
	srv.SetBrokerClient(mock)

	ctx := context.Background()
	_, err := srv.startOAuthViaBroker(ctx, mock, "slack")
	if err != nil {
		t.Fatalf("startOAuthViaBroker: %v", err)
	}

	// relay_challenge must be exactly 43 base64url chars.
	challenge := mock.gotChallenge
	if len(challenge) != 43 {
		t.Errorf("relay_challenge length: got %d, want 43", len(challenge))
	}
	for _, ch := range challenge {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			t.Errorf("relay_challenge contains invalid char %q in %q", ch, challenge)
			break
		}
	}

	// Verify the challenge decodes to exactly 32 bytes (SHA-256 output).
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	if err != nil {
		t.Errorf("relay_challenge is not valid base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("relay_challenge decodes to %d bytes, want 32", len(decoded))
	}
}
