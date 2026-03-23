package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadProjectInstructions walks up from root toward the filesystem root,
// stopping at the git root (directory containing ".git"), looking for:
//   - .huginn.md
//   - .huginn/instructions.md
//
// The first file found wins. Returns "" if none found.
// root must be an absolute path. The walk stops after 32 levels to prevent
// runaway traversal on systems without a git repo in the path.
func LoadProjectInstructions(root string) string {
	const maxDepth = 32
	dir := filepath.Clean(root)

	for i := 0; i < maxDepth; i++ {
		for _, candidate := range []string{
			filepath.Join(dir, ".huginn.md"),
			filepath.Join(dir, ".huginn", "instructions.md"),
		} {
			if data, err := os.ReadFile(candidate); err == nil {
				return strings.TrimSpace(string(data))
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
