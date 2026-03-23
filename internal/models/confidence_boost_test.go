package models

// confidence_boost_test.go — Iteration 4 targeted coverage improvements.
// Targets: writeLock error path, LoadMerged with user manifest overlay,
// userManifestPath HOME unavailable, Record/Remove error from corrupted lock,
// Installed with corrupted lock file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── Store.Installed — corrupted lock file ────────────────────────────────────

// TestStore_Installed_CorruptedLock verifies that Installed() returns an error
// (not a panic) when the lock file contains invalid JSON.
func TestStore_Installed_CorruptedLock(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Write corrupt JSON directly to the lock file path.
	if err := os.WriteFile(s.lockPath, []byte("{corrupt json"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = s.Installed()
	if err == nil {
		t.Error("expected error from Installed() with corrupt lock file")
	}
	if !strings.Contains(err.Error(), "corrupted") && !strings.Contains(err.Error(), "invalid") {
		t.Logf("error message: %v", err)
	}
}

// ─── Store.Record — error propagation from Installed ─────────────────────────

// TestStore_Record_InstalledError verifies that Record() returns an error when
// the lock file is corrupted (so Installed() fails).
func TestStore_Record_InstalledError(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// First record a valid entry.
	if err := s.Record("m1", LockEntry{Name: "m1", Filename: "m1.gguf"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Now corrupt the lock file.
	if err := os.WriteFile(s.lockPath, []byte("{bad json"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Record should fail because Installed() returns an error.
	err = s.Record("m2", LockEntry{Name: "m2", Filename: "m2.gguf"})
	if err == nil {
		t.Error("expected error from Record() when lock file is corrupted")
	}
}

// ─── Store.Remove — error propagation from Installed ─────────────────────────

// TestStore_Remove_InstalledError verifies that Remove() returns an error when
// the lock file is corrupted.
func TestStore_Remove_InstalledError(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Corrupt the lock file directly.
	if err := os.WriteFile(s.lockPath, []byte("{bad json"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = s.Remove("nonexistent")
	if err == nil {
		t.Error("expected error from Remove() when lock file is corrupted")
	}
}

// ─── Store.Record — writeLock round-trip ─────────────────────────────────────

// TestStore_Record_WriteLock_Success exercises the writeLock happy path by
// recording a model and verifying the lock file is created.
func TestStore_Record_WriteLock_Success(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entry := LockEntry{
		Name:        "test-wl",
		Filename:    "test-wl.gguf",
		Path:        filepath.Join(dir, "models", "test-wl.gguf"),
		SHA256:      "deadbeef",
		SizeBytes:   1_000_000,
		InstalledAt: time.Now().UTC(),
	}
	if err := s.Record("test-wl", entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Verify lock file was written.
	if _, err := os.Stat(s.lockPath); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}

	// Read it back.
	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	got, ok := entries["test-wl"]
	if !ok {
		t.Fatal("expected test-wl in lock file")
	}
	if got.SHA256 != "deadbeef" {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, "deadbeef")
	}
}

// ─── Store.Remove — complete round-trip ──────────────────────────────────────

// TestStore_Remove_WriteLock_Success exercises the full remove cycle.
func TestStore_Remove_WriteLock_Success(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Record two entries.
	for _, name := range []string{"alpha", "beta"} {
		e := LockEntry{Name: name, Filename: name + ".gguf", SizeBytes: 100}
		if err := s.Record(name, e); err != nil {
			t.Fatalf("Record %s: %v", name, err)
		}
	}

	// Remove one.
	if err := s.Remove("alpha"); err != nil {
		t.Fatalf("Remove alpha: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if _, ok := entries["alpha"]; ok {
		t.Error("expected alpha to be removed")
	}
	if _, ok := entries["beta"]; !ok {
		t.Error("expected beta to remain after removing alpha")
	}
}

// ─── LoadMerged — user manifest overlay ──────────────────────────────────────

// TestLoadMerged_WithUserManifest exercises the user manifest merge path
// by writing a user manifest to ~/.huginn/models.user.json.
func TestLoadMerged_WithUserManifest(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create ~/.huginn/ directory.
	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a valid user manifest.
	manifest := `{
		"huginn_manifest_version": 1,
		"models": {
			"user-custom-model": {
				"url": "https://example.com/user-model.gguf",
				"description": "A user-provided model",
				"context_length": 8192
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(huginnDir, "models.user.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}

	// The user model should appear in the merged results.
	got, ok := entries["user-custom-model"]
	if !ok {
		t.Fatal("expected user-custom-model in merged entries")
	}
	if got.Source != "user" {
		t.Errorf("expected Source=user, got %q", got.Source)
	}
	if got.ContextLength != 8192 {
		t.Errorf("expected ContextLength=8192, got %d", got.ContextLength)
	}
}

// TestLoadMerged_UserModelOverridesCurated verifies that a user model with the
// same name as a curated model wins (user takes precedence).
func TestLoadMerged_UserModelOverridesCurated(t *testing.T) {
	// First, get a curated model name to use as override.
	curatedEntries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged (baseline): %v", err)
	}
	if len(curatedEntries) == 0 {
		t.Skip("no curated entries to override")
	}
	// Pick any curated model name.
	var targetName string
	for name := range curatedEntries {
		targetName = name
		break
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	manifest := `{
		"huginn_manifest_version": 1,
		"models": {
			"` + targetName + `": {
				"url": "https://user-override.example.com/override.gguf",
				"description": "User override"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(huginnDir, "models.user.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged with override: %v", err)
	}

	got, ok := entries[targetName]
	if !ok {
		t.Fatalf("expected %q in merged entries", targetName)
	}
	if got.Source != "user" {
		t.Errorf("expected Source=user for overridden entry, got %q", got.Source)
	}
	if got.URL != "https://user-override.example.com/override.gguf" {
		t.Errorf("expected user URL, got %q", got.URL)
	}
}

// TestLoadMerged_UserManifestWithBadEntry exercises the warning path where
// a user manifest has an entry without a URL (it is skipped with a warning).
func TestLoadMerged_UserManifestWithBadEntry(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	huginnDir := filepath.Join(tmpHome, ".huginn")
	if err := os.MkdirAll(huginnDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	manifest := `{
		"huginn_manifest_version": 1,
		"models": {
			"no-url-model": {
				"description": "This model has no URL — should be skipped"
			},
			"good-user-model": {
				"url": "https://example.com/good.gguf"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(huginnDir, "models.user.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}

	// "no-url-model" should be absent (skipped due to missing URL).
	if _, ok := entries["no-url-model"]; ok {
		t.Error("expected no-url-model to be skipped in merged results")
	}
	// "good-user-model" should be present.
	if _, ok := entries["good-user-model"]; !ok {
		t.Error("expected good-user-model to be present in merged results")
	}
}

// ─── userManifestPath — HOME not set ─────────────────────────────────────────

// TestUserManifestPath_HomeNotSet exercises the error path in userManifestPath
// when the home directory cannot be determined.
func TestUserManifestPath_HomeNotSet(t *testing.T) {
	origHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	defer os.Setenv("HOME", origHome)

	// userManifestPath is package-internal; exercise via LoadMerged.
	// When HOME is unset on macOS, os.UserHomeDir may still succeed via passwd.
	// The important thing is LoadMerged must not panic.
	_, err := LoadMerged()
	// err may or may not be nil depending on platform behaviour.
	_ = err
}

// TestStore_Remove_DoesNotErrorOnMissing verifies that Remove on a valid but
// empty store does not error (map delete of missing key is a no-op).
func TestStore_Remove_DoesNotErrorOnMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Record one entry, then remove a different (non-existent) key.
	if err := s.Record("m1", LockEntry{Name: "m1", Filename: "m1.gguf"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := s.Remove("m-does-not-exist"); err != nil {
		t.Errorf("Remove of non-existent key must not error, got: %v", err)
	}
}
