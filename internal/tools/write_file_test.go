package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteFileTool_Name verifies the tool name.
func TestWriteFileTool_Name(t *testing.T) {
	tool := &WriteFileTool{}
	if tool.Name() != "write_file" {
		t.Errorf("expected name 'write_file', got %q", tool.Name())
	}
}

// TestWriteFileTool_Description verifies description is non-empty.
func TestWriteFileTool_Description(t *testing.T) {
	tool := &WriteFileTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestWriteFileTool_Permission verifies write_file is PermWrite.
func TestWriteFileTool_Permission(t *testing.T) {
	tool := &WriteFileTool{}
	if tool.Permission() != PermWrite {
		t.Errorf("expected PermWrite, got %v", tool.Permission())
	}
}

// TestWriteFileTool_Schema verifies the schema is properly formed.
func TestWriteFileTool_Schema(t *testing.T) {
	tool := &WriteFileTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected schema type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "write_file" {
		t.Errorf("expected function name 'write_file', got %q", schema.Function.Name)
	}
	if schema.Function.Description == "" {
		t.Error("expected non-empty function description")
	}
	if _, ok := schema.Function.Parameters.Properties["file_path"]; !ok {
		t.Error("expected 'file_path' property in schema")
	}
	if _, ok := schema.Function.Parameters.Properties["content"]; !ok {
		t.Error("expected 'content' property in schema")
	}
	requiredFields := map[string]bool{}
	for _, r := range schema.Function.Parameters.Required {
		requiredFields[r] = true
	}
	if !requiredFields["file_path"] {
		t.Error("expected 'file_path' in required parameters")
	}
	if !requiredFields["content"] {
		t.Error("expected 'content' in required parameters")
	}
}

// TestWriteFileTool_Execute_BasicWrite verifies writing content to a new file.
func TestWriteFileTool_Execute_BasicWrite(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "newfile.txt",
		"content":   "hello world",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, "newfile.txt"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected content 'hello world', got %q", string(data))
	}
}

// TestWriteFileTool_Execute_OutputMessage verifies the output includes byte count.
func TestWriteFileTool_Execute_OutputMessage(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	content := "test content"
	result := tool.Execute(nil, map[string]any{
		"file_path": "out.txt",
		"content":   content,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "wrote") {
		t.Errorf("expected 'wrote' in output, got %q", result.Output)
	}
	// Check metadata
	bytes, ok := result.Metadata["bytes_written"].(int)
	if !ok {
		t.Fatalf("expected int metadata 'bytes_written', got %T", result.Metadata["bytes_written"])
	}
	if bytes != len(content) {
		t.Errorf("expected bytes_written=%d, got %d", len(content), bytes)
	}
}

// TestWriteFileTool_Execute_MissingFilePath verifies error when file_path is missing.
func TestWriteFileTool_Execute_MissingFilePath(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"content": "some content",
	})
	if !result.IsError {
		t.Fatal("expected error when file_path is missing")
	}
	if !strings.Contains(result.Error, "file_path") {
		t.Errorf("expected 'file_path' in error, got: %s", result.Error)
	}
}

// TestWriteFileTool_Execute_EmptyFilePath verifies error for empty file_path.
func TestWriteFileTool_Execute_EmptyFilePath(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "",
		"content":   "data",
	})
	if !result.IsError {
		t.Fatal("expected error for empty file_path")
	}
}

// TestWriteFileTool_Execute_MissingContent verifies error when content is missing.
func TestWriteFileTool_Execute_MissingContent(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "file.txt",
	})
	if !result.IsError {
		t.Fatal("expected error when content is missing")
	}
	if !strings.Contains(result.Error, "content") {
		t.Errorf("expected 'content' in error, got: %s", result.Error)
	}
}

// TestWriteFileTool_Execute_SandboxEscape verifies path traversal is blocked.
func TestWriteFileTool_Execute_SandboxEscape(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "../../etc/evil.txt",
		"content":   "malicious",
	})
	if !result.IsError {
		t.Fatal("expected error for path traversal attempt")
	}
}

// TestWriteFileTool_Execute_Overwrite verifies that overwriting an existing file works.
func TestWriteFileTool_Execute_Overwrite(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &WriteFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "existing.txt",
		"content":   "overwritten",
	})

	if result.IsError {
		t.Fatalf("unexpected error on overwrite: %s", result.Error)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "overwritten" {
		t.Errorf("expected content 'overwritten', got %q", string(data))
	}
}

// TestWriteFileTool_Execute_PreservesPermissions verifies file permissions are preserved on overwrite.
func TestWriteFileTool_Execute_PreservesPermissions(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "perm.txt")
	if err := os.WriteFile(filePath, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}

	tool := &WriteFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "perm.txt",
		"content":   "new content",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != 0600 {
		t.Errorf("expected permissions 0600 preserved, got %o", info.Mode())
	}
}

// TestWriteFileTool_Execute_CreatesParentDirs verifies parent directories are created.
func TestWriteFileTool_Execute_CreatesParentDirs(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "nested/deep/file.txt",
		"content":   "nested content",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, "nested", "deep", "file.txt"))
	if err != nil {
		t.Fatalf("nested file not created: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("expected 'nested content', got %q", string(data))
	}
}

// TestWriteFileTool_Execute_EmptyContent verifies writing an empty file is valid.
func TestWriteFileTool_Execute_EmptyContent(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "empty.txt",
		"content":   "",
	})

	if result.IsError {
		t.Fatalf("unexpected error for empty content: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, "empty.txt"))
	if err != nil {
		t.Fatalf("empty file not created: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

// TestWriteFileTool_Execute_WithFileLock verifies file locking mechanism works.
func TestWriteFileTool_Execute_WithFileLock(t *testing.T) {
	root := t.TempDir()
	lock := NewFileLockManager()
	tool := &WriteFileTool{SandboxRoot: root, FileLock: lock}

	result := tool.Execute(nil, map[string]any{
		"file_path": "locked.txt",
		"content":   "locked content",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, "locked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "locked content" {
		t.Errorf("expected 'locked content', got %q", string(data))
	}
}

// TestWriteFileTool_Execute_ContentWrongType verifies error when content is wrong type.
func TestWriteFileTool_Execute_ContentWrongType(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": "file.txt",
		"content":   123, // wrong type
	})
	if !result.IsError {
		t.Fatal("expected error for non-string content")
	}
	if !strings.Contains(result.Error, "content") {
		t.Errorf("expected 'content' in error, got: %s", result.Error)
	}
}

// TestWriteFileTool_Execute_FilePathWrongType verifies error when file_path is wrong type.
func TestWriteFileTool_Execute_FilePathWrongType(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{
		"file_path": 123, // wrong type
		"content":   "data",
	})
	if !result.IsError {
		t.Fatal("expected error for non-string file_path")
	}
}

// TestWriteFileTool_Execute_LargeContent verifies writing large files works.
func TestWriteFileTool_Execute_LargeContent(t *testing.T) {
	root := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: root}

	// Create 1MB content
	largeContent := strings.Repeat("a", 1024*1024)
	result := tool.Execute(nil, map[string]any{
		"file_path": "large.txt",
		"content":   largeContent,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	data, err := os.ReadFile(filepath.Join(root, "large.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(largeContent) {
		t.Errorf("expected %d bytes, got %d", len(largeContent), len(data))
	}
}

// TestWriteFileTool_Execute_Schema verifies the schema structure.
func TestWriteFileTool_Execute_Schema(t *testing.T) {
	tool := &WriteFileTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "write_file" {
		t.Errorf("expected name 'write_file', got %q", schema.Function.Name)
	}
}
