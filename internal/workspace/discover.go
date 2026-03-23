package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspaceConfig is the optional .huginn/workspace.json config.
type WorkspaceConfig struct {
	Root    string   `json:"root,omitempty"`
	Name    string   `json:"name,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// maxDiscoverDepth caps the number of parent directories DiscoverRoot will
// traverse. In practice, no legitimate workspace lives more than 100 levels
// deep, and a cap prevents infinite loops if the filesystem reports a cycle
// (e.g. via malformed bind mounts or unusual symlink arrangements).
const maxDiscoverDepth = 100

// DiscoverRoot walks up from startDir applying 5-step discovery.
// Returns (rootPath, method, error) where method is one of:
//
//	"config"      — found .huginn/workspace.json
//	"git"         — found .git directory
//	"gomod"       — found go.mod
//	"packagejson" — found package.json
//	"cwd"         — fallback: startDir itself
//
// Returns an error only if startDir cannot be made absolute.
func DiscoverRoot(startDir string) (root string, method string, err error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("workspace: DiscoverRoot: abs(%q): %w", startDir, err)
	}

	// Walk up from startDir, check each level in priority order.
	dir := abs
	depth := 0
	for {
		if depth >= maxDiscoverDepth {
			break
		}
		depth++
		// Step 1: .huginn/workspace.json
		if _, err := os.Stat(filepath.Join(dir, ".huginn", "workspace.json")); err == nil {
			return dir, "config", nil
		}
		// Step 2: .git directory
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, "git", nil
		}
		// Step 3: go.mod
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, "gomod", nil
		}
		// Step 4: package.json
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			return dir, "packagejson", nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root — fallback to startDir.
			break
		}
		dir = parent
	}

	// Step 5: fallback to startDir.
	return abs, "cwd", nil
}

// LoadConfig reads .huginn/workspace.json from root.
// Returns nil, nil if the file does not exist.
func LoadConfig(root string) (*WorkspaceConfig, error) {
	path := filepath.Join(root, ".huginn", "workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("workspace: LoadConfig: %w", err)
	}
	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("workspace: LoadConfig: parse: %w", err)
	}
	return &cfg, nil
}
