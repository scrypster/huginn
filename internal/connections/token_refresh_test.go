package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// tokenRefreshServer creates an httptest.Server that serves a fresh token on each request.
// refreshCount is incremented each time the token endpoint is called.
func tokenRefreshServer(t *testing.T, refreshCount *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*refreshCount++
		resp := map[string]any{
			"access_token":  "new-access-token",
			"token_type":    "bearer",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// tokenRefreshErrorServer creates an httptest.Server that always returns 400
// simulating a failed token refresh.
func tokenRefreshErrorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked"}`, http.StatusBadRequest)
	}))
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestGetHTTPClient_ExpiredToken_TokenSourceConfigured verifies that GetHTTPClient
// succeeds for an expired token — the oauth2.TokenSource handles refresh lazily.
func TestGetHTTPClient_ExpiredToken_TokenSourceConfigured(t *testing.T) {
	refreshCount := 0
	srv := tokenRefreshServer(t, &refreshCount)
	defer srv.Close()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	t.Cleanup(func() { m.Close() })

	conn := makeConn("conn-expired", ProviderGoogle)
	if err := store.Add(conn); err != nil {
		t.Fatal(err)
	}

	expiredToken := &oauth2.Token{
		AccessToken:  "expired-access",
		RefreshToken: "still-valid-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour), // already expired
	}
	if err := secrets.StoreToken(conn.ID, expiredToken); err != nil {
		t.Fatal(err)
	}

	p := &fakeTokenProvider{tokenURL: srv.URL}
	client, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err != nil {
		t.Fatalf("GetHTTPClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}
	// The client is returned successfully; token refresh happens lazily on first use.
}

// TestGetHTTPClient_ValidToken_DoesNotRefreshImmediately verifies that a valid
// token does not cause an immediate refresh call.
func TestGetHTTPClient_ValidToken_DoesNotRefreshImmediately(t *testing.T) {
	refreshCount := 0
	srv := tokenRefreshServer(t, &refreshCount)
	defer srv.Close()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	t.Cleanup(func() { m.Close() })

	conn := makeConn("conn-valid-token", ProviderGoogle)
	if err := store.Add(conn); err != nil {
		t.Fatal(err)
	}

	validToken := &oauth2.Token{
		AccessToken:  "still-valid-access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}
	if err := secrets.StoreToken(conn.ID, validToken); err != nil {
		t.Fatal(err)
	}

	p := &fakeTokenProvider{tokenURL: srv.URL}
	client, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err != nil {
		t.Fatalf("GetHTTPClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}
	// No refresh should have been triggered by GetHTTPClient itself.
	if refreshCount != 0 {
		t.Errorf("expected 0 refresh calls during GetHTTPClient, got %d", refreshCount)
	}
}

// TestGetHTTPClient_NotFound_ReturnsError verifies behavior when connection doesn't exist.
func TestGetHTTPClient_NotFound_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(store, NewMemoryStore(), "http://localhost/cb")
	t.Cleanup(func() { m.Close() })

	p := &fakeTokenProvider{tokenURL: "http://localhost/token"}
	_, err = m.GetHTTPClient(context.Background(), "nonexistent", p)
	if err == nil {
		t.Fatal("expected error for nonexistent connection")
	}
}

// TestGetHTTPClient_NoToken_ReturnsError verifies behavior when connection exists but no token stored.
func TestGetHTTPClient_NoToken_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	t.Cleanup(func() { m.Close() })

	conn := makeConn("conn-no-tok-refresh", ProviderGoogle)
	if err := store.Add(conn); err != nil {
		t.Fatal(err)
	}
	// Deliberately do NOT store a token.

	p := &fakeTokenProvider{tokenURL: "http://localhost/token"}
	_, err = m.GetHTTPClient(context.Background(), conn.ID, p)
	if err == nil {
		t.Fatal("expected error when no token is stored")
	}
}

// TestPersistingTokenSource_Token_PersistsRefreshedToken verifies that the
// persistingTokenSource writes back a freshly-issued token to secrets.
func TestPersistingTokenSource_Token_PersistsRefreshedToken(t *testing.T) {
	refreshCount := 0
	srv := tokenRefreshServer(t, &refreshCount)
	defer srv.Close()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost/cb")
	t.Cleanup(func() { m.Close() })

	conn := makeConn("conn-persist-refresh", ProviderGoogle)
	if err := store.Add(conn); err != nil {
		t.Fatal(err)
	}

	// Store an expired token with a refresh token.
	expiredToken := &oauth2.Token{
		AccessToken:  "old-access",
		RefreshToken: "good-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := secrets.StoreToken(conn.ID, expiredToken); err != nil {
		t.Fatal(err)
	}

	p := &fakeTokenProvider{tokenURL: srv.URL}

	// Build a persistingTokenSource directly (same as GetHTTPClient does internally).
	cfg := p.OAuthConfig("http://localhost/cb")
	ts := cfg.TokenSource(context.Background(), expiredToken)
	pts := &persistingTokenSource{
		inner:   ts,
		connID:  conn.ID,
		store:   store,
		secrets: secrets,
	}

	newTok, err := pts.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if newTok == nil {
		t.Fatal("expected non-nil token")
	}

	// Token() should have caused a refresh and re-persisted it.
	if refreshCount == 0 {
		t.Error("expected at least one refresh call to the token server")
	}

	// The secrets store should now have the new token.
	stored, err := secrets.GetToken(conn.ID)
	if err != nil {
		t.Fatalf("GetToken after refresh: %v", err)
	}
	if stored.AccessToken != "new-access-token" {
		t.Errorf("stored AccessToken = %q, want %q", stored.AccessToken, "new-access-token")
	}
}

// TestPersistingTokenSource_Token_ReturnsErrorOnRefreshFailure verifies that
// a failing token server causes Token() to return an error (not panic).
func TestPersistingTokenSource_Token_ReturnsErrorOnRefreshFailure(t *testing.T) {
	failSrv := tokenRefreshErrorServer(t)
	defer failSrv.Close()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()

	conn := makeConn("conn-fail-refresh", ProviderGoogle)
	if err := store.Add(conn); err != nil {
		t.Fatal(err)
	}

	// Expired token with a refresh token — so the library will attempt to refresh.
	expiredToken := &oauth2.Token{
		AccessToken:  "old",
		RefreshToken: "revoked-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := secrets.StoreToken(conn.ID, expiredToken); err != nil {
		t.Fatal(err)
	}

	p := &fakeTokenProvider{tokenURL: failSrv.URL}
	cfg := p.OAuthConfig("http://localhost/cb")
	ts := cfg.TokenSource(context.Background(), expiredToken)
	pts := &persistingTokenSource{
		inner:   ts,
		connID:  conn.ID,
		store:   store,
		secrets: secrets,
	}

	_, err = pts.Token()
	if err == nil {
		t.Fatal("expected error from failed token refresh")
	}
}
