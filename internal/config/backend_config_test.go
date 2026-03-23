package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

func TestBackendConfig_ProviderField(t *testing.T) {
	cfg := config.Default()
	if cfg.Backend.Provider == "" {
		t.Error("expected non-empty default Provider")
	}
}

func TestBackendConfig_APIKeyEnvResolution(t *testing.T) {
	t.Setenv("HUGINN_TEST_API_KEY", "secret-value-xyz")
	bc := config.BackendConfig{
		Provider: "anthropic",
		APIKey:   "$HUGINN_TEST_API_KEY",
	}
	got := bc.ResolvedAPIKey()
	if got != "secret-value-xyz" {
		t.Errorf("ResolvedAPIKey() = %q, want %q", got, "secret-value-xyz")
	}
}

func TestBackendConfig_APIKeyLiteral(t *testing.T) {
	bc := config.BackendConfig{
		Provider: "openai",
		APIKey:   "sk-literal-key",
	}
	got := bc.ResolvedAPIKey()
	if got != "sk-literal-key" {
		t.Errorf("ResolvedAPIKey() = %q, want %q", got, "sk-literal-key")
	}
}

func TestBackendConfig_APIKeyEmpty_ReturnsEmpty(t *testing.T) {
	bc := config.BackendConfig{Provider: "ollama"}
	got := bc.ResolvedAPIKey()
	if got != "" {
		t.Errorf("ResolvedAPIKey() = %q, want empty string", got)
	}
}

func TestBackendConfig_APIKeyEnvMissing_ReturnsEmpty(t *testing.T) {
	os.Unsetenv("HUGINN_NONEXISTENT_VAR_12345")
	bc := config.BackendConfig{
		Provider: "openai",
		APIKey:   "$HUGINN_NONEXISTENT_VAR_12345",
	}
	got := bc.ResolvedAPIKey()
	if got != "" {
		t.Errorf("ResolvedAPIKey() = %q, want empty string for unset env", got)
	}
}

func TestConfig_MigrationV5toV6_AddsProvider(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	// Use current version minus 1 to simulate migration
	v5Data := `{"version":5,"backend":{"type":"external","endpoint":"http://localhost:11434"}}`
	if err := os.WriteFile(path, []byte(v5Data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend.Provider == "" {
		t.Error("migration should set Backend.Provider")
	}
	if cfg.Backend.Provider != "ollama" {
		t.Errorf("Provider = %q, want %q", cfg.Backend.Provider, "ollama")
	}
}
