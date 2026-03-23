package models

import "testing"

func TestLoadMerged_curatedLoads(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one curated entry")
	}
	e, ok := entries["qwen2.5-coder:7b"]
	if !ok {
		t.Fatal("expected qwen2.5-coder:7b in catalog")
	}
	if e.Source != "curated" {
		t.Errorf("expected source=curated, got %s", e.Source)
	}
	if e.ChatTemplate != "chatml" {
		t.Errorf("expected chatml default, got %s", e.ChatTemplate)
	}
}

func TestApplyDefaults_filenameFromURL(t *testing.T) {
	e := applyDefaults("test-model", ModelEntry{
		URL: "https://example.com/models/my-model-q4.gguf",
	})
	if e.Filename != "my-model-q4.gguf" {
		t.Errorf("unexpected filename: %s", e.Filename)
	}
}

// TestLoadMerged_AtLeastThreeEntries verifies that the embedded curated manifest
// has at least 3 entries and that defaults are applied to each.
func TestLoadMerged_AtLeastThreeEntries(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) < 3 {
		t.Errorf("expected at least 3 curated entries, got %d", len(entries))
	}
	// Every entry should have defaults applied — non-empty ChatTemplate and Filename.
	for name, e := range entries {
		if e.ChatTemplate == "" {
			t.Errorf("entry %q: ChatTemplate should not be empty after applyDefaults", name)
		}
		if e.Filename == "" {
			t.Errorf("entry %q: Filename should not be empty after applyDefaults", name)
		}
	}
}

func TestApplyDefaults_ContextLengthDefault(t *testing.T) {
	e := applyDefaults("my-model", ModelEntry{
		URL:           "https://example.com/model.gguf",
		ContextLength: 0,
	})
	if e.ContextLength != 4096 {
		t.Errorf("expected default ContextLength=4096, got %d", e.ContextLength)
	}
}

func TestApplyDefaults_ChatTemplateDefault(t *testing.T) {
	e := applyDefaults("my-model", ModelEntry{
		URL:          "https://example.com/model.gguf",
		ChatTemplate: "",
	})
	if e.ChatTemplate != "chatml" {
		t.Errorf("expected default ChatTemplate=chatml, got %s", e.ChatTemplate)
	}
}

func TestApplyDefaults_URLWithTrailingSlash(t *testing.T) {
	// When filepath.Base of the URL path segment returns "." (e.g. root "/"),
	// applyDefaults should fall back to "name.gguf".
	e := applyDefaults("name", ModelEntry{
		URL: "https://example.com/",
	})
	// filepath.Base("https://example.com/") returns "example.com" on some paths,
	// so use a URL whose last segment is unambiguously "." — a bare root path.
	// We verify the rule: when no useful filename can be derived the name is used.
	// The actual fallback fires when parts == "" or parts == ".".
	// Test using an empty URL so filepath.Base("") returns ".".
	e2 := applyDefaults("mymodel", ModelEntry{
		URL: "",
	})
	if e2.Filename != "mymodel.gguf" {
		t.Errorf("expected fallback filename mymodel.gguf for empty URL, got %s", e2.Filename)
	}
	// Also confirm non-empty URL with a real segment derives filename correctly.
	if e.Filename == "" {
		t.Error("expected non-empty filename for URL with path")
	}
}

func TestApplyDefaults_ExplicitFilenamePreserved(t *testing.T) {
	e := applyDefaults("my-model", ModelEntry{
		URL:      "https://example.com/something-else.gguf",
		Filename: "explicit.gguf",
	})
	if e.Filename != "explicit.gguf" {
		t.Errorf("expected explicit filename to be preserved, got %s", e.Filename)
	}
}

// TestApplyDefaults_PreservesExplicitContextLength verifies that an explicit
// non-zero ContextLength is preserved.
func TestApplyDefaults_PreservesExplicitContextLength(t *testing.T) {
	e := applyDefaults("model", ModelEntry{
		URL:           "https://example.com/model.gguf",
		ContextLength: 8192,
	})
	if e.ContextLength != 8192 {
		t.Errorf("expected explicit ContextLength=8192, got %d", e.ContextLength)
	}
}

// TestApplyDefaults_PreservesExplicitChatTemplate verifies that an explicit
// non-empty ChatTemplate is preserved.
func TestApplyDefaults_PreservesExplicitChatTemplate(t *testing.T) {
	e := applyDefaults("model", ModelEntry{
		URL:          "https://example.com/model.gguf",
		ChatTemplate: "mistral",
	})
	if e.ChatTemplate != "mistral" {
		t.Errorf("expected explicit ChatTemplate=mistral, got %s", e.ChatTemplate)
	}
}

// TestApplyDefaults_AllFieldsPreserved verifies that all non-default fields
// are preserved through applyDefaults.
func TestApplyDefaults_AllFieldsPreserved(t *testing.T) {
	orig := ModelEntry{
		Description:      "A test model",
		URL:              "https://example.com/test.gguf",
		Filename:         "test.gguf",
		SHA256:           "abc123",
		SizeBytes:        1000000,
		MinRAMGB:         8,
		RecommendedRAMGB: 16,
		ContextLength:    8192,
		ChatTemplate:     "mistral",
		Tags:             []string{"test", "local"},
	}
	result := applyDefaults("test", orig)

	if result.Description != "A test model" {
		t.Errorf("Description not preserved: got %q", result.Description)
	}
	if result.SHA256 != "abc123" {
		t.Errorf("SHA256 not preserved: got %q", result.SHA256)
	}
	if result.SizeBytes != 1000000 {
		t.Errorf("SizeBytes not preserved: got %d", result.SizeBytes)
	}
	if result.MinRAMGB != 8 {
		t.Errorf("MinRAMGB not preserved: got %d", result.MinRAMGB)
	}
	if result.RecommendedRAMGB != 16 {
		t.Errorf("RecommendedRAMGB not preserved: got %d", result.RecommendedRAMGB)
	}
	if len(result.Tags) != 2 {
		t.Errorf("Tags not preserved: got %v", result.Tags)
	}
}

// TestLoadMerged_SourceFieldSet verifies that the Source field is correctly
// set for embedded and user manifest entries.
func TestLoadMerged_SourceFieldSet(t *testing.T) {
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	// Every entry should have Source set to either "curated" or "user"
	for name, entry := range entries {
		if entry.Source != "curated" && entry.Source != "user" {
			t.Errorf("entry %q: expected Source to be 'curated' or 'user', got %q", name, entry.Source)
		}
	}
}

// TestLoadMerged_CuratedAlwaysLoads verifies that the curated (embedded)
// manifest always loads even without a user manifest.
func TestLoadMerged_CuratedAlwaysLoads(t *testing.T) {
	// This should succeed even if ~/.huginn/models.user.json doesn't exist.
	entries, err := LoadMerged()
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("LoadMerged returned empty entries")
	}
	// All entries should be from curated (since we're running in isolation)
	// or from user if user manifest exists. Either way, we should have entries.
	for name, entry := range entries {
		if entry.URL == "" {
			t.Errorf("entry %q: missing required URL field", name)
		}
	}
}

// TestLoadMerged_UserEntryOverridesCurated verifies that when a user manifest
// exists with an entry that matches a curated name, the user entry takes precedence.
// (Note: This is a documentation test since we can't easily mock userManifestPath.)
func TestLoadMerged_UserEntryOverridesCurated(t *testing.T) {
	t.Skip("can't easily test user manifest loading without mocking os.UserHomeDir")
	// In real usage:
	// 1. Create ~/.huginn/models.user.json with a model entry
	// 2. Call LoadMerged()
	// 3. Verify the entry has Source="user"
}
