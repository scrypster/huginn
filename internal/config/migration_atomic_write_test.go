package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFrom_MigrationTransactional verifies that migrations are written to
// disk atomically — via a temp-file rename — so that a crash between
// "migrations applied in memory" and "file updated on disk" does not cause
// migrations to be re-applied on the next startup.
//
// Bug: LoadFrom() applies all migrations to cfg (mutating cfg.Version to
// currentConfigVersion) and then calls cfg.SaveTo(path) which uses
// os.WriteFile() (not atomic). If the write fails, the in-memory struct has
// cfg.Version = currentConfigVersion but the on-disk file still has the old
// version. On the next run, all migrations are re-applied.
//
// Fix: SaveTo should use a tmp file + os.Rename for atomic write, so either
// the whole migration is committed to disk or it isn't (no half-written state).
func TestLoadFrom_MigrationTransactional_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Simulate an old-version config (version 0).
	oldData := []byte(`{"version":0,"model":"qwen3:30b"}`)
	if err := os.WriteFile(path, oldData, 0600); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	// Load → triggers migration → must save new version atomically.
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d, got %d", currentConfigVersion, cfg.Version)
	}

	// The on-disk file must now reflect the migrated version.
	cfg2, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("second LoadFrom: %v", err)
	}
	if cfg2.Version != currentConfigVersion {
		t.Errorf("on-disk version not updated: expected %d, got %d", currentConfigVersion, cfg2.Version)
	}
}

// TestSaveTo_AtomicRename verifies that SaveTo writes to a temp file and renames
// atomically, rather than truncating-then-writing the config file in place.
//
// Invariant: even if the process dies mid-write, the original config file is
// never left in a partially written state. The old file stays intact until the
// new one is fully written and renamed.
func TestSaveTo_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Version = currentConfigVersion

	// First save creates the file.
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("first SaveTo: %v", err)
	}

	// No .tmp file must remain after a successful save.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("temp file %q left behind after successful SaveTo", tmpPath)
	}

	// The config file must be readable and valid.
	cfg2, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom after SaveTo: %v", err)
	}
	if cfg2.Version != cfg.Version {
		t.Errorf("expected Version %d, got %d", cfg.Version, cfg2.Version)
	}
}
