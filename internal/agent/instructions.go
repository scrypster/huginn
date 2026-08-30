package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxProjectInstructionsBytes caps how much of a single .huginn.md (or
// .huginn/instructions.md) file is loaded into context. Matches the notepad
// 32KB precedent (see maxNotepadsChars in context.go) — an oversized project
// instructions file is truncated with a visible marker rather than silently
// blowing the context budget.
const maxProjectInstructionsBytes = 32 * 1024

// LoadProjectInstructions walks up from root toward the filesystem root,
// stopping at the git root (directory containing ".git"), looking for:
//   - .huginn.md
//   - .huginn/instructions.md
//
// The first file found wins. Returns "" if none found.
// root must be an absolute path. The walk stops after 32 levels to prevent
// runaway traversal on systems without a git repo in the path.
//
// Files larger than maxProjectInstructionsBytes are truncated with a
// trailing "…[truncated, N KB over cap]" marker rather than being loaded
// in full.
func LoadProjectInstructions(root string) string {
	const maxDepth = 32
	dir := filepath.Clean(root)

	for i := 0; i < maxDepth; i++ {
		for _, candidate := range []string{
			filepath.Join(dir, ".huginn.md"),
			filepath.Join(dir, ".huginn", "instructions.md"),
		} {
			if data, err := os.ReadFile(candidate); err == nil {
				return truncateInstructions(strings.TrimSpace(string(data)))
			}
		}
		// Stop at git root.
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return ""
}

// truncateInstructions caps s at maxProjectInstructionsBytes, appending a
// clear marker naming how much was cut when truncation occurs. No-op for
// content already within the cap.
func truncateInstructions(s string) string {
	if len(s) <= maxProjectInstructionsBytes {
		return s
	}
	overBytes := len(s) - maxProjectInstructionsBytes
	overKB := (overBytes + 1023) / 1024
	if overKB < 1 {
		overKB = 1
	}
	// Back the cut off any partial UTF-8 rune so the prompt never carries an
	// invalid byte sequence (a raw byte slice can split a multi-byte rune).
	cut := runeSafeCut(s, maxProjectInstructionsBytes)
	return s[:cut] + fmt.Sprintf("\n…[truncated, %d KB over cap]", overKB)
}

// runeSafeCut returns the largest index <= n at which s can be sliced without
// splitting a multi-byte UTF-8 rune. It walks back over any trailing UTF-8
// continuation bytes (0b10xxxxxx) that would be orphaned by a cut at n.
func runeSafeCut(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return n
}

// LoadGlobalInstructions reads ~/.config/huginn/instructions.md.
// Returns "" if the file does not exist.
func LoadGlobalInstructions() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".config", "huginn", "instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
