package models

// store_coverage_test.go — Additional coverage for models.Store (Iteration 2)
// Complements store_test.go with focused tests on:
//   - Installed(): returns empty map on fresh store (no lock file)
//   - Installed(): returns entries after Record()
//   - Record(): persists entries across independent Installed() calls (upsert)
//   - ModelPath(): returns correct absolute path for a given filename

import (
	"path/filepath"
	"testing"
	"time"
)

// ─── Store.Installed() — empty on fresh store ─────────────────────────────────

// TestStore_Installed_EmptyOnFreshStore verifies that Installed() on a newly
// created Store (no lock file written yet) returns an empty map, not an error.
func TestStore_Installed_EmptyOnFreshStore(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: unexpected error: %v", err)
	}
	if entries == nil {
		t.Fatal("Installed: expected non-nil map, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("Installed: expected empty map, got %d entries", len(entries))
	}
}

// ─── Store.Installed() — returns entries after Record() ──────────────────────

// TestStore_Installed_ReturnsEntriesAfterRecord verifies that entries added via
// Record() appear in subsequent Installed() calls.
func TestStore_Installed_ReturnsEntriesAfterRecord(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entry := LockEntry{
		Name:        "test-model",
		Filename:    "test-model.gguf",
		Path:        "/tmp/test-model.gguf",
		SHA256:      "abc123",
		SizeBytes:   750000000,
		InstalledAt: time.Now(),
	}
	if err := s.Record("test-model", entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	got, ok := entries["test-model"]
	if !ok {
		t.Fatal("expected test-model in Installed() results")
	}
	if got.Filename != "test-model.gguf" {
		t.Errorf("expected Filename=test-model.gguf, got %q", got.Filename)
	}
	if got.SizeBytes != 750000000 {
		t.Errorf("expected SizeBytes=750000000, got %d", got.SizeBytes)
	}
	if got.SHA256 != "abc123" {
		t.Errorf("expected SHA256=abc123, got %q", got.SHA256)
	}
}

// ─── Store.Record() — upsert behavior ────────────────────────────────────────

// TestStore_Record_UpsertOverwritesExistingEntry verifies that recording the
// same model name twice replaces the previous entry (upsert semantics).
func TestStore_Record_UpsertOverwritesExistingEntry(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	first := LockEntry{Name: "m", Filename: "m-v1.gguf", SizeBytes: 100}
	second := LockEntry{Name: "m", Filename: "m-v2.gguf", SizeBytes: 200}

	if err := s.Record("m", first); err != nil {
		t.Fatalf("Record first: %v", err)
	}
	if err := s.Record("m", second); err != nil {
		t.Fatalf("Record second: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	// Should have exactly one entry.
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after upsert, got %d", len(entries))
	}
	got, ok := entries["m"]
	if !ok {
		t.Fatal("expected entry 'm' in Installed()")
	}
	if got.Filename != "m-v2.gguf" {
		t.Errorf("expected Filename=m-v2.gguf after upsert, got %q", got.Filename)
	}
	if got.SizeBytes != 200 {
		t.Errorf("expected SizeBytes=200 after upsert, got %d", got.SizeBytes)
	}
}

// TestStore_Record_PreservesOtherEntries verifies that recording a new model
// does not remove previously recorded models.
func TestStore_Record_PreservesOtherEntries(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := s.Record("model-a", LockEntry{Name: "model-a", Filename: "a.gguf"}); err != nil {
		t.Fatalf("Record model-a: %v", err)
	}
	if err := s.Record("model-b", LockEntry{Name: "model-b", Filename: "b.gguf"}); err != nil {
		t.Fatalf("Record model-b: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if _, ok := entries["model-a"]; !ok {
		t.Error("model-a missing after recording model-b")
	}
	if _, ok := entries["model-b"]; !ok {
		t.Error("model-b missing")
	}
}

// ─── Store.ModelPath() — correct path ────────────────────────────────────────

// TestStore_ModelPath_ReturnsCorrectAbsolutePath verifies that ModelPath returns
// the full path by joining the store's models directory with the given filename.
func TestStore_ModelPath_ReturnsCorrectAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	filename := "llama-3-8b-q4_K_M.gguf"
	got := s.ModelPath(filename)
	want := filepath.Join(dir, "models", filename)
	if got != want {
		t.Errorf("ModelPath mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestStore_ModelPath_IsAbsolute verifies that ModelPath always returns an
// absolute path (starts with "/").
func TestStore_ModelPath_IsAbsolute(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	path := s.ModelPath("some-model.gguf")
	if !filepath.IsAbs(path) {
		t.Errorf("ModelPath returned non-absolute path: %q", path)
	}
}

// TestStore_ModelPath_ContainsFilename verifies that ModelPath includes the
// filename in the returned path.
func TestStore_ModelPath_ContainsFilename(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	filename := "unique-model-name-q8.gguf"
	path := s.ModelPath(filename)
	if filepath.Base(path) != filename {
		t.Errorf("expected base of ModelPath to be %q, got %q", filename, filepath.Base(path))
	}
}
