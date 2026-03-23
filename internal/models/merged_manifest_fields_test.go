package models

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadMerged_SourceFieldSetToCurated verifies that all entries loaded from
// the embedded curated manifest have Source=="curated".
func TestLoadMerged_SourceFieldSetToCurated(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one curated entry")
	}
	for name, e := range entries {
		if e.Source != "curated" {
			t.Errorf("entry %q: expected Source='curated', got %q", name, e.Source)
		}
	}
}

// TestLoadMerged_AllEntriesHaveFilename verifies that applyDefaults has been
// applied to every entry (Filename must be non-empty).
func TestLoadMerged_AllEntriesHaveFilename(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	for name, e := range entries {
		if e.Filename == "" {
			t.Errorf("entry %q: expected non-empty Filename after applyDefaults", name)
		}
	}
}

// TestLoadMerged_AllEntriesHaveContextLength verifies that every curated entry
// has a non-zero ContextLength after applyDefaults.
func TestLoadMerged_AllEntriesHaveContextLength(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	for name, e := range entries {
		if e.ContextLength == 0 {
			t.Errorf("entry %q: expected non-zero ContextLength after applyDefaults", name)
		}
	}
}

// TestLoadMerged_AllEntriesHaveChatTemplate verifies that every curated entry
// has a non-empty ChatTemplate after applyDefaults.
func TestLoadMerged_AllEntriesHaveChatTemplate(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	for name, e := range entries {
		if e.ChatTemplate == "" {
			t.Errorf("entry %q: expected non-empty ChatTemplate after applyDefaults", name)
		}
	}
}

// TestApplyDefaults_FilenameFromURLWithQueryString verifies that applyDefaults
// correctly uses the last URL path segment even when no query string is present.
func TestApplyDefaults_FilenameFromURLWithPath(t *testing.T) {
	e := ModelEntry{URL: "https://example.com/models/subdir/model.gguf"}
	got := applyDefaults("test-model", e)
	if got.Filename != "model.gguf" {
		t.Errorf("expected Filename='model.gguf', got %q", got.Filename)
	}
}

// TestApplyDefaults_ExplicitFilenameNotOverridden verifies that a pre-set Filename
// is not overridden by applyDefaults.
func TestApplyDefaults_ExplicitFilenameNotOverridden(t *testing.T) {
	e := ModelEntry{
		URL:      "https://example.com/models/real.gguf",
		Filename: "my-custom-name.gguf",
	}
	got := applyDefaults("test-model", e)
	if got.Filename != "my-custom-name.gguf" {
		t.Errorf("expected Filename='my-custom-name.gguf' (preserved), got %q", got.Filename)
	}
}

// TestApplyDefaults_ContextLengthPreservedWhenNonZero verifies that a non-zero
// ContextLength is not overridden.
func TestApplyDefaults_ContextLengthPreservedWhenNonZero(t *testing.T) {
	e := ModelEntry{URL: "https://example.com/m.gguf", ContextLength: 65536}
	got := applyDefaults("m", e)
	if got.ContextLength != 65536 {
		t.Errorf("expected ContextLength=65536, got %d", got.ContextLength)
	}
}

// TestApplyDefaults_ChatTemplatePreservedWhenSet verifies that a pre-set
// ChatTemplate is not overridden.
func TestApplyDefaults_ChatTemplatePreservedWhenSet(t *testing.T) {
	e := ModelEntry{URL: "https://example.com/m.gguf", ChatTemplate: "llama3"}
	got := applyDefaults("m", e)
	if got.ChatTemplate != "llama3" {
		t.Errorf("expected ChatTemplate='llama3' (preserved), got %q", got.ChatTemplate)
	}
}

// TestStore_RecordThenInstalled_FieldsRoundTrip verifies that all LockEntry
// fields survive a Record → Installed round-trip.
func TestStore_RecordThenInstalled_FieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	entry := LockEntry{
		Name:      "llama3:8b",
		Filename:  "llama3-8b.gguf",
		Path:      "/tmp/models/llama3-8b.gguf",
		SHA256:    "abc123def456",
		SizeBytes: 4_831_838_208,
	}
	if err := s.Record("llama3:8b", entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := s.Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	got, ok := entries["llama3:8b"]
	if !ok {
		t.Fatal("expected entry for 'llama3:8b'")
	}
	if got.Name != entry.Name {
		t.Errorf("Name: got %q, want %q", got.Name, entry.Name)
	}
	if got.Filename != entry.Filename {
		t.Errorf("Filename: got %q, want %q", got.Filename, entry.Filename)
	}
	if got.Path != entry.Path {
		t.Errorf("Path: got %q, want %q", got.Path, entry.Path)
	}
	if got.SHA256 != entry.SHA256 {
		t.Errorf("SHA256: got %q, want %q", got.SHA256, entry.SHA256)
	}
	if got.SizeBytes != entry.SizeBytes {
		t.Errorf("SizeBytes: got %d, want %d", got.SizeBytes, entry.SizeBytes)
	}
}

// TestStore_LockFileIsValidJSON verifies that after recording a model,
// the lock file contains valid JSON.
func TestStore_LockFileIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Record("model", LockEntry{Name: "model", Filename: "m.gguf"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	data, err := os.ReadFile(s.lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("lock file is empty")
	}
	// Minimal JSON validity check — must start with '{'.
	if data[0] != '{' {
		t.Errorf("lock file should start with '{', got %q", string(data[:1]))
	}
}

// TestStore_ModelPath_LockPath verifies that ModelPath and lockPath are
// rooted under the huginnDir passed to NewStore.
func TestStore_ModelPath_IsUnderHuginnDir(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mp := s.ModelPath("test.gguf")
	// Must be under dir/models/
	expected := filepath.Join(dir, "models", "test.gguf")
	if mp != expected {
		t.Errorf("ModelPath: got %q, want %q", mp, expected)
	}
}

// TestLoadUserManifest_WithValidEntry verifies that a user manifest with a
// valid entry is loaded correctly.
func TestLoadUserManifest_WithValidEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.user.json")

	content := `{
		"huginn_manifest_version": 1,
		"models": {
			"my-custom": {
				"url": "https://example.com/custom.gguf",
				"description": "custom model"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, errs := loadUserManifest(path)
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if _, ok := entries["my-custom"]; !ok {
		t.Error("expected 'my-custom' in user manifest entries")
	}
}

// TestLoadUserManifest_MissingURL verifies that an entry without a URL is
// skipped with an error.
func TestLoadUserManifest_EntryMissingURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.user.json")

	content := `{
		"huginn_manifest_version": 1,
		"models": {
			"bad-entry": {
				"description": "no url here"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, errs := loadUserManifest(path)
	if len(errs) == 0 {
		t.Error("expected error for entry missing URL")
	}
	if _, ok := entries["bad-entry"]; ok {
		t.Error("expected entry missing URL to be skipped")
	}
}
