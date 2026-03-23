package notepad

import "testing"

func TestParseNotepad_NoFrontmatter(t *testing.T) {
	data := []byte("All routes must be versioned.")
	np, err := ParseNotepad("api-rules", "global", "/tmp/api-rules.md", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Name != "api-rules" {
		t.Errorf("Name = %q", np.Name)
	}
	if np.Priority != 0 {
		t.Errorf("Priority = %d, want 0", np.Priority)
	}
	if np.Scope != "global" {
		t.Errorf("Scope = %q, want global", np.Scope)
	}
	if np.Content != "All routes must be versioned." {
		t.Errorf("Content = %q", np.Content)
	}
}

func TestParseNotepad_FrontmatterHighPriority(t *testing.T) {
	data := []byte("---\npriority: high\ntags: [arch, api]\nscope: project\n---\nBody text.")
	np, err := ParseNotepad("arch", "global", "/tmp/arch.md", data)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if np.Priority != 1 {
		t.Errorf("Priority = %d, want 1", np.Priority)
	}
	if np.Scope != "project" {
		t.Errorf("Scope = %q, want project", np.Scope)
	}
	if len(np.Tags) != 2 {
		t.Errorf("Tags = %v", np.Tags)
	}
	if np.Content != "Body text." {
		t.Errorf("Content = %q", np.Content)
	}
}

func TestParseNotepad_MalformedYAMLReturnsError(t *testing.T) {
	data := []byte("---\npriority: [unclosed\n---\nBody.")
	_, err := ParseNotepad("bad", "global", "/tmp/bad.md", data)
	if err == nil {
		t.Error("expected error for malformed YAML")
	}
}
