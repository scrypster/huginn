// manager_ttl_test.go — tests for pending OAuth flow TTL/expiry behavior.
package connections

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestManager_PurgeStalePendingFlows verifies expired flows are cleaned up.
func TestManager_PurgeStalePendingFlows(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NewManager(store, NewMemoryStore(), "http://localhost:9999/oauth/callback")

	// Manually insert an already-expired pending flow.
	m.mu.Lock()
	m.pendingFlows["expired-state"] = &pendingFlow{
		provider:     &fakeProvider{},
		config:       nil,
		codeVerifier: "verifier",
		redirectURL:  "http://localhost/callback",
		expiresAt:    time.Now().Add(-1 * time.Second), // already expired
	}
	m.mu.Unlock()

	// Trigger a new StartOAuthFlow which calls purgeStalePendingFlows internally.
	_, err = m.StartOAuthFlow(&fakeProvider{})
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	_, stillPresent := m.pendingFlows["expired-state"]
	pendingCount := len(m.pendingFlows)
	m.mu.Unlock()

	if stillPresent {
		t.Error("expected expired flow to be purged, but it's still present")
	}
	// One new flow should exist (the one we just started).
	if pendingCount != 1 {
		t.Errorf("expected 1 pending flow (new), got %d", pendingCount)
	}
}

// TestManager_HandleCallback_ExpiredState verifies that a flow completed after its TTL is rejected.
func TestManager_HandleCallback_ExpiredState(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NewManager(store, NewMemoryStore(), "http://localhost:9999/oauth/callback")

	// Insert a flow that expires right now.
	m.mu.Lock()
	m.pendingFlows["test-expired"] = &pendingFlow{
		provider:     &fakeProvider{},
		config:       (&fakeProvider{}).OAuthConfig("http://localhost/callback"),
		codeVerifier: "some-verifier",
		redirectURL:  "http://localhost/callback",
		expiresAt:    time.Now().Add(-1 * time.Millisecond), // already expired
	}
	m.mu.Unlock()

	_, err = m.HandleOAuthCallback(context.Background(), "test-expired", "some-code")
	if err == nil {
		t.Fatal("expected error for expired state, got nil")
	}
}

// TestManager_StoreExternalToken verifies external token storage.
func TestManager_StoreExternalToken(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	tok := sampleToken()
	if err := m.StoreExternalToken(context.Background(), ProviderGoogle, tok, "user@example.com"); err != nil {
		t.Fatalf("StoreExternalToken: %v", err)
	}

	conns, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Provider != ProviderGoogle {
		t.Errorf("provider: want %s, got %s", ProviderGoogle, conns[0].Provider)
	}
	if conns[0].AccountLabel != "user@example.com" {
		t.Errorf("account label: want %q, got %q", "user@example.com", conns[0].AccountLabel)
	}
}

// TestManager_StoreExternalTokenWithMeta verifies external token storage with metadata.
func TestManager_StoreExternalTokenWithMeta(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	tok := sampleToken()
	meta := map[string]string{"workspace": "my-workspace", "team": "eng"}
	if err := m.StoreExternalTokenWithMeta(context.Background(), ProviderGitHub, tok, "github-user", meta); err != nil {
		t.Fatalf("StoreExternalTokenWithMeta: %v", err)
	}

	conns, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Metadata["workspace"] != "my-workspace" {
		t.Errorf("metadata workspace: want %q, got %q", "my-workspace", conns[0].Metadata["workspace"])
	}
}
