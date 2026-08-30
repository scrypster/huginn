package config

import (
	"path/filepath"
	"testing"
)

// TestUpdateAt_SurvivesStaleSnapshot is the regression test for the config
// clobber defect: a writer holding a Config it loaded at startup (e.g. a
// relay satellite or the TUI) must not be able to revert a field another
// writer (e.g. the config API handler) saved to disk in the meantime.
//
// Scenario: field X (ToolsEnabled) is saved via the "API" path. A second
// component that loaded its own snapshot of the config BEFORE that API save
// (so its in-memory copy still has the old value of X) then saves field Y
// (Backend.Provider) via UpdateAt. X must survive — UpdateAt must read the
// current on-disk state, not the stale in-memory one, before writing.
func TestUpdateAt_SurvivesStaleSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Seed the file with defaults (ToolsEnabled=false initially, WebUI.Port set).
	seed := Default()
	seed.ToolsEnabled = false
	seed.WebUI.Port = 9100
	if err := seed.SaveTo(path); err != nil {
		t.Fatalf("seed SaveTo: %v", err)
	}

	// Another component (e.g. a relay satellite or the TUI) loads its own
	// long-lived snapshot BEFORE the API save happens below. This snapshot
	// intentionally goes unused for anything other than proving it must NOT
	// be the source of truth for the later write.
	staleSnapshot, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom (stale snapshot): %v", err)
	}
	if staleSnapshot.ToolsEnabled {
		t.Fatalf("precondition: stale snapshot should have ToolsEnabled=false")
	}

	// Field X: the "API" saves ToolsEnabled=true via a read-modify-write,
	// simulating handleUpdateConfig's PUT /api/v1/config.
	if err := UpdateAt(path, func(cfg *Config) {
		cfg.ToolsEnabled = true
	}); err != nil {
		t.Fatalf("UpdateAt (field X): %v", err)
	}

	// Field Y: the stale-snapshot-holding component now saves an unrelated
	// field via UpdateAt (the fix) rather than staleSnapshot.Save() (the bug).
	if err := UpdateAt(path, func(cfg *Config) {
		cfg.Backend.Provider = "anthropic"
	}); err != nil {
		t.Fatalf("UpdateAt (field Y): %v", err)
	}

	final, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom (final): %v", err)
	}
	if !final.ToolsEnabled {
		t.Fatalf("field X (ToolsEnabled) did not survive the second writer's save — got false, want true")
	}
	if final.WebUI.Port != 9100 {
		t.Fatalf("unrelated field WebUI.Port was clobbered: got %d, want 9100", final.WebUI.Port)
	}
	if final.Backend.Provider != "anthropic" {
		t.Fatalf("field Y (Backend.Provider) was not applied — got %q, want %q", final.Backend.Provider, "anthropic")
	}
}

// TestUpdateAt_NaiveStaleSaveWouldClobber documents the bug UpdateAt fixes:
// if the second writer instead saves its ENTIRE stale in-memory snapshot
// (the old behaviour of main.go's relay UpdateModelConfig closures and
// internal/tui's DirectConfigService.Save), field X is lost. This is not a
// call into UpdateAt — it exercises the old pattern directly so the fix's
// value is provable, not just asserted.
func TestUpdateAt_NaiveStaleSaveWouldClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	seed := Default()
	seed.ToolsEnabled = false
	if err := seed.SaveTo(path); err != nil {
		t.Fatalf("seed SaveTo: %v", err)
	}

	staleSnapshot, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom (stale snapshot): %v", err)
	}

	// API save of field X.
	if err := UpdateAt(path, func(cfg *Config) {
		cfg.ToolsEnabled = true
	}); err != nil {
		t.Fatalf("UpdateAt (field X): %v", err)
	}

	// The naive/buggy pattern: mutate the stale snapshot and Save() the
	// whole thing, reverting field X as a side effect of changing Y.
	staleSnapshot.Backend.Provider = "anthropic"
	if err := staleSnapshot.SaveTo(path); err != nil {
		t.Fatalf("staleSnapshot.SaveTo: %v", err)
	}

	final, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom (final): %v", err)
	}
	if final.ToolsEnabled {
		t.Fatalf("expected the naive save to demonstrate the clobber (ToolsEnabled reverted to false), got true — has SaveTo grown its own merge logic?")
	}
}
