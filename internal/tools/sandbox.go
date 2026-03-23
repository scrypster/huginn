package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveSandboxed resolves path relative to root, returning an absolute path.
// Returns an error if the resolved path escapes root (symlink-safe).
func ResolveSandboxed(root, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("sandbox: path must not be empty")
	}
	// Join and clean first
	joined := filepath.Join(root, path)
	// Evaluate symlinks to prevent escape via symlinks
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// File doesn't exist yet (e.g., new file to be written).
		// Resolve the parent directory via EvalSymlinks so that a symlinked
		// parent pointing outside the sandbox is caught. For deeply nested
		// new paths where even the parent doesn't exist, walk up until we
		// find an existing ancestor and resolve from there.
		parent := filepath.Dir(joined)
		resolvedParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			// Parent also doesn't exist; walk up to find deepest existing ancestor.
			var walkErr error
			resolved, walkErr = resolveDeepNewPath(joined)
			if walkErr != nil {
				return "", fmt.Errorf("resolve path %q: %w", path, walkErr)
			}
		} else {
			// Reconstruct the full path using the symlink-resolved parent.
			resolved = filepath.Join(resolvedParent, filepath.Base(joined))
		}
	}
	// Ensure the resolved path is within root.
	// Use EvalSymlinks on root as well so that both sides of the comparison
	// are fully resolved (important on macOS where /tmp -> /private/tmp).
	absRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// Root doesn't exist; fall back to Abs.
		absRoot, err = filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("resolve root %q: %w", root, err)
		}
	}
	// Normalize root to have trailing separator for prefix check
	if !strings.HasSuffix(absRoot, string(os.PathSeparator)) {
		absRoot += string(os.PathSeparator)
	}
	if resolved != strings.TrimSuffix(absRoot, string(os.PathSeparator)) &&
		!strings.HasPrefix(resolved, absRoot) {
		return "", fmt.Errorf("path %q escapes sandbox root %q", path, root)
	}
	return resolved, nil
}

// resolveDeepNewPath walks up from joined until it finds an existing ancestor,
// resolves that ancestor via EvalSymlinks, then reconstructs the full path
// by appending the non-existent tail segments. This correctly handles macOS
// symlinked temp dirs (e.g. /var/folders -> /private/var/folders) even when
// multiple levels of the path don't exist yet.
func resolveDeepNewPath(joined string) (string, error) {
	var parts []string
	p := joined
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			// Found an existing ancestor — reconstruct full path.
			result := resolved
			for i := len(parts) - 1; i >= 0; i-- {
				result = filepath.Join(result, parts[i])
			}
			return result, nil
		}
		parent := filepath.Dir(p)
		if parent == p {
			// Reached filesystem root with no existing ancestor.
			return filepath.Abs(joined)
		}
		parts = append(parts, filepath.Base(p))
		p = parent
	}
}

// ErrSandboxEscape is a ToolResult representing a sandbox violation.
func ErrSandboxEscape(root, path string) ToolResult {
	return ToolResult{
		IsError: true,
		Error:   fmt.Sprintf("path %q escapes sandbox root %q — operation denied", path, root),
	}
}
