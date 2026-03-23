package config

import (
	"testing"
)

func TestResolvedAPIKey_LiteralKey_Returned(t *testing.T) {
	bc := &BackendConfig{APIKey: "sk-literal-key"}
	if got := bc.ResolvedAPIKey(); got != "sk-literal-key" {
		t.Errorf("expected literal key, got %q", got)
	}
}

func TestResolvedAPIKey_EmptyKey_ReturnsEmpty(t *testing.T) {
	bc := &BackendConfig{APIKey: ""}
	if got := bc.ResolvedAPIKey(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestResolvedAPIKey_EnvVar_ResolvedFromEnvironment(t *testing.T) {
	t.Setenv("HUGINN_TEST_API_KEY_9321", "resolved-value")
	bc := &BackendConfig{APIKey: "$HUGINN_TEST_API_KEY_9321"}
	if got := bc.ResolvedAPIKey(); got != "resolved-value" {
		t.Errorf("expected resolved-value, got %q", got)
	}
}

func TestResolvedAPIKey_UnsetEnvVar_ReturnsEmpty(t *testing.T) {
	// Clear the warned map entry so we can observe the warning path cleanly.
	warnedEnvVars.Delete("HUGINN_TEST_UNSET_9999")
	t.Setenv("HUGINN_TEST_UNSET_9999", "") // ensure truly unset
	bc := &BackendConfig{APIKey: "$HUGINN_TEST_UNSET_9999"}
	if got := bc.ResolvedAPIKey(); got != "" {
		t.Errorf("expected empty for unset var, got %q", got)
	}
}

func TestResolvedAPIKey_WarnedOnce_NotSpammed(t *testing.T) {
	// Remove any prior entry so the first call logs a warning.
	warnedEnvVars.Delete("HUGINN_TEST_WARN_ONCE_7654")

	bc := &BackendConfig{APIKey: "$HUGINN_TEST_WARN_ONCE_7654"}

	// Call twice — both should return "" but the warning should fire only once.
	// We cannot assert the log count directly, but we can verify the sync.Map
	// stores the key after the first call.
	_ = bc.ResolvedAPIKey() // first call: warn + store
	_, loaded := warnedEnvVars.Load("HUGINN_TEST_WARN_ONCE_7654")
	if !loaded {
		t.Error("expected key to be stored in warnedEnvVars after first call")
	}
	_ = bc.ResolvedAPIKey() // second call: no warn (already stored)
}
