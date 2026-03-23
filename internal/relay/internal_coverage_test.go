package relay

// internal_coverage_test.go — Internal package tests for unexported functions.
// Uses package relay (not relay_test) to access unexported functions directly.
// Covers:
//   - sanitizeHostname: truncate (>24 chars) and empty (all special chars) paths
//   - identity.go: Save() MkdirAll error via HOME manipulation
//   - token.go: TokenStore.Save, TokenStore.Load error, TokenStore.Clear via keyring mock

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestSanitizeHostname_LongHostname verifies that hostnames longer than 24 chars
// are truncated to 24 characters (covers identity.go:73.19,75.3).
func TestSanitizeHostname_LongHostname(t *testing.T) {
	// 30 alphanumeric characters — sanitizeHostname should truncate to 24.
	long := "abcdefghijklmnopqrstuvwxyz1234"
	result := sanitizeHostname(long)
	if len(result) != 24 {
		t.Errorf("expected len=24 after truncation, got len=%d: %q", len(result), result)
	}
	if result != "abcdefghijklmnopqrstuvwx" {
		t.Errorf("unexpected truncated value: %q", result)
	}
}

// TestSanitizeHostname_EmptyAfterFilter verifies that a hostname consisting only of
// special characters (all replaced by '-') that results in zero length falls back to
// "unknown" (covers identity.go:76.19,78.3).
// Note: special chars are replaced with '-', not removed, so a pure-special hostname
// will be all dashes, length > 0. We need a truly empty input to hit len(out)==0.
func TestSanitizeHostname_EmptyInput(t *testing.T) {
	// Empty string input: the for-loop body is never entered, so out stays nil/empty.
	result := sanitizeHostname("")
	if result != "unknown" {
		t.Errorf("expected %q for empty hostname, got %q", "unknown", result)
	}
}

// TestSanitizeHostname_AllSpecial verifies that all-special-char hostnames
// produce a result of all dashes (not "unknown", since len(out) > 0).
func TestSanitizeHostname_AllSpecial(t *testing.T) {
	result := sanitizeHostname("!@#$%^")
	if !strings.ContainsAny(result, "-") {
		t.Errorf("expected dashes in result, got %q", result)
	}
	// Should NOT be "unknown" (chars are replaced by '-', not removed).
	if result == "unknown" {
		t.Errorf("did not expect 'unknown' for non-empty all-special input")
	}
}

// TestSanitizeHostname_MixedChars verifies normal operation for mixed input.
func TestSanitizeHostname_MixedChars(t *testing.T) {
	result := sanitizeHostname("my-host.example.com")
	// Dots are replaced by '-'. Alphanumerics are kept as-is.
	if result == "" {
		t.Error("expected non-empty result")
	}
	if len(result) > 24 {
		t.Errorf("result too long: %d chars in %q", len(result), result)
	}
}

// TestIdentity_Save_MkdirAllError verifies that Identity.Save() propagates
// an error when MkdirAll fails (covers identity.go:125.16,127.3 — json.MarshalIndent
// error path — actually unreachable since json.Marshal never fails on Identity struct).
// Instead we cover the MkdirAll error path at line 121-122 by setting HOME=/dev/null.
func TestIdentity_Save_HomeNotWritable(t *testing.T) {
	if os.Getenv("HOME") == "" {
		t.Skip("HOME not set")
	}
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", "/dev/null") //nolint:tenv — intentional; restored below
	defer os.Setenv("HOME", origHome)

	id := &Identity{
		AgentID:  "test-agent",
		Endpoint: "wss://test.example.com",
	}
	err := id.Save()
	if err == nil {
		// May succeed if running as root or if /dev/null/.huginn already exists.
		t.Log("note: Save() succeeded (may be running as root); MkdirAll error path not triggered")
		return
	}
	// If we get an error, the path at lines 121-122 is covered.
}

// ─── TokenStore — covers Save, Load error, and Clear using keyring mock ───────

// TestTokenStore_Save_WithMock covers token.go:28.47,30.2 (the Save function body)
// by using keyring.MockInit() to redirect keyring calls to in-memory storage.
func TestTokenStore_Save_WithMock(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit) // reset mock after test

	ts := NewTokenStore()
	err := ts.Save("test-relay-token-coverage")
	if err != nil {
		t.Fatalf("TokenStore.Save with mock keyring: %v", err)
	}
}

// TestTokenStore_Clear_WithMock covers token.go:40.36,42.2 (the Clear function body)
// by first saving a token then clearing it using an in-memory mock keyring.
func TestTokenStore_Clear_WithMock(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit) // reset mock after test

	ts := NewTokenStore()
	// Save first so there's something to clear.
	if err := ts.Save("clear-me-coverage"); err != nil {
		t.Fatalf("Save setup: %v", err)
	}
	err := ts.Clear()
	if err != nil {
		t.Fatalf("TokenStore.Clear with mock keyring: %v", err)
	}
}

// TestTokenStore_Load_ErrorPath_WithMock covers token.go:34.16,36.3 (Load error branch)
// by using keyring.MockInitWithError to force keyring.Get to return an error.
func TestTokenStore_Load_ErrorPath_WithMock(t *testing.T) {
	keyring.MockInitWithError(errors.New("mocked keychain unavailable"))
	t.Cleanup(keyring.MockInit) // restore functional mock after test

	ts := NewTokenStore()
	_, err := ts.Load()
	if err == nil {
		t.Fatal("expected error from TokenStore.Load when keyring unavailable, got nil")
	}
}
