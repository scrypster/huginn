package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateV0toV1_DirectCall verifies that migrateV0toV1 can be called
// directly (it is a no-op but must not panic).
func TestMigrateV0toV1_DirectCall(t *testing.T) {
	cfg := Default()
	before := cfg.ReasonerModel
	migrateV0toV1(cfg)
	if cfg.ReasonerModel != before {
		t.Errorf("migrateV0toV1: should not modify PlannerModel, got %q", cfg.ReasonerModel)
	}
}

// TestMigrateV6toV7_DirectCall verifies that migrateV6toV7 is a no-op that
// does not panic or modify any existing field.
func TestMigrateV6toV7_DirectCall(t *testing.T) {
	cfg := Default()
	cfg.BraveAPIKey = "existing-key"
	migrateV6toV7(cfg)
	// Must not clear or change existing fields.
	if cfg.BraveAPIKey != "existing-key" {
		t.Errorf("migrateV6toV7: should not modify BraveAPIKey, got %q", cfg.BraveAPIKey)
	}
	if cfg.ReasonerModel == "" {
		t.Error("migrateV6toV7: should not wipe PlannerModel")
	}
}

// TestMigrateV8toV9_DirectCall verifies that migrateV8toV9 is a no-op that
// does not panic or modify any existing field (active_agent field was removed).
func TestMigrateV8toV9_DirectCall(t *testing.T) {
	cfg := Default()
	before := cfg.ReasonerModel
	migrateV8toV9(cfg)
	if cfg.ReasonerModel != before {
		t.Errorf("migrateV8toV9: should not modify ReasonerModel, got %q", cfg.ReasonerModel)
	}
}

// TestMigrateV10toV11_PortAlreadySet verifies that when WebUI.Port is already
// non-zero, migrateV10toV11 leaves it unchanged.
func TestMigrateV10toV11_PortAlreadySet(t *testing.T) {
	cfg := Default()
	cfg.WebUI.Port = 9999
	migrateV10toV11(cfg)
	if cfg.WebUI.Port != 9999 {
		t.Errorf("migrateV10toV11: expected port=9999 to be preserved, got %d", cfg.WebUI.Port)
	}
}

// TestMigrateV10toV11_ZeroPortSetTo8421 verifies that when WebUI.Port is 0,
// migrateV10toV11 sets it to 8421.
func TestMigrateV10toV11_ZeroPortSetTo8421(t *testing.T) {
	cfg := Default()
	cfg.WebUI.Port = 0
	migrateV10toV11(cfg)
	if cfg.WebUI.Port != 8421 {
		t.Errorf("migrateV10toV11: expected port=8421 for zero port, got %d", cfg.WebUI.Port)
	}
}

// TestLoad_HomeDir_UsedForConfigPath verifies that Load() reads from
// $HOME/.huginn/config.json by writing a sentinel there and confirming Load reads it.
func TestLoad_HomeDir_UsedForConfigPath(t *testing.T) {
	dir := t.TempDir()
	huginnDir := filepath.Join(dir, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	cfg := Default()
	cfg.Theme = "sentinel-load-home"
	if err := cfg.SaveTo(filepath.Join(huginnDir, "config.json")); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Theme != "sentinel-load-home" {
		t.Errorf("expected Theme=sentinel-load-home, got %q", loaded.Theme)
	}
}

// TestSave_WritesToHuginnDir verifies that Save() writes to $HOME/.huginn/config.json.
func TestSave_WritesToHuginnDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := Default()
	cfg.Theme = "sentinel-save-home"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	expected := filepath.Join(dir, ".huginn", "config.json")
	data, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("expected config at %q: %v", expected, err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty config file")
	}
}

// TestSaveTo_TmpFileCleanedUp verifies that no .tmp file is left after SaveTo.
func TestSaveTo_TmpFileCleanedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to be removed after SaveTo, found: %s", tmpPath)
	}
}

// TestMigrateV0_ThroughLoadFrom verifies that a version-0 config (no version
// field) goes through migrateV0toV1 during LoadFrom.
func TestMigrateV0_ThroughLoadFrom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Version 0: no version field at all.
	raw := `{"reasoner_model":"old-reasoner"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	// After running all migrations, version should be currentConfigVersion.
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d, got %d", currentConfigVersion, cfg.Version)
	}
	// V0 config fields must survive migration unchanged.
	if cfg.ReasonerModel != "old-reasoner" {
		t.Errorf("expected ReasonerModel='old-reasoner', got %q", cfg.ReasonerModel)
	}
}

// TestDefault_SchedulerEnabled verifies that SchedulerEnabled defaults to true.
func TestDefault_SchedulerEnabled(t *testing.T) {
	cfg := Default()
	if !cfg.SchedulerEnabled {
		t.Error("expected SchedulerEnabled=true by default")
	}
}

// TestDefault_WebUIPort verifies that Default() sets WebUI.Port to 8421.
func TestDefault_WebUIPort(t *testing.T) {
	cfg := Default()
	if cfg.WebUI.Port != 8421 {
		t.Errorf("expected WebUI.Port=8421, got %d", cfg.WebUI.Port)
	}
}
