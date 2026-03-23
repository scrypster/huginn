package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_Basic(t *testing.T) {
	root := t.TempDir()
	content := "line one\nline two\nline three\n"
	filePath := filepath.Join(root, "test.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "test.txt"})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Verify line number format: "     1\tline one"
	expected := fmt.Sprintf("%6d\t%s\n", 1, "line one")
	if !strings.Contains(result.Output, expected) {
		t.Errorf("output missing expected line 1 format %q\ngot:\n%s", expected, result.Output)
	}

	expected2 := fmt.Sprintf("%6d\t%s\n", 2, "line two")
	if !strings.Contains(result.Output, expected2) {
		t.Errorf("output missing expected line 2 format %q\ngot:\n%s", expected2, result.Output)
	}
}

func TestReadFile_OffsetBeyondEOF(t *testing.T) {
	root := t.TempDir()
	content := "only one line\n"
	filePath := filepath.Join(root, "small.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "small.txt",
		"offset":    float64(999),
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output for offset beyond EOF, got %q", result.Output)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	filePath := filepath.Join(root, "multiline.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "multiline.txt",
		"offset":    float64(3), // start at line 3
		"limit":     float64(3), // read 3 lines
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Should contain lines 3, 4, 5
	for _, lineNum := range []int{3, 4, 5} {
		expected := fmt.Sprintf("%6d\tline %d\n", lineNum, lineNum)
		if !strings.Contains(result.Output, expected) {
			t.Errorf("expected line %d in output, not found\ngot:\n%s", lineNum, result.Output)
		}
	}

	// Should NOT contain line 6
	line6 := fmt.Sprintf("%6d\tline %d\n", 6, 6)
	if strings.Contains(result.Output, line6) {
		t.Errorf("line 6 should not appear in limited output\ngot:\n%s", result.Output)
	}
}

func TestReadFile_BinaryDetection(t *testing.T) {
	root := t.TempDir()
	// Place null byte within the first 512 bytes (position 100)
	data := make([]byte, 200)
	for i := range data {
		data[i] = 'a'
	}
	data[100] = 0x00 // null byte
	filePath := filepath.Join(root, "binary.bin")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "binary.bin"})

	if !result.IsError {
		t.Fatal("expected error for binary file, got none")
	}
	if !strings.Contains(result.Error, "binary") {
		t.Errorf("expected error message to mention 'binary', got: %s", result.Error)
	}
}

func TestReadFile_NullByteAfter512(t *testing.T) {
	root := t.TempDir()
	// Null byte at position 600 — beyond the 512-byte sniff window
	data := make([]byte, 700)
	for i := range data {
		data[i] = 'a'
	}
	data[600] = 0x00
	filePath := filepath.Join(root, "notbinary.txt")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "notbinary.txt"})

	if result.IsError {
		t.Fatalf("file with null byte after 512 bytes should NOT be detected as binary, got error: %s", result.Error)
	}
}

func TestReadFile_SandboxViolation(t *testing.T) {
	root := t.TempDir()
	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "../../etc/passwd"})

	if !result.IsError {
		t.Fatal("expected error for path traversal attempt, got none")
	}
}

// TestReadFile_MissingFilePath verifies that omitting file_path returns an error.
func TestReadFile_MissingFilePath(t *testing.T) {
	root := t.TempDir()
	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing file_path")
	}
	if !strings.Contains(result.Error, "file_path") {
		t.Errorf("expected 'file_path' in error, got: %s", result.Error)
	}
}

// TestReadFile_EmptyFilePath verifies that an empty file_path returns an error.
func TestReadFile_EmptyFilePath(t *testing.T) {
	root := t.TempDir()
	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": ""})
	if !result.IsError {
		t.Fatal("expected error for empty file_path")
	}
}

// TestReadFile_NonExistentFile verifies that reading a missing file returns an error.
func TestReadFile_NonExistentFile(t *testing.T) {
	root := t.TempDir()
	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "does_not_exist.txt"})
	if !result.IsError {
		t.Fatal("expected error for non-existent file")
	}
}

// TestReadFile_EmptyFile verifies that an empty file returns empty output without error.
func TestReadFile_EmptyFile(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "empty.txt")
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "empty.txt"})
	if result.IsError {
		t.Fatalf("unexpected error for empty file: %s", result.Error)
	}
}

// TestReadFile_ZeroOffset verifies that offset=0 and offset=1 both start from line 1.
func TestReadFile_ZeroOffset(t *testing.T) {
	root := t.TempDir()
	content := "line1\nline2\n"
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}

	// offset=0 is equivalent to no offset (1-indexed, 0 => clamped to 0)
	result0 := tool.Execute(nil, map[string]any{
		"file_path": "file.txt",
		"offset":    float64(0),
	})
	result1 := tool.Execute(nil, map[string]any{
		"file_path": "file.txt",
		"offset":    float64(1),
	})

	if result0.IsError {
		t.Fatalf("unexpected error for offset=0: %s", result0.Error)
	}
	if result1.IsError {
		t.Fatalf("unexpected error for offset=1: %s", result1.Error)
	}
	// Both should show line1
	if !strings.Contains(result0.Output, "line1") {
		t.Error("offset=0 should start at line 1")
	}
	if !strings.Contains(result1.Output, "line1") {
		t.Error("offset=1 should start at line 1")
	}
}

// TestReadFile_LimitZero verifies that limit=0 or negative limit returns all lines.
func TestReadFile_LimitZero(t *testing.T) {
	root := t.TempDir()
	content := "line1\nline2\nline3\n"
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "file.txt",
		"limit":     float64(0), // 0 means no limit
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// All 3 lines should be present.
	for _, l := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(result.Output, l) {
			t.Errorf("expected %q in output with limit=0", l)
		}
	}
}

// TestReadFile_OffsetAsInt verifies that int offset (not float64) is handled correctly.
func TestReadFile_OffsetAsInt(t *testing.T) {
	root := t.TempDir()
	content := "a\nb\nc\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{
		"file_path": "f.txt",
		"offset":    int(2), // line 2
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "b") {
		t.Error("expected line 'b' starting from offset=2")
	}
}

// TestReadFile_LargeLineNumbers verifies the 6-digit line number format.
func TestReadFile_LargeLineNumbers(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 1; i <= 5; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFileTool{SandboxRoot: root}
	result := tool.Execute(nil, map[string]any{"file_path": "f.txt"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Line numbers should be 6 characters wide (right-justified).
	expected := fmt.Sprintf("%6d\tline1\n", 1)
	if !strings.Contains(result.Output, expected) {
		t.Errorf("expected 6-digit line number format %q, got:\n%s", expected, result.Output)
	}
}

// TestIsBinaryBytes_EmptyData verifies empty data is not binary.
func TestIsBinaryBytes_EmptyData(t *testing.T) {
	if isBinaryBytes([]byte{}) {
		t.Error("empty data should not be binary")
	}
}

// TestIsBinaryBytes_NullInSecondHalf verifies null byte after 512 bytes is ignored.
func TestIsBinaryBytes_NullInSecondHalf(t *testing.T) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}
	data[600] = 0x00
	if isBinaryBytes(data) {
		t.Error("null byte after 512 should not be detected as binary")
	}
}
