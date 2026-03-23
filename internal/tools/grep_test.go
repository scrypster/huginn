package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	tool := &GrepTool{SandboxRoot: root}

	result := tool.Execute(nil, map[string]any{"pattern": "("})

	if !result.IsError {
		t.Fatal("expected error for invalid regex, got none")
	}
	if !strings.Contains(result.Error, "invalid pattern") {
		t.Errorf("expected error to mention 'invalid pattern', got: %s", result.Error)
	}
}

func TestGrep_BasicMatch(t *testing.T) {
	root := t.TempDir()
	content := "hello world\nfoo bar\nhello again\n"
	filePath := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "hello",
		"path":    ".",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Should have found matches
	if strings.Contains(result.Output, "no matches") {
		t.Fatalf("expected matches for 'hello', got: %s", result.Output)
	}

	// Should contain line numbers for matching lines (line 1 and line 3)
	if !strings.Contains(result.Output, ":1:") {
		t.Errorf("expected line 1 match in output, got:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, ":3:") {
		t.Errorf("expected line 3 match in output, got:\n%s", result.Output)
	}
}

func TestGrep_NoMatches(t *testing.T) {
	root := t.TempDir()
	content := "hello world\nfoo bar\n"
	filePath := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "xyzzy_no_match_here",
		"path":    ".",
	})

	if result.IsError {
		t.Fatalf("no matches should not produce an error, got: %s", result.Error)
	}

	// Output should indicate no matches (not be empty, but not be an error)
	if !strings.Contains(result.Output, "no matches") {
		t.Errorf("expected 'no matches' message in output, got: %s", result.Output)
	}
}

// TestGrep_MissingPattern verifies that omitting the pattern argument returns an error.
func TestGrep_MissingPattern(t *testing.T) {
	root := t.TempDir()
	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{})
	if !result.IsError {
		t.Fatal("expected error when pattern is missing")
	}
}

// TestGrep_EmptyPattern verifies that an empty pattern returns an error.
func TestGrep_EmptyPattern(t *testing.T) {
	root := t.TempDir()
	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"pattern": ""})
	if !result.IsError {
		t.Fatal("expected error for empty pattern")
	}
}

// TestGrep_ContextLines verifies that context_lines includes surrounding lines.
func TestGrep_ContextLines(t *testing.T) {
	root := t.TempDir()
	content := "before\nmatch_me\nafter\n"
	filePath := filepath.Join(root, "ctx.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern":       "match_me",
		"path":          ".",
		"context_lines": 1,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "before") {
		t.Error("expected context line 'before' in output")
	}
	if !strings.Contains(result.Output, "after") {
		t.Error("expected context line 'after' in output")
	}
	// Separator "--" is written between context blocks.
	if !strings.Contains(result.Output, "--") {
		t.Error("expected '--' separator for context mode")
	}
}

// TestGrep_IncludeFilter verifies that the include glob filters files.
func TestGrep_IncludeFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "match.go"), []byte("target_word\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("target_word\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "target_word",
		"path":    ".",
		"include": "*.go",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "match.go") {
		t.Error("expected match.go in output")
	}
	if strings.Contains(result.Output, "skip.txt") {
		t.Error("expected skip.txt to be excluded by include filter")
	}
}

// TestGrep_BinaryFileSkipped verifies that binary files are skipped.
func TestGrep_BinaryFileSkipped(t *testing.T) {
	root := t.TempDir()
	// Create a binary file with a null byte in the first 512 bytes.
	binary := make([]byte, 100)
	binary[50] = 0x00
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), binary, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": ".",
		"path":    ".",
	})
	// Should not error, but binary file should not be in output.
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if strings.Contains(result.Output, "binary.bin") {
		t.Error("expected binary file to be skipped")
	}
}

// TestGrep_SandboxEscape verifies path traversal is blocked.
func TestGrep_SandboxEscape(t *testing.T) {
	root := t.TempDir()
	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "root",
		"path":    "../../etc",
	})
	if !result.IsError {
		t.Fatal("expected error for path traversal attempt")
	}
}

// TestGrep_SingleFile verifies grep against a single file path.
func TestGrep_SingleFile(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "single.txt")
	if err := os.WriteFile(filePath, []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "needle",
		"path":    "single.txt",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "needle") {
		t.Error("expected 'needle' in grep output")
	}
}

// TestGrep_ContextLinesAsInt verifies context_lines accepts int type (not only float64).
func TestGrep_ContextLinesAsInt(t *testing.T) {
	root := t.TempDir()
	content := "a\nb\nc\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern":       "b",
		"path":          ".",
		"context_lines": int(1), // int not float64
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "a") {
		t.Error("expected context line 'a' in output")
	}
}

// TestGrep_MetadataFilesMatched verifies that metadata reports correct file count.
func TestGrep_MetadataFilesMatched(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("find_me\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "c.txt"), []byte("nothing\n"), 0644)

	tool := &GrepTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"pattern": "find_me",
		"path":    ".",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	count, ok := result.Metadata["files_matched"].(int)
	if !ok {
		t.Fatalf("expected int metadata 'files_matched', got %T", result.Metadata["files_matched"])
	}
	if count != 2 {
		t.Errorf("expected 2 files_matched, got %d", count)
	}
}
