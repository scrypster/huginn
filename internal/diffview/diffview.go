package diffview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FileDiff represents the computed diff for a single file.
type FileDiff struct {
	Path        string
	OldContent  []byte
	NewContent  []byte
	Added       int
	Deleted     int
	UnifiedDiff string
	Hunks       []DiffHunk
}

// DiffHunk represents a hunk (change block) in the diff.
type DiffHunk struct {
	Header string
	Lines  []DiffLine
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Op      byte   // '+', '-', ' '
	Content string
}

// Color styles for diff rendering
var (
	styleAdd  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950"))
	styleDel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149"))
	styleCtx  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681"))
	stylePath = lipgloss.NewStyle().Foreground(lipgloss.Color("#BB86FC")).Bold(true)
)

// ComputeDiff computes the unified diff between old and new content.
// path is the file path (used in the diff header).
// oldContent may be nil (for new files).
// newContent is the target content.
func ComputeDiff(path string, oldContent, newContent []byte) FileDiff {
	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))
	hunks, added, deleted := computeLCS(oldLines, newLines)
	unified := buildUnified(path, hunks)
	return FileDiff{
		Path:        path,
		OldContent:  oldContent,
		NewContent:  newContent,
		Added:       added,
		Deleted:     deleted,
		UnifiedDiff: unified,
		Hunks:       hunks,
	}
}

// RenderDiff renders a single file diff with colored output.
func RenderDiff(d FileDiff, width int) string {
	var sb strings.Builder

	// Header with file name and stats
	sb.WriteString(stylePath.Render(d.Path))
	sb.WriteString("  ")
	sb.WriteString(styleAdd.Render(fmt.Sprintf("+%d", d.Added)))
	sb.WriteString(" ")
	sb.WriteString(styleDel.Render(fmt.Sprintf("-%d", d.Deleted)))
	sb.WriteString("\n")

	// Hunks
	for _, hunk := range d.Hunks {
		sb.WriteString(styleCtx.Render(hunk.Header))
		sb.WriteString("\n")
		for _, line := range hunk.Lines {
			switch line.Op {
			case '+':
				sb.WriteString(styleAdd.Render("+ " + line.Content))
			case '-':
				sb.WriteString(styleDel.Render("- " + line.Content))
			default:
				sb.WriteString(styleCtx.Render("  " + line.Content))
			}
			sb.WriteString("\n")
		}
	}

	// Hint for single-file review
	hint := styleAdd.Render("[a]ccept") + "  " + styleCtx.Render("[s]kip") + "  " + styleDel.Render("[r]eject")
	sb.WriteString(hint)

	return sb.String()
}

// RenderBatch renders multiple file diffs with batch control options.
func RenderBatch(diffs []FileDiff, width int) string {
	if width <= 0 {
		width = 80
	}
	var sb strings.Builder
	div := styleCtx.Render(strings.Repeat("─", width))

	for i, d := range diffs {
		sb.WriteString(RenderDiff(d, width))
		if i < len(diffs)-1 {
			sb.WriteString("\n" + div + "\n")
		}
	}

	// Batch controls
	sb.WriteString("\n" + div + "\n")
	sb.WriteString(styleAdd.Render("[A]ccept all") + "  " + styleDel.Render("[R]eject all"))

	return sb.String()
}

// splitLines splits a string by newlines, removing trailing empty line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// computeLCS computes longest-common-subsequence based diff.
// Returns hunks, added count, deleted count.
func computeLCS(old, new []string) ([]DiffHunk, int, int) {
	m, n := len(old), len(new)

	// Build DP table for LCS
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if old[i] == new[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] > dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// Backtrack to build edit script
	type edit struct {
		op      byte
		content string
	}
	var edits []edit

	i, j := 0, 0
	for i < m || j < n {
		if i < m && j < n && old[i] == new[j] {
			edits = append(edits, edit{' ', old[i]})
			i++
			j++
		} else if j < n && (i >= m || dp[i][j+1] >= dp[i+1][j]) {
			edits = append(edits, edit{'+', new[j]})
			j++
		} else {
			edits = append(edits, edit{'-', old[i]})
			i++
		}
	}

	// Count changes
	added, deleted := 0, 0
	for _, e := range edits {
		if e.op == '+' {
			added++
		} else if e.op == '-' {
			deleted++
		}
	}

	// Group edits into hunks with context
	const ctxLines = 3
	type span struct {
		start, end int
	}
	var changed []span

	for idx, e := range edits {
		if e.op != ' ' {
			if len(changed) == 0 || changed[len(changed)-1].end < idx-ctxLines {
				s := idx - ctxLines
				if s < 0 {
					s = 0
				}
				changed = append(changed, span{s, idx + 1})
			} else {
				changed[len(changed)-1].end = idx + 1
			}
		}
	}

	var hunks []DiffHunk
	for _, s := range changed {
		end := s.end + ctxLines
		if end > len(edits) {
			end = len(edits)
		}
		hunk := DiffHunk{
			Header: fmt.Sprintf("@@ -%d +%d @@", s.start+1, s.start+1),
		}
		for _, e := range edits[s.start:end] {
			hunk.Lines = append(hunk.Lines, DiffLine{Op: e.op, Content: e.content})
		}
		hunks = append(hunks, hunk)
	}

	return hunks, added, deleted
}

// buildUnified builds the unified diff string from hunks.
func buildUnified(path string, hunks []DiffHunk) string {
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path))

	for _, h := range hunks {
		sb.WriteString(h.Header + "\n")
		for _, l := range h.Lines {
			sb.WriteString(string(l.Op) + l.Content + "\n")
		}
	}

	return sb.String()
}
