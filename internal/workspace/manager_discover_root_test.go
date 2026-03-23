package workspace_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/workspace"
)

// TestNewManager_DiscoverRoot_MaxDepthFallback creates a very shallow temp dir
// that has neither .huginn/workspace.json, .git, go.mod, nor package.json so
// DiscoverRoot falls back to "cwd". This exercises the fallback "cwd" return
// in NewManager (the uncovered branch at line 27 of manager.go).
func TestNewManager_DiscoverRoot_FallbackCWD(t *testing.T) {
	// Use a temp dir with no markers — DiscoverRoot will walk up to fs root
	// and fall back to abs(startDir) with method "cwd".
	// We cannot easily control the parent hierarchy in CI, but we can use a
	// sub-directory that won't find any markers within itself.
	dir := t.TempDir()
	subDir := filepath.Join(dir, "empty-sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// DiscoverRoot may find a .git or go.mod higher up in the real environment,
	// but NewManager should always succeed (it ignores cfg load errors).
	mgr, err := workspace.NewManager(subDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Root() == "" {
		t.Error("expected non-empty Root()")
	}
}

// TestNewManager_WithWorkspaceConfig_LoadConfig exercises the LoadConfig path
// inside NewManager. When .huginn/workspace.json exists at the discovered root,
// NewManager should store it and Config() should return non-nil.
func TestNewManager_WithWorkspaceConfig_LoadConfig(t *testing.T) {
	root := t.TempDir()
	huginnDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(huginnDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := workspace.WorkspaceConfig{
		Name:    "boost-project",
		Exclude: []string{"vendor", "node_modules"},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(huginnDir, "workspace.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Method() != "config" {
		t.Errorf("expected method 'config', got %q", mgr.Method())
	}
	if mgr.Config() == nil {
		t.Fatal("expected non-nil Config()")
	}
	if mgr.Config().Name != "boost-project" {
		t.Errorf("Config.Name: got %q, want 'boost-project'", mgr.Config().Name)
	}
	if len(mgr.Config().Exclude) != 2 {
		t.Errorf("Config.Exclude: got %v", mgr.Config().Exclude)
	}
}

// TestDiscoverRoot_PackageJSON exercises the "packagejson" discovery path.
func TestDiscoverRoot_PackageJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(root, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	gotRoot, gotMethod, err := workspace.DiscoverRoot(subDir)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if gotMethod != "packagejson" {
		t.Errorf("expected method 'packagejson', got %q", gotMethod)
	}
	if gotRoot != root {
		t.Errorf("expected root %q, got %q", root, gotRoot)
	}
}

// TestManager_Refresh_Error exercises the error branch in Refresh
// by using a startDir that resolves via "cwd" initially, then refreshes.
// Refresh itself only errors when DiscoverRoot errors, which requires
// filepath.Abs to fail — not easily reproducible. We instead exercise
// the happy-path Refresh to ensure the cfg re-assignment branch runs
// even when no workspace.json is present (cfg stays nil across refresh).
func TestManager_Refresh_NoCfgChange(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Call Refresh — should succeed and keep cfg nil (no workspace.json).
	if err := mgr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if mgr.Config() != nil {
		t.Error("expected nil Config() after Refresh with no workspace.json")
	}
	if mgr.Method() != "git" {
		t.Errorf("expected method 'git' after Refresh, got %q", mgr.Method())
	}
}

// TestLoadConfig_InvalidJSON verifies LoadConfig returns an error for malformed JSON.
func TestLoadConfig_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	huginnDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(huginnDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(huginnDir, "workspace.json"), []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := workspace.LoadConfig(root)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil cfg for parse error, got %+v", cfg)
	}
}

// TestLoadConfig_NotExist verifies LoadConfig returns nil, nil when file is absent.
func TestLoadConfig_NotExist(t *testing.T) {
	root := t.TempDir()
	cfg, err := workspace.LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig with no file: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil cfg when file absent, got %+v", cfg)
	}
}

// TestLoadConfig_PermissionDenied exercises the non-IsNotExist error branch in
// LoadConfig. We create an unreadable workspace.json to trigger os.ReadFile to
// return a permission-denied error (not os.IsNotExist).
func TestLoadConfig_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root (chmod 0 still readable)")
	}
	root := t.TempDir()
	huginnDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(huginnDir, 0755); err != nil {
		t.Fatal(err)
	}
	wsFile := filepath.Join(huginnDir, "workspace.json")
	if err := os.WriteFile(wsFile, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Make the file unreadable.
	if err := os.Chmod(wsFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(wsFile, 0o644) })

	cfg, err := workspace.LoadConfig(root)
	if err == nil {
		t.Error("expected error for unreadable workspace.json, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil cfg for error case, got %+v", cfg)
	}
}
