package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFrom_MissingFile verifies that LoadFrom returns defaults (not an
// error) when the config file does not exist. This is the first-run behavior.
func TestLoadFrom_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(missing): unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadFrom(missing): expected non-nil config")
	}
	// Machine ID should be generated.
	if cfg.MachineID == "" {
		t.Error("LoadFrom(missing): expected MachineID to be generated")
	}
	// Defaults should be populated.
	if cfg.MaxTurns <= 0 {
		t.Errorf("LoadFrom(missing): expected MaxTurns > 0, got %d", cfg.MaxTurns)
	}
}

// TestValidate_ClampsInvalidCompactTrigger verifies that Validate() resets an
// out-of-range CompactTrigger to the safe default (0.70).
func TestValidate_ClampsInvalidCompactTrigger(t *testing.T) {
	c := Default()
	c.CompactTrigger = 1.5 // out of range

	c.Validate()

	if c.CompactTrigger != 0.70 {
		t.Errorf("Validate(): expected CompactTrigger=0.70 after clamp, got %f", c.CompactTrigger)
	}
}

// TestValidate_ClampsNegativeMaxTurns verifies that Validate() resets a
// non-positive MaxTurns to the safe default (50).
func TestValidate_ClampsNegativeMaxTurns(t *testing.T) {
	c := Default()
	c.MaxTurns = -5

	c.Validate()

	if c.MaxTurns != 50 {
		t.Errorf("Validate(): expected MaxTurns=50 after clamp, got %d", c.MaxTurns)
	}
}

// TestValidate_InvalidDiffReviewMode verifies that an unknown DiffReviewMode
// is reset to the safe default ("always").
func TestValidate_InvalidDiffReviewMode(t *testing.T) {
	c := Default()
	c.DiffReviewMode = "bogus"

	c.Validate()

	if c.DiffReviewMode != "always" {
		t.Errorf("Validate(): expected DiffReviewMode='always', got %q", c.DiffReviewMode)
	}
}

// TestValidate_InvalidCompactMode verifies that an unknown CompactMode is
// reset to the safe default ("auto").
func TestValidate_InvalidCompactMode(t *testing.T) {
	c := Default()
	c.CompactMode = "invalid"

	c.Validate()

	if c.CompactMode != "auto" {
		t.Errorf("Validate(): expected CompactMode='auto', got %q", c.CompactMode)
	}
}

// TestValidate_PortOutOfRange verifies that Validate (the exported function)
// rejects ports outside 1024-65535.
func TestValidate_PortOutOfRange(t *testing.T) {
	cfg := *Default()
	cfg.WebUI.Port = 80 // below 1024

	if err := Validate(cfg); err == nil {
		t.Error("Validate(): expected error for port 80, got nil")
	}
}

// TestLoadFrom_CorruptFile verifies that LoadFrom returns an error (not a
// panic) when the config file contains invalid JSON.
func TestLoadFrom_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte("{not: valid json}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Error("LoadFrom(corrupt): expected error for invalid JSON, got nil")
	}
}
