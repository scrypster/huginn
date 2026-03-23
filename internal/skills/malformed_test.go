package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMalformed_NoFrontmatter_ParseError verifies that a .md file without YAML
// frontmatter is rejected with an error (not a panic).
func TestMalformed_NoFrontmatter_ParseError(t *testing.T) {
	_, err := ParseMarkdownSkillBytes([]byte("No frontmatter here, just text."))
	if err == nil {
		t.Error("expected error for content without YAML frontmatter, got nil")
	}
}

// TestMalformed_UnclosedFrontmatter_ParseError verifies that a file with an unclosed
// frontmatter block is rejected gracefully.
func TestMalformed_UnclosedFrontmatter_ParseError(t *testing.T) {
	content := "---\nname: test\ndescription: Test\n" // missing closing ---
	_, err := ParseMarkdownSkillBytes([]byte(content))
	if err == nil {
		t.Error("expected error for unclosed frontmatter, got nil")
	}
}

// TestMalformed_MissingName_ParseError verifies that a skill with missing 'name'
// field in frontmatter is rejected.
func TestMalformed_MissingName_ParseError(t *testing.T) {
	content := "---\ndescription: A skill without a name\nauthor: nobody\n---\nSome content here.\n"
	_, err := ParseMarkdownSkillBytes([]byte(content))
	if err == nil {
		t.Error("expected error for skill missing 'name' field, got nil")
	}
}

// TestMalformed_EmptyName_ParseError verifies that an explicitly empty name is rejected.
func TestMalformed_EmptyName_ParseError(t *testing.T) {
	content := "---\nname: \"\"\ndescription: empty name\n---\nContent.\n"
	_, err := ParseMarkdownSkillBytes([]byte(content))
	if err == nil {
		t.Error("expected error for empty name field, got nil")
	}
}

// TestMalformed_InvalidYAML_ParseError verifies that malformed YAML in frontmatter is rejected.
func TestMalformed_InvalidYAML_ParseError(t *testing.T) {
	// Indentation error makes this YAML invalid.
	content := "---\nname: bad\n  invalid_indent: [unclosed\n---\nBody.\n"
	_, err := ParseMarkdownSkillBytes([]byte(content))
	if err == nil {
		t.Error("expected error for invalid YAML frontmatter, got nil")
	}
}

// TestMalformed_ValidFrontmatter_EmptyBody verifies that a skill with valid frontmatter
// but empty body loads successfully with empty prompt.
func TestMalformed_ValidFrontmatter_EmptyBody(t *testing.T) {
	content := "---\nname: empty-body-skill\ndescription: Skill with no body content\n---\n"
	s, err := ParseMarkdownSkillBytes([]byte(content))
	if err != nil {
		t.Fatalf("expected success for valid frontmatter with empty body, got: %v", err)
	}
	if s.Name() != "empty-body-skill" {
		t.Errorf("expected name=empty-body-skill, got %q", s.Name())
	}
	if s.SystemPromptFragment() != "" {
		t.Errorf("expected empty prompt for empty body, got %q", s.SystemPromptFragment())
	}
}

// TestMalformed_MixedValidInvalid_ValidOnesPresent verifies that a loader skips invalid
// skill files while still loading valid ones.
func TestMalformed_MixedValidInvalid_ValidOnesPresent(t *testing.T) {
	dir := t.TempDir()

	// Valid skill.
	valid := "---\nname: valid-skill\ndescription: This one is fine\n---\nValid prompt content.\n"
	if err := os.WriteFile(filepath.Join(dir, "valid.md"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invalid: missing frontmatter.
	invalid1 := "No frontmatter at all"
	if err := os.WriteFile(filepath.Join(dir, "invalid1.md"), []byte(invalid1), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invalid: missing name.
	invalid2 := "---\ndescription: no name\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "invalid2.md"), []byte(invalid2), 0o644); err != nil {
		t.Fatal(err)
	}
	// Manifest enabling the valid skill only (deny-by-default).
	if err := os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"valid-skill","source":"local","enabled":true}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()

	// Should have errors for the invalid files.
	if len(errs) == 0 {
		t.Error("expected errors for invalid skill files, got none")
	}

	// Valid skill must still be present.
	if len(skills) != 1 {
		t.Errorf("expected 1 valid skill, got %d", len(skills))
	}
	if len(skills) > 0 && skills[0].Name() != "valid-skill" {
		t.Errorf("expected valid-skill to be loaded, got %q", skills[0].Name())
	}
}

// TestMalformed_NoPanic_VariousInputs verifies that ParseMarkdownSkillBytes never panics
// on unusual inputs.
func TestMalformed_NoPanic_VariousInputs(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"whitespace_only", "   \n\n\t  "},
		{"only_dashes", "---"},
		{"binary_like", "---\nname: \x00\x01\x02\n---\n"},
		{"very_long_name", "---\nname: " + string(make([]byte, 10000)) + "\n---\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic; error is expected for most of these.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseMarkdownSkillBytes panicked: %v", r)
				}
			}()
			_, _ = ParseMarkdownSkillBytes([]byte(tc.content))
		})
	}
}

// TestMalformed_LoadMarkdownSkill_MissingFile_ReturnsError verifies that trying to
// load a skill from a non-existent path returns an error.
func TestMalformed_LoadMarkdownSkill_MissingFile_ReturnsError(t *testing.T) {
	_, err := LoadMarkdownSkill("/nonexistent/path/skill.md")
	if err == nil {
		t.Error("expected error for non-existent file path, got nil")
	}
}

// TestMalformed_WithRulesSection_SplitsCorrectly verifies that ## Rules section
// is split from prompt content correctly.
func TestMalformed_WithRulesSection_SplitsCorrectly(t *testing.T) {
	content := "---\nname: has-rules\ndescription: Has rules section\n---\nThis is the prompt.\n## Rules\nThis is the rules section.\n"
	s, err := ParseMarkdownSkillBytes([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.SystemPromptFragment() == "" {
		t.Error("expected non-empty prompt before ## Rules")
	}
	if s.RuleContent() == "" {
		t.Error("expected non-empty rules after ## Rules")
	}
	if s.SystemPromptFragment() == s.RuleContent() {
		t.Error("prompt and rules should differ")
	}
}

// TestMalformed_InvalidUTF8_ParseError verifies that a .md file containing
// invalid UTF-8 byte sequences is rejected with a descriptive error.
func TestMalformed_InvalidUTF8_ParseError(t *testing.T) {
	// \x80\x81 are invalid UTF-8 continuation bytes without a leading byte.
	invalidUTF8 := []byte("---\nname: bad-encoding\n---\nContent with invalid \x80\x81 bytes.\n")
	_, err := ParseMarkdownSkillBytes(invalidUTF8)
	if err == nil {
		t.Error("expected error for skill file with invalid UTF-8, got nil")
	}
}

// TestMalformed_InvalidUTF8_LoadAll_Skipped verifies that a .md file with invalid
// UTF-8 is skipped (counted as an error) while valid skills in the same dir load fine.
func TestMalformed_InvalidUTF8_LoadAll_Skipped(t *testing.T) {
	dir := t.TempDir()

	// Write a valid skill.
	valid := "---\nname: valid-utf8-skill\ndescription: Fine\n---\nClean content.\n"
	if err := os.WriteFile(filepath.Join(dir, "valid.md"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a skill with invalid UTF-8.
	bad := []byte("---\nname: broken\n---\nContent \x80\x81 bad.\n")
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), bad, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a manifest explicitly enabling the valid skill (deny-by-default requires this).
	manifest := `[{"name":"valid-utf8-skill","source":"local","enabled":true}]`
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir)
	loaded, errs := loader.LoadAll()

	if len(errs) == 0 {
		t.Error("expected at least one error for invalid UTF-8 skill file")
	}
	if len(loaded) != 1 || loaded[0].Name() != "valid-utf8-skill" {
		t.Errorf("expected exactly 1 valid skill, got %d: %v", len(loaded), loaded)
	}
}
