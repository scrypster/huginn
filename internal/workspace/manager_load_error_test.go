package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ReadError(t *testing.T) {
	dir := t.TempDir()
	huginnDir := filepath.Join(dir, ".huginn")
	os.MkdirAll(huginnDir, 0755)
	// Create workspace.json as a directory instead of a file
	os.MkdirAll(filepath.Join(huginnDir, "workspace.json"), 0755)

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error when workspace.json is a directory")
	}
}

func TestNewManager_CWDFallback(t *testing.T) {
	dir := t.TempDir()
	// No .git, go.mod, package.json, or .huginn/workspace.json
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.Method() != "cwd" {
		t.Errorf("expected method 'cwd', got %q", m.Method())
	}
	if m.Config() != nil {
		t.Error("expected nil config for CWD fallback")
	}
}

func TestManager_Refresh_WithNewConfig(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m.Method() != "cwd" {
		t.Errorf("expected initial method 'cwd', got %q", m.Method())
	}

	// Now create a workspace.json
	huginnDir := filepath.Join(dir, ".huginn")
	os.MkdirAll(huginnDir, 0755)
	os.WriteFile(filepath.Join(huginnDir, "workspace.json"), []byte(`{"name": "test"}`), 0644)

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if m.Method() != "config" {
		t.Errorf("expected method 'config' after refresh, got %q", m.Method())
	}
	if m.Config() == nil {
		t.Error("expected non-nil config after refresh")
	}
	if m.Config().Name != "test" {
		t.Errorf("expected config name 'test', got %q", m.Config().Name)
	}
}

func TestDiscoverRoot_PackageJSON_InSubdir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "src", "deep")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)

	root, method, err := DiscoverRoot(subdir)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "packagejson" {
		t.Errorf("expected 'packagejson', got %q", method)
	}
	if root != dir {
		t.Errorf("expected root=%q, got %q", dir, root)
	}
}
