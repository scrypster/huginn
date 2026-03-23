package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeTokenProvider is an IntegrationProvider whose TokenURL points to a local
// test server so we can simulate a real token exchange in HandleOAuthCallback tests.
type fakeTokenProvider struct {
	tokenURL string
}

func (f *fakeTokenProvider) Name() Provider      { return ProviderSlack }
func (f *fakeTokenProvider) DisplayName() string { return "Slack (test)" }
func (f *fakeTokenProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "fake-client",
		ClientSecret: "fake-secret",
		RedirectURL:  redirectURL,
		Scopes:       []string{"chat:write"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://slack.com/oauth/v2/authorize",
			TokenURL: f.tokenURL,
		},
	}
}
func (f *fakeTokenProvider) GetAccountInfo(_ context.Context, _ *http.Client) (*AccountInfo, error) {
	return &AccountInfo{ID: "U123", Label: "test-workspace"}, nil
}

// newTokenServer creates an httptest.Server that returns a valid OAuth token response.
func newTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"access_token":  "sl-access-token",
			"token_type":    "bearer",
			"refresh_token": "sl-refresh-token",
			"expires_in":    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// newManagerWithTokenSrv creates a Manager backed by a temp store and a
// fakeTokenProvider whose token URL resolves to srv.
func newManagerWithTokenSrv(t *testing.T, srv *httptest.Server) (*Manager, *fakeTokenProvider) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	t.Cleanup(func() { m.Close() })
	p := &fakeTokenProvider{tokenURL: srv.URL}
	return m, p
}

// ─── StartOAuthFlow ────────────────────────────────────────────────────────────

// TestStartOAuthFlow_ReturnsURL verifies the URL contains expected OAuth params.
func TestStartOAuthFlow_ReturnsURL(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	authURL, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}
	if authURL == "" {
		t.Fatal("expected non-empty auth URL")
	}
	for _, param := range []string{"state=", "code_challenge=", "code_challenge_method=S256"} {
		if !strings.Contains(authURL, param) {
			t.Errorf("authURL missing %q\nURL: %s", param, authURL)
		}
	}
}

// TestStartOAuthFlow_StateIsUniqueBetweenCalls verifies two flows get different state tokens.
func TestStartOAuthFlow_StateIsUniqueBetweenCalls(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	url1, err1 := m.StartOAuthFlow(p)
	url2, err2 := m.StartOAuthFlow(p)
	if err1 != nil || err2 != nil {
		t.Fatalf("StartOAuthFlow errors: %v / %v", err1, err2)
	}

	// Extract state= values and compare.
	stateOf := func(u string) string {
		for _, part := range strings.Split(u, "&") {
			if strings.HasPrefix(part, "state=") {
				return strings.TrimPrefix(part, "state=")
			}
		}
		return ""
	}
	s1, s2 := stateOf(url1), stateOf(url2)
	if s1 == "" || s2 == "" {
		t.Fatalf("could not extract state from URLs: %q / %q", url1, url2)
	}
	if s1 == s2 {
		t.Errorf("expected unique state tokens, got %q twice", s1)
	}
}

// TestStartOAuthFlow_PendingFlowStored verifies a pending flow entry is created.
func TestStartOAuthFlow_PendingFlowStored(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	count := len(m.pendingFlows)
	m.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 pending flow, got %d", count)
	}
}

// ─── HandleOAuthCallback ───────────────────────────────────────────────────────

// TestHandleOAuthCallback_ValidState_StoresConnection verifies a successful full flow.
func TestHandleOAuthCallback_ValidState_StoresConnection(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	// Start the flow to get a valid state token.
	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	// Extract the state from pendingFlows directly.
	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	conn, err := m.HandleOAuthCallback(context.Background(), state, "fake-code")
	if err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}
	if conn.ID == "" {
		t.Error("expected non-empty connection ID")
	}
	if conn.Provider != ProviderSlack {
		t.Errorf("provider: got %q, want %q", conn.Provider, ProviderSlack)
	}
	if conn.AccountLabel != "test-workspace" {
		t.Errorf("AccountLabel = %q, want test-workspace", conn.AccountLabel)
	}

	// Verify the connection was persisted in the store.
	stored, ok := m.store.Get(conn.ID)
	if !ok {
		t.Fatal("expected connection to be persisted in store")
	}
	if stored.Provider != ProviderSlack {
		t.Errorf("stored provider = %q", stored.Provider)
	}
}

// TestHandleOAuthCallback_PendingFlowConsumed verifies the pending flow is removed after use.
func TestHandleOAuthCallback_PendingFlowConsumed(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	if _, err := m.HandleOAuthCallback(context.Background(), state, "any-code"); err != nil {
		t.Fatalf("HandleOAuthCallback: %v", err)
	}

	// pendingFlows should be empty now.
	m.mu.Lock()
	count := len(m.pendingFlows)
	m.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 pending flows after callback, got %d", count)
	}
}

// TestHandleOAuthCallback_UnknownState returns an error for an unknown state.
func TestHandleOAuthCallback_UnknownState(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, _ := newManagerWithTokenSrv(t, srv)

	_, err := m.HandleOAuthCallback(context.Background(), "totally-bogus-state", "code")
	if err == nil {
		t.Fatal("expected error for unknown state")
	}
	if !strings.Contains(err.Error(), "unknown or expired") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleOAuthCallback_ExpiredState returns an error when the flow has expired.
func TestHandleOAuthCallback_ExpiredState(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	// Manually expire the flow.
	m.mu.Lock()
	for k, flow := range m.pendingFlows {
		flow.expiresAt = time.Now().Add(-time.Minute)
		m.pendingFlows[k] = flow
	}
	m.mu.Unlock()

	// Collect state key before the callback (which deletes it from the map).
	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	_, err = m.HandleOAuthCallback(context.Background(), state, "code")
	if err == nil {
		t.Fatal("expected error for expired state")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got: %v", err)
	}
}

// TestHandleOAuthCallback_TokenExchangeFailure returns error when token server fails.
func TestHandleOAuthCallback_TokenExchangeFailure(t *testing.T) {
	// Create a server that returns an error.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer failSrv.Close()

	m, p := newManagerWithTokenSrv(t, failSrv)

	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	_, err = m.HandleOAuthCallback(context.Background(), state, "bad-code")
	if err == nil {
		t.Fatal("expected token exchange error")
	}
	if !strings.Contains(err.Error(), "token exchange") {
		t.Errorf("expected 'token exchange' in error, got: %v", err)
	}
}

// TestHandleOAuthCallback_SecretStoreFails returns error when secrets cannot be stored.
func TestHandleOAuthCallback_SecretStoreFails(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NewManager(store, &failingSecretStore{}, "http://localhost:9999/oauth/callback")
	t.Cleanup(func() { m.Close() })
	p := &fakeTokenProvider{tokenURL: srv.URL}

	_, err = m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	_, err = m.HandleOAuthCallback(context.Background(), state, "good-code")
	if err == nil {
		t.Fatal("expected error when secrets store fails")
	}
}

// ─── GetHTTPClient / token refresh ────────────────────────────────────────────

// TestGetHTTPClient_TokenNearExpiry_ConfiguresTokenSource verifies GetHTTPClient
// returns a non-nil client when a near-expiry token is stored; the oauth2 library
// will refresh it on next use via the persisting TokenSource.
func TestGetHTTPClient_TokenNearExpiry_ConfiguresTokenSource(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	conn := makeConn("conn-near-expiry", ProviderSlack)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	// Store a token that is about to expire (within the oauth2 library's 10s threshold).
	nearExpiry := &oauth2.Token{
		AccessToken:  "stale-access",
		RefreshToken: "valid-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(5 * time.Second), // expires very soon
	}
	if err := m.secrets.StoreToken(conn.ID, nearExpiry); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	client, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err != nil {
		t.Fatalf("GetHTTPClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}
}

// TestGetHTTPClient_MissingConnection returns an error for an unknown connection ID.
func TestGetHTTPClient_MissingConnection(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	_, err := m.GetHTTPClient(context.Background(), "no-such-conn", p)
	if err == nil {
		t.Fatal("expected error for missing connection")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// TestGetHTTPClient_MissingToken returns an error when no token is stored.
func TestGetHTTPClient_MissingToken(t *testing.T) {
	srv := newTokenServer(t)
	defer srv.Close()
	m, p := newManagerWithTokenSrv(t, srv)

	conn := makeConn("conn-no-token", ProviderSlack)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	// Intentionally do NOT store a token.

	_, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}
