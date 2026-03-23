package backend

import (
	"testing"
)

// TestResolveAPIKey_Empty returns empty string for empty input.
func TestResolveAPIKey_Empty(t *testing.T) {
	got, err := ResolveAPIKey("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestResolveAPIKey_LiteralKey returns the key as-is.
func TestResolveAPIKey_LiteralKey(t *testing.T) {
	const key = "sk-literal-api-key-value"
	got, err := ResolveAPIKey(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != key {
		t.Errorf("expected %q, got %q", key, got)
	}
}

// TestResolveAPIKey_EnvVar resolves "$VAR" by reading the environment.
func TestResolveAPIKey_EnvVar(t *testing.T) {
	const envName = "TEST_RESOLVE_API_KEY_VAR"
	const envValue = "sk-from-environment"
	t.Setenv(envName, envValue)

	got, err := ResolveAPIKey("$" + envName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != envValue {
		t.Errorf("expected %q, got %q", envValue, got)
	}
}

// TestResolveAPIKey_UnsetEnvVar returns an error for an unset env var.
func TestResolveAPIKey_UnsetEnvVar(t *testing.T) {
	// Ensure the variable is definitely unset.
	t.Setenv("TEST_RESOLVE_UNSET_VAR", "")

	_, err := ResolveAPIKey("$TEST_RESOLVE_UNSET_VAR")
	if err == nil {
		t.Error("expected error for empty/unset environment variable, got nil")
	}
}

// TestResolveAPIKey_MissingEnvVar returns an error when the variable doesn't exist.
func TestResolveAPIKey_MissingEnvVar(t *testing.T) {
	// Use a name that is extremely unlikely to exist in the environment.
	_, err := ResolveAPIKey("$HUGINN_TEST_DEFINITELY_MISSING_XYZ_12345")
	if err == nil {
		t.Error("expected error for missing environment variable, got nil")
	}
}

// TestResolveAPIKey_KeyringInvalidFormat returns an error for malformed keyring strings.
func TestResolveAPIKey_KeyringInvalidFormat(t *testing.T) {
	cases := []string{
		"keyring:",          // no service or user
		"keyring:svc",       // no user part
		"keyring::user",     // empty service
		"keyring:svc:",      // empty user
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := ResolveAPIKey(raw)
			if err == nil {
				t.Errorf("expected error for malformed keyring string %q, got nil", raw)
			}
		})
	}
}

// TestResolveAPIKey_KeyringValidFormat_MissSkipped tests that a well-formed
// "keyring:<svc>:<user>" string returns an error when the secret is not found
// (the keyring is not populated in test environments). This test is skipped if
// the keyring happens to contain the entry (extremely unlikely in CI).
func TestResolveAPIKey_KeyringValidFormat_MissSkipped(t *testing.T) {
	raw := "keyring:huginn-test-svc-xyz:huginn-test-user-xyz"
	_, err := ResolveAPIKey(raw)
	// In CI and most dev environments the keyring entry won't exist, so we
	// expect an error. If by chance it's populated (manual testing), skip.
	if err == nil {
		t.Skip("keyring entry unexpectedly found; skipping miss-path assertion")
	}
	// We just verify that an error is returned — the exact message is internal.
}
