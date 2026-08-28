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
	// LastTestCommand is the most recent test command that exited 0 in this
	// workspace, remembered so tools like run_tests can surface it instead of
	// the model guessing between `make test`, `go test ./...`, etc.
	LastTestCommand string `json:"last_test_command,omitempty"`
	// SyntaxValidation controls edit-time syntax validation (G1) for
	// write_file/edit_file: "block" (default — reject syntactically broken
	// writes), "warn" (write anyway, annotate the tool result), or "off"
	// (disable). See internal/tools/validation.NormalizeMode for parsing.
	SyntaxValidation string `json:"syntax_validation,omitempty"`
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

// SaveConfig writes cfg to root/.huginn/workspace.json, creating the
// .huginn directory if needed. It overwrites the whole file — callers that
// want to preserve existing fields should load first (see
// SaveLastTestCommand for that pattern).
func SaveConfig(root string, cfg *WorkspaceConfig) error {
	dir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: SaveConfig: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: SaveConfig: marshal: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace.json"), data, 0o644); err != nil {
		return fmt.Errorf("workspace: SaveConfig: write: %w", err)
	}
	return nil
}

// SaveLastTestCommand records command as the last test command that exited 0
// in root's .huginn/workspace.json, preserving any other fields already
// present in the file (root/name/exclude). Safe to call even when no
// workspace.json exists yet.
func SaveLastTestCommand(root, command string) error {
	cfg, err := LoadConfig(root)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &WorkspaceConfig{}
	}
	cfg.LastTestCommand = command
	return SaveConfig(root, cfg)
}

// LoadLastTestCommand returns the last remembered successful test command for
// root, or "" if none is recorded (including when no workspace.json exists).
func LoadLastTestCommand(root string) string {
	cfg, err := LoadConfig(root)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.LastTestCommand
}
