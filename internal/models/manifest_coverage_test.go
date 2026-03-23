package models

// manifest_coverage_test.go — Coverage for manifest loading logic (Iteration 2)
// Covers:
//   - loadUserManifest: missing file returns nil,nil
//   - loadUserManifest: corrupt JSON returns error
//   - loadUserManifest: entries with empty URL are rejected with error
//   - loadUserManifest: valid entry passes through
//   - applyDefaults: Filename derived from URL last segment
//   - applyDefaults: default ContextLength=4096 when zero
//   - applyDefaults: default ChatTemplate=chatml when empty
//   - LoadMerged: curated catalog is non-empty and all entries have Source="curated"

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── loadUserManifest — missing file ─────────────────────────────────────────

// TestLoadUserManifest_MissingFile verifies that loadUserManifest returns
// nil entries and nil errors when the file does not exist.
func TestLoadUserManifest_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	entries, errs := loadUserManifest(path)
	if entries != nil {
		t.Errorf("expected nil entries for missing file, got %v", entries)
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors for missing file, got %v", errs)
	}
}

// ─── loadUserManifest — corrupt JSON ─────────────────────────────────────────

// TestLoadUserManifest_CorruptJSON verifies that loadUserManifest returns an
// error (not a panic) when the file contains invalid JSON.
func TestLoadUserManifest_CorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.user.json")
	if err := os.WriteFile(path, []byte("{this is not valid json!!!}"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, errs := loadUserManifest(path)
	if entries != nil {
		t.Errorf("expected nil entries for corrupt JSON, got %v", entries)
	}
	if len(errs) == 0 {
		t.Error("expected at least one error for corrupt JSON, got none")
	}
}

// ─── loadUserManifest — empty URL rejected ────────────────────────────────────

// TestLoadUserManifest_EmptyURL verifies that entries missing the required
// "url" field are excluded from results and cause a warning error to be reported.
func TestLoadUserManifest_EmptyURL(t *testing.T) {
	manifest := `{
		"huginn_manifest_version": 1,
		"models": {
			"good-model": {
				"url": "https://example.com/good.gguf",
				"description": "A good model"
			},
			"bad-model": {
				"description": "No URL — should be rejected"
			}
		}
	}`
	path := filepath.Join(t.TempDir(), "models.user.json")
	if err := os.WriteFile(path, []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, errs := loadUserManifest(path)

	// "good-model" should be in entries.
	if _, ok := entries["good-model"]; !ok {
		t.Error("expected good-model in valid entries")
	}
	// "bad-model" should be excluded.
	if _, ok := entries["bad-model"]; ok {
		t.Error("expected bad-model to be rejected due to missing URL")
	}
	// An error must be reported for "bad-model".
	if len(errs) == 0 {
		t.Error("expected at least one error for the entry with missing URL")
	}
}

// ─── loadUserManifest — all valid entries ────────────────────────────────────

// TestLoadUserManifest_ValidEntries verifies that a well-formed user manifest
// is parsed correctly and all valid entries are returned.
func TestLoadUserManifest_ValidEntries(t *testing.T) {
	manifest := `{
		"huginn_manifest_version": 1,
		"models": {
			"model-alpha": {
				"url": "https://example.com/alpha.gguf",
				"description": "Alpha model",
				"size_bytes": 1000000
			},
			"model-beta": {
				"url": "https://example.com/beta.gguf",
				"description": "Beta model",
				"size_bytes": 2000000
			}
		}
	}`
	path := filepath.Join(t.TempDir(), "models.user.json")
	if err := os.WriteFile(path, []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, errs := loadUserManifest(path)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries["model-alpha"].URL != "https://example.com/alpha.gguf" {
		t.Errorf("unexpected URL for model-alpha: %q", entries["model-alpha"].URL)
	}
}

// ─── applyDefaults — Filename from URL ───────────────────────────────────────

// TestApplyDefaults_FilenameFromURLLastSegment verifies that Filename is
// derived from the last path segment of the URL when not explicitly set.
func TestApplyDefaults_FilenameFromURLLastSegment(t *testing.T) {
	e := applyDefaults("my-model", ModelEntry{
		URL: "https://huggingface.co/models/repo/resolve/main/model-q4.gguf",
	})
	if e.Filename != "model-q4.gguf" {
		t.Errorf("expected filename=model-q4.gguf, got %q", e.Filename)
	}
}

// TestApplyDefaults_FilenameFromURLFallback verifies the fallback to "name.gguf"
// when the URL produces an unusable base name (empty URL).
func TestApplyDefaults_FilenameFromURLFallback(t *testing.T) {
	e := applyDefaults("fallback-model", ModelEntry{
		URL: "", // empty — filepath.Base("") returns "."
	})
	if e.Filename != "fallback-model.gguf" {
		t.Errorf("expected fallback-model.gguf, got %q", e.Filename)
	}
}

// ─── applyDefaults — ContextLength default ───────────────────────────────────

// TestApplyDefaults_DefaultContextLength verifies that ContextLength defaults
// to 4096 when zero.
func TestApplyDefaults_DefaultContextLength(t *testing.T) {
	e := applyDefaults("m", ModelEntry{URL: "https://example.com/m.gguf", ContextLength: 0})
	if e.ContextLength != 4096 {
		t.Errorf("expected 4096, got %d", e.ContextLength)
	}
}

// TestApplyDefaults_NonZeroContextLengthPreserved verifies that a non-zero
// ContextLength is not overwritten.
func TestApplyDefaults_NonZeroContextLengthPreserved(t *testing.T) {
	e := applyDefaults("m", ModelEntry{URL: "https://example.com/m.gguf", ContextLength: 32768})
	if e.ContextLength != 32768 {
		t.Errorf("expected 32768, got %d", e.ContextLength)
	}
}

// ─── applyDefaults — ChatTemplate default ────────────────────────────────────

// TestApplyDefaults_DefaultChatTemplate verifies that ChatTemplate defaults to
// "chatml" when empty.
func TestApplyDefaults_DefaultChatTemplate(t *testing.T) {
	e := applyDefaults("m", ModelEntry{URL: "https://example.com/m.gguf", ChatTemplate: ""})
	if e.ChatTemplate != "chatml" {
		t.Errorf("expected chatml, got %q", e.ChatTemplate)
	}
}

// TestApplyDefaults_ExplicitChatTemplateKept verifies that an explicitly set
// ChatTemplate (e.g. "llama3") is not overwritten.
func TestApplyDefaults_ExplicitChatTemplateKept(t *testing.T) {
	e := applyDefaults("m", ModelEntry{URL: "https://example.com/m.gguf", ChatTemplate: "llama3"})
	if e.ChatTemplate != "llama3" {
		t.Errorf("expected llama3, got %q", e.ChatTemplate)
	}
}

// ─── LoadMerged — curated catalog ────────────────────────────────────────────

// TestLoadMerged_CuratedEntriesHaveSourceSet verifies every entry returned by
// LoadMerged has a non-empty Source field.
func TestLoadMerged_CuratedEntriesHaveSourceSet(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("LoadMerged returned no entries")
	}
	for name, e := range entries {
		if e.Source == "" {
			t.Errorf("entry %q: Source is empty", name)
		}
	}
}

// TestLoadMerged_AllCuratedEntriesHaveURL verifies every curated catalog entry
// has a non-empty URL (required for downloads).
func TestLoadMerged_AllCuratedEntriesHaveURL(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	for name, e := range entries {
		if e.URL == "" {
			t.Errorf("entry %q: URL is empty", name)
		}
	}
}

// TestLoadMerged_DefaultsAppliedToCuratedEntries verifies that applyDefaults
// runs for all curated entries (Filename and ContextLength are set).
func TestLoadMerged_DefaultsAppliedToCuratedEntries(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	for name, e := range entries {
		if e.Filename == "" {
			t.Errorf("entry %q: Filename is empty after applyDefaults", name)
		}
		if e.ContextLength == 0 {
			t.Errorf("entry %q: ContextLength is 0 after applyDefaults", name)
		}
		if e.ChatTemplate == "" {
			t.Errorf("entry %q: ChatTemplate is empty after applyDefaults", name)
		}
	}
}
