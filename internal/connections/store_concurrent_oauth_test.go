package connections

// hardening_iter7_test.go — Iteration 7 hardening tests for connections package.
// Covers:
//   - Store concurrent writes (race safety with -race flag)
//   - Store reload persists exact data across instances
//   - Manager.StartOAuthFlow concurrent calls (race safety)
//   - Manager.RemoveConnection on non-existent + token cleanup
//   - Connection metadata JSON roundtrip (nil vs empty map)
//   - Store.ListByProvider returns copy (not shared slice)
//   - purgeStalePendingFlows only purges expired, not valid flows
//   - Multiple connections same provider can coexist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// ---------------------------------------------------------------------------
// Store concurrent writes (race detector)
// Multiple goroutines adding connections simultaneously must not corrupt data.
// ---------------------------------------------------------------------------

func TestStoreConcurrentWrites_RaceFree(t *testing.T) {
	s := newTestStore(t)

	var wg sync.WaitGroup
	const goroutines = 8
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			conn := makeConn(fmt.Sprintf("concurrent-conn-%d", n), ProviderGoogle)
			if err := s.Add(conn); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Add error: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != goroutines {
		t.Errorf("expected %d connections, got %d", goroutines, len(list))
	}
}

// ---------------------------------------------------------------------------
// Store reload — persists exact data across instances
// Create store, add data, create new store from same file, verify data matches.
// ---------------------------------------------------------------------------

func TestStoreReload_DataIntegrity(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")

	s1, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore s1: %v", err)
	}

	expiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	conn := Connection{
		ID:           "reload-test-id",
		Provider:     ProviderGitHub,
		Type:         ConnectionTypeOAuth,
		AccountLabel: "user@github.com",
		AccountID:    "gh-12345",
		Scopes:       []string{"repo", "read:user"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		ExpiresAt:    expiry,
		Metadata:     map[string]string{"install_id": "abc123"},
	}
	if err := s1.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Open the same file as a new store instance.
	s2, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore s2: %v", err)
	}

	list, err := s2.List()
	if err != nil {
		t.Fatalf("s2.List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(list))
	}

	got := list[0]
	if got.ID != conn.ID {
		t.Errorf("ID: got %q, want %q", got.ID, conn.ID)
	}
	if got.Provider != conn.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, conn.Provider)
	}
	if got.Type != conn.Type {
		t.Errorf("Type: got %q, want %q", got.Type, conn.Type)
	}
	if got.AccountLabel != conn.AccountLabel {
		t.Errorf("AccountLabel: got %q, want %q", got.AccountLabel, conn.AccountLabel)
	}
	if got.AccountID != conn.AccountID {
		t.Errorf("AccountID: got %q, want %q", got.AccountID, conn.AccountID)
	}
	if len(got.Scopes) != len(conn.Scopes) {
		t.Errorf("Scopes: got %v, want %v", got.Scopes, conn.Scopes)
	}
	if !got.ExpiresAt.Equal(conn.ExpiresAt) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, conn.ExpiresAt)
	}
	if got.Metadata["install_id"] != "abc123" {
		t.Errorf("Metadata install_id: got %q, want abc123", got.Metadata["install_id"])
	}
}

// ---------------------------------------------------------------------------
// Store.ListByProvider — returns independent copy, not shared slice
// Mutating the returned slice should not affect internal store state.
// ---------------------------------------------------------------------------

func TestStoreListByProvider_ReturnsCopy(t *testing.T) {
	s := newTestStore(t)
	conn := makeConn("copy-test", ProviderSlack)
	if err := s.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	list1, err := s.ListByProvider(ProviderSlack)
	if err != nil {
		t.Fatalf("ListByProvider: %v", err)
	}
	if len(list1) != 1 {
		t.Fatalf("expected 1, got %d", len(list1))
	}

	// Mutate the returned slice item.
	list1[0].AccountLabel = "MUTATED"

	// The store should still return the original value.
	list2, err := s.ListByProvider(ProviderSlack)
	if err != nil {
		t.Fatalf("ListByProvider 2: %v", err)
	}
	if list2[0].AccountLabel == "MUTATED" {
		t.Error("ListByProvider returned a shared reference; mutation affected internal state")
	}
}

// ---------------------------------------------------------------------------
// Manager.StartOAuthFlow concurrent calls — race safe
// ---------------------------------------------------------------------------

func TestManagerStartOAuthFlow_Concurrent_RaceFree(t *testing.T) {
	m := newTestManager(t)
	p := &fakeProvider{}

	const goroutines = 8
	authURLs := make(chan string, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := m.StartOAuthFlow(p)
			if err != nil {
				t.Errorf("StartOAuthFlow: %v", err)
				return
			}
			authURLs <- u
		}()
	}
	wg.Wait()
	close(authURLs)

	// Collect unique state tokens — all must be unique.
	seen := map[string]bool{}
	for u := range authURLs {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("parse URL: %v", err)
			continue
		}
		state := parsed.Query().Get("state")
		if seen[state] {
			t.Errorf("duplicate state token: %s", state)
		}
		seen[state] = true
	}
	if len(seen) != goroutines {
		t.Errorf("expected %d unique states, got %d", goroutines, len(seen))
	}
}

// ---------------------------------------------------------------------------
// purgeStalePendingFlows — only purges expired flows, not valid ones
// ---------------------------------------------------------------------------

func TestManager_PurgeStalePendingFlows_PreservesValid(t *testing.T) {
	m := newTestManager(t)

	// Insert one expired flow and one still-valid flow.
	m.mu.Lock()
	m.pendingFlows["expired-flow"] = &pendingFlow{
		provider:     &fakeProvider{},
		config:       (&fakeProvider{}).OAuthConfig("http://localhost/cb"),
		codeVerifier: "expired-verifier",
		redirectURL:  "http://localhost/cb",
		expiresAt:    time.Now().Add(-5 * time.Minute), // already expired
	}
	m.pendingFlows["valid-flow"] = &pendingFlow{
		provider:     &fakeProvider{},
		config:       (&fakeProvider{}).OAuthConfig("http://localhost/cb"),
		codeVerifier: "valid-verifier",
		redirectURL:  "http://localhost/cb",
		expiresAt:    time.Now().Add(5 * time.Minute), // still valid
	}
	m.purgeStalePendingFlows()
	_, expiredStillPresent := m.pendingFlows["expired-flow"]
	_, validStillPresent := m.pendingFlows["valid-flow"]
	m.mu.Unlock()

	if expiredStillPresent {
		t.Error("expected expired flow to be purged")
	}
	if !validStillPresent {
		t.Error("expected valid flow to be preserved")
	}
}

// ---------------------------------------------------------------------------
// Multiple connections with the same provider can coexist
// ---------------------------------------------------------------------------

func TestStore_MultipleConnectionsSameProvider(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		conn := makeConn(fmt.Sprintf("multi-slack-%d", i), ProviderSlack)
		conn.AccountLabel = fmt.Sprintf("workspace%d@slack.com", i)
		if err := s.Add(conn); err != nil {
			t.Fatalf("Add[%d]: %v", i, err)
		}
	}

	list, err := s.ListByProvider(ProviderSlack)
	if err != nil {
		t.Fatalf("ListByProvider: %v", err)
	}
	if len(list) != 5 {
		t.Errorf("expected 5 Slack connections, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// Connection JSON roundtrip — nil Metadata omitted, non-nil included
// ---------------------------------------------------------------------------

func TestConnection_NilMetadata_OmittedFromJSON(t *testing.T) {
	conn := Connection{
		ID:       "test-id",
		Provider: ProviderGoogle,
		Metadata: nil,
	}
	data, err := json.Marshal(conn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	// With omitempty, nil map should not appear in JSON.
	if containsStr(s, `"metadata"`) {
		t.Errorf("expected 'metadata' to be omitted for nil map, got: %s", s)
	}
}

func TestConnection_NonNilMetadata_IncludedInJSON(t *testing.T) {
	conn := Connection{
		ID:       "test-id",
		Provider: ProviderGoogle,
		Metadata: map[string]string{"key": "value"},
	}
	data, err := json.Marshal(conn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !containsStr(s, `"metadata"`) {
		t.Errorf("expected 'metadata' in JSON for non-nil map, got: %s", s)
	}
	if !containsStr(s, `"key"`) {
		t.Errorf("expected 'key' in metadata, got: %s", s)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

// ---------------------------------------------------------------------------
// MemoryStore — overwrite existing token
// Storing a second token for the same connID should replace the first.
// ---------------------------------------------------------------------------

func TestMemoryStore_OverwriteToken(t *testing.T) {
	m := NewMemoryStore()

	tok1 := &oauth2.Token{AccessToken: "first-token", TokenType: "Bearer"}
	tok2 := &oauth2.Token{AccessToken: "second-token", TokenType: "Bearer"}

	if err := m.StoreToken("conn-x", tok1); err != nil {
		t.Fatalf("StoreToken tok1: %v", err)
	}
	if err := m.StoreToken("conn-x", tok2); err != nil {
		t.Fatalf("StoreToken tok2: %v", err)
	}

	got, err := m.GetToken("conn-x")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != "second-token" {
		t.Errorf("expected second-token, got %q", got.AccessToken)
	}
}

// ---------------------------------------------------------------------------
// Store.UpdateExpiry — concurrent updates do not corrupt data
// ---------------------------------------------------------------------------

func TestStoreUpdateExpiry_Concurrent(t *testing.T) {
	s := newTestStore(t)

	conn := makeConn("expiry-concurrent", ProviderJira)
	if err := s.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 5

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			newExpiry := time.Now().UTC().Add(time.Duration(n) * time.Hour)
			// Ignore errors — some may win the race; we just want no panic/deadlock.
			_ = s.UpdateExpiry(conn.ID, newExpiry)
		}(i)
	}
	wg.Wait()

	// After concurrent updates, the connection should still exist.
	got, ok := s.Get(conn.ID)
	if !ok {
		t.Fatal("connection not found after concurrent UpdateExpiry calls")
	}
	if got.ID != conn.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, conn.ID)
	}
}

// ---------------------------------------------------------------------------
// Manager.HandleOAuthCallback — empty state string returns error
// ---------------------------------------------------------------------------

func TestManagerHandleCallback_EmptyState(t *testing.T) {
	m := newTestManager(t)

	_, err := m.HandleOAuthCallback(context.Background(), "", "any-code")
	if err == nil {
		t.Fatal("expected error for empty state, got nil")
	}
}

// ---------------------------------------------------------------------------
// Manager.RemoveConnection — best-effort token deletion (token missing is OK)
// ---------------------------------------------------------------------------

func TestManagerRemoveConnection_TokenAlreadyMissing(t *testing.T) {
	m := newTestManager(t)

	// Add connection to store but deliberately skip storing a token.
	conn := makeConn("no-token-conn", ProviderBitbucket)
	if err := m.store.Add(conn); err != nil {
		t.Fatalf("store.Add: %v", err)
	}

	// RemoveConnection should succeed even though no token was stored.
	// The token deletion is best-effort and its error is ignored.
	if err := m.RemoveConnection(conn.ID); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}

	_, ok := m.store.Get(conn.ID)
	if ok {
		t.Error("connection should have been removed from store")
	}
}

// ---------------------------------------------------------------------------
// Manager — concurrent RemoveConnection calls on different connections
// ---------------------------------------------------------------------------

func TestManagerRemoveConnection_Concurrent(t *testing.T) {
	m := newTestManager(t)
	secrets := m.secrets

	const count = 5
	ids := make([]string, count)

	for i := 0; i < count; i++ {
		ids[i] = fmt.Sprintf("rm-concurrent-%d", i)
		conn := makeConn(ids[i], ProviderGoogle)
		if err := m.store.Add(conn); err != nil {
			t.Fatalf("store.Add[%d]: %v", i, err)
		}
		if err := secrets.StoreToken(ids[i], sampleToken()); err != nil {
			t.Fatalf("StoreToken[%d]: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(connID string) {
			defer wg.Done()
			_ = m.RemoveConnection(connID)
		}(id)
	}
	wg.Wait()

	// All connections should be gone.
	list, err := m.store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 connections after removal, got %d", len(list))
	}
}
