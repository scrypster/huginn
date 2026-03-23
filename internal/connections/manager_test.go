package connections

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// fakeProvider is a minimal IntegrationProvider for testing.
type fakeProvider struct{}

func (f *fakeProvider) Name() Provider        { return ProviderGoogle }
func (f *fakeProvider) DisplayName() string   { return "Google (test)" }
func (f *fakeProvider) OAuthConfig(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
}
func (f *fakeProvider) GetAccountInfo(_ context.Context, _ *http.Client) (*AccountInfo, error) {
	return &AccountInfo{
		ID:    "fake-account-id",
		Label: "test@example.com",
	}, nil
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	return NewManager(store, secrets, "http://localhost:9999/oauth/callback")
}

func TestManagerStartOAuthFlow_GeneratesURL(t *testing.T) {
	m := newTestManager(t)
	p := &fakeProvider{}

	authURL, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	if authURL == "" {
		t.Fatal("expected non-empty auth URL")
	}

	// Verify required OAuth + PKCE parameters are present in the URL
	for _, param := range []string{"state=", "code_challenge=", "code_challenge_method=S256"} {
		if !strings.Contains(authURL, param) {
			t.Errorf("authURL missing expected parameter %q\nURL: %s", param, authURL)
		}
	}

	// Verify that a pending flow was stored (state is in pendingFlows)
	m.mu.Lock()
	pendingCount := len(m.pendingFlows)
	m.mu.Unlock()
	if pendingCount != 1 {
		t.Errorf("expected 1 pending flow, got %d", pendingCount)
	}
}

func TestManagerHandleCallback_UnknownState(t *testing.T) {
	m := newTestManager(t)

	_, err := m.HandleOAuthCallback(context.Background(), "unknown-state-token", "some-code")
	if err == nil {
		t.Fatal("expected error for unknown state, got nil")
	}
	if !strings.Contains(err.Error(), "unknown or expired state") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManager_SetRedirectURL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:8000/oauth/callback")
	// Must not panic
	m.SetRedirectURL("http://localhost:9999/oauth/callback")
}

func TestManagerRemoveConnection(t *testing.T) {
	m := newTestManager(t)

	// Manually add a connection + token to simulate a completed OAuth flow
	conn := makeConn("conn-remove-test", ProviderGoogle)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	if err := m.secrets.StoreToken(conn.ID, sampleToken()); err != nil {
		t.Fatalf("secrets.StoreToken: %v", err)
	}

	// Verify it's there
	_, ok := m.store.Get(conn.ID)
	if !ok {
		t.Fatal("expected connection in store before removal")
	}

	// Remove it
	if err := m.RemoveConnection(conn.ID); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}

	// Verify it's gone from the store
	_, ok = m.store.Get(conn.ID)
	if ok {
		t.Fatal("expected connection to be removed from store")
	}

	// Verify the token is also gone
	_, err := m.secrets.GetToken(conn.ID)
	if err == nil {
		t.Fatal("expected token to be deleted after RemoveConnection")
	}

	// Removing a non-existent connection should return an error
	if err := m.RemoveConnection("nonexistent-id"); err == nil {
		t.Fatal("expected error removing nonexistent connection")
	}
}
