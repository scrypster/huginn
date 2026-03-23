package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderLoadAll_TwoValidOneInvalid(t *testing.T) {
	root := t.TempDir()
	// Write valid markdown skills
	skillAContent := "---\nname: skill-a\n---\n\nPrompt A"
	if err := os.WriteFile(filepath.Join(root, "skill-a.md"), []byte(skillAContent), 0644); err != nil {
		t.Fatalf("write skill-a.md: %v", err)
	}

	skillBContent := "---\nname: skill-b\n---\n\nPrompt B"
	if err := os.WriteFile(filepath.Join(root, "skill-b.md"), []byte(skillBContent), 0644); err != nil {
		t.Fatalf("write skill-b.md: %v", err)
	}

	// Write invalid markdown skill (no frontmatter)
	badSkillContent := "not a skill"
	if err := os.WriteFile(filepath.Join(root, "bad-skill.md"), []byte(badSkillContent), 0644); err != nil {
		t.Fatalf("write bad-skill.md: %v", err)
	}
	os.WriteFile(filepath.Join(root, "installed.json"), []byte(`[`+
		`{"name":"skill-a","source":"local","enabled":true},`+
		`{"name":"skill-b","source":"local","enabled":true}]`), 0644)

	loader := NewLoader(root)
	skills, errs := loader.LoadAll()
	// Expect one load error for bad-skill.md
	if len(errs) != 1 {
		t.Errorf("LoadAll returned %d errors, want 1 (for bad-skill.md)", len(errs))
	}
	if len(skills) != 2 {
		t.Errorf("LoadAll returned %d skills, want 2", len(skills))
	}
	names := make(map[string]bool)
	for _, s := range skills {
		names[s.Name()] = true
	}
	if !names["skill-a"] {
		t.Error("expected skill-a in results")
	}
	if !names["skill-b"] {
		t.Error("expected skill-b in results")
	}
}

func TestLoaderLoadAll_NonExistentDir_ReturnsEmptySlice(t *testing.T) {
	loader := NewLoader("/nonexistent/path/that/does/not/exist")
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: expected no error for missing dir, got: %v", errs)
	}
	if skills == nil {
		t.Error("LoadAll returned nil slice, want empty non-nil slice")
	}
	if len(skills) != 0 {
		t.Errorf("LoadAll returned %d skills for nonexistent dir, want 0", len(skills))
	}
}

func TestLoaderLoadAll_EmptyDir_ReturnsEmptySlice(t *testing.T) {
	root := t.TempDir()
	loader := NewLoader(root)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	if len(skills) != 0 {
		t.Errorf("LoadAll returned %d skills for empty dir, want 0", len(skills))
	}
}

func TestLoaderLoadRuleFiles_FindsCursorrules(t *testing.T) {
	wsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsRoot, ".cursorrules"), []byte("use tabs not spaces"), 0644); err != nil {
		t.Fatalf("write .cursorrules: %v", err)
	}
	loader := NewLoader("")
	result := loader.LoadRuleFiles(wsRoot)
	if !strings.Contains(result, "// Rules from: .cursorrules") {
		t.Errorf("result missing header, got: %q", result)
	}
	if !strings.Contains(result, "use tabs not spaces") {
		t.Errorf("result missing content, got: %q", result)
	}
}

func TestLoaderLoadRuleFiles_EmptyWorkspaceRoot_ReturnsEmpty(t *testing.T) {
	loader := NewLoader("")
	result := loader.LoadRuleFiles("")
	if result != "" {
		t.Errorf("LoadRuleFiles(\"\") = %q, want empty string", result)
	}
}

func TestLoaderLoadRuleFiles_NoFilesFound_ReturnsEmpty(t *testing.T) {
	wsRoot := t.TempDir()
	loader := NewLoader("")
	result := loader.LoadRuleFiles(wsRoot)
	if result != "" {
		t.Errorf("LoadRuleFiles with no rule files = %q, want empty string", result)
	}
}

func TestLoaderLoadRuleFiles_MultipleFiles_ConcatenatedWithHeaders(t *testing.T) {
	wsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsRoot, ".cursorrules"), []byte("cursor rules here"), 0644); err != nil {
		t.Fatalf("write .cursorrules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsRoot, "CLAUDE.md"), []byte("claude rules here"), 0644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	loader := NewLoader("")
	result := loader.LoadRuleFiles(wsRoot)
	if !strings.Contains(result, "// Rules from: .cursorrules") {
		t.Errorf("missing .cursorrules header in: %q", result)
	}
	if !strings.Contains(result, "cursor rules here") {
		t.Errorf("missing .cursorrules content in: %q", result)
	}
	if !strings.Contains(result, "// Rules from: CLAUDE.md") {
		t.Errorf("missing CLAUDE.md header in: %q", result)
	}
	if !strings.Contains(result, "claude rules here") {
		t.Errorf("missing CLAUDE.md content in: %q", result)
	}
	cursorIdx := strings.Index(result, ".cursorrules")
	claudeIdx := strings.Index(result, "CLAUDE.md")
	if cursorIdx > claudeIdx {
		t.Errorf(".cursorrules header should appear before CLAUDE.md header")
	}
}

func TestLoader_LoadAllMarkdown(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go-expert.md"), []byte("---\nname: go-expert\n---\n\nGo prompt.\n"), 0644)
	os.WriteFile(filepath.Join(dir, "rust-guide.md"), []byte("---\nname: rust-guide\n---\n\nRust prompt.\n"), 0644)
	os.WriteFile(filepath.Join(dir, "installed.json"), []byte(`[`+
		`{"name":"go-expert","source":"local","enabled":true},`+
		`{"name":"rust-guide","source":"local","enabled":true}]`), 0644)

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
}

func TestLoader_LoadAll_SkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "enabled-skill.md"), []byte("---\nname: enabled-skill\n---\n\nEnabled.\n"), 0644)
	os.WriteFile(filepath.Join(dir, "disabled-skill.md"), []byte("---\nname: disabled-skill\n---\n\nDisabled.\n"), 0644)
	manifest := `[{"name":"enabled-skill","source":"local","enabled":true},{"name":"disabled-skill","source":"registry","enabled":false}]`
	os.WriteFile(filepath.Join(dir, "installed.json"), []byte(manifest), 0644)

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill (enabled only), got %d", len(skills))
	}
	if skills[0].Name() != "enabled-skill" {
		t.Errorf("unexpected skill: %s", skills[0].Name())
	}
}

func TestLoader_LoadAll_SkipsInvalidMarkdown(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("not a skill"), 0644)
	os.WriteFile(filepath.Join(dir, "good.md"), []byte("---\nname: good\n---\n\nbody\n"), 0644)
	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"good","source":"local","enabled":true}]`), 0644)

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	// Expect one error for bad.md, but good.md should still load
	if len(errs) != 1 {
		t.Errorf("LoadAll returned %d errors, want 1 (for bad.md)", len(errs))
	}
	if len(skills) != 1 || skills[0].Name() != "good" {
		t.Errorf("expected only good skill, got %d skills", len(skills))
	}
}
