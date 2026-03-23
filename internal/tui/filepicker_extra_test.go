package tui

import (
	"strings"
	"testing"
)

// ============================================================
// highlightMatches
// ============================================================

func TestHighlightMatches_EmptyMatches_ReturnsOriginal(t *testing.T) {
	result := highlightMatches("main.go", nil, 0)
	if result != "main.go" {
		t.Errorf("expected original string for empty matches, got %q", result)
	}
}

func TestHighlightMatches_EmptyMatchesSlice_ReturnsOriginal(t *testing.T) {
	result := highlightMatches("app.go", []int{}, 0)
	if result != "app.go" {
		t.Errorf("expected original string for empty match slice, got %q", result)
	}
}

func TestHighlightMatches_AllCharsMatched_NonEmpty(t *testing.T) {
	// All positions matched: 0,1,2
	result := highlightMatches("foo", []int{0, 1, 2}, 0)
	if result == "" {
		t.Error("expected non-empty result for all-matched string")
	}
	// The result should still contain the characters (wrapped in style)
	if !strings.Contains(result, "f") || !strings.Contains(result, "o") {
		t.Errorf("expected original chars in highlighted result, got %q", result)
	}
}

func TestHighlightMatches_NoCharsMatched_ReturnsPlain(t *testing.T) {
	// This won't happen in practice (matchSet empty = no match), but
	// since matches is nil empty check, the nil branch returns original.
	result := highlightMatches("main.go", nil, 0)
	if result != "main.go" {
		t.Errorf("expected plain text for nil matches, got %q", result)
	}
}

func TestHighlightMatches_PartialMatch_NonEmpty(t *testing.T) {
	// Only position 0 matched
	result := highlightMatches("main.go", []int{0}, 0)
	if result == "" {
		t.Error("expected non-empty result for partial match")
	}
}

func TestHighlightMatches_WithOffset_AdjustsPositions(t *testing.T) {
	// Offset of 2 means match position 2 refers to text position 0
	result := highlightMatches("foo", []int{2}, 2)
	if result == "" {
		t.Error("expected non-empty result with offset")
	}
}

func TestHighlightMatches_MiddleCharMatched(t *testing.T) {
	// Only position 2 (the 'i') matched in "main"
	result := highlightMatches("main", []int{2}, 0)
	if result == "" {
		t.Error("expected non-empty for middle char match")
	}
	// Result should still contain all original characters
	if !strings.Contains(result, "m") {
		t.Error("expected 'm' to appear in result")
	}
}

// ============================================================
// renderRow
// ============================================================

func TestRenderRow_ActiveDirectory(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "internal", isDir: true},
	}
	result := fp.renderRow(entry, true, 60)
	if result == "" {
		t.Error("renderRow should return non-empty for active directory")
	}
	if !strings.Contains(result, "internal") {
		t.Errorf("expected directory name in row, got %q", result)
	}
}

func TestRenderRow_InactiveDirectory(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "cmd", isDir: true},
	}
	result := fp.renderRow(entry, false, 60)
	if result == "" {
		t.Error("renderRow should return non-empty for inactive directory")
	}
	if !strings.Contains(result, "cmd") {
		t.Errorf("expected 'cmd' in row, got %q", result)
	}
}

func TestRenderRow_ActiveFile(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "main.go", isDir: false, size: 1024},
	}
	result := fp.renderRow(entry, true, 60)
	if result == "" {
		t.Error("renderRow should return non-empty for active file")
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected 'main.go' in row, got %q", result)
	}
}

func TestRenderRow_InactiveFile(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "go.mod", isDir: false, size: 200},
	}
	result := fp.renderRow(entry, false, 60)
	if result == "" {
		t.Error("renderRow should return non-empty for inactive file")
	}
	if !strings.Contains(result, "go.mod") {
		t.Errorf("expected 'go.mod' in row, got %q", result)
	}
}

func TestRenderRow_SelectedFile(t *testing.T) {
	fp := newFilePickerModel()
	fp.selected["app.go"] = true
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "app.go", isDir: false},
	}
	result := fp.renderRow(entry, false, 60)
	// Selected file should show check mark indicator
	if !strings.Contains(result, "✓") {
		t.Errorf("expected '✓' indicator for selected file, got %q", result)
	}
}

func TestRenderRow_FileWithMatches(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "main.go", isDir: false},
		matches:   []int{0, 1},
	}
	result := fp.renderRow(entry, false, 60)
	if result == "" {
		t.Error("renderRow with matches should return non-empty")
	}
}

func TestRenderRow_ZeroSizeFileNoSizeText(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "empty.go", isDir: false, size: 0},
	}
	result := fp.renderRow(entry, false, 60)
	// Size text should not appear for zero-size files
	if strings.Contains(result, " B") || strings.Contains(result, " KB") {
		t.Errorf("expected no size text for zero-size file, got %q", result)
	}
}

func TestRenderRow_DirectoryShowsArrow(t *testing.T) {
	fp := newFilePickerModel()
	entry := scoredEntry{
		fileEntry: fileEntry{rel: "src", isDir: true},
	}
	result := fp.renderRow(entry, false, 60)
	if !strings.Contains(result, "→") {
		t.Errorf("expected '→' arrow for directory row, got %q", result)
	}
}
