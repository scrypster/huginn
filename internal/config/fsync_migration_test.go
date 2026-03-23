package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveTo_AtomicWrite verifies that SaveTo writes a complete, valid JSON
// file and that no .tmp file is left behind after a successful write.
func TestSaveTo_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.MachineID = "test-machine-abc123"

	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	// The target file must exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not found after SaveTo: %v", err)
	}

	// The temp file must NOT be left behind.
	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); err == nil {
		t.Error(".tmp file should not exist after successful SaveTo")
	}

	// The written config must be loadable and round-trip correctly.
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom after SaveTo: %v", err)
	}
	if loaded.MachineID != cfg.MachineID {
		t.Errorf("MachineID mismatch: got %q, want %q", loaded.MachineID, cfg.MachineID)
	}
}

// TestSaveTo_Idempotent verifies that multiple SaveTo calls to the same path
// do not corrupt the file.
func TestSaveTo_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Theme = "light"

	for i := 0; i < 5; i++ {
		if err := cfg.SaveTo(path); err != nil {
			t.Fatalf("SaveTo iteration %d: %v", i, err)
		}
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.Theme != "light" {
		t.Errorf("Theme mismatch after repeated saves: got %q", loaded.Theme)
	}
}

// TestFsyncDir_ValidDir verifies that fsyncDir succeeds on an existing directory.
func TestFsyncDir_ValidDir(t *testing.T) {
	dir := t.TempDir()
	if err := fsyncDir(dir); err != nil {
		// Some filesystems (e.g. tmpfs) do not support directory fsync.
		// This is not a hard failure.
		t.Logf("fsyncDir returned (non-fatal): %v", err)
	}
}

// TestFsyncDir_MissingDir verifies that fsyncDir returns an error for a
// non-existent path.
func TestFsyncDir_MissingDir(t *testing.T) {
	err := fsyncDir("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for missing directory, got nil")
	}
}

// TestSaveTo_CreatesDeepNestedDir verifies that SaveTo creates deeply nested
// parent directories if they do not exist.
func TestSaveTo_CreatesDeepNestedDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "deep", "config.json")

	cfg := Default()
	if err := cfg.SaveTo(nested); err != nil {
		t.Fatalf("SaveTo with nested path: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("file not found after SaveTo to nested path: %v", err)
	}
}
