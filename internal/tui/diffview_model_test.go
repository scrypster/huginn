package tui_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/diffview"
	"github.com/scrypster/huginn/internal/tui"
)

// newTestDiff creates a test FileDiff for testing.
func newTestDiff(path string, added, deleted int) diffview.FileDiff {
	return diffview.FileDiff{
		Path:    path,
		Added:   added,
		Deleted: deleted,
		Hunks: []diffview.DiffHunk{
			{
				Header: "@@ 1,1 @@",
				Lines: []diffview.DiffLine{
					{Op: '+', Content: "added line"},
				},
			},
		},
	}
}

func TestDiffReviewModel_SingleFile_Approve(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("test.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	if m.Done() {
		t.Error("expected not done initially")
	}

	m = m.HandleKey("a")
	if !m.Done() {
		t.Error("expected done after approve")
	}

	decisions := m.Decisions()
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}
	if !decisions[0] {
		t.Error("expected decision[0] = true (approved)")
	}
}

func TestDiffReviewModel_SingleFile_Reject(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("test.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	m = m.HandleKey("r")
	if !m.Done() {
		t.Error("expected done after reject")
	}

	decisions := m.Decisions()
	if len(decisions) != 1 {
		t.Errorf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0] {
		t.Error("expected decision[0] = false (rejected)")
	}
}

func TestDiffReviewModel_MultipleFiles_ApproveAll(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
		newTestDiff("b.go", 1, 0),
		newTestDiff("c.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	m = m.HandleKey("A")
	if !m.Done() {
		t.Error("expected done after approve all")
	}

	decisions := m.Decisions()
	if len(decisions) != 3 {
		t.Errorf("expected 3 decisions, got %d", len(decisions))
	}
	for i, d := range decisions {
		if !d {
			t.Errorf("expected decision[%d] = true, got false", i)
		}
	}
}

func TestDiffReviewModel_MultipleFiles_RejectAll(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
		newTestDiff("b.go", 1, 0),
		newTestDiff("c.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	m = m.HandleKey("R")
	if !m.Done() {
		t.Error("expected done after reject all")
	}

	decisions := m.Decisions()
	for i, d := range decisions {
		if d {
			t.Errorf("expected decision[%d] = false, got true", i)
		}
	}
}

func TestDiffReviewModel_MultipleFiles_Sequential(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
		newTestDiff("b.go", 1, 0),
		newTestDiff("c.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	// Approve a.go
	m = m.HandleKey("a")
	if m.Done() {
		t.Error("should not be done after first approval")
	}

	// Reject b.go
	m = m.HandleKey("r")
	if m.Done() {
		t.Error("should not be done after second rejection")
	}

	// Approve c.go
	m = m.HandleKey("a")
	if !m.Done() {
		t.Error("should be done after final approval")
	}

	decisions := m.Decisions()
	expected := []bool{true, false, true}
	for i, e := range expected {
		if decisions[i] != e {
			t.Errorf("decision[%d]: expected %v, got %v", i, e, decisions[i])
		}
	}
}

func TestDiffReviewModel_IgnoredKeysWhenDone(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	m = m.HandleKey("a")
	if !m.Done() {
		t.Error("should be done")
	}

	// Try to change the decision after done
	oldDecisions := append([]bool(nil), m.Decisions()...)
	m = m.HandleKey("r")

	if !m.Done() {
		t.Error("should still be done")
	}
	if m.Decisions()[0] != oldDecisions[0] {
		t.Error("decision should not change after done")
	}
}

func TestDiffReviewModel_View_EmptyWhenDone(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	// View before done should have content
	view := m.View()
	if view == "" {
		t.Error("expected non-empty view before done")
	}

	m = m.HandleKey("a")
	if !m.Done() {
		t.Fatal("should be done")
	}

	// View after done should be empty
	view = m.View()
	if view != "" {
		t.Error("expected empty view when done")
	}
}

func TestDiffReviewModel_ViewBatch_ShowsAllFiles(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
		newTestDiff("b.go", 2, 1),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	view := m.ViewBatch()
	if !strings.Contains(view, "a.go") {
		t.Error("expected a.go in batch view")
	}
	if !strings.Contains(view, "b.go") {
		t.Error("expected b.go in batch view")
	}
	if !strings.Contains(view, "[A]ccept all") {
		t.Error("expected batch controls in batch view")
	}
}

func TestDiffReviewModel_SkipKey(t *testing.T) {
	diffs := []diffview.FileDiff{
		newTestDiff("a.go", 1, 0),
		newTestDiff("b.go", 1, 0),
	}
	m := tui.NewDiffReviewModel(diffs, 80)

	// 's' should skip (approve) and advance
	m = m.HandleKey("s")
	if m.Done() {
		t.Error("should not be done after skip")
	}

	decisions := m.Decisions()
	if !decisions[0] {
		t.Error("expected skip to approve")
	}

	m = m.HandleKey("r")
	if !m.Done() {
		t.Error("should be done after final rejection")
	}

	if !decisions[0] || decisions[1] {
		t.Error("decisions don't match expected pattern")
	}
}

func TestDiffReviewModel_EmptyDiffs(t *testing.T) {
	diffs := []diffview.FileDiff{}
	m := tui.NewDiffReviewModel(diffs, 80)

	if !m.Done() {
		t.Error("expected done with empty diffs")
	}

	if len(m.Decisions()) != 0 {
		t.Error("expected no decisions for empty diffs")
	}

	if m.View() != "" {
		t.Error("expected empty view with no diffs")
	}
}
