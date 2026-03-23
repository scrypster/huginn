package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileTool_Permission(t *testing.T) {
	tool := &EditFileTool{}
	if tool.Permission() != PermWrite {
		t.Errorf("expected PermWrite, got %v", tool.Permission())
	}
}

func TestGrepTool_Permission(t *testing.T) {
	tool := &GrepTool{}
	if tool.Permission() != PermRead {
		t.Errorf("expected PermRead, got %v", tool.Permission())
	}
}

func TestReadFileTool_Permission(t *testing.T) {
	tool := &ReadFileTool{}
	if tool.Permission() != PermRead {
		t.Errorf("expected PermRead, got %v", tool.Permission())
	}
}

func TestListDirTool_RecursiveDepthLimit(t *testing.T) {
	dir := t.TempDir()
	// Create a deeply nested structure: a/b/c/d/e (5 levels)
	deep := filepath.Join(dir, "a", "b", "c", "d", "e")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	// Put a file at depth 5
	os.WriteFile(filepath.Join(deep, "deep.txt"), []byte("deep"), 0644)
	// Put a file at depth 2
	os.WriteFile(filepath.Join(dir, "a", "b", "shallow.txt"), []byte("shallow"), 0644)

	tool := &ListDirTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"path":      ".",
		"recursive": true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// shallow.txt (depth 2) should appear
	if !strings.Contains(result.Output, "shallow.txt") {
		t.Error("expected shallow.txt in output")
	}
	// deep.txt (depth 5) should NOT appear due to depth-3 limit
	if strings.Contains(result.Output, "deep.txt") {
		t.Error("deep.txt should be excluded by depth-3 limit")
	}
}

func TestListDirTool_SymlinkDetected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	os.WriteFile(target, []byte("content"), 0644)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	tool := &ListDirTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"path": "."})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The listing should contain both entries
	if !strings.Contains(result.Output, "target.txt") {
		t.Error("expected target.txt")
	}
	if !strings.Contains(result.Output, "link.txt") {
		t.Error("expected link.txt")
	}
}

func TestSearchFilesTool_DoubleGlobRelPathMatch(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "src", "pkg")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(sub, "util.py"), []byte("pass"), 0644)

	tool := &SearchFilesTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "main.go") {
		t.Error("expected main.go in double-glob results")
	}
	if strings.Contains(result.Output, "util.py") {
		t.Error("util.py should not match **/*.go")
	}
}

func TestSearchFilesTool_PathSubdir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("hi"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hi"), 0644)

	tool := &SearchFilesTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.txt",
		"path":    "subdir",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "a.txt") {
		t.Error("expected a.txt from subdir search")
	}
}

func TestSearchFilesTool_SandboxEscape(t *testing.T) {
	dir := t.TempDir()
	tool := &SearchFilesTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    "../../etc",
	})
	if !result.IsError {
		t.Error("expected sandbox escape error")
	}
}

func TestSearchFilesTool_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	tool := &SearchFilesTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "",
	})
	if !result.IsError {
		t.Error("expected error for empty pattern")
	}
}

func TestGrepTool_ContextLines(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nTARGET\nline4\nline5\n"
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644)

	tool := &GrepTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern":       "TARGET",
		"context_lines": float64(1),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "line2") {
		t.Error("expected context line 'line2' before match")
	}
	if !strings.Contains(result.Output, "line4") {
		t.Error("expected context line 'line4' after match")
	}
}

func TestGrepTool_IntContextLines(t *testing.T) {
	dir := t.TempDir()
	content := "aaa\nbbb\nMATCH\nddd\neee\n"
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644)

	tool := &GrepTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern":       "MATCH",
		"context_lines": 1,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "bbb") {
		t.Error("expected context line before match")
	}
}

func TestGrepTool_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	tool := &GrepTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "[invalid",
	})
	if !result.IsError {
		t.Error("expected error for invalid regex")
	}
}

func TestGrepTool_SingleFileSearch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "single.txt"), []byte("hello world"), 0644)

	tool := &GrepTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    "single.txt",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Error("expected match in single file")
	}
}

func TestWriteFileTool_MissingContent(t *testing.T) {
	dir := t.TempDir()
	tool := &WriteFileTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"file_path": "test.txt",
	})
	if !result.IsError {
		t.Error("expected error for missing content")
	}
}

func TestReadFileTool_OffsetBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "short.txt"), []byte("one\ntwo\n"), 0644)

	tool := &ReadFileTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"file_path": "short.txt",
		"offset":    float64(999),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output for offset beyond EOF, got %q", result.Output)
	}
}

func TestReadFileTool_NegativeOffset(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "neg.txt"), []byte("line1\nline2\n"), 0644)

	tool := &ReadFileTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"file_path": "neg.txt",
		"offset":    float64(-5),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "line1") {
		t.Error("negative offset should clamp to 0")
	}
}

func TestReadFileTool_IntOffset(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "int.txt"), []byte("a\nb\nc\n"), 0644)

	tool := &ReadFileTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"file_path": "int.txt",
		"offset":    2,
		"limit":     1,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "b") {
		t.Error("expected line 'b' at offset 2")
	}
}
