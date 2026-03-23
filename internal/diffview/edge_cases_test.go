package diffview

import (
	"strings"
	"testing"
)

// TestComputeDiff_NewFile verifies diff for completely new file.
func TestComputeDiff_NewFile(t *testing.T) {
	newContent := []byte("line1\nline2\nline3")
	result := ComputeDiff("newfile.go", nil, newContent)

	if result.Path != "newfile.go" {
		t.Errorf("expected path='newfile.go', got %q", result.Path)
	}
	if result.Added != 3 {
		t.Errorf("expected 3 lines added for new file, got %d", result.Added)
	}
	if result.Deleted != 0 {
		t.Errorf("expected 0 lines deleted for new file, got %d", result.Deleted)
	}
	if result.OldContent != nil {
		t.Error("expected OldContent to be nil for new file")
	}
}

// TestComputeDiff_DeletedFile verifies diff when file is deleted.
func TestComputeDiff_DeletedFile(t *testing.T) {
	oldContent := []byte("line1\nline2\nline3")
	result := ComputeDiff("deleted.go", oldContent, []byte{})

	if result.Path != "deleted.go" {
		t.Errorf("expected path='deleted.go', got %q", result.Path)
	}
	if result.Added != 0 {
		t.Errorf("expected 0 lines added for deleted file, got %d", result.Added)
	}
	if result.Deleted != 3 {
		t.Errorf("expected 3 lines deleted for deleted file, got %d", result.Deleted)
	}
}

// TestComputeDiff_NoChanges verifies diff when content is identical.
func TestComputeDiff_NoChanges(t *testing.T) {
	content := []byte("line1\nline2\nline3")
	result := ComputeDiff("unchanged.go", content, content)

	if result.Added != 0 {
		t.Errorf("expected 0 lines added for unchanged file, got %d", result.Added)
	}
	if result.Deleted != 0 {
		t.Errorf("expected 0 lines deleted for unchanged file, got %d", result.Deleted)
	}
}

// TestComputeDiff_EmptyFiles verifies diff of empty files.
func TestComputeDiff_EmptyToEmpty(t *testing.T) {
	result := ComputeDiff("empty.go", []byte{}, []byte{})

	if result.Added != 0 || result.Deleted != 0 {
		t.Errorf("expected no changes for empty to empty, got +%d -%d", result.Added, result.Deleted)
	}
}

// TestComputeDiff_EmptyToContent verifies transition from empty to content.
func TestComputeDiff_EmptyToContent(t *testing.T) {
	result := ComputeDiff("new.go", []byte{}, []byte("package main"))

	if result.Added < 1 {
		t.Error("expected at least 1 line added")
	}
}

// TestComputeDiff_SingleLineNoNewline verifies handling of files without trailing newline.
func TestComputeDiff_NoTrailingNewline(t *testing.T) {
	old := []byte("no newline")
	new := []byte("no newline")
	result := ComputeDiff("test.go", old, new)

	if result.Added != 0 || result.Deleted != 0 {
		t.Error("expected no changes for identical content without newlines")
	}
}

// TestComputeDiff_WindowsLineEndings verifies handling of CRLF line endings.
func TestComputeDiff_CRLFLineEndings(t *testing.T) {
	old := []byte("line1\r\nline2\r\n")
	new := []byte("line1\r\nline2\r\nline3\r\n")
	result := ComputeDiff("windows.go", old, new)

	// The diff engine should handle CRLF correctly
	if result.Added < 1 {
		t.Error("expected at least 1 line added despite CRLF")
	}
}

// TestRenderDiff_EmptyDiff verifies rendering of a diff with no changes.
func TestRenderDiff_EmptyDiff(t *testing.T) {
	fd := FileDiff{
		Path:    "empty.go",
		Added:   0,
		Deleted: 0,
		Hunks:   []DiffHunk{},
	}
	output := RenderDiff(fd, 80)

	if !strings.Contains(output, "empty.go") {
		t.Errorf("RenderDiff output should contain file path, got: %q", output)
	}
	if !strings.Contains(output, "+0") {
		t.Errorf("RenderDiff output should show +0, got: %q", output)
	}
	if !strings.Contains(output, "-0") {
		t.Errorf("RenderDiff output should show -0, got: %q", output)
	}
}

// TestRenderDiff_LargeStats verifies rendering with large change counts.
func TestRenderDiff_LargeStats(t *testing.T) {
	fd := FileDiff{
		Path:    "huge.go",
		Added:   5000,
		Deleted: 3000,
		Hunks:   []DiffHunk{},
	}
	output := RenderDiff(fd, 80)

	if !strings.Contains(output, "5000") {
		t.Errorf("RenderDiff should show +5000, got: %q", output)
	}
	if !strings.Contains(output, "3000") {
		t.Errorf("RenderDiff should show -3000, got: %q", output)
	}
}

// TestRenderDiff_ZeroWidth verifies rendering with minimal width doesn't crash.
func TestRenderDiff_ZeroWidth(t *testing.T) {
	fd := FileDiff{
		Path:    "test.go",
		Added:   1,
		Deleted: 0,
		Hunks: []DiffHunk{
			{
				Header: "@@ -0,0 +1,1 @@",
				Lines: []DiffLine{
					{Op: '+', Content: "added line"},
				},
			},
		},
	}

	// Should not crash even with width 0
	output := RenderDiff(fd, 0)
	if output == "" {
		t.Error("RenderDiff with width 0 should still produce output")
	}
}

// TestRenderDiff_LongLine verifies rendering of very long lines.
func TestRenderDiff_LongLine(t *testing.T) {
	longLine := strings.Repeat("x", 1000)
	fd := FileDiff{
		Path:    "long.go",
		Added:   1,
		Deleted: 0,
		Hunks: []DiffHunk{
			{
				Header: "@@ -0,0 +1,1 @@",
				Lines: []DiffLine{
					{Op: '+', Content: longLine},
				},
			},
		},
	}

	output := RenderDiff(fd, 80)
	// Should include the long line (possibly wrapped by terminal)
	if !strings.Contains(output, "x") {
		t.Error("RenderDiff should include long line content")
	}
}

// TestRenderBatch_EmptyBatch verifies rendering of zero diffs.
func TestRenderBatch_EmptyBatch(t *testing.T) {
	output := RenderBatch([]FileDiff{}, 80)
	if output == "" {
		t.Error("RenderBatch of empty slice should produce output")
	}
}

// TestRenderBatch_MultipleDiffs verifies batch rendering with multiple files.
func TestRenderBatch_Multiple(t *testing.T) {
	diffs := []FileDiff{
		{
			Path:    "file1.go",
			Added:   5,
			Deleted: 0,
		},
		{
			Path:    "file2.go",
			Added:   0,
			Deleted: 3,
		},
		{
			Path:    "file3.go",
			Added:   2,
			Deleted: 1,
		},
	}

	output := RenderBatch(diffs, 80)
	if !strings.Contains(output, "file1.go") {
		t.Error("RenderBatch output should include first file")
	}
	if !strings.Contains(output, "file2.go") {
		t.Error("RenderBatch output should include second file")
	}
	if !strings.Contains(output, "file3.go") {
		t.Error("RenderBatch output should include third file")
	}
}

// TestRenderBatch_VerySmallWidth verifies rendering at minimum width.
func TestRenderBatch_SmallWidth(t *testing.T) {
	diffs := []FileDiff{
		{
			Path:    "test.go",
			Added:   1,
			Deleted: 0,
		},
	}

	// Very small width should not crash
	output := RenderBatch(diffs, 10)
	if output == "" {
		t.Error("RenderBatch with small width should produce output")
	}
}
