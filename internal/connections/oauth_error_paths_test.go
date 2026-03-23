package connections

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestHandleOAuthCallback_UnknownState verifies that an unknown state token is rejected.
func TestHandleOAuthCallback_UnknownState_Returns_Error(t *testing.T) {
	m := newTestManager(t)
	_, err := m.HandleOAuthCallback(context.Background(), "no-such-state", "any-code")
	if err == nil {
		t.Fatal("expected error for unknown state, got nil")
	}
}

// TestHandleOAuthCallback_ExpiredState verifies that a state token that has passed
// its TTL is rejected even if it exists in the pending map.
func TestHandleOAuthCallback_ExpiredState_Returns_Error(t *testing.T) {
	m := newTestManager(t)

	// Manually inject an expired pending flow.
	m.mu.Lock()
	m.pendingFlows["expired-state"] = &pendingFlow{
		provider:     &fakeProvider{},
		config:       (&fakeProvider{}).OAuthConfig(m.redirectURL),
		codeVerifier: "verifier",
		redirectURL:  m.redirectURL,
		expiresAt:    time.Now().Add(-time.Minute), // already expired
	}
	m.mu.Unlock()

	_, err := m.HandleOAuthCallback(context.Background(), "expired-state", "code")
	if err == nil {
		t.Fatal("expected error for expired state, got nil")
	}
}

// TestPurgeStalePendingFlows_RemovesExpired verifies that expired flows are removed
// while valid flows are kept.
func TestPurgeStalePendingFlows_RemovesExpired(t *testing.T) {
	m := newTestManager(t)

	m.mu.Lock()
	// Add one expired flow.
	m.pendingFlows["old"] = &pendingFlow{expiresAt: time.Now().Add(-time.Second)}
	// Add one valid flow.
	m.pendingFlows["new"] = &pendingFlow{expiresAt: time.Now().Add(time.Hour)}
	m.purgeStalePendingFlows()
	m.mu.Unlock()

	m.mu.Lock()
	_, oldOk := m.pendingFlows["old"]
	_, newOk := m.pendingFlows["new"]
	m.mu.Unlock()

	if oldOk {
		t.Error("expected expired flow 'old' to be purged")
	}
	if !newOk {
		t.Error("expected valid flow 'new' to be kept")
	}
}

// TestPurgeStalePendingFlows_EmptyMap verifies it doesn't panic on an empty map.
func TestPurgeStalePendingFlows_EmptyMap_NoPanic(t *testing.T) {
	m := newTestManager(t)
	m.mu.Lock()
	m.purgeStalePendingFlows() // must not panic
	m.mu.Unlock()
}

// TestSetDefaultConnection_SetsDefault verifies that SetDefaultConnection works
// on an existing connection.
func TestSetDefaultConnection_SetsDefault(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NewManager(store, NewMemoryStore(), "http://localhost:9999/oauth/callback")
	defer m.Close()

	// Add a connection manually.
	conn := Connection{
		ID:           "conn-1",
		Provider:     ProviderGoogle,
		AccountLabel: "alice@example.com",
		AccountID:    "alice-id",
		CreatedAt:    time.Now(),
	}
	if err := store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	// SetDefault should succeed.
	if err := m.SetDefaultConnection("conn-1"); err != nil {
		t.Fatalf("SetDefaultConnection: %v", err)
	}
}

// TestSetDefaultConnection_NonexistentConnection verifies that setting default for
// a nonexistent connection returns an error.
func TestSetDefaultConnection_NonexistentConnection_Error(t *testing.T) {
	m := newTestManager(t)
	err := m.SetDefaultConnection("nonexistent-conn-id")
	if err == nil {
		t.Fatal("expected error for nonexistent connection, got nil")
	}
}

// TestRemoveConnection_Success verifies that RemoveConnection deletes the connection.
func TestRemoveConnection_RemovesFromStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:9999/oauth/callback")
	defer m.Close()

	conn := Connection{
		ID:       "conn-to-remove",
		Provider: ProviderGoogle,
	}
	if err := store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	if err := m.RemoveConnection("conn-to-remove"); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}

	if _, ok := store.Get("conn-to-remove"); ok {
		t.Error("expected connection to be removed from store")
	}
}

// TestManagerClose_StopsBackgroundGoroutine verifies that Close doesn't block
// and idempotent Close calls don't panic.
func TestManagerClose_Idempotent(t *testing.T) {
	m := newTestManager(t)
	m.Close()
	m.Close() // second close must not panic
}
