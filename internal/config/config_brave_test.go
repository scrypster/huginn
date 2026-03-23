package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_BraveAPIKeyRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	cfg := Default()
	cfg.BraveAPIKey = "bsak_test123"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BraveAPIKey != "bsak_test123" {
		t.Errorf("BraveAPIKey = %q, want %q", loaded.BraveAPIKey, "bsak_test123")
	}
}

func TestConfig_BraveAPIKeyDefaultsEmpty(t *testing.T) {
	cfg := Default()
	if cfg.BraveAPIKey != "" {
		t.Errorf("default BraveAPIKey should be empty, got %q", cfg.BraveAPIKey)
	}
}

func TestConfig_MigrationPreservesBraveKey(t *testing.T) {
	// Simulate a v5 config file without brave_api_key field.
	raw := map[string]any{
		"version":         5,
		"planner_model":   "qwen3-coder:30b",
		"tools_enabled":   true,
		"compact_mode":    "auto",
		"compact_trigger": 0.70,
	}
	data, _ := json.Marshal(raw)

	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BraveAPIKey != "" {
		t.Errorf("expected empty BraveAPIKey after migration, got %q", cfg.BraveAPIKey)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, currentConfigVersion)
	}
}
