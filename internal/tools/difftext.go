package tools

import (
	"fmt"
	"strings"
)

// maxDiffLines caps the number of unified-diff lines captured per file
// change. Larger diffs are truncated with a note so the tool_result payload
// (and the chip the UI renders it into) stays bounded regardless of how big
// the underlying file edit was.
const maxDiffLines = 200

// FileDiff is a before/after unified diff for a single file, captured at
// tool-execution time by the write-path tools (write_file, edit_file).
type FileDiff struct {
	Path      string // path as passed to the tool (relative to sandbox root)
	Unified   string // unified diff text (no a/ b/ prefixes, just "--- path" / "+++ path")
	Added     int    // lines added
	Removed   int    // lines removed
	Truncated bool   // true if Unified was cut short of the full diff
	IsNew     bool   // true if before was empty (file created)
	IsDelete  bool   // true if after is empty (file emptied/removed via write)
}

// BuildFileDiff computes a unified diff between before and after content for
// path, capped at maxDiffLines lines of hunk output. It always returns a
// result even when before == after (Added == Removed == 0, Unified == "").
func BuildFileDiff(path, before, after string) FileDiff {
	fd := FileDiff{Path: path, IsNew: before == "" && after != "", IsDelete: before != "" && after == ""}
	if before == after {
		return fd
	}

	beforeLines := splitLines(before)
	afterLines := splitLines(after)

	ops := diffLines(beforeLines, afterLines)

	var b strings.Builder
	lineCount := 0
	truncated := false
	fmt.Fprintf(&b, "--- %s\n", path)
	fmt.Fprintf(&b, "+++ %s\n", path)
	lineCount += 2

	for _, op := range ops {
		if truncated {
			break
		}
		switch op.kind {
		case opEqual:
			// Context lines aren't emitted in this compact form; unified
			// diffs are grouped into hunks below via buildHunks instead.
		case opAdd:
			fd.Added++
		case opRemove:
			fd.Removed++
		}
	}

	hunks := buildHunks(ops)
	for _, h := range hunks {
		if truncated {
			break
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldLines, h.newStart, h.newLines)
		if lineCount+1 > maxDiffLines {
			truncated = true
			break
		}
		b.WriteString(header)
		lineCount++
		for _, l := range h.lines {
			if lineCount >= maxDiffLines {
				truncated = true
				break
			}
			b.WriteString(l)
			b.WriteString("\n")
			lineCount++
		}
	}

	fd.Unified = strings.TrimRight(b.String(), "\n")
	fd.Truncated = truncated
	if truncated {
		fd.Unified += fmt.Sprintf("\n… diff truncated (%d+ lines)", maxDiffLines)
	}
	return fd
}

// attachDiffMetadata computes a FileDiff for the given before/after content
// and, if there's an actual change, writes it into metadata under the
// "diff" key as a plain map (JSON-serializable, and readable by the wire
// path without importing this package's types). Callers pass their
// already-built Metadata map; a nil/empty diff (content unchanged) leaves
// metadata untouched.
func attachDiffMetadata(metadata map[string]any, path, before, after string) {
	fd := BuildFileDiff(path, before, after)
	if fd.Unified == "" {
		return
	}
	metadata["diff"] = map[string]any{
		"path":      fd.Path,
		"unified":   fd.Unified,
		"added":     fd.Added,
		"removed":   fd.Removed,
		"truncated": fd.Truncated,
		"is_new":    fd.IsNew,
		"is_delete": fd.IsDelete,
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// strings.Split on a trailing newline produces a trailing "" element;
	// drop it so a file ending in \n doesn't diff as if it had an extra
	// blank line.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type opKind int

const (
	opEqual opKind = iota
	opAdd
	opRemove
)

type lineOp struct {
	kind opKind
	line string
}

// diffLines computes a minimal line-level diff using an LCS dynamic program.
// It's O(n*m) — fine for source files in the sizes these tools touch; very
// large files fall back to a whole-file replace (see the caller-side cap
// applied via truncation of the resulting hunk output).
func diffLines(a, b []string) []lineOp {
	n, m := len(a), len(b)
	// Guard against pathological input: if either side is huge, skip the
	// O(n*m) LCS and treat it as a full remove+add so we never hang.
	const maxDim = 4000
	if n > maxDim || m > maxDim {
		ops := make([]lineOp, 0, n+m)
		for _, l := range a {
			ops = append(ops, lineOp{opRemove, l})
		}
		for _, l := range b {
			ops = append(ops, lineOp{opAdd, l})
		}
		return ops
	}

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	ops := make([]lineOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, lineOp{opEqual, a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, lineOp{opRemove, a[i]})
			i++
		default:
			ops = append(ops, lineOp{opAdd, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, lineOp{opRemove, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, lineOp{opAdd, b[j]})
	}
	return ops
}

type hunk struct {
	oldStart, oldLines int
	newStart, newLines int
	lines              []string
}

// hunkContext is the number of unchanged lines kept around each change,
// matching standard unified-diff convention.
const hunkContext = 3

// buildHunks groups a flat op list into unified-diff hunks with contextual
// surrounding lines, splitting on runs of more than 2*hunkContext equal
// lines between changes.
func buildHunks(ops []lineOp) []hunk {
	var hunks []hunk
	oldLine, newLine := 1, 1

	i := 0
	for i < len(ops) {
		if ops[i].kind == opEqual {
			oldLine++
			newLine++
			i++
			continue
		}
		// Found a change. Start a hunk, backing up into context.
		start := i
		ctxStart := start - hunkContext
		if ctxStart < 0 {
			ctxStart = 0
		}
		oldStart := oldLine - (start - ctxStart)
		newStart := newLine - (start - ctxStart)

		// Advance through changes and equal-runs, ending the hunk when we
		// hit a run of equals longer than 2*hunkContext (or EOF).
		end := start
		for end < len(ops) {
			if ops[end].kind == opEqual {
				// Look ahead: how long is this equal run?
				j := end
				for j < len(ops) && ops[j].kind == opEqual {
					j++
				}
				if j-end > 2*hunkContext || j == len(ops) {
					end += hunkContext
					if end > len(ops) {
						end = len(ops)
					}
					break
				}
				end = j
				continue
			}
			end++
		}

		var lines []string
		oldCount, newCount := 0, 0
		ol, nl := oldStart, newStart
		for k := ctxStart; k < end; k++ {
			switch ops[k].kind {
			case opEqual:
				lines = append(lines, " "+ops[k].line)
				oldCount++
				newCount++
				ol++
				nl++
			case opRemove:
				lines = append(lines, "-"+ops[k].line)
				oldCount++
				ol++
			case opAdd:
				lines = append(lines, "+"+ops[k].line)
				newCount++
				nl++
			}
		}
		hunks = append(hunks, hunk{
			oldStart: oldStart, oldLines: oldCount,
			newStart: newStart, newLines: newCount,
			lines: lines,
		})

		// Advance oldLine/newLine to match consumed ops up to end.
		for k := start; k < end; k++ {
			switch ops[k].kind {
			case opEqual:
				oldLine++
				newLine++
			case opRemove:
				oldLine++
			case opAdd:
				newLine++
			}
		}
		i = end
	}
	return hunks
}
