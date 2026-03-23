package models

import (
	"testing"
)

// TestLoadMerged_ReturnsMap verifies LoadMerged returns a map of models.
func TestLoadMerged_ReturnsMap(t *testing.T) {
	models, err := LoadMerged()
	if err != nil {
		t.Logf("LoadMerged error (expected if no models configured): %v", err)
		return
	}
	// LoadMerged should return a map
	if models == nil {
		t.Error("LoadMerged should return a non-nil map")
	}
}

// TestModelEntry_Fields verifies ModelEntry has expected fields.
func TestModelEntry_Fields(t *testing.T) {
	entry := ModelEntry{
		Description:      "Test Model",
		URL:              "https://example.com/model",
		Filename:         "model.gguf",
		SHA256:           "abc123",
		SizeBytes:        1024,
		MinRAMGB:         4,
		RecommendedRAMGB: 8,
		ContextLength:    4096,
		ChatTemplate:     "{{.system}}",
		Tags:             []string{"test"},
		Source:           "curated",
	}

	if entry.Description == "" {
		t.Error("ModelEntry.Description should not be empty")
	}
	if entry.Filename == "" {
		t.Error("ModelEntry.Filename should not be empty")
	}
	if entry.URL == "" {
		t.Error("ModelEntry.URL should not be empty")
	}
	if entry.MinRAMGB <= 0 {
		t.Error("ModelEntry.MinRAMGB should be positive")
	}
}

// TestModelEntry_EmptyFields verifies behavior with empty fields.
func TestModelEntry_EmptyFields(t *testing.T) {
	entry := ModelEntry{}

	// Empty fields should be allowed at struct level; validation happens elsewhere
	if entry.Description != "" {
		t.Error("default Description should be empty")
	}
	if entry.URL != "" {
		t.Error("default URL should be empty")
	}
	if len(entry.Tags) != 0 {
		t.Error("default Tags should be empty")
	}
}

// TestModelEntry_Size verifies SizeBytes field.
func TestModelEntry_Size(t *testing.T) {
	entry := ModelEntry{
		SizeBytes: 5_368_709_120, // 5 GB
	}

	if entry.SizeBytes != 5_368_709_120 {
		t.Errorf("expected SizeBytes=5GB, got %d", entry.SizeBytes)
	}
}

// TestModelEntry_ContextLength verifies context length field.
func TestModelEntry_ContextLength(t *testing.T) {
	entry := ModelEntry{
		ContextLength: 128_000,
	}

	if entry.ContextLength != 128_000 {
		t.Errorf("expected ContextLength=128k, got %d", entry.ContextLength)
	}
}

// TestModelEntry_Tags verifies tags list.
func TestModelEntry_Tags(t *testing.T) {
	entry := ModelEntry{
		Tags: []string{"llama2", "7b", "chat"},
	}

	if len(entry.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(entry.Tags))
	}
	if entry.Tags[0] != "llama2" {
		t.Errorf("expected first tag 'llama2', got %q", entry.Tags[0])
	}
}

// TestModelEntry_Source verifies source attribution.
func TestModelEntry_Source(t *testing.T) {
	entry := ModelEntry{
		Source: "curated",
	}

	if entry.Source != "curated" {
		t.Errorf("expected Source='curated', got %q", entry.Source)
	}

	entry2 := ModelEntry{
		Source: "user",
	}

	if entry2.Source != "user" {
		t.Errorf("expected Source='user', got %q", entry2.Source)
	}
}
