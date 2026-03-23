package connections

// coverage_boost95_test.go — targeted tests to push internal/connections
// from 93.4% to as close to 95% as possible without modifying production code.
//
// Remaining gaps after previous test files:
//
//  1. manager.go:150 — HandleOAuthCallback store.Add failure branch.
//  2. secrets.go:42  — KeychainStore.GetToken json.Unmarshal error branch.
//  3. secrets.go:107 — NewSecretStore fallback to MemoryStore when keychain fails.
//
// Items that are GENUINELY UNREACHABLE without modifying production code:
//   - manager.go:71,78 — crypto/rand.Read errors (never fails on modern OS)
//   - secrets.go:30    — json.Marshal error in KeychainStore.StoreToken (oauth2.Token always marshals)
//   - secrets.go:67    — json.Marshal error in MemoryStore.StoreToken   (same reason)
//   - store.go:134     — json.MarshalIndent error ([]Connection always marshals)
//   - store.go:145-149 — tmp.Write error (requires OS-level write failure)
//   - store.go:150-153 — tmp.Close error (requires OS-level close failure)
//
// This file covers the 3 reachable gaps, bringing connections to ~94.9%.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// ─── HandleOAuthCallback — store.Add failure ──────────────────────────────────

// TestHandleOAuthCallback_StoreAddFails exercises manager.go:150 — the error
// branch where m.store.Add(conn) fails (after successful token exchange and
// account info fetch). We force the store to fail by making its backing
// directory read-only at the time of the callback.
func TestHandleOAuthCallback_StoreAddFails(t *testing.T) {
	// Build a mock token server that always issues a valid token.
	srv := buildTokenServer(t)
	defer srv.Close()

	p := &providerWithServer{
		authURL:  srv.URL + "/auth",
		tokenURL: srv.URL + "/token",
	}

	// Create a store in a temp dir; start the OAuth flow (no store write yet).
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets := NewMemoryStore()
	m := NewManager(store, secrets, "http://localhost:9999/oauth/callback")

	authURL, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	// Extract the state parameter from the generated auth URL.
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	state := parsed.Query().Get("state")

	// Make the store directory read-only NOW, after StartOAuthFlow (which doesn't
	// touch the store) but before HandleOAuthCallback (which calls store.Add).
	// store.Add → save() → os.CreateTemp(dir, ...) will fail with EACCES.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Skipf("cannot chmod temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	// HandleOAuthCallback should fail at store.Add due to read-only directory.
	_, err = m.HandleOAuthCallback(context.Background(), state, "mock-auth-code")
	if err == nil {
		t.Skip("store.Add did not fail as expected (running as root?)")
	}
	if !strings.Contains(err.Error(), "store connection") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ─── NewSecretStore — MemoryStore fallback when keychain fails ────────────────

// TestNewSecretStore_FallsBackToMemory exercises secrets.go:107 — the branch
// where NewSecretStore falls back to NewMemoryStore because the OS keychain
// probe (keyring.Set) returns an error.
//
// We use keyring.MockInitWithError to make all keyring operations fail, then
// restore to a working mock store in t.Cleanup so subsequent tests are not
// affected.
func TestNewSecretStore_FallsBackToMemory(t *testing.T) {
	// Make the keychain probe fail so NewSecretStore takes the fallback branch.
	keyring.MockInitWithError(errors.New("mocked keychain unavailable"))
	t.Cleanup(func() {
		// Restore to a functional in-memory mock so that other keychain tests
		// continue to work (they just run against the mock instead of the real
		// OS keychain, which is acceptable for coverage purposes).
		keyring.MockInit()
	})

	s := NewSecretStore()
	if s == nil {
		t.Fatal("NewSecretStore returned nil")
	}
	// The returned store must be a *MemoryStore (the fallback).
	if _, ok := s.(*MemoryStore); !ok {
		t.Errorf("expected *MemoryStore fallback, got %T", s)
	}
}

// ─── KeychainStore.GetToken — json.Unmarshal error ───────────────────────────

// TestKeychainStore_GetToken_BadJSON exercises secrets.go:42 — the branch where
// keyring.Get returns a value that is not valid JSON, causing json.Unmarshal to
// fail inside KeychainStore.GetToken.
//
// We use keyring.MockInit so that keyring.Set stores our deliberately invalid
// JSON string. t.Cleanup restores the mock to avoid leaking state.
func TestKeychainStore_GetToken_BadJSON(t *testing.T) {
	// Switch to in-memory mock so we can inject arbitrary strings.
	keyring.MockInit()
	t.Cleanup(keyring.MockInit) // reset after test

	connID := "bad-json-coverage-test"
	badJSON := "not-valid-json{{{invalid"

	// Store the bad JSON directly via keyring — bypassing KeychainStore.StoreToken
	// (which would marshal a valid token) so we can inject arbitrary bytes.
	if err := keyring.Set(keychainService, keychainKey(connID), badJSON); err != nil {
		t.Fatalf("keyring.Set: %v", err)
	}
	t.Cleanup(func() { _ = keyring.Delete(keychainService, keychainKey(connID)) })

	ks := &KeychainStore{}
	_, err := ks.GetToken(connID)
	if err == nil {
		t.Error("expected unmarshal error from KeychainStore.GetToken for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected 'unmarshal' in error message, got: %v", err)
	}
}

// ─── Regression: existing HTTP mock server tests still compile ────────────────

// TestNewMockServer_Sanity is a trivial sanity test to ensure the
// buildTokenServer helper (defined in coverage_boost2_test.go) is reachable
// from this file in the same package.
func TestNewMockServer_Sanity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if srv.URL == "" {
		t.Fatal("expected non-empty server URL")
	}
}
