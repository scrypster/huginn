package connections

// confidence_boost_test.go — Iteration 4 targeted coverage improvements.
// Exercises Manager.GetHTTPClient, Manager.Close, Manager.StoreExternalToken,
// Manager.StoreExternalTokenWithMeta, and persistingTokenSource.Token.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// ─── failingSecretStore: a SecretStore that always fails on StoreToken ────────

var errStoreTokenFailed = errors.New("store token: simulated failure")

type failingSecretStore struct{}

func (f *failingSecretStore) StoreToken(_ string, _ *oauth2.Token) error {
	return errStoreTokenFailed
}
func (f *failingSecretStore) GetToken(_ string) (*oauth2.Token, error) {
	return nil, errStoreTokenFailed
}
func (f *failingSecretStore) DeleteToken(_ string) error { return nil }

func (f *failingSecretStore) StoreCredentials(_ string, _ map[string]string) error {
	return errStoreTokenFailed
}
func (f *failingSecretStore) GetCredentials(_ string) (map[string]string, error) {
	return nil, errStoreTokenFailed
}
func (f *failingSecretStore) DeleteCredentials(_ string) error { return nil }

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestManagerClose_NoOp verifies that Close() does not panic.
func TestManagerClose_NoOp(t *testing.T) {
	m := newTestManager(t)
	m.Close() // must not panic
}

// TestManagerGetHTTPClient_NotFound exercises the "connection not found" error branch.
func TestManagerGetHTTPClient_NotFound(t *testing.T) {
	m := newTestManager(t)
	p := &fakeProvider{}
	_, err := m.GetHTTPClient(context.Background(), "does-not-exist", p)
	if err == nil {
		t.Fatal("expected error for unknown connID")
	}
}

// TestManagerGetHTTPClient_NoToken exercises the "token not found" error branch.
func TestManagerGetHTTPClient_NoToken(t *testing.T) {
	m := newTestManager(t)
	// Add a connection but do NOT store a token.
	conn := makeConn("conn-no-tok", ProviderGoogle)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	p := &fakeProvider{}
	_, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err == nil {
		t.Fatal("expected error when token is not stored")
	}
}

// TestManagerGetHTTPClient_Success exercises the happy path.
func TestManagerGetHTTPClient_Success(t *testing.T) {
	m := newTestManager(t)
	conn := makeConn("conn-with-tok", ProviderGoogle)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	tok := sampleToken()
	if err := m.secrets.StoreToken(conn.ID, tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	p := &fakeProvider{}
	client, err := m.GetHTTPClient(context.Background(), conn.ID, p)
	if err != nil {
		t.Fatalf("GetHTTPClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil http.Client")
	}
}

// TestStoreExternalToken_Success exercises the successful path of StoreExternalToken.
func TestStoreExternalToken_Success(t *testing.T) {
	m := newTestManager(t)
	tok := &oauth2.Token{
		AccessToken: "ext-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}
	err := m.StoreExternalToken(context.Background(), ProviderGoogle, tok, "external@example.com")
	if err != nil {
		t.Fatalf("StoreExternalToken: %v", err)
	}
	conns, err2 := m.store.List()
	if err2 != nil {
		t.Fatalf("store.List: %v", err2)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].AccountLabel != "external@example.com" {
		t.Errorf("AccountLabel = %q, want %q", conns[0].AccountLabel, "external@example.com")
	}
	if conns[0].Type != ConnectionTypeOAuth {
		t.Errorf("Type = %q, want %q", conns[0].Type, ConnectionTypeOAuth)
	}
}

// TestStoreExternalToken_TokenStoreFails exercises rollback when StoreToken fails.
func TestStoreExternalToken_TokenStoreFails(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(store, &failingSecretStore{}, "http://localhost/cb")
	tok := &oauth2.Token{AccessToken: "tok", TokenType: "Bearer"}
	err = m.StoreExternalToken(context.Background(), ProviderSlack, tok, "label")
	if err == nil {
		t.Fatal("expected error when StoreToken fails")
	}
	// The connection should have been rolled back.
	conns2, _ := store.List()
	if len(conns2) != 0 {
		t.Error("expected connection to be rolled back after token store failure")
	}
}

// TestStoreExternalTokenWithMeta_Success exercises StoreExternalTokenWithMeta.
func TestStoreExternalTokenWithMeta_Success(t *testing.T) {
	m := newTestManager(t)
	tok := &oauth2.Token{
		AccessToken: "meta-access",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}
	meta := map[string]string{
		"instance_url": "https://example.atlassian.net",
		"cloud_id":     "abc123",
	}
	err := m.StoreExternalTokenWithMeta(context.Background(), ProviderJira, tok, "jira@example.com", meta)
	if err != nil {
		t.Fatalf("StoreExternalTokenWithMeta: %v", err)
	}
	conns, err2 := m.store.List()
	if err2 != nil {
		t.Fatalf("store.List: %v", err2)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Metadata["cloud_id"] != "abc123" {
		t.Errorf("Metadata cloud_id = %q, want %q", conns[0].Metadata["cloud_id"], "abc123")
	}
}

// TestStoreExternalTokenWithMeta_TokenStoreFails exercises rollback in StoreExternalTokenWithMeta.
func TestStoreExternalTokenWithMeta_TokenStoreFails(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(store, &failingSecretStore{}, "http://localhost/cb")
	tok := &oauth2.Token{AccessToken: "tok", TokenType: "Bearer"}
	err = m.StoreExternalTokenWithMeta(context.Background(), ProviderGitHub, tok, "gh@example.com", nil)
	if err == nil {
		t.Fatal("expected error when StoreToken fails")
	}
	conns, _ := store.List()
	if len(conns) != 0 {
		t.Error("expected rollback when token store fails")
	}
}

// TestNewSecretStore_ReturnsSomething verifies NewSecretStore returns a non-nil store.
// In CI (no keychain), it falls back to MemoryStore.
func TestNewSecretStore_ReturnsSomething(t *testing.T) {
	s := NewSecretStore()
	if s == nil {
		t.Fatal("NewSecretStore returned nil")
	}
}

// TestKeychainKey_Format verifies the key format used for the OS keychain.
func TestKeychainKey_Format(t *testing.T) {
	key := keychainKey("test-conn-id")
	expected := "connection/test-conn-id"
	if key != expected {
		t.Errorf("keychainKey = %q, want %q", key, expected)
	}
}

// TestMemoryStore_DeleteToken_NotFound verifies DeleteToken on missing key is a no-op.
func TestMemoryStore_DeleteToken_NotFound(t *testing.T) {
	m := NewMemoryStore()
	// Deleting a non-existent key must not error.
	if err := m.DeleteToken("nonexistent"); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// TestMemoryStore_GetToken_NotFound verifies GetToken returns error for missing key.
func TestMemoryStore_GetToken_NotFound(t *testing.T) {
	m := NewMemoryStore()
	_, err := m.GetToken("does-not-exist")
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}
