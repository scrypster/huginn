package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func makeSkillDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestLoadFromDir_FullSkill(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"test-skill"}`,
		"prompt.md":  "You are a helpful assistant specializing in Go.",
		"rules.md":   "Always write tests first.",
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if s.Name() != "test-skill" {
		t.Errorf("Name() = %q, want %q", s.Name(), "test-skill")
	}
	if s.SystemPromptFragment() != "You are a helpful assistant specializing in Go." {
		t.Errorf("SystemPromptFragment() = %q", s.SystemPromptFragment())
	}
	if s.RuleContent() != "Always write tests first." {
		t.Errorf("RuleContent() = %q", s.RuleContent())
	}
	if s.Tools() != nil {
		t.Error("Tools() should return nil in Phase 1")
	}
}

func TestLoadFromDir_MissingSkillJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadFromDir(dir)
	if err == nil {
		t.Fatal("expected error when skill.json is missing, got nil")
	}
}

func TestLoadFromDir_MissingPromptMD_OK(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"no-prompt"}`,
		"rules.md":   "some rules",
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if s.SystemPromptFragment() != "" {
		t.Errorf("SystemPromptFragment() = %q, want empty string", s.SystemPromptFragment())
	}
	if s.RuleContent() != "some rules" {
		t.Errorf("RuleContent() = %q, want %q", s.RuleContent(), "some rules")
	}
}

func TestLoadFromDir_MissingRulesMD_OK(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"no-rules"}`,
		"prompt.md":  "some prompt",
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if s.RuleContent() != "" {
		t.Errorf("RuleContent() = %q, want empty string", s.RuleContent())
	}
	if s.SystemPromptFragment() != "some prompt" {
		t.Errorf("SystemPromptFragment() = %q, want %q", s.SystemPromptFragment(), "some prompt")
	}
}

func TestLoadFromDir_CustomPromptFile(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json":    `{"name":"custom","prompt_file":"my-prompt.txt"}`,
		"my-prompt.txt": "Custom prompt content.",
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if s.SystemPromptFragment() != "Custom prompt content." {
		t.Errorf("SystemPromptFragment() = %q, want %q", s.SystemPromptFragment(), "Custom prompt content.")
	}
}

func TestLoadFromDir_InvalidJSON_ReturnsError(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{not valid json`,
	})
	_, err := LoadFromDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid skill.json JSON, got nil")
	}
}

func TestFilesystemSkill_Tools_WithValidToolFiles(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"tool-skill"}`,
		"tools/example.md": `---
tool: test_tool
description: A test tool
schema:
  type: object
  properties:
    arg1:
      type: string
---
This is the tool body with {{arg1}}.`,
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	tools := s.Tools()
	if tools == nil {
		t.Fatal("Tools() returned nil, expected non-nil slice")
	}
	if len(tools) != 1 {
		t.Errorf("Tools() returned %d elements, want 1", len(tools))
	}
	if tools[0].Name() != "test_tool" {
		t.Errorf("tool Name() = %q, want %q", tools[0].Name(), "test_tool")
	}
	if tools[0].Description() != "A test tool" {
		t.Errorf("tool Description() = %q, want %q", tools[0].Description(), "A test tool")
	}
}

func TestFilesystemSkill_Tools_EmptyToolsDir(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"no-tools"}`,
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	tools := s.Tools()
	if tools != nil {
		t.Errorf("Tools() returned %v, expected nil for empty tools dir", tools)
	}
}

func TestFilesystemSkill_Tools_MultipleToolFiles(t *testing.T) {
	dir := makeSkillDir(t, map[string]string{
		"skill.json": `{"name":"multi-tools"}`,
		"tools/tool1.md": `---
tool: tool_one
description: First tool
---
Body of tool 1`,
		"tools/tool2.md": `---
tool: tool_two
description: Second tool
---
Body of tool 2`,
	})
	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	tools := s.Tools()
	if tools == nil {
		t.Fatal("Tools() returned nil, expected non-nil slice")
	}
	if len(tools) != 2 {
		t.Errorf("Tools() returned %d elements, want 2", len(tools))
	}
}
