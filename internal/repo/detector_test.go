package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeWorkspaceFile writes a minimal huginn.workspace.json at dir/huginn.workspace.json.
func writeWorkspaceFile(t *testing.T, dir string, name string, repos []RepoEntry) string {
	t.Helper()
	cfg := WorkspaceConfig{
		Name:  name,
		Repos: repos,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal workspace: %v", err)
	}
	path := filepath.Join(dir, "huginn.workspace.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}
	return path
}

// --- loadWorkspaceFile ---

// TestLoadWorkspaceFile_Valid verifies that a well-formed workspace file is parsed.
func TestLoadWorkspaceFile_Valid(t *testing.T) {
	dir := t.TempDir()
	repos := []RepoEntry{{Path: "./api", Tags: []string{"backend"}}}
	path := writeWorkspaceFile(t, dir, "my-workspace", repos)

	cfg, wdir, err := loadWorkspaceFile(path)
	if err != nil {
		t.Fatalf("loadWorkspaceFile: %v", err)
	}
	if cfg.Name != "my-workspace" {
		t.Errorf("expected name my-workspace, got %q", cfg.Name)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Path != "./api" {
		t.Errorf("unexpected repos: %v", cfg.Repos)
	}
	if wdir != dir {
		t.Errorf("expected dir %q, got %q", dir, wdir)
	}
}

// TestLoadWorkspaceFile_MissingFile verifies that a missing file returns an error.
func TestLoadWorkspaceFile_MissingFile(t *testing.T) {
	_, _, err := loadWorkspaceFile("/nonexistent/huginn.workspace.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadWorkspaceFile_EmptyFile verifies that an empty file returns an error.
func TestLoadWorkspaceFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huginn.workspace.json")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadWorkspaceFile(path)
	if err == nil {
		t.Fatal("expected error for empty workspace file")
	}
}

// TestLoadWorkspaceFile_InvalidJSON verifies that malformed JSON returns an error.
func TestLoadWorkspaceFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huginn.workspace.json")
	if err := os.WriteFile(path, []byte("{invalid!!!}"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadWorkspaceFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- findGitRoot ---

// TestFindGitRoot_WithGitDir verifies that findGitRoot finds the .git directory.
func TestFindGitRoot_WithGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	got := findGitRoot(dir)
	if got != dir {
		t.Errorf("expected %q, got %q", dir, got)
	}
}

// TestFindGitRoot_InSubdir verifies that findGitRoot walks up to find .git.
func TestFindGitRoot_InSubdir(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// The walk-up stops at $HOME, so set HOME to a parent outside our temp tree.
	t.Setenv("HOME", "/tmp")

	got := findGitRoot(sub)
	if got != root {
		t.Errorf("expected %q, got %q", root, got)
	}
}

// TestFindGitRoot_NoGitDir verifies findGitRoot returns "" when there is no .git.
func TestFindGitRoot_NoGitDir(t *testing.T) {
	dir := t.TempDir()
	// Ensure HOME is set to a place that prevents the walk from leaving our temp dir.
	t.Setenv("HOME", dir)

	got := findGitRoot(dir)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- Detect ---

// TestDetect_ModeRepo verifies Detect returns ModeRepo when inside a git repo
// and there is no workspace file.
func TestDetect_ModeRepo(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Set HOME above our temp dir so the walk-up doesn't go too far.
	t.Setenv("HOME", filepath.Dir(dir))

	result := Detect(dir, "")
	if result.Mode != ModeRepo {
		t.Errorf("expected ModeRepo, got %v", result.Mode)
	}
	if result.Root != dir {
		t.Errorf("expected root %q, got %q", dir, result.Root)
	}
}

// TestDetect_ModePlain verifies Detect returns ModePlain when there is no git
// repo and no workspace file.
func TestDetect_ModePlain(t *testing.T) {
	dir := t.TempDir()
	// Set HOME to our dir so the upward walk stops immediately.
	t.Setenv("HOME", dir)

	result := Detect(dir, "")
	if result.Mode != ModePlain {
		t.Errorf("expected ModePlain, got %v", result.Mode)
	}
	if result.Root != dir {
		t.Errorf("expected root %q, got %q", dir, result.Root)
	}
}

// TestDetect_ModeWorkspace_CWD verifies Detect returns ModeWorkspace when
// huginn.workspace.json exists in cwd.
func TestDetect_ModeWorkspace_CWD(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "ws", []RepoEntry{{Path: "./repo1"}})
	t.Setenv("HOME", dir)

	result := Detect(dir, "")
	if result.Mode != ModeWorkspace {
		t.Errorf("expected ModeWorkspace, got %v", result.Mode)
	}
	if result.WorkspaceConfig == nil {
		t.Fatal("expected non-nil WorkspaceConfig")
	}
	if result.WorkspaceConfig.Name != "ws" {
		t.Errorf("expected name ws, got %q", result.WorkspaceConfig.Name)
	}
}

// TestDetect_ModeWorkspace_ExplicitPath verifies that an explicit workspace path
// takes priority.
func TestDetect_ModeWorkspace_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	wsPath := writeWorkspaceFile(t, dir, "explicit-ws", []RepoEntry{{Path: "./svc"}})
	t.Setenv("HOME", dir)

	// cwd is different from the workspace file location.
	cwd := t.TempDir()
	result := Detect(cwd, wsPath)
	if result.Mode != ModeWorkspace {
		t.Errorf("expected ModeWorkspace, got %v", result.Mode)
	}
	if result.WorkspaceFile != wsPath {
		t.Errorf("expected WorkspaceFile=%q, got %q", wsPath, result.WorkspaceFile)
	}
}

// TestDetect_ModeWorkspace_WalkUp verifies Detect finds huginn.workspace.json
// by walking up the directory tree from cwd.
func TestDetect_ModeWorkspace_WalkUp(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "parent-ws", []RepoEntry{{Path: "./src"}})

	// Create a subdirectory as cwd.
	sub := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	// Set HOME above root so the walk includes root.
	t.Setenv("HOME", filepath.Dir(root))

	result := Detect(sub, "")
	if result.Mode != ModeWorkspace {
		t.Errorf("expected ModeWorkspace (walk-up), got %v", result.Mode)
	}
	if result.WorkspaceConfig == nil {
		t.Fatal("expected non-nil WorkspaceConfig from walk-up")
	}
	if result.WorkspaceConfig.Name != "parent-ws" {
		t.Errorf("expected name parent-ws, got %q", result.WorkspaceConfig.Name)
	}
}

// TestDetect_ExplicitPath_InvalidFallsThrough verifies that an invalid explicit
// workspace path falls through to subsequent detection steps.
func TestDetect_ExplicitPath_InvalidFallsThrough(t *testing.T) {
	dir := t.TempDir()
	// Set HOME to dir so upward walk stops.
	t.Setenv("HOME", dir)

	// Provide a non-existent explicit path; detection should fall through to ModePlain.
	result := Detect(dir, "/nonexistent/huginn.workspace.json")
	// No .git dir, no workspace file -> ModePlain.
	if result.Mode != ModePlain {
		t.Errorf("expected ModePlain after invalid explicit path, got %v", result.Mode)
	}
}
