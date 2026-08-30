package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConfig_KeepAliveDefaultsTo10m verifies the perf-wave 2b default: a
// fresh config carries "10m" for Backend.KeepAlive so ollama keeps a warm
// model resident between turns out of the box.
func TestConfig_KeepAliveDefaultsTo10m(t *testing.T) {
	cfg := Default()
	if cfg.Backend.KeepAlive != "10m" {
		t.Errorf("default Backend.KeepAlive = %q, want %q", cfg.Backend.KeepAlive, "10m")
	}
}

// TestConfig_KeepAliveRoundTrip verifies an explicit override survives a
// save/load cycle.
func TestConfig_KeepAliveRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	cfg := Default()
	cfg.Backend.KeepAlive = "1h"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Backend.KeepAlive != "1h" {
		t.Errorf("Backend.KeepAlive = %q, want %q", loaded.Backend.KeepAlive, "1h")
	}
}

// TestConfig_KeepAlive_PreExistingConfigWithoutFieldGetsDefault verifies a
// config.json written before this field existed (no "keep_alive" key at
// all) picks up the "10m" default via Default()+json.Unmarshal merging,
// same as every other field added this way (see migrateV6toV7's comment).
func TestConfig_KeepAlive_PreExistingConfigWithoutFieldGetsDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")

	raw := map[string]any{
		"version": currentConfigVersion,
		"backend": map[string]any{
			"type":     "external",
			"endpoint": "http://localhost:11434",
			"provider": "ollama",
			// no "keep_alive" key — simulates a pre-upgrade config file.
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Backend.KeepAlive != "10m" {
		t.Errorf("pre-existing config without keep_alive field: got %q, want default %q", loaded.Backend.KeepAlive, "10m")
	}
}
