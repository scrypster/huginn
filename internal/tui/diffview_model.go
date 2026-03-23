package tui

import (
	"github.com/scrypster/huginn/internal/diffview"
)

// DiffReviewModel manages the state of a multi-file diff review.
type DiffReviewModel struct {
	diffs     []diffview.FileDiff
	decisions []bool // true = approved, false = rejected
	cursor    int
	done      bool
	width     int
}

// NewDiffReviewModel creates a new diff review model.
func NewDiffReviewModel(diffs []diffview.FileDiff, width int) DiffReviewModel {
	// If there are no diffs, mark as done immediately
	done := len(diffs) == 0
	return DiffReviewModel{
		diffs:     diffs,
		decisions: make([]bool, len(diffs)),
		width:     width,
		done:      done,
	}
}

// Done returns true if the review is complete.
func (m DiffReviewModel) Done() bool {
	return m.done
}

// Decisions returns the approval/rejection decisions for each file.
// true = approved, false = rejected.
func (m DiffReviewModel) Decisions() []bool {
	return m.decisions
}

// HandleKey processes keyboard input during review.
// 'A' = approve all, 'R' = reject all
// 'a'/'s' = approve current file and advance, 'r' = reject and advance
// Returns the updated model.
func (m DiffReviewModel) HandleKey(key string) DiffReviewModel {
	if m.done {
		return m
	}

	switch key {
	case "A":
		// Approve all remaining files
		for i := range m.decisions {
			m.decisions[i] = true
		}
		m.done = true
	case "R":
		// Reject all remaining files
		for i := range m.decisions {
			m.decisions[i] = false
		}
		m.done = true
	case "a", "s":
		// Approve current and advance
		m.decisions[m.cursor] = true
		m.cursor++
		if m.cursor >= len(m.diffs) {
			m.done = true
		}
	case "r":
		// Reject current and advance
		m.decisions[m.cursor] = false
		m.cursor++
		if m.cursor >= len(m.diffs) {
			m.done = true
		}
	}

	return m
}

// View renders the current diff with hints.
func (m DiffReviewModel) View() string {
	if m.done || len(m.diffs) == 0 {
		return ""
	}
	return diffview.RenderDiff(m.diffs[m.cursor], m.width)
}

// ViewBatch renders all diffs with batch controls.
func (m DiffReviewModel) ViewBatch() string {
	if len(m.diffs) == 0 {
		return ""
	}
	return diffview.RenderBatch(m.diffs, m.width)
}
