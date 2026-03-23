package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadFrom — existing config with MachineID already set (no regen)
// ---------------------------------------------------------------------------

func TestLoadFrom_ExistingMachineID_NotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.MachineID = "existing-machine-id"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.MachineID != "existing-machine-id" {
		t.Errorf("expected MachineID='existing-machine-id', got %q", loaded.MachineID)
	}
}

// ---------------------------------------------------------------------------
// LoadFrom — existing config with empty MachineID triggers generation
// ---------------------------------------------------------------------------

func TestLoadFrom_EmptyMachineID_Generated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a config with an empty MachineID explicitly.
	raw := map[string]any{
		"reasoner_model": "test:7b",
		"machine_id":     "",
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.MachineID == "" {
		t.Error("expected MachineID to be generated for empty string")
	}
	// Verify the custom field was preserved.
	if loaded.ReasonerModel != "test:7b" {
		t.Errorf("expected ReasonerModel='test:7b', got %q", loaded.ReasonerModel)
	}
}

// ---------------------------------------------------------------------------
// LoadFrom — file read error (not ErrNotExist)
// ---------------------------------------------------------------------------

func TestLoadFrom_ReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Create a directory where a file is expected → ReadFile returns error.
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error when config path is a directory")
	}
}

// ---------------------------------------------------------------------------
// generateMachineID — basic format validation
// ---------------------------------------------------------------------------

func TestGenerateMachineID_Format(t *testing.T) {
	id := generateMachineID()
	if id == "" {
		t.Fatal("expected non-empty machine ID")
	}
	// Must contain at least one hyphen (hostname-hexbytes).
	parts := splitOnLastHyphen(id)
	if len(parts) != 2 {
		t.Errorf("expected format hostname-hex, got %q", id)
	}
	// The hex part should be 8 hex chars (4 bytes → 8 hex chars).
	if len(parts[1]) != 8 {
		t.Errorf("expected 8 hex chars after last hyphen, got %d in %q", len(parts[1]), id)
	}
}

func TestGenerateMachineID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := generateMachineID()
		if ids[id] {
			t.Errorf("duplicate machine ID: %q", id)
		}
		ids[id] = true
	}
}

// ---------------------------------------------------------------------------
// Save — verify HOME override produces file at correct location
// ---------------------------------------------------------------------------

func TestSave_CreatesHuginnDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := Default()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	huginnDir := filepath.Join(dir, ".huginn")
	info, err := os.Stat(huginnDir)
	if err != nil {
		t.Fatalf("expected .huginn dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected .huginn to be a directory")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// splitOnLastHyphen splits s at the last occurrence of '-'.
func splitOnLastHyphen(s string) []string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
