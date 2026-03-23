package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFile_MultipleOccurrencesRejected(t *testing.T) {
	root := t.TempDir()
	content := "foo bar\nfoo baz\nfoo qux\n"
	filePath := filepath.Join(root, "multi.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "multi.txt",
		"old_string": "foo",
		"new_string": "bar",
		// replace_all intentionally omitted (defaults to false)
	})

	if !result.IsError {
		t.Fatal("expected error when old_string appears multiple times without replace_all")
	}

	// Error should mention the count
	if !strings.Contains(result.Error, "3") {
		t.Errorf("expected error to mention count 3, got: %s", result.Error)
	}
}

func TestEditFile_ReplaceAll(t *testing.T) {
	root := t.TempDir()
	content := "foo bar\nfoo baz\nfoo qux\n"
	filePath := filepath.Join(root, "multi.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":   "multi.txt",
		"old_string":  "foo",
		"new_string":  "REPLACED",
		"replace_all": true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "foo") {
		t.Errorf("expected all 'foo' occurrences to be replaced, got:\n%s", got)
	}
	if strings.Count(got, "REPLACED") != 3 {
		t.Errorf("expected 3 replacements, got content:\n%s", got)
	}
}

func TestEditFile_OldStringNotFound(t *testing.T) {
	root := t.TempDir()
	content := "hello world\n"
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "file.txt",
		"old_string": "this does not exist in the file",
		"new_string": "replacement",
	})

	if !result.IsError {
		t.Fatal("expected error when old_string is not found")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected 'not found' in error message, got: %s", result.Error)
	}
}

func TestEditFile_PreservesPermissions(t *testing.T) {
	root := t.TempDir()
	content := "original content\n"
	filePath := filepath.Join(root, "perm.txt")
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Verify initial permissions
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != 0600 {
		t.Fatalf("precondition failed: expected 0600, got %o", info.Mode())
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "perm.txt",
		"old_string": "original content",
		"new_string": "modified content",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	info, err = os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != 0600 {
		t.Errorf("expected file permissions 0600 to be preserved after edit, got %o", info.Mode())
	}
}

// TestEditFile_MissingFilePath verifies that omitting file_path returns an error.
func TestEditFile_MissingFilePath(t *testing.T) {
	root := t.TempDir()
	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"old_string": "a",
		"new_string": "b",
	})
	if !result.IsError {
		t.Fatal("expected error when file_path is missing")
	}
	if !strings.Contains(result.Error, "file_path") {
		t.Errorf("expected 'file_path' in error, got: %s", result.Error)
	}
}

// TestEditFile_MissingOldString verifies that omitting old_string returns an error.
func TestEditFile_MissingOldString(t *testing.T) {
	root := t.TempDir()
	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "file.txt",
		"new_string": "b",
	})
	if !result.IsError {
		t.Fatal("expected error when old_string is missing")
	}
}

// TestEditFile_MissingNewString verifies that omitting new_string returns an error.
func TestEditFile_MissingNewString(t *testing.T) {
	root := t.TempDir()
	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "file.txt",
		"old_string": "a",
	})
	if !result.IsError {
		t.Fatal("expected error when new_string is missing")
	}
}

// TestEditFile_SandboxEscape verifies that path traversal in file_path is rejected.
func TestEditFile_SandboxEscape(t *testing.T) {
	root := t.TempDir()
	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "../../etc/passwd",
		"old_string": "root",
		"new_string": "pwned",
	})
	if !result.IsError {
		t.Fatal("expected error for path traversal, got none")
	}
}

// TestEditFile_UnicodeContent verifies that unicode content is edited correctly.
func TestEditFile_UnicodeContent(t *testing.T) {
	root := t.TempDir()
	content := "héllo wörld\ncafé\nünicode\n"
	filePath := filepath.Join(root, "unicode.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "unicode.txt",
		"old_string": "café",
		"new_string": "kaffee",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, _ := os.ReadFile(filePath)
	got := string(data)
	if strings.Contains(got, "café") {
		t.Error("expected 'café' to be replaced")
	}
	if !strings.Contains(got, "kaffee") {
		t.Error("expected 'kaffee' in result")
	}
}

// TestEditFile_EmptyOldString verifies that empty old_string is rejected.
// Empty string would match everywhere via strings.Count("x", "") = len(x)+1,
// which could destroy file content. Must be rejected early.
func TestEditFile_EmptyOldString(t *testing.T) {
	root := t.TempDir()
	content := "abc"
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(context.Background(), map[string]any{
		"file_path":  "file.txt",
		"old_string": "",
		"new_string": "X",
	})
	if !result.IsError {
		t.Fatal("expected error for empty old_string")
	}
	if !strings.Contains(result.Error, "old_string") {
		t.Errorf("expected 'old_string' in error, got: %s", result.Error)
	}
}

// TestEditFile_NonExistentFile verifies that editing a file that doesn't exist returns an error.
func TestEditFile_NonExistentFile(t *testing.T) {
	root := t.TempDir()
	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":  "does_not_exist.txt",
		"old_string": "foo",
		"new_string": "bar",
	})
	if !result.IsError {
		t.Fatal("expected error for non-existent file")
	}
}

// TestEditFile_MetadataReplacementCount verifies the metadata key.
func TestEditFile_MetadataReplacementCount(t *testing.T) {
	root := t.TempDir()
	content := "aaa"
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path":   "file.txt",
		"old_string":  "a",
		"new_string":  "b",
		"replace_all": true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	count, ok := result.Metadata["replacements"].(int)
	if !ok {
		t.Fatalf("expected int metadata 'replacements', got %T", result.Metadata["replacements"])
	}
	if count != 3 {
		t.Errorf("expected 3 replacements, got %d", count)
	}
}
