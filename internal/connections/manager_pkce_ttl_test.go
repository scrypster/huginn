package connections

// manager_pkce_ttl_test.go — Tests for PKCE server-side verification and
// configurable pendingFlowTTL.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// ─── verifyPKCE ────────────────────────────────────────────────────────────────

// TestVerifyPKCE_ValidPair verifies that a correctly derived challenge passes.
func TestVerifyPKCE_ValidPair(t *testing.T) {
	verifier := "test-code-verifier-1234567890abcdef"
	h := sha256.New()
	h.Write([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	if !verifyPKCE(verifier, challenge) {
		t.Error("expected verifyPKCE to return true for a valid pair")
	}
}

// TestVerifyPKCE_TamperedChallenge verifies that a mismatched challenge fails.
func TestVerifyPKCE_TamperedChallenge(t *testing.T) {
	verifier := "my-verifier"
	wrongChallenge := base64.RawURLEncoding.EncodeToString([]byte("completely-wrong"))

	if verifyPKCE(verifier, wrongChallenge) {
		t.Error("expected verifyPKCE to return false for a tampered challenge")
	}
}

// TestVerifyPKCE_EmptyVerifier verifies that an empty verifier fails against any challenge.
func TestVerifyPKCE_EmptyVerifier(t *testing.T) {
	h := sha256.New()
	h.Write([]byte(""))
	challengeForEmpty := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	// An empty verifier should produce a specific (predictable) hash.
	// If someone tampers the stored verifier to be empty, it should only match
	// the challenge derived from an empty string — a legitimate challenge
	// derived from a 32-byte random verifier will not match.
	if !verifyPKCE("", challengeForEmpty) {
		t.Error("verifyPKCE should return true when empty verifier matches challenge for empty string")
	}
	if verifyPKCE("", "anything-else") {
		t.Error("verifyPKCE should return false when empty verifier does not match challenge")
	}
}

// ─── PKCE stored in pendingFlow ────────────────────────────────────────────────

// TestStartOAuthFlow_StoresCodeChallenge verifies that StartOAuthFlow stores the
// code_challenge in the pendingFlow alongside the code_verifier.
func TestStartOAuthFlow_StoresCodeChallenge(t *testing.T) {
	m := newTestManager(t)
	p := &fakeProvider{}

	_, err := m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.pendingFlows) != 1 {
		t.Fatalf("expected 1 pending flow, got %d", len(m.pendingFlows))
	}
	for _, flow := range m.pendingFlows {
		if flow.codeVerifier == "" {
			t.Error("codeVerifier must be non-empty")
		}
		if flow.codeChallenge == "" {
			t.Error("codeChallenge must be non-empty")
		}
		// Verify internal consistency: challenge == SHA256(verifier) base64url-encoded.
		if !verifyPKCE(flow.codeVerifier, flow.codeChallenge) {
			t.Error("stored codeChallenge does not match SHA256(codeVerifier)")
		}
	}
}

// TestHandleOAuthCallback_PKCETampered verifies that HandleOAuthCallback rejects
// a callback when the pending flow's PKCE pair is internally inconsistent
// (simulating an attacker that tampered with the stored flow state).
func TestHandleOAuthCallback_PKCETampered(t *testing.T) {
	m := newTestManager(t)

	// Inject a flow with a mismatched verifier/challenge pair directly.
	m.mu.Lock()
	m.pendingFlows["tampered-state"] = &pendingFlow{
		provider:      &fakeProvider{},
		config:        (&fakeProvider{}).OAuthConfig("http://localhost:9999/oauth/callback"),
		codeVerifier:  "real-verifier",
		codeChallenge: base64.RawURLEncoding.EncodeToString([]byte("wrong-challenge")), // tampered
		redirectURL:   "http://localhost:9999/oauth/callback",
		expiresAt:     time.Now().Add(10 * time.Minute),
	}
	m.mu.Unlock()

	_, err := m.HandleOAuthCallback(context.Background(), "tampered-state", "any-code")
	if err == nil {
		t.Fatal("expected PKCE verification error, got nil")
	}
	if !strings.Contains(err.Error(), "PKCE verification failed") {
		t.Errorf("expected 'PKCE verification failed' in error, got: %v", err)
	}
}

// ─── WithPendingFlowTTL ────────────────────────────────────────────────────────

// TestWithPendingFlowTTL_ShortTTL verifies that flows older than the configured
// TTL are purged and rejected in HandleOAuthCallback.
func TestWithPendingFlowTTL_ShortTTL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Configure a very short TTL so we can exercise expiry without sleeping.
	shortTTL := 10 * time.Millisecond
	m := NewManager(store, NewMemoryStore(), "http://localhost:9999/oauth/callback",
		WithPendingFlowTTL(shortTTL))
	t.Cleanup(func() { m.Close() })

	p := &fakeProvider{}
	_, err = m.StartOAuthFlow(p)
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}

	// Capture state before TTL expires.
	m.mu.Lock()
	var state string
	for k := range m.pendingFlows {
		state = k
	}
	m.mu.Unlock()

	if state == "" {
		t.Fatal("no pending flow registered")
	}

	// Wait for the TTL to expire.
	time.Sleep(shortTTL + 5*time.Millisecond)

	// Now the flow should be rejected by HandleOAuthCallback.
	_, err = m.HandleOAuthCallback(context.Background(), state, "some-code")
	if err == nil {
		t.Fatal("expected error for TTL-expired flow, got nil")
	}
}

// TestWithPendingFlowTTL_DefaultIsPreserved verifies that the default TTL is
// defaultPendingFlowTTL (10 minutes) when no option is provided.
func TestWithPendingFlowTTL_DefaultIsPreserved(t *testing.T) {
	m := newTestManager(t)

	if m.pendingFlowTTL != defaultPendingFlowTTL {
		t.Errorf("expected default TTL %v, got %v", defaultPendingFlowTTL, m.pendingFlowTTL)
	}
}

// TestWithPendingFlowTTL_CustomTTL verifies that WithPendingFlowTTL overrides
// the default and that the custom value is used when setting flow expiry.
func TestWithPendingFlowTTL_CustomTTL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	customTTL := 3 * time.Minute
	m := NewManager(store, NewMemoryStore(), "http://localhost:9999/oauth/callback",
		WithPendingFlowTTL(customTTL))
	t.Cleanup(func() { m.Close() })

	if m.pendingFlowTTL != customTTL {
		t.Errorf("expected custom TTL %v, got %v", customTTL, m.pendingFlowTTL)
	}

	// Start a flow and verify its expiresAt is approximately now + customTTL.
	before := time.Now()
	_, err = m.StartOAuthFlow(&fakeProvider{})
	if err != nil {
		t.Fatalf("StartOAuthFlow: %v", err)
	}
	after := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, flow := range m.pendingFlows {
		if flow.expiresAt.Before(before.Add(customTTL)) || flow.expiresAt.After(after.Add(customTTL+time.Second)) {
			t.Errorf("flow.expiresAt = %v, expected roughly %v + %v", flow.expiresAt, before, customTTL)
		}
	}
}

// ─── KeychainStore security comment smoke test ─────────────────────────────────

// TestKeychainStore_TypeExists is a compile-time assertion that KeychainStore
// implements SecretStore — ensuring the security-model comment stays accurate.
func TestKeychainStore_TypeExists(t *testing.T) {
	var _ SecretStore = &KeychainStore{}
}

// ─── validateMCPTool equivalent — token security ───────────────────────────────

// TestMemoryStore_NoPersistence verifies that MemoryStore is ephemeral:
// creating a new instance does not inherit data from a previous instance.
// This confirms the documented security property (no plaintext file on disk).
func TestMemoryStore_NoPersistence(t *testing.T) {
	s1 := NewMemoryStore()
	tok := &oauth2.Token{AccessToken: "secret", TokenType: "Bearer"}

	if err := s1.StoreToken("conn1", tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	// A brand-new MemoryStore instance has no knowledge of s1's data.
	s2 := NewMemoryStore()
	_, err := s2.GetToken("conn1")
	if err == nil {
		t.Error("expected error: new MemoryStore should not inherit previous instance's tokens")
	}
}
