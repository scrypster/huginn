package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinSkills_AllParse(t *testing.T) {
	entries, err := os.ReadDir("builtin")
	if err != nil {
		t.Fatalf("ReadDir builtin: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("builtin", e.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		s, err := ParseMarkdownSkillBytes(data)
		if err != nil {
			t.Errorf("built-in skill %s failed to parse: %v", e.Name(), err)
			continue
		}
		if s.Description() == "" {
			t.Errorf("built-in skill %s has empty description", e.Name())
		}
	}
	if len(entries) == 0 {
		t.Error("no built-in skills found")
	}
}

func TestParseMarkdownSkill_Basic(t *testing.T) {
	content := `---
name: go-expert
author: official
source: github:scrypster/huginn-skills/skills/official/go-expert
huginn:
  priority: 10
---

# Go Expert

Go expert provides knowledge about Go programming language.

This is the system prompt fragment content for Go experts.

## Rules

Never use init() functions in production code.
Always write tests first.`

	s, err := parseMarkdownSkill(content)
	if err != nil {
		t.Fatalf("parseMarkdownSkill: %v", err)
	}

	if s.Name() != "go-expert" {
		t.Errorf("Name() = %q, want %q", s.Name(), "go-expert")
	}

	if s.Author() != "official" {
		t.Errorf("Author() = %q, want %q", s.Author(), "official")
	}

	if s.Source() != "github:scrypster/huginn-skills/skills/official/go-expert" {
		t.Errorf("Source() = %q, want %q", s.Source(), "github:scrypster/huginn-skills/skills/official/go-expert")
	}

	prompt := s.SystemPromptFragment()
	if prompt == "" {
		t.Error("SystemPromptFragment() is empty, expected non-empty")
	}
	// Should contain the Go expert reference
	if !strings.Contains(prompt, "Go expert") {
		t.Errorf("SystemPromptFragment() does not contain 'Go expert': %s", prompt)
	}

	rules := s.RuleContent()
	if rules == "" {
		t.Error("RuleContent() is empty, expected non-empty")
	}
	if !strings.Contains(rules, "Never use init()") {
		t.Errorf("RuleContent() does not contain 'Never use init()': %s", rules)
	}

	if s.Tools() != nil {
		t.Error("Tools() should return nil for SKILL.md (Phase 1)")
	}

	if s.Priority() != 10 {
		t.Errorf("Priority() = %d, want %d", s.Priority(), 10)
	}
}

func TestParseMarkdownSkill_NoRules(t *testing.T) {
	content := `---
name: test-skill
---

This is a skill with no rules section.`

	s, err := parseMarkdownSkill(content)
	if err != nil {
		t.Fatalf("parseMarkdownSkill: %v", err)
	}

	if s.Name() != "test-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "test-skill")
	}

	if s.RuleContent() != "" {
		t.Errorf("RuleContent() = %q, want empty string", s.RuleContent())
	}

	prompt := s.SystemPromptFragment()
	if prompt != "This is a skill with no rules section." {
		t.Errorf("SystemPromptFragment() = %q, want %q", prompt, "This is a skill with no rules section.")
	}
}

func TestParseMarkdownSkill_MissingName(t *testing.T) {
	content := `---
author: someone
---

Some content`

	_, err := parseMarkdownSkill(content)
	if err == nil {
		t.Fatal("expected error when name is missing, got nil")
	}
}

func TestParseMarkdownSkill_NoVersionOk(t *testing.T) {
	content := `---
name: test-skill
---

Some content`

	s, err := parseMarkdownSkill(content)
	if err != nil {
		t.Fatalf("expected no error when version is absent, got: %v", err)
	}
	if s.Name() != "test-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "test-skill")
	}
}

func TestParseMarkdownSkill_NoFrontMatter(t *testing.T) {
	content := `
This is content without YAML frontmatter.
name: test-skill`

	_, err := parseMarkdownSkill(content)
	if err == nil {
		t.Fatal("expected error when frontmatter is missing, got nil")
	}
}

func TestParseMarkdownSkill_InvalidYAML(t *testing.T) {
	content := `---
name: test-skill
invalid: [
---

Some content`

	_, err := parseMarkdownSkill(content)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestParseMarkdownSkillBytes(t *testing.T) {
	data := []byte(`---
name: byte-skill
---

Content from bytes`)

	s, err := ParseMarkdownSkillBytes(data)
	if err != nil {
		t.Fatalf("ParseMarkdownSkillBytes: %v", err)
	}

	if s.Name() != "byte-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "byte-skill")
	}

	if s.SystemPromptFragment() != "Content from bytes" {
		t.Errorf("SystemPromptFragment() = %q, want %q", s.SystemPromptFragment(), "Content from bytes")
	}
}

func TestLoadMarkdownSkill_File(t *testing.T) {
	tmpdir := t.TempDir()
	skillPath := filepath.Join(tmpdir, "SKILL.md")

	content := `---
name: file-skill
author: test-author
---

File-based skill content`

	if err := os.WriteFile(skillPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := LoadMarkdownSkill(skillPath)
	if err != nil {
		t.Fatalf("LoadMarkdownSkill: %v", err)
	}

	if s.Name() != "file-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "file-skill")
	}

	if s.Author() != "test-author" {
		t.Errorf("Author() = %q, want %q", s.Author(), "test-author")
	}
}

func TestLoadMarkdownSkill_FileNotFound(t *testing.T) {
	_, err := LoadMarkdownSkill("/nonexistent/path/to/SKILL.md")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestMarkdownSkill_HuginnPriority(t *testing.T) {
	content := `---
name: priority-skill
huginn:
  priority: 42
---

Content`

	s, err := parseMarkdownSkill(content)
	if err != nil {
		t.Fatalf("parseMarkdownSkill: %v", err)
	}

	if s.Priority() != 42 {
		t.Errorf("Priority() = %d, want %d", s.Priority(), 42)
	}
}

func TestMarkdownSkill_DefaultPriority(t *testing.T) {
	content := `---
name: no-priority-skill
---

Content`

	s, err := parseMarkdownSkill(content)
	if err != nil {
		t.Fatalf("parseMarkdownSkill: %v", err)
	}

	if s.Priority() != 0 {
		t.Errorf("Priority() = %d, want %d", s.Priority(), 0)
	}
}

func TestMarkdownSkill_SplitAtRules(t *testing.T) {
	body := `First part of the prompt.

More content here.

## Rules

Never do this.
Always do that.
One more rule.`

	prompt, rules := splitAtRules(body)

	if !strings.Contains(prompt, "First part of the prompt") {
		t.Errorf("prompt does not contain 'First part of the prompt': %s", prompt)
	}

	if strings.Contains(prompt, "## Rules") {
		t.Errorf("prompt should not contain '## Rules': %s", prompt)
	}

	if !strings.Contains(rules, "Never do this") {
		t.Errorf("rules does not contain 'Never do this': %s", rules)
	}
}

func TestMarkdownSkill_RulesEdgeCases(t *testing.T) {
	// Test with ## Rules in the middle but not on its own line
	body := `This has ## Rules inline, not as a header.

Some more content.`

	_, rules := splitAtRules(body)
	if rules != "" {
		t.Errorf("rules should be empty when ## Rules is not a heading, got: %s", rules)
	}
}

func TestMarkdownSkill_Description(t *testing.T) {
	content := "---\nname: plan\nauthor: huginn\ndescription: Plan before coding\n---\n\nPlan prompt here.\n"
	s, err := ParseMarkdownSkillBytes([]byte(content))
	if err != nil {
		t.Fatalf("ParseMarkdownSkillBytes: %v", err)
	}
	if s.Description() != "Plan before coding" {
		t.Errorf("Description: got %q, want %q", s.Description(), "Plan before coding")
	}
}

func TestMarkdownSkill_Description_EmptyWhenNotSet(t *testing.T) {
	content := "---\nname: plan\n---\n\nContent here.\n"
	s, err := ParseMarkdownSkillBytes([]byte(content))
	if err != nil {
		t.Fatalf("ParseMarkdownSkillBytes: %v", err)
	}
	if s.Description() != "" {
		t.Errorf("expected empty description, got %q", s.Description())
	}
}
