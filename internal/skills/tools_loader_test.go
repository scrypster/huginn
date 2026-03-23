package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/skills"
)

func writeTool(t *testing.T, dir, name, content string) {
	t.Helper()
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const validToolMD = "---\ntool: run_tests\ndescription: Run the test suite\nschema:\n  type: object\n  properties:\n    path:\n      type: string\n---\nRun `go test {{path}} -v` and return the output.\n"

func TestLoadToolsFromDir_ParsesValidTool(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "run_tests.md", validToolMD)

	loaded, err := skills.LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(loaded))
	}
	if loaded[0].Name() != "run_tests" {
		t.Errorf("expected 'run_tests', got %q", loaded[0].Name())
	}
	if loaded[0].Description() != "Run the test suite" {
		t.Errorf("unexpected description: %q", loaded[0].Description())
	}
}

func TestLoadToolsFromDir_MultipleTools(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "tool_a.md", "---\ntool: tool_a\ndescription: Tool A\n---\nDo A.\n")
	writeTool(t, dir, "tool_b.md", "---\ntool: tool_b\ndescription: Tool B\n---\nDo B.\n")

	loaded, err := skills.LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(loaded))
	}
}

func TestLoadToolsFromDir_NoToolsDir_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	loaded, err := skills.LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error for missing tools dir: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected empty slice, got %d", len(loaded))
	}
}

func TestLoadToolsFromDir_InvalidFrontmatter_Skipped(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "bad.md", "no frontmatter here\njust body")
	writeTool(t, dir, "good.md", validToolMD)

	loaded, err := skills.LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 tool (bad skipped), got %d", len(loaded))
	}
}

func TestLoadToolsFromDir_NonMDFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "tools")
	_ = os.MkdirAll(toolsDir, 0o750)
	_ = os.WriteFile(filepath.Join(toolsDir, "README.txt"), []byte("ignore me"), 0o644)
	writeTool(t, dir, "real_tool.md", validToolMD)

	loaded, err := skills.LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 tool, got %d", len(loaded))
	}
}
