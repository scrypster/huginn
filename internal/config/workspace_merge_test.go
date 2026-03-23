package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFrom_OverridesBaseField verifies that a config file overrides specific base defaults.
func TestLoadFrom_OverridesBaseField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a config that overrides two base fields.
	data := map[string]any{
		"version":       currentConfigVersion,
		"default_model": "custom-override-model",
		"theme":         "light",
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DefaultModel != "custom-override-model" {
		t.Errorf("expected default_model override, got %q", cfg.DefaultModel)
	}
	if cfg.Theme != "light" {
		t.Errorf("expected theme override, got %q", cfg.Theme)
	}
	// Unset fields should remain at base defaults.
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("expected default OllamaBaseURL preserved, got %q", cfg.OllamaBaseURL)
	}
}

// TestLoadFrom_MissingFile_BaseConfigUsed verifies that a missing config file results in
// the default base config being returned with no error.
func TestLoadFrom_MissingFile_BaseConfigUsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	def := Default()
	if cfg.OllamaBaseURL != def.OllamaBaseURL {
		t.Errorf("expected default OllamaBaseURL, got %q", cfg.OllamaBaseURL)
	}
	if cfg.MaxTurns != def.MaxTurns {
		t.Errorf("expected default MaxTurns=%d, got %d", def.MaxTurns, cfg.MaxTurns)
	}
}

// TestLoadFrom_InvalidJSON_Error verifies that a config file with invalid JSON returns an error.
func TestLoadFrom_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{this is not valid json!!"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestLoadFrom_NestedFieldOverride verifies that nested struct fields can be overridden.
func TestLoadFrom_NestedFieldOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := map[string]any{
		"version": currentConfigVersion,
		"backend": map[string]any{
			"type":     "managed",
			"endpoint": "http://custom-backend:9000",
			"provider": "openai",
		},
		"web_ui": map[string]any{
			"port": 9999,
			"bind": "localhost",
		},
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend.Type != "managed" {
		t.Errorf("expected backend.type=managed, got %q", cfg.Backend.Type)
	}
	if cfg.Backend.Endpoint != "http://custom-backend:9000" {
		t.Errorf("expected backend.endpoint override, got %q", cfg.Backend.Endpoint)
	}
	if cfg.Backend.Provider != "openai" {
		t.Errorf("expected backend.provider=openai, got %q", cfg.Backend.Provider)
	}
	if cfg.WebUI.Port != 9999 {
		t.Errorf("expected web_ui.port=9999, got %d", cfg.WebUI.Port)
	}
	if cfg.WebUI.Bind != "localhost" {
		t.Errorf("expected web_ui.bind=localhost, got %q", cfg.WebUI.Bind)
	}
}

// TestLoadFrom_PartialOverride_UnsetFieldsPreserved verifies that fields not in the
// config file retain their default values after merge.
func TestLoadFrom_PartialOverride_UnsetFieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write config with only a few fields set.
	data := map[string]any{
		"version":          currentConfigVersion,
		"bash_timeout_secs": 60,
		"max_turns":         25,
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.BashTimeoutSecs != 60 {
		t.Errorf("expected bash_timeout_secs=60, got %d", cfg.BashTimeoutSecs)
	}
	if cfg.MaxTurns != 25 {
		t.Errorf("expected max_turns=25, got %d", cfg.MaxTurns)
	}
	// These were not set; they should be defaults.
	if cfg.ContextLimitKB != 128 {
		t.Errorf("expected default context_limit_kb=128, got %d", cfg.ContextLimitKB)
	}
	if cfg.Theme != "dark" {
		t.Errorf("expected default theme=dark, got %q", cfg.Theme)
	}
}
