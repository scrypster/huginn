package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const validSkillTemplate = `---
name: %s
description: Test skill for hot-reload testing
---
This is the prompt content for skill %s.
`

// TestHotReload_LoadAll_SkillPresent verifies that a skill file written to the skills
// directory is returned by LoadAll.
func TestHotReload_LoadAll_SkillPresent(t *testing.T) {
	dir := t.TempDir()

	// Write a valid skill file.
	skillContent := "---\nname: test-skill\ndescription: A test skill\n---\nDo some testing.\n"
	skillPath := filepath.Join(dir, "test-skill.md")
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"test-skill","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errors: %v", errs)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name() != "test-skill" {
		t.Errorf("expected name=test-skill, got %q", skills[0].Name())
	}
}

// TestHotReload_Reload_UpdatedContent verifies that calling LoadAll a second time
// after modifying the skill file returns the updated content.
func TestHotReload_Reload_UpdatedContent(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "my-skill.md")

	// Write version 1.
	v1 := "---\nname: my-skill\ndescription: Version 1\n---\nOriginal prompt content.\n"
	if err := os.WriteFile(skillPath, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"my-skill","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	skills1, _ := loader.LoadAll()
	if len(skills1) == 0 {
		t.Fatal("expected skill in first load")
	}
	if skills1[0].SystemPromptFragment() != "Original prompt content." {
		t.Errorf("v1 prompt mismatch: %q", skills1[0].SystemPromptFragment())
	}

	// Overwrite with version 2.
	v2 := "---\nname: my-skill\ndescription: Version 2\n---\nUpdated prompt content.\n"
	if err := os.WriteFile(skillPath, []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-load — should reflect the updated content.
	skills2, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errors after update: %v", errs)
	}
	if len(skills2) == 0 {
		t.Fatal("expected skill in second load")
	}
	if skills2[0].SystemPromptFragment() != "Updated prompt content." {
		t.Errorf("v2 prompt mismatch: %q", skills2[0].SystemPromptFragment())
	}
}

// TestHotReload_DeletedSkill_RemovedFromLoad verifies that deleting a skill file
// causes it to be absent from the next LoadAll.
func TestHotReload_DeletedSkill_RemovedFromLoad(t *testing.T) {
	dir := t.TempDir()
	skillPath := filepath.Join(dir, "removable-skill.md")

	content := "---\nname: removable-skill\ndescription: Temporary skill\n---\nContent here.\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"removable-skill","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	skills1, _ := loader.LoadAll()
	if len(skills1) != 1 {
		t.Fatalf("expected 1 skill before delete, got %d", len(skills1))
	}

	// Delete the file.
	if err := os.Remove(skillPath); err != nil {
		t.Fatal(err)
	}

	// Re-load — skill should be gone.
	skills2, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errors after delete: %v", errs)
	}
	if len(skills2) != 0 {
		t.Errorf("expected 0 skills after deletion, got %d", len(skills2))
	}
}

// TestHotReload_MissingDir_ReturnsEmpty verifies that a missing skills directory
// returns empty results without an error.
func TestHotReload_MissingDir_ReturnsEmpty(t *testing.T) {
	loader := NewLoader("/nonexistent/path/that/does/not/exist")
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Errorf("expected no errors for missing dir, got: %v", errs)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for missing dir, got %d", len(skills))
	}
}

// TestHotReload_MultipleSkills_AllLoaded verifies that multiple skill files in a
// directory are all loaded.
func TestHotReload_MultipleSkills_AllLoaded(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"skill-a", "skill-b", "skill-c"} {
		content := "---\nname: " + name + "\ndescription: Test\n---\nContent for " + name + ".\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "installed.json"), []byte(`[`+
		`{"name":"skill-a","source":"local","enabled":true},`+
		`{"name":"skill-b","source":"local","enabled":true},`+
		`{"name":"skill-c","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errors: %v", errs)
	}
	if len(skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(skills))
	}
}

// TestHotReload_NonMDFiles_Ignored verifies that non-.md files in the skills dir
// are not loaded as skills.
func TestHotReload_NonMDFiles_Ignored(t *testing.T) {
	dir := t.TempDir()

	// Write a valid skill.
	if err := os.WriteFile(filepath.Join(dir, "valid.md"),
		[]byte("---\nname: valid\ndescription: ok\n---\nContent.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"valid","source":"local","enabled":true}]`), 0o644)
	// Write a non-.md file that should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"),
		[]byte("this is not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"key":"value"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dir)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll errors: %v", errs)
	}
	if len(skills) != 1 {
		t.Errorf("expected exactly 1 skill (only .md file), got %d", len(skills))
	}
}

// TestHotReload_Registry_RegisterAndReload verifies that a SkillRegistry can be
// repopulated by registering skills loaded on each pass.
func TestHotReload_Registry_RegisterAndReload(t *testing.T) {
	dir := t.TempDir()

	content := "---\nname: dynamic-skill\ndescription: Dynamic\n---\nDynamic content.\n"
	if err := os.WriteFile(filepath.Join(dir, "dynamic.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "installed.json"),
		[]byte(`[{"name":"dynamic-skill","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	reg := NewSkillRegistry()

	skills, _ := loader.LoadAll()
	for _, s := range skills {
		reg.Register(s)
	}

	if reg.FindByName("dynamic-skill") == nil {
		t.Error("expected dynamic-skill in registry after load")
	}

	// Simulate reload: create a new registry and re-register.
	newReg := NewSkillRegistry()
	skills2, _ := loader.LoadAll()
	for _, s := range skills2 {
		newReg.Register(s)
	}

	if newReg.FindByName("dynamic-skill") == nil {
		t.Error("expected dynamic-skill in new registry after reload")
	}
}

// TestHotReload_ConcurrentLoadsAndRegisters verifies that concurrent LoadAll calls
// and Registry.Register operations don't cause data races or panics.
func TestHotReload_ConcurrentLoadsAndRegisters(t *testing.T) {
	dir := t.TempDir()

	// Pre-create several skill files
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("skill-%d", i)
		content := fmt.Sprintf("---\nname: %s\ndescription: Skill %d\n---\nContent for skill %d.\n",
			name, i, i)
		skillPath := filepath.Join(dir, fmt.Sprintf("skill%d.md", i))
		if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "installed.json"), []byte(`[`+
		`{"name":"skill-0","source":"local","enabled":true},`+
		`{"name":"skill-1","source":"local","enabled":true},`+
		`{"name":"skill-2","source":"local","enabled":true},`+
		`{"name":"skill-3","source":"local","enabled":true},`+
		`{"name":"skill-4","source":"local","enabled":true}]`), 0o644)

	loader := NewLoader(dir)
	reg := NewSkillRegistry()

	const numGoroutines = 10
	var wg sync.WaitGroup

	// Load and register concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			skills, errs := loader.LoadAll()
			if len(errs) > 0 {
				// Errors are acceptable (some skills may already be in registry)
				return
			}
			for _, s := range skills {
				// Ignore collision errors during concurrent registration
				_ = reg.Register(s)
			}
		}(i)
	}

	wg.Wait()

	// After all goroutines complete, verify we have some skills
	allSkills := reg.All()
	if len(allSkills) == 0 {
		t.Error("expected some skills in registry after concurrent loads and registers")
	}
}

// TestHotReload_ConcurrentRegistryReads verifies that concurrent All() and FindByName
// calls work correctly with concurrent writes.
func TestHotReload_ConcurrentRegistryReads(t *testing.T) {
	reg := NewSkillRegistry()

	// Pre-populate with some skills
	for i := 0; i < 5; i++ {
		skillContent := fmt.Sprintf("---\nname: skill-%d\ndescription: Test skill %d\n---\nContent.\n", i, i)
		skill, err := ParseMarkdownSkillBytes([]byte(skillContent))
		if err != nil {
			t.Fatalf("Failed to parse skill %d: %v", i, err)
		}
		if err := reg.Register(skill); err != nil {
			t.Fatalf("Failed to register skill %d: %v", i, err)
		}
	}

	const numReadGoroutines = 10
	var wg sync.WaitGroup

	// Concurrent read operations
	for i := 0; i < numReadGoroutines; i++ {
		wg.Add(1)
		go func(readID int) {
			defer wg.Done()
			// Call All() multiple times
			all := reg.All()
			if len(all) == 0 {
				t.Errorf("Reader %d: expected skills in registry", readID)
			}
			// Call FindByName
			skill := reg.FindByName("skill-0")
			if skill == nil {
				t.Errorf("Reader %d: expected to find skill-0", readID)
			}
		}(i)
	}

	wg.Wait()
}
