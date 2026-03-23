package patch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hunk represents a single diff hunk.
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []HunkLine
}

// HunkLine is a single line in a hunk with its operation.
type HunkLine struct {
	Op      HunkOp
	Content string
}

// HunkOp is the operation for a hunk line.
type HunkOp byte

const (
	OpContext HunkOp = ' '
	OpAdd     HunkOp = '+'
	OpDelete  HunkOp = '-'
)

// Diff represents a parsed unified diff for a single file.
type Diff struct {
	FilePath     string
	Hunks        []Hunk
	ExpectedHash string // SHA-256 hex of file content at plan time; "" skips hash check
}

// ErrStaleFile is returned when the file changed since plan time.
type ErrStaleFile struct {
	Path     string
	Expected string
	Actual   string
}

func (e ErrStaleFile) Error() string {
	return fmt.Sprintf("stale file %s: expected hash %s, got %s", e.Path, truncHash(e.Expected), truncHash(e.Actual))
}

// truncHash returns the first 8 chars of a hash, or the full string if shorter.
func truncHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

// Store is the interface for invalidating file records after apply.
type Store interface {
	Invalidate(paths []string) error
}

// Parse parses a unified diff string into a slice of Diffs, one per file.
func Parse(unifiedDiff string) ([]Diff, error) {
	lines := strings.Split(unifiedDiff, "\n")
	var diffs []Diff
	var current *Diff
	var currentHunk *Hunk
	var hunkOldRemaining int // tracks old-side lines remaining (context + deletes)
	var hunkNewRemaining int // tracks new-side lines remaining (context + adds)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(line, "--- ") {
			// Start of a new file diff
			if current != nil {
				if currentHunk != nil {
					current.Hunks = append(current.Hunks, *currentHunk)
					currentHunk = nil
				}
				diffs = append(diffs, *current)
			}
			// Next line should be +++
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ") {
				path := extractPath(lines[i+1][4:])
				current = &Diff{FilePath: path}
				i++ // consume the +++ line
			} else {
				current = &Diff{FilePath: extractPath(line[4:])}
			}
			continue
		}

		if strings.HasPrefix(line, "@@ ") && current != nil {
			// Flush previous hunk
			if currentHunk != nil {
				current.Hunks = append(current.Hunks, *currentHunk)
			}
			hunk, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("parse hunk header %q: %w", line, err)
			}
			currentHunk = &hunk
			hunkOldRemaining = hunk.OldLines
			hunkNewRemaining = hunk.NewLines
			continue
		}

		if currentHunk != nil && current != nil && (hunkOldRemaining > 0 || hunkNewRemaining > 0) {
			switch {
			case strings.HasPrefix(line, "+") && hunkNewRemaining > 0:
				currentHunk.Lines = append(currentHunk.Lines, HunkLine{Op: OpAdd, Content: line[1:]})
				hunkNewRemaining--
			case strings.HasPrefix(line, "-") && hunkOldRemaining > 0:
				currentHunk.Lines = append(currentHunk.Lines, HunkLine{Op: OpDelete, Content: line[1:]})
				hunkOldRemaining--
			case strings.HasPrefix(line, " ") && hunkOldRemaining > 0 && hunkNewRemaining > 0:
				currentHunk.Lines = append(currentHunk.Lines, HunkLine{Op: OpContext, Content: line[1:]})
				hunkOldRemaining--
				hunkNewRemaining--
			}
		}
	}

	// Flush last hunk and diff
	if currentHunk != nil && current != nil {
		current.Hunks = append(current.Hunks, *currentHunk)
	}
	if current != nil {
		diffs = append(diffs, *current)
	}

	return diffs, nil
}

// parseHunkHeader parses "@@ -a,b +c,d @@" header and all valid variants.
// Per the unified diff spec, line counts may be omitted when they equal 1.
func parseHunkHeader(line string) (Hunk, error) {
	var h Hunk
	// Format: @@ -OldStart,OldLines +NewStart,NewLines @@
	if _, err := fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &h.OldStart, &h.OldLines, &h.NewStart, &h.NewLines); err == nil {
		return h, nil
	}
	// Mixed format: @@ -OldStart,OldLines +NewStart @@  (new side has 1 line)
	if _, err := fmt.Sscanf(line, "@@ -%d,%d +%d @@", &h.OldStart, &h.OldLines, &h.NewStart); err == nil {
		h.NewLines = 1
		return h, nil
	}
	// Mixed format: @@ -OldStart +NewStart,NewLines @@  (old side has 1 line)
	if _, err := fmt.Sscanf(line, "@@ -%d +%d,%d @@", &h.OldStart, &h.NewStart, &h.NewLines); err == nil {
		h.OldLines = 1
		return h, nil
	}
	// Both sides single line: @@ -OldStart +NewStart @@
	if _, err := fmt.Sscanf(line, "@@ -%d +%d @@", &h.OldStart, &h.NewStart); err == nil {
		h.OldLines = 1
		h.NewLines = 1
		return h, nil
	}
	return h, fmt.Errorf("unrecognized hunk header format: %s", line)
}

// extractPath strips git diff path prefixes (a/, b/) and timestamp suffixes.
func extractPath(s string) string {
	s = strings.TrimSpace(s)
	// Remove git diff prefix
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		s = s[2:]
	}
	// Remove timestamp (e.g., "\t2024-01-01 00:00:00")
	if idx := strings.Index(s, "\t"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// sha256hex computes the hex-encoded SHA-256 of data.
func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate checks that the current on-disk content matches d.ExpectedHash.
// Returns ErrStaleFile if the hash doesn't match.
// Returns an error if the file does not exist but a hash was expected.
func Validate(d Diff, _ Store) error {
	if d.ExpectedHash == "" {
		return nil // no hash check requested
	}
	data, err := os.ReadFile(d.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If an expected hash was provided, the file should exist
			return ErrStaleFile{Path: d.FilePath, Expected: d.ExpectedHash, Actual: "<missing>"}
		}
		return fmt.Errorf("read %s: %w", d.FilePath, err)
	}
	actual := sha256hex(data)
	if actual != d.ExpectedHash {
		return ErrStaleFile{Path: d.FilePath, Expected: d.ExpectedHash, Actual: actual}
	}
	return nil
}

// DryRun applies d to content in memory and returns the result without writing.
func DryRun(d Diff, content []byte) ([]byte, error) {
	lines := splitLines(string(content))
	result, err := applyHunks(lines, d.Hunks)
	if err != nil {
		return nil, err
	}
	return []byte(strings.Join(result, "\n")), nil
}

// Apply validates, applies the diff, writes the result, and invalidates the store record.
func Apply(d Diff, store Store) error {
	// Read current content (single read to avoid TOCTOU race between validate and apply)
	content, err := os.ReadFile(d.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			content = []byte{} // new file
		} else {
			return fmt.Errorf("read %s: %w", d.FilePath, err)
		}
	}

	// Validate hash against the content we just read (not a second read)
	if d.ExpectedHash != "" {
		actual := sha256hex(content)
		if actual != d.ExpectedHash {
			return ErrStaleFile{Path: d.FilePath, Expected: d.ExpectedHash, Actual: actual}
		}
	}

	// Apply hunks
	lines := splitLines(string(content))
	result, err := applyHunks(lines, d.Hunks)
	if err != nil {
		return fmt.Errorf("apply hunks to %s: %w", d.FilePath, err)
	}

	// Write result — ensure parent directory exists for new files
	output := strings.Join(result, "\n")
	if dir := filepath.Dir(d.FilePath); dir != "." && dir != "" {
		if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
			return fmt.Errorf("create directory for %s: %w", d.FilePath, mkErr)
		}
	}
	if err := os.WriteFile(d.FilePath, []byte(output), 0644); err != nil {
		return fmt.Errorf("write %s: %w", d.FilePath, err)
	}

	// Invalidate store record so next index re-reads the file
	if store != nil {
		if err := store.Invalidate([]string{d.FilePath}); err != nil {
			// Non-fatal: log is not available in this package, just continue
			_ = err
		}
	}

	return nil
}

// applyHunks applies a sequence of hunks to a slice of lines.
func applyHunks(lines []string, hunks []Hunk) ([]string, error) {
	result := make([]string, 0, len(lines)+16)
	pos := 0 // 0-indexed position in source lines

	for hi, hunk := range hunks {
		start := hunk.OldStart - 1 // convert to 0-indexed
		if start < 0 {
			start = 0
		}

		// Hunks must be in ascending order; reject overlapping or out-of-order hunks
		if start < pos {
			return nil, fmt.Errorf("hunk %d (OldStart=%d) overlaps with previous hunk (already at line %d)", hi+1, hunk.OldStart, pos+1)
		}

		// Copy unchanged lines before this hunk
		if start > pos {
			if start > len(lines) {
				start = len(lines)
			}
			result = append(result, lines[pos:start]...)
			pos = start
		}

		// Apply hunk lines
		for _, hl := range hunk.Lines {
			switch hl.Op {
			case OpContext:
				if pos >= len(lines) {
					return nil, fmt.Errorf("hunk context line past end of file at line %d (file has %d lines)", pos+1, len(lines))
				}
				result = append(result, lines[pos])
				pos++
			case OpAdd:
				result = append(result, hl.Content)
			case OpDelete:
				if pos >= len(lines) {
					return nil, fmt.Errorf("hunk delete line past end of file at line %d (file has %d lines)", pos+1, len(lines))
				}
				pos++ // skip deleted line
			}
		}
	}

	// Copy remaining lines after last hunk
	if pos < len(lines) {
		result = append(result, lines[pos:]...)
	}

	return result, nil
}

// splitLines splits content into lines, preserving empty trailing line handling.
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	// If content ends with newline, the last element is ""; keep it for join consistency
	return lines
}
