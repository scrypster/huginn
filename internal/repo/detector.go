package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WorkspaceMode describes how Huginn detected its working context.
type WorkspaceMode int

const (
	ModeRepo      WorkspaceMode = iota // Inside a git repo
	ModeWorkspace                       // huginn.workspace.json found
	ModePlain                           // Plain directory, no git
)

// RepoEntry is one repo inside a multi-repo workspace.
type RepoEntry struct {
	Path string   `json:"path"`
	Tags []string `json:"tags,omitempty"`
}

// WorkspaceConfig is parsed from huginn.workspace.json.
type WorkspaceConfig struct {
	Name     string      `json:"name"`
	Repos    []RepoEntry `json:"repos"`
	Exclude  []string    `json:"exclude,omitempty"`
	Settings struct {
		IndexOnOpen bool `json:"indexOnOpen"`
	} `json:"settings"`
}

// DetectionResult is returned by Detect.
type DetectionResult struct {
	Mode            WorkspaceMode
	Root            string           // git repo root OR workspace file dir OR cwd
	WorkspaceConfig *WorkspaceConfig // non-nil only in ModeWorkspace
	WorkspaceFile   string           // path to huginn.workspace.json if found
}

// Detect discovers the workspace context using this 5-step algorithm:
//
//	1. explicitPath non-empty → load huginn.workspace.json at that path
//	2. cwd contains huginn.workspace.json → load it
//	3. Walk up from cwd (stop at $HOME) → first huginn.workspace.json found → load it
//	4. cwd is inside a git repo → single-repo mode (ModeRepo)
//	5. None of the above → plain mode (ModePlain)
//
// explicitPath is from the --workspace flag (may be "").
func Detect(cwd, explicitPath string) DetectionResult {
	// 1. If explicitPath is provided, try to load it
	if explicitPath != "" {
		if cfg, dir, err := loadWorkspaceFile(explicitPath); err == nil {
			return DetectionResult{
				Mode:            ModeWorkspace,
				Root:            dir,
				WorkspaceConfig: cfg,
				WorkspaceFile:   explicitPath,
			}
		}
		// Fall through if loading fails
	}

	// 2. Check if cwd contains huginn.workspace.json
	cwdWorkspaceFile := filepath.Join(cwd, "huginn.workspace.json")
	if cfg, dir, err := loadWorkspaceFile(cwdWorkspaceFile); err == nil {
		return DetectionResult{
			Mode:            ModeWorkspace,
			Root:            dir,
			WorkspaceConfig: cfg,
			WorkspaceFile:   cwdWorkspaceFile,
		}
	}

	// 3. Walk up from cwd looking for huginn.workspace.json.
	// Step 2 already checked cwd, so we start from cwd's parent and walk up
	// to (and including) $HOME, but not above it.
	home, _ := os.UserHomeDir()
	current := filepath.Dir(cwd)
	for {
		// Check for workspace file at current directory
		workspaceFile := filepath.Join(current, "huginn.workspace.json")
		if cfg, dir, err := loadWorkspaceFile(workspaceFile); err == nil {
			return DetectionResult{
				Mode:            ModeWorkspace,
				Root:            dir,
				WorkspaceConfig: cfg,
				WorkspaceFile:   workspaceFile,
			}
		}

		// Stop after checking $HOME (don't go above it)
		if home != "" && current == home {
			break
		}

		parent := filepath.Dir(current)
		// Stop at filesystem root
		if parent == current {
			break
		}
		current = parent
	}

	// 4. Check if inside a git repo
	gitRoot := findGitRoot(cwd)
	if gitRoot != "" {
		return DetectionResult{
			Mode: ModeRepo,
			Root: gitRoot,
		}
	}

	// 5. Plain mode: no git, no workspace file
	return DetectionResult{
		Mode: ModePlain,
		Root: cwd,
	}
}

// loadWorkspaceFile reads and parses a huginn.workspace.json file.
// Returns the config, the directory containing the file, and any error.
// Returns an error if the file is empty or contains no repos.
func loadWorkspaceFile(path string) (*WorkspaceConfig, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	if len(data) == 0 {
		return nil, "", fmt.Errorf("workspace file %s is empty", path)
	}

	var cfg WorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", err
	}

	dir := filepath.Dir(path)
	return &cfg, dir, nil
}

// findGitRoot walks up from dir to find the directory containing .git/.
// Returns "" if not found before reaching above $HOME or filesystem root.
func findGitRoot(dir string) string {
	home, _ := os.UserHomeDir()
	current := dir

	for {
		// Check for .git directory BEFORE checking stop conditions,
		// so that a repo rooted at $HOME is found.
		gitDir := filepath.Join(current, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return current
		}

		// Stop after checking $HOME (don't go above it)
		if home != "" && current == home {
			break
		}

		parent := filepath.Dir(current)
		// Stop at filesystem root
		if parent == current {
			break
		}
		current = parent
	}

	return ""
}
