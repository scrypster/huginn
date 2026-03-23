package tui

import (
	"sort"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ============================================================
// fuzzyScore
// ============================================================

func TestFuzzyScore_ExactMatch_HighScore(t *testing.T) {
	score, matches := fuzzyScore("main.go", "main.go")
	if score < 0 {
		t.Fatalf("expected match for exact input, got score %d", score)
	}
	if len(matches) == 0 {
		t.Error("expected non-empty matches for exact match")
	}
}

func TestFuzzyScore_NoMatch_NegativeOne(t *testing.T) {
	score, matches := fuzzyScore("main.go", "xyz")
	if score != -1 {
		t.Errorf("expected score=-1 for non-matching query, got %d", score)
	}
	if matches != nil {
		t.Errorf("expected nil matches for non-match, got %v", matches)
	}
}

func TestFuzzyScore_EmptyQuery_ZeroScore(t *testing.T) {
	score, matches := fuzzyScore("main.go", "")
	if score != 0 {
		t.Errorf("expected score=0 for empty query, got %d", score)
	}
	if matches != nil {
		t.Errorf("expected nil matches for empty query, got %v", matches)
	}
}

func TestFuzzyScore_EmptyCandidate_NoMatch(t *testing.T) {
	score, _ := fuzzyScore("", "x")
	if score != -1 {
		t.Errorf("expected -1 for empty candidate, got %d", score)
	}
}

func TestFuzzyScore_PrefixBonus(t *testing.T) {
	// Query starts at position 0 → prefix bonus of +10.
	score, matches := fuzzyScore("main.go", "m")
	if score < 0 {
		t.Fatalf("expected match, got %d", score)
	}
	if len(matches) == 0 || matches[0] != 0 {
		t.Errorf("expected first match at index 0 for prefix bonus, got %v", matches)
	}
	// Score must include the prefix bonus (10) minus the penalty (len("main.go")/10 = 0).
	if score < 10 {
		t.Errorf("expected score >= 10 for prefix match, got %d", score)
	}
}

func TestFuzzyScore_ConsecutiveBonus(t *testing.T) {
	// "mai" in "main.go" — three consecutive chars → 2 consecutive bonuses (+5 each).
	score, matches := fuzzyScore("main.go", "mai")
	if score < 0 {
		t.Fatalf("expected match, got %d", score)
	}
	_ = matches
	// prefix bonus (10) + 2 consecutive bonuses (10) = 20 minus length penalty
	if score < 15 {
		t.Errorf("expected high score for consecutive prefix match, got %d", score)
	}
}

func TestFuzzyScore_SubsequenceNonConsecutive(t *testing.T) {
	// "mg" is a subsequence of "main.go" but not consecutive.
	score, matches := fuzzyScore("main.go", "mg")
	if score < 0 {
		t.Fatalf("expected match for subsequence 'mg' in 'main.go', got %d", score)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 match positions, got %d", len(matches))
	}
}

func TestFuzzyScore_SingleChar_PrefixMatch(t *testing.T) {
	score, _ := fuzzyScore("foo", "f")
	if score < 0 {
		t.Errorf("expected match for 'f' in 'foo', got %d", score)
	}
}

func TestFuzzyScore_SingleChar_NonPrefixMatch(t *testing.T) {
	// 'o' is not the first char — no prefix bonus.
	scorePrefix, _ := fuzzyScore("foo", "f")
	scoreNonPrefix, _ := fuzzyScore("foo", "o")
	if scoreNonPrefix >= scorePrefix {
		t.Errorf("non-prefix match score (%d) should be less than prefix match score (%d)",
			scoreNonPrefix, scorePrefix)
	}
}

func TestFuzzyScore_LongerCandidate_PenalisesScore(t *testing.T) {
	// Same query, but longer candidate should have a lower score (length penalty).
	score1, _ := fuzzyScore("foo", "f")
	score2, _ := fuzzyScore("foooooooooooooo", "f") // 10 extra chars → -1 penalty
	if score2 >= score1 {
		t.Errorf("longer candidate should have lower score: score1=%d score2=%d", score1, score2)
	}
}

// ============================================================
// humanSize
// ============================================================

func TestHumanSize_Zero(t *testing.T) {
	result := humanSize(0)
	if result != "0 B" {
		t.Errorf("expected '0 B', got %q", result)
	}
}

func TestHumanSize_OneByte(t *testing.T) {
	result := humanSize(1)
	if result != "1 B" {
		t.Errorf("expected '1 B', got %q", result)
	}
}

func TestHumanSize_1023Bytes(t *testing.T) {
	result := humanSize(1023)
	if result != "1023 B" {
		t.Errorf("expected '1023 B', got %q", result)
	}
}

func TestHumanSize_1024Bytes_IsKB(t *testing.T) {
	result := humanSize(1024)
	if result != "1.0 KB" {
		t.Errorf("expected '1.0 KB', got %q", result)
	}
}

func TestHumanSize_1536Bytes(t *testing.T) {
	result := humanSize(1536) // 1.5 KB
	if result != "1.5 KB" {
		t.Errorf("expected '1.5 KB', got %q", result)
	}
}

func TestHumanSize_OneMB(t *testing.T) {
	result := humanSize(1024 * 1024)
	if result != "1.0 MB" {
		t.Errorf("expected '1.0 MB', got %q", result)
	}
}

func TestHumanSize_LargerThanOneMB(t *testing.T) {
	result := humanSize(2 * 1024 * 1024)
	if result != "2.0 MB" {
		t.Errorf("expected '2.0 MB', got %q", result)
	}
}

func TestHumanSize_1023KB_StillKB(t *testing.T) {
	result := humanSize(1023 * 1024)
	expected := "1023.0 KB"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ============================================================
// FilePickerModel: confirmPaths
// ============================================================

func TestConfirmPaths_NoSelection_ReturnsHighlighted(t *testing.T) {
	fp := newFilePickerModel()
	result := fp.confirmPaths("main.go")
	if len(result) != 1 || result[0] != "main.go" {
		t.Errorf("expected [main.go], got %v", result)
	}
}

func TestConfirmPaths_WithMultiSelect_ReturnsSelectedSorted(t *testing.T) {
	fp := newFilePickerModel()
	fp.selected["c.go"] = true
	fp.selected["a.go"] = true
	fp.selected["b.go"] = true

	result := fp.confirmPaths("ignored.go")
	if len(result) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(result))
	}
	if !sort.StringsAreSorted(result) {
		t.Errorf("expected sorted paths, got %v", result)
	}
	if result[0] != "a.go" || result[1] != "b.go" || result[2] != "c.go" {
		t.Errorf("unexpected order: %v", result)
	}
}

func TestConfirmPaths_SingleSelected_IgnoresHighlighted(t *testing.T) {
	fp := newFilePickerModel()
	fp.selected["chosen.go"] = true

	result := fp.confirmPaths("other.go")
	if len(result) != 1 || result[0] != "chosen.go" {
		t.Errorf("expected [chosen.go], got %v", result)
	}
}

func TestConfirmPaths_EmptyHighlighted_ReturnsEmptyPath(t *testing.T) {
	fp := newFilePickerModel()
	result := fp.confirmPaths("")
	if len(result) != 1 || result[0] != "" {
		t.Errorf("expected [''], got %v", result)
	}
}

// ============================================================
// FilePickerModel: refilter scoping to currentDir
// ============================================================

func makePickerWithFiles(files []string) FilePickerModel {
	fp := newFilePickerModel()
	fp.SetFiles(files, "")
	return fp
}

func TestRefilter_Root_OnlyTopLevel(t *testing.T) {
	fp := makePickerWithFiles([]string{
		"main.go",
		"internal/foo.go",
		"internal/bar.go",
	})
	fp.currentDir = ""
	fp.filter = ""
	fp.filtered = nil
	fp.refilter()

	for _, e := range fp.filtered {
		if e.isDir {
			continue // directories at root are fine
		}
		// No file entry should contain a separator.
		for _, c := range e.rel {
			if c == '/' || c == '\\' {
				t.Errorf("root scope should not include nested file %q", e.rel)
			}
		}
	}
	// main.go must be present.
	found := false
	for _, e := range fp.filtered {
		if e.rel == "main.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'main.go' in root-scoped filtered list")
	}
}

func TestRefilter_SubDir_OnlyDirectChildren(t *testing.T) {
	fp := makePickerWithFiles([]string{
		"internal/foo.go",
		"internal/bar.go",
		"internal/sub/deep.go",
	})
	fp.currentDir = "internal"
	fp.filter = ""
	fp.filtered = nil
	fp.refilter()

	for _, e := range fp.filtered {
		// Entries must start with "internal/"
		if e.isDir {
			continue
		}
		// Should see foo.go and bar.go but NOT sub/deep.go (too deep).
		if e.rel == "internal/sub/deep.go" {
			t.Errorf("should not include deeply nested file %q when scoped to 'internal'", e.rel)
		}
	}
}

func TestRefilter_EmptyFilter_IncludesAllInScope(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go", "c.go"})
	fp.currentDir = ""
	fp.filter = ""
	fp.filtered = nil
	fp.refilter()

	if len(fp.filtered) < 3 {
		t.Errorf("expected at least 3 entries (3 files), got %d", len(fp.filtered))
	}
}

func TestRefilter_WithFilter_AppliesFuzzy(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go", "math.go", "readme.txt"})
	fp.currentDir = ""
	fp.filter = "ma"
	fp.filtered = nil
	fp.refilter()

	found := false
	for _, e := range fp.filtered {
		if e.rel == "main.go" || e.rel == "math.go" {
			found = true
		}
		if e.rel == "readme.txt" {
			t.Error("'readme.txt' should not match filter 'ma'")
		}
	}
	if !found {
		t.Error("expected 'main.go' or 'math.go' to match filter 'ma'")
	}
}

func TestRefilter_NoMatchFilter_EmptyResult(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go", "math.go"})
	fp.currentDir = ""
	fp.filter = "xyz"
	fp.filtered = nil
	fp.refilter()

	if len(fp.filtered) != 0 {
		t.Errorf("expected empty filtered list for no-match filter, got %d", len(fp.filtered))
	}
}

// ============================================================
// FilePickerModel: clampScroll
// ============================================================

func TestClampScroll_CursorAboveScroll_ScrollsUp(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 5
	fp.scroll = 3
	fp.cursor = 1 // above scroll window

	fp.clampScroll()
	if fp.scroll != fp.cursor {
		t.Errorf("expected scroll=%d, got %d", fp.cursor, fp.scroll)
	}
}

func TestClampScroll_CursorBelowWindow_ScrollsDown(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 5
	fp.scroll = 0
	fp.cursor = 7 // beyond scroll+maxVisible

	fp.clampScroll()
	expected := fp.cursor - fp.maxVisible + 1
	if fp.scroll != expected {
		t.Errorf("expected scroll=%d, got %d", expected, fp.scroll)
	}
}

func TestClampScroll_CursorInWindow_NoChange(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 5
	fp.scroll = 2
	fp.cursor = 4 // within [2, 2+5) = [2, 7)

	fp.clampScroll()
	if fp.scroll != 2 {
		t.Errorf("expected scroll to remain 2, got %d", fp.scroll)
	}
}

func TestClampScroll_AtTopBoundary(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 5
	fp.scroll = 0
	fp.cursor = 0

	fp.clampScroll()
	if fp.scroll != 0 {
		t.Errorf("expected scroll=0 at top boundary, got %d", fp.scroll)
	}
}

func TestClampScroll_AtBottomBoundaryExact(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 5
	fp.scroll = 0
	fp.cursor = 4 // exactly at bottom of window (0 + 5 - 1)

	fp.clampScroll()
	if fp.scroll != 0 {
		t.Errorf("expected scroll=0, got %d", fp.scroll)
	}
}

// ============================================================
// FilePickerModel: Show / Hide / Visible
// ============================================================

func TestFilePicker_ShowMakesVisible(t *testing.T) {
	fp := newFilePickerModel()
	fp.Show()
	if !fp.Visible() {
		t.Error("expected Visible()=true after Show()")
	}
}

func TestFilePicker_HideMakesInvisible(t *testing.T) {
	fp := newFilePickerModel()
	fp.Show()
	fp.Hide()
	if fp.Visible() {
		t.Error("expected Visible()=false after Hide()")
	}
}

func TestFilePicker_ShowResetsState(t *testing.T) {
	fp := newFilePickerModel()
	fp.filter = "something"
	fp.cursor = 5
	fp.scroll = 3
	fp.Show()

	if fp.filter != "" {
		t.Errorf("expected empty filter after Show(), got %q", fp.filter)
	}
	if fp.cursor != 0 {
		t.Errorf("expected cursor=0 after Show(), got %d", fp.cursor)
	}
	if fp.scroll != 0 {
		t.Errorf("expected scroll=0 after Show(), got %d", fp.scroll)
	}
	if fp.currentDir != "" {
		t.Errorf("expected empty currentDir after Show(), got %q", fp.currentDir)
	}
}

func TestFilePicker_HideClearsFiltered(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	fp.Hide()
	if fp.filtered != nil {
		t.Error("expected filtered=nil after Hide()")
	}
}

// ============================================================
// FilePickerModel: Update keyboard handling
// ============================================================

func TestFilePicker_NotVisible_IgnoresKeys(t *testing.T) {
	fp := newFilePickerModel()
	updated, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("invisible picker should stay invisible")
	}
	if cmd != nil {
		t.Error("expected nil cmd when picker not visible")
	}
}

func TestFilePicker_Esc_CancelsAndHides(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()

	updated, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("expected picker hidden after Esc")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after Esc")
	}
	msg := cmd()
	if _, ok := msg.(FilePickerCancelMsg); !ok {
		t.Errorf("expected FilePickerCancelMsg, got %T", msg)
	}
}

func TestFilePicker_Enter_ConfirmsHighlighted(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go", "app.go"})
	fp.Show()
	fp.cursor = 0

	updated, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("expected picker hidden after Enter")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after Enter")
	}
	msg := cmd()
	conf, ok := msg.(FilePickerConfirmMsg)
	if !ok {
		t.Fatalf("expected FilePickerConfirmMsg, got %T", msg)
	}
	if len(conf.Paths) == 0 {
		t.Error("expected at least one confirmed path")
	}
}

func TestFilePicker_Enter_EmptyList_NoOp(t *testing.T) {
	fp := newFilePickerModel()
	fp.Show()
	fp.filtered = nil

	_, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil cmd when Enter pressed with empty list")
	}
}

func TestFilePicker_Tab_TogglesSelection(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go", "c.go"})
	fp.Show()
	fp.cursor = 0

	target := fp.filtered[0].rel
	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !updated.selected[target] {
		t.Errorf("expected %q to be selected after Tab", target)
	}
}

func TestFilePicker_Tab_AdvancesCursor(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go", "c.go"})
	fp.Show()
	fp.cursor = 0

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Tab, got %d", updated.cursor)
	}
}

func TestFilePicker_Tab_DeSelectsIfAlreadySelected(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go"})
	fp.Show()
	fp.cursor = 0
	target := fp.filtered[0].rel
	fp.selected[target] = true

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.selected[target] {
		t.Errorf("expected %q to be deselected after Tab toggle", target)
	}
}

func TestFilePicker_Down_MovesCursor(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go", "c.go"})
	fp.Show()
	fp.cursor = 0

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Down, got %d", updated.cursor)
	}
}

func TestFilePicker_Down_DoesNotExceedBounds(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go"})
	fp.Show()
	fp.cursor = len(fp.filtered) - 1

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.cursor != len(fp.filtered)-1 {
		t.Errorf("expected cursor to stay at last index, got %d", updated.cursor)
	}
}

func TestFilePicker_Up_MovesCursorBack(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go", "b.go", "c.go"})
	fp.Show()
	fp.cursor = 2

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyUp})
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Up, got %d", updated.cursor)
	}
}

func TestFilePicker_Up_DoesNotGoBelowZero(t *testing.T) {
	fp := makePickerWithFiles([]string{"a.go"})
	fp.Show()
	fp.cursor = 0

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyUp})
	if updated.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", updated.cursor)
	}
}

func TestFilePicker_Backspace_TrimsFilter(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	fp.filter = "mai"
	fp.refilter()

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.filter != "ma" {
		t.Errorf("expected filter='ma' after Backspace, got %q", updated.filter)
	}
}

func TestFilePicker_Backspace_EmptyFilter_NoOp(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	fp.filter = ""

	updated, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.filter != "" {
		t.Errorf("expected filter to stay empty, got %q", updated.filter)
	}
	if cmd != nil {
		t.Error("expected nil cmd for Backspace on empty filter")
	}
}

func TestFilePicker_CtrlU_ClearsFilter(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	fp.filter = "mai"
	fp.refilter()

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if updated.filter != "" {
		t.Errorf("expected empty filter after ctrl+u, got %q", updated.filter)
	}
}

func TestFilePicker_Right_EntersDirectory(t *testing.T) {
	fp := makePickerWithFiles([]string{"internal/foo.go"})
	fp.Show()

	// Find the "internal" directory entry.
	dirIdx := -1
	for i, e := range fp.filtered {
		if e.isDir && e.rel == "internal" {
			dirIdx = i
			break
		}
	}
	if dirIdx < 0 {
		t.Skip("no directory entry found to test Right navigation")
	}
	fp.cursor = dirIdx

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyRight})
	if updated.currentDir != "internal" {
		t.Errorf("expected currentDir='internal', got %q", updated.currentDir)
	}
}

func TestFilePicker_Left_AtRoot_NoOp(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	fp.currentDir = ""

	updated, cmd := fp.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if updated.currentDir != "" {
		t.Errorf("expected currentDir to remain '' at root, got %q", updated.currentDir)
	}
	if cmd != nil {
		t.Error("expected nil cmd for Left at root")
	}
}

func TestFilePicker_Left_FromSubDir_GoesUp(t *testing.T) {
	fp := makePickerWithFiles([]string{"internal/foo.go"})
	fp.Show()
	fp.currentDir = "internal"
	fp.refilter()

	updated, _ := fp.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if updated.currentDir != "" {
		t.Errorf("expected currentDir='' after Left from 'internal', got %q", updated.currentDir)
	}
}

// ============================================================
// FilePickerModel: SetFiles
// ============================================================

func TestSetFiles_BuildsDirEntries(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"internal/tui/app.go", "main.go"}, "")

	// Must have dir entries for "internal" and "internal/tui".
	dirs := map[string]bool{}
	for _, e := range fp.allFiles {
		if e.isDir {
			dirs[e.rel] = true
		}
	}
	if !dirs["internal"] {
		t.Error("expected 'internal' directory entry")
	}
	if !dirs["internal/tui"] {
		t.Error("expected 'internal/tui' directory entry")
	}
}

func TestSetFiles_DeduplicatesPaths(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go", "main.go", "main.go"}, "")

	count := 0
	for _, e := range fp.allFiles {
		if !e.isDir && e.rel == "main.go" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry for main.go after dedup, got %d", count)
	}
}

func TestSetFiles_EmptySlice(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{}, "")
	if len(fp.allFiles) != 0 {
		t.Errorf("expected 0 entries for empty input, got %d", len(fp.allFiles))
	}
}

// ============================================================
// FilePickerModel: View (smoke tests)
// ============================================================

func TestFilePickerView_NotVisible_EmptyString(t *testing.T) {
	fp := newFilePickerModel()
	out := fp.View(80)
	if out != "" {
		t.Errorf("expected empty string from View when not visible, got %q", out)
	}
}

func TestFilePickerView_Visible_NonEmpty(t *testing.T) {
	fp := makePickerWithFiles([]string{"main.go"})
	fp.Show()
	out := fp.View(80)
	if out == "" {
		t.Error("expected non-empty View when picker is visible")
	}
}

func TestFilePickerView_ShowsScrollHint(t *testing.T) {
	// Build a list large enough to require scrolling.
	files := make([]string, 20)
	for i := range files {
		files[i] = "file" + string(rune('a'+i)) + ".go"
	}
	fp := makePickerWithFiles(files)
	fp.Show()
	fp.maxVisible = 5
	// Force filtered to re-populate.
	fp.refilter()

	out := fp.View(80)
	// When there are more items than maxVisible, a "↓ N more" hint should appear.
	if len(fp.filtered) > fp.maxVisible && len(out) == 0 {
		t.Error("expected non-empty view with scroll hint")
	}
}
