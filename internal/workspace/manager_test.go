package workspace_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/workspace"
)

func TestNewManager_BasicGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(subDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Root() != root {
		t.Errorf("expected root %q, got %q", root, mgr.Root())
	}
	if mgr.Method() != "git" {
		t.Errorf("expected method 'git', got %q", mgr.Method())
	}
}

func TestNewManager_Root_IsStable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	r1 := mgr.Root()
	r2 := mgr.Root()
	r3 := mgr.Root()
	if r1 != r2 || r2 != r3 {
		t.Errorf("Root() is not stable: %q %q %q", r1, r2, r3)
	}
}

func TestNewManager_Config_WhenPresent(t *testing.T) {
	root := t.TempDir()
	huginnDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(huginnDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := workspace.WorkspaceConfig{Name: "my-project", Exclude: []string{"dist"}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(huginnDir, "workspace.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Config() == nil {
		t.Fatal("expected non-nil Config() when workspace.json is present")
	}
	if mgr.Config().Name != "my-project" {
		t.Errorf("expected Name 'my-project', got %q", mgr.Config().Name)
	}
}

func TestNewManager_Config_WhenAbsent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Config() != nil {
		t.Errorf("expected nil Config() when no workspace.json, got %+v", mgr.Config())
	}
}

func TestManager_Refresh_UpdatesRoot(t *testing.T) {
	root := t.TempDir()
	subDir := filepath.Join(root, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(subDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Now add a .huginn/workspace.json which should take priority after Refresh.
	huginnDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(huginnDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgData, _ := json.Marshal(workspace.WorkspaceConfig{Name: "refreshed"})
	if err := os.WriteFile(filepath.Join(huginnDir, "workspace.json"), cfgData, 0644); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if mgr.Method() != "config" {
		t.Errorf("expected method 'config' after Refresh, got %q", mgr.Method())
	}
	if mgr.Config() == nil {
		t.Error("expected non-nil Config() after Refresh with workspace.json present")
	}
}

func TestManager_ConcurrentRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = mgr.Root()
			_ = mgr.Method()
			_ = mgr.Config()
		}()
	}
	wg.Wait()
}
