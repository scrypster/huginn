package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchFilesTool_Name verifies the tool name.
func TestSearchFilesTool_Name(t *testing.T) {
	tool := &SearchFilesTool{}
	if tool.Name() != "search_files" {
		t.Errorf("expected name 'search_files', got %q", tool.Name())
	}
}

// TestSearchFilesTool_Description verifies description is non-empty.
func TestSearchFilesTool_Description(t *testing.T) {
	tool := &SearchFilesTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestSearchFilesTool_Permission verifies search_files is PermRead.
func TestSearchFilesTool_Permission(t *testing.T) {
	tool := &SearchFilesTool{}
	if tool.Permission() != PermRead {
		t.Errorf("expected PermRead, got %v", tool.Permission())
	}
}

// TestSearchFilesTool_Schema verifies the schema is properly formed.
func TestSearchFilesTool_Schema(t *testing.T) {
	tool := &SearchFilesTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected schema type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "search_files" {
		t.Errorf("expected function name 'search_files', got %q", schema.Function.Name)
	}
	if schema.Function.Description == "" {
		t.Error("expected non-empty function description")
	}
	if _, ok := schema.Function.Parameters.Properties["pattern"]; !ok {
		t.Error("expected 'pattern' property in schema")
	}
	if _, ok := schema.Function.Parameters.Properties["path"]; !ok {
		t.Error("expected 'path' property in schema")
	}
	found := false
	for _, req := range schema.Function.Parameters.Required {
		if req == "pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'pattern' to be in required parameters")
	}
}

// TestSearchFilesTool_Execute_MissingPattern verifies error when pattern is omitted.
func TestSearchFilesTool_Execute_MissingPattern(t *testing.T) {
	root := t.TempDir()
	tool := &SearchFilesTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{})
	if !result.IsError {
		t.Fatal("expected error when pattern is missing")
	}
	if !strings.Contains(result.Error, "pattern") {
		t.Errorf("expected 'pattern' in error, got: %s", result.Error)
	}
}

// TestSearchFilesTool_Execute_EmptyPattern verifies error for empty pattern.
func TestSearchFilesTool_Execute_EmptyPattern(t *testing.T) {
	root := t.TempDir()
	tool := &SearchFilesTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{"pattern": ""})
	if !result.IsError {
		t.Fatal("expected error for empty pattern")
	}
}

// TestSearchFilesTool_Execute_GlobPattern finds files matching *.go.
func TestSearchFilesTool_Execute_GlobPattern(t *testing.T) {
	root := t.TempDir()
	// Create some files
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "util.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": "*.go"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "main.go") {
		t.Error("expected main.go in search results")
	}
	if !strings.Contains(result.Output, "util.go") {
		t.Error("expected util.go in search results")
	}
	if strings.Contains(result.Output, "readme.txt") {
		t.Error("expected readme.txt to be excluded from *.go search")
	}
}

// TestSearchFilesTool_Execute_NoMatches verifies informative output when nothing matches.
func TestSearchFilesTool_Execute_NoMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("text"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": "*.go"})

	if result.IsError {
		t.Fatalf("no-match should not be an error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "no files") {
		t.Errorf("expected 'no files' message, got: %s", result.Output)
	}
}

// TestSearchFilesTool_Execute_DoubleGlob verifies **/*.go style pattern.
func TestSearchFilesTool_Execute_DoubleGlob(t *testing.T) {
	root := t.TempDir()
	// Create a nested go file
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "sub", "nested.go"), []byte("package sub"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": "**/*.go"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Should find nested.go and top.go
	if !strings.Contains(result.Output, "nested.go") {
		t.Error("expected nested.go in double-glob results")
	}
	if !strings.Contains(result.Output, "top.go") {
		t.Error("expected top.go in double-glob results")
	}
}

// TestSearchFilesTool_Execute_SandboxEscape verifies path traversal is blocked.
func TestSearchFilesTool_Execute_SandboxEscape(t *testing.T) {
	root := t.TempDir()
	tool := &SearchFilesTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"pattern": "*.go",
		"path":    "../../etc",
	})
	if !result.IsError {
		t.Fatal("expected error for path traversal")
	}
}

// TestSearchFilesTool_Execute_WithPath searches within a subdirectory.
func TestSearchFilesTool_Execute_WithPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "app.go"), []byte("package app"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "*.go",
		"path":    "src",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "app.go") {
		t.Error("expected app.go in results when searching in 'src'")
	}
}

// TestSearchFilesTool_Execute_MetadataCount verifies the count metadata.
func TestSearchFilesTool_Execute_MetadataCount(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": "*.go"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	count, ok := result.Metadata["count"].(int)
	if !ok {
		t.Fatalf("expected int metadata 'count', got %T", result.Metadata["count"])
	}
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}
}

// TestSearchFilesTool_Execute_SkipsGitDir verifies .git directory is excluded.
func TestSearchFilesTool_Execute_SkipsGitDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("gitconfig"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("main"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &SearchFilesTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": "config"})

	// config inside .git should not appear
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The output either says no files or doesn't contain .git/config
	if strings.Contains(result.Output, ".git") {
		t.Error("expected .git directory contents to be skipped")
	}
}
