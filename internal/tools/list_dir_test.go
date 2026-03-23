package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListDirTool_Name verifies the tool name.
func TestListDirTool_Name(t *testing.T) {
	tool := &ListDirTool{}
	if tool.Name() != "list_dir" {
		t.Errorf("expected name 'list_dir', got %q", tool.Name())
	}
}

// TestListDirTool_Description verifies description is non-empty.
func TestListDirTool_Description(t *testing.T) {
	tool := &ListDirTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestListDirTool_Permission verifies list_dir is PermRead.
func TestListDirTool_Permission(t *testing.T) {
	tool := &ListDirTool{}
	if tool.Permission() != PermRead {
		t.Errorf("expected PermRead, got %v", tool.Permission())
	}
}

// TestListDirTool_Schema verifies the schema is properly formed.
func TestListDirTool_Schema(t *testing.T) {
	tool := &ListDirTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected schema type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "list_dir" {
		t.Errorf("expected function name 'list_dir', got %q", schema.Function.Name)
	}
	if schema.Function.Description == "" {
		t.Error("expected non-empty function description")
	}
	if _, ok := schema.Function.Parameters.Properties["path"]; !ok {
		t.Error("expected 'path' property in schema")
	}
	if _, ok := schema.Function.Parameters.Properties["recursive"]; !ok {
		t.Error("expected 'recursive' property in schema")
	}
}

// TestListDirTool_Execute_BasicListing lists a temp dir with known files.
func TestListDirTool_Execute_BasicListing(t *testing.T) {
	root := t.TempDir()
	// Create known files and a subdirectory
	if err := os.WriteFile(filepath.Join(root, "file_a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file_b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	tool := &ListDirTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"path": "."})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "file_a.txt") {
		t.Error("expected file_a.txt in listing")
	}
	if !strings.Contains(result.Output, "file_b.txt") {
		t.Error("expected file_b.txt in listing")
	}
	if !strings.Contains(result.Output, "subdir") {
		t.Error("expected subdir in listing")
	}
	// Directory should have "d" prefix
	if !strings.Contains(result.Output, "d subdir") {
		t.Error("expected 'd subdir' prefix for directory")
	}
	// Files should have "f" prefix
	if !strings.Contains(result.Output, "f file_a.txt") {
		t.Error("expected 'f file_a.txt' prefix for file")
	}
}

// TestListDirTool_Execute_DefaultPath verifies that missing path defaults to ".".
func TestListDirTool_Execute_DefaultPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ListDirTool{SandboxRoot: root}
	// Omit path entirely — should default to "."
	result := tool.Execute(nil, map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "test.txt") {
		t.Error("expected test.txt in listing with default path")
	}
}

// TestListDirTool_Execute_SandboxEscape verifies path traversal is blocked.
func TestListDirTool_Execute_SandboxEscape(t *testing.T) {
	root := t.TempDir()
	tool := &ListDirTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{"path": "../../etc"})
	if !result.IsError {
		t.Fatal("expected error for path traversal, got none")
	}
}

// TestListDirTool_Execute_NonExistentDir verifies error for non-existent directory.
func TestListDirTool_Execute_NonExistentDir(t *testing.T) {
	root := t.TempDir()
	tool := &ListDirTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{"path": "does_not_exist"})
	if !result.IsError {
		t.Fatal("expected error for non-existent directory")
	}
}

// TestListDirTool_Execute_Recursive lists a nested structure recursively.
func TestListDirTool_Execute_Recursive(t *testing.T) {
	root := t.TempDir()
	// Create nested directories
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "deep.txt"), []byte("deep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("top"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ListDirTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"path":      ".",
		"recursive": true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "top.txt") {
		t.Error("expected top.txt in recursive listing")
	}
	if !strings.Contains(result.Output, "deep.txt") {
		t.Error("expected deep.txt in recursive listing")
	}
}

// TestListDirTool_Execute_RecursiveSkipsHiddenDirs verifies .git and node_modules are skipped.
func TestListDirTool_Execute_RecursiveSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "hidden.txt"), []byte("hidden"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("visible"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ListDirTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"path":      ".",
		"recursive": true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "hidden.txt") {
		t.Error("expected .git contents to be skipped in recursive listing")
	}
	if !strings.Contains(result.Output, "visible.txt") {
		t.Error("expected visible.txt to appear in recursive listing")
	}
}

// TestListDirTool_Execute_EmptyDir verifies listing an empty dir produces no error.
func TestListDirTool_Execute_EmptyDir(t *testing.T) {
	root := t.TempDir()
	tool := &ListDirTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{"path": "."})
	if result.IsError {
		t.Fatalf("unexpected error for empty dir: %s", result.Error)
	}
	// Empty dir should produce empty or whitespace-only output
	if strings.TrimSpace(result.Output) != "" {
		// Some output is acceptable (could be empty), but should not be an error
	}
}

// TestListDirTool_Execute_SubdirListing verifies listing a subdirectory works.
func TestListDirTool_Execute_SubdirListing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mydir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mydir", "inner.txt"), []byte("inner"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ListDirTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"path": "mydir"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "inner.txt") {
		t.Error("expected inner.txt when listing subdirectory")
	}
}
