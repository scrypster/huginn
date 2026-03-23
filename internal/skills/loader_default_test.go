package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLoader_ReturnsLoader(t *testing.T) {
	l := DefaultLoader()
	if l == nil {
		t.Fatal("expected non-nil loader")
	}
	if l.skillsDir == "" {
		t.Error("expected non-empty skillsDir")
	}
	if !strings.Contains(l.skillsDir, "skills") {
		t.Errorf("expected skillsDir to contain 'skills', got %q", l.skillsDir)
	}
}

func TestDefaultLoader_WithHOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	l := DefaultLoader()
	if l == nil {
		t.Fatal("expected non-nil loader")
	}
	expected := filepath.Join(dir, ".huginn", "skills")
	if l.skillsDir != expected {
		t.Errorf("expected skillsDir=%q, got %q", expected, l.skillsDir)
	}
}

func TestLoaderLoadAll_NonDirEntriesSkipped(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file (not a directory) in the skills dir
	os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hello"), 0644)

	l := NewLoader(dir)
	skills, errs := l.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (file should be skipped), got %d", len(skills))
	}
}

func TestLoaderLoadAll_ReadDirError(t *testing.T) {
	// Use a path that exists but is a file, not a directory
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	os.WriteFile(filePath, []byte("content"), 0644)

	l := NewLoader(filePath)
	_, errs := l.LoadAll()
	if len(errs) == 0 {
		t.Fatal("expected error when skillsDir is a file")
	}
}

func TestLoaderLoadRuleFiles_HuginnRules(t *testing.T) {
	dir := t.TempDir()
	huginnDir := filepath.Join(dir, ".huginn")
	os.MkdirAll(huginnDir, 0755)
	os.WriteFile(filepath.Join(huginnDir, "rules.md"), []byte("custom rule"), 0644)

	l := NewLoader(dir)
	result := l.LoadRuleFiles(dir)
	if !strings.Contains(result, "custom rule") {
		t.Errorf("expected 'custom rule' in output, got %q", result)
	}
	if !strings.Contains(result, ".huginn/rules.md") {
		t.Errorf("expected rules.md header in output, got %q", result)
	}
}

func TestLoaderLoadRuleFiles_ClaudeMD(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude instructions"), 0644)

	l := NewLoader(dir)
	result := l.LoadRuleFiles(dir)
	if !strings.Contains(result, "claude instructions") {
		t.Errorf("expected 'claude instructions' in output, got %q", result)
	}
}
