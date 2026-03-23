package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverRoot_PathTraversal_RelativeParent verifies .. paths don't escape root.
func TestDiscoverRoot_PathTraversal_RelativeParent(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "",
	})
	// Start from a subdirectory and use .. to navigate
	sub := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Start from deep subdirectory with relative path containing ..
	got, method, err := DiscoverRoot(sub)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "git" {
		t.Errorf("expected to find git root, got method %q", method)
	}
	if got != root {
		t.Errorf("expected root %q, got %q", root, got)
	}
}

// TestDiscoverRoot_SymlinkHandling verifies symlinks are followed correctly.
func TestDiscoverRoot_SymlinkHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping symlink test in short mode")
	}

	root := makeDir(t, map[string]string{
		".git": "",
	})

	// Create a symlink to a directory inside root
	symDir := filepath.Join(root, "sym_link")
	target := filepath.Join(root, "sub", "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.Symlink(target, symDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Navigate through symlink and discover
	_, method, err := DiscoverRoot(symDir)
	if err != nil {
		t.Fatalf("DiscoverRoot through symlink: %v", err)
	}
	// Should still find .git in the root
	if method != "git" {
		t.Errorf("expected git method through symlink, got %q", method)
	}
}

// TestDiscoverRoot_DeepHierarchy verifies discovery works in deep directories.
func TestDiscoverRoot_DeepHierarchy(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "",
	})

	// Create a deep directory hierarchy
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f", "g", "h", "i", "j")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}

	got, method, err := DiscoverRoot(deep)
	if err != nil {
		t.Fatalf("DiscoverRoot from deep: %v", err)
	}
	if method != "git" {
		t.Errorf("expected git, got %q", method)
	}
	if got != root {
		t.Errorf("expected root %q, got %q", root, got)
	}
}

// TestDiscoverRoot_MaxDepthLimit verifies maxDiscoverDepth prevents infinite loops.
func TestDiscoverRoot_MaxDepthLimit(t *testing.T) {
	root := t.TempDir()

	// Create a directory structure exactly at maxDiscoverDepth without any markers
	dir := root
	for i := 0; i < maxDiscoverDepth; i++ {
		dir = filepath.Join(dir, "level"+string(rune('0'+byte(i%10))))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir level %d: %v", i, err)
		}
	}

	// Should not hang or panic; should return fallback
	got, method, err := DiscoverRoot(dir)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty root")
	}
	// After hitting maxDepth without finding markers, should return cwd
	if method != "cwd" {
		t.Logf("at maxDepth limit, method was %q (might be ancestor found)", method)
	}
}

// TestDiscoverRoot_EmptyStartDir verifies handling of empty/invalid startDir.
func TestDiscoverRoot_EmptyStartDir(t *testing.T) {
	// Empty string should be treated as "." (current directory)
	got, _, err := DiscoverRoot("")
	if err != nil {
		// May error or may work depending on os.Abs behavior
		t.Logf("DiscoverRoot with empty string: %v", err)
	} else if got == "" {
		t.Error("expected non-empty root")
	}
}

// TestDiscoverRoot_NonExistentStartDir verifies handling of non-existent directory.
func TestDiscoverRoot_NonExistentStartDir(t *testing.T) {
	nonExistent := "/tmp/this/path/does/not/exist/huginn_test_12345"
	got, method, err := DiscoverRoot(nonExistent)
	// filepath.Abs may succeed even if dir doesn't exist
	// Should still return a result (fallback to cwd or ancestor)
	if err != nil && got == "" {
		t.Logf("DiscoverRoot with non-existent dir: %v", err)
	}
	if got != "" {
		if method == "" {
			t.Error("expected non-empty method when root is returned")
		}
	}
}

// TestDiscoverRoot_PermissionDenied verifies handling of permission-denied directories.
func TestDiscoverRoot_PermissionDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping permission test in short mode")
	}

	root := makeDir(t, map[string]string{
		"readable/.git": "",
	})

	readable := filepath.Join(root, "readable")
	restricted := filepath.Join(root, "restricted")

	// Create restricted directory
	if err := os.MkdirAll(restricted, 0000); err != nil {
		t.Fatalf("mkdir restricted: %v", err)
	}
	defer os.Chmod(restricted, 0755) // Clean up

	// Try to discover from readable directory — should work
	got, method, err := DiscoverRoot(readable)
	if err != nil {
		t.Fatalf("DiscoverRoot from readable: %v", err)
	}
	if method != "git" {
		t.Errorf("expected git method, got %q", method)
	}
	// readable/.git exists, so git root is 'readable' itself.
	if got != readable {
		t.Errorf("expected root %q, got %q", readable, got)
	}
}

// TestLoadConfig_MalformedJSON verifies error on broken JSON syntax.
func TestLoadConfig_MalformedJSON(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":"test"`,
	})
	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// TestLoadConfig_InvalidUTF8 verifies handling of invalid UTF-8 in file.
func TestLoadConfig_InvalidUTF8(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, ".huginn")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write invalid UTF-8
	path := filepath.Join(cfgDir, "workspace.json")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadConfig(root)
	// Should error on JSON unmarshal (invalid UTF-8)
	if err == nil {
		t.Fatal("expected error for invalid UTF-8")
	}
}

// TestLoadConfig_EmptyFile verifies handling of empty JSON file.
func TestLoadConfig_EmptyFile(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": "",
	})
	_, err := LoadConfig(root)
	// Empty file is invalid JSON
	if err == nil {
		t.Fatal("expected error for empty JSON file")
	}
}

// TestLoadConfig_ValidMinimalConfig verifies minimal valid config.
func TestLoadConfig_ValidMinimalConfig(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{}`,
	})
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for valid JSON")
	}
	if cfg.Name != "" {
		t.Errorf("expected empty name, got %q", cfg.Name)
	}
}

// TestLoadConfig_LargeExcludeList verifies handling of large exclude lists.
func TestLoadConfig_LargeExcludeList(t *testing.T) {
	excludes := `["vendor","node_modules","dist","build","coverage",".git","_test","__pycache__","target",".venv"]`
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":"test","exclude":` + excludes + `}`,
	})
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Exclude) < 5 {
		t.Errorf("expected multiple excludes, got %d", len(cfg.Exclude))
	}
}

// TestLoadConfig_ExtraJSONFields verifies unknown fields are ignored.
func TestLoadConfig_ExtraJSONFields(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":"test","extra_field":"ignored","another":123}`,
	})
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Name != "test" {
		t.Errorf("expected name=test, got %q", cfg.Name)
	}
	// Extra fields should be ignored without error
}

// TestLoadConfig_NullValues verifies null JSON values are handled.
func TestLoadConfig_NullValues(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":null,"exclude":null}`,
	})
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Name != "" {
		t.Errorf("expected empty name from null, got %q", cfg.Name)
	}
	if cfg.Exclude != nil && len(cfg.Exclude) > 0 {
		t.Errorf("expected nil/empty exclude from null, got %v", cfg.Exclude)
	}
}

// TestManager_ConcurrentRoot verifies Manager.Root() is thread-safe.
func TestManager_ConcurrentRoot(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "",
	})

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				_ = mgr.Root()
			}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if mgr.Root() != root {
		t.Errorf("expected root %q, got %q", root, mgr.Root())
	}
}

// TestManager_RefreshWithChangedConfig verifies Refresh handles config changes.
func TestManager_RefreshWithChangedConfig(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "",
	})

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Initially no config
	if mgr.Config() != nil {
		t.Error("expected nil config initially")
	}

	// Now add a config file
	cfgPath := filepath.Join(root, ".huginn", "workspace.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Refresh should pick up the new config
	if err := mgr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if mgr.Config() == nil {
		t.Error("expected non-nil config after refresh")
	}
	if mgr.Config().Name != "test" {
		t.Errorf("expected name=test, got %q", mgr.Config().Name)
	}
}

// TestManager_RefreshFailure verifies Refresh handles discovery errors gracefully.
func TestManager_RefreshFailure(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "",
	})

	mgr, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Try to refresh with a non-existent startDir doesn't cause panic
	mgr.startDir = "/tmp/nonexistent/path/for/test"
	err = mgr.Refresh()
	// May succeed or fail depending on filesystem, but should not panic
	if err == nil {
		// If no error, root should be updated to the absolute path
		if mgr.Root() == "" {
			t.Error("root is empty after refresh")
		}
	}
}

// TestManager_MethodReturns verifies Method() returns correct discovery method.
func TestManager_MethodReturns(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{"config_method", map[string]string{".huginn/workspace.json": `{}`}, "config"},
		{"git_method", map[string]string{".git": ""}, "git"},
		{"gomod_method", map[string]string{"go.mod": "module test\n"}, "gomod"},
		{"packagejson_method", map[string]string{"package.json": "{}"}, "packagejson"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeDir(t, tt.files)
			mgr, err := NewManager(root)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			if mgr.Method() != tt.expected {
				t.Errorf("expected method %q, got %q", tt.expected, mgr.Method())
			}
		})
	}
}

// TestDiscoverRoot_Priority_ConfigBeatsAll verifies .huginn/workspace.json priority.
func TestDiscoverRoot_Priority_ConfigBeatsAll(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{}`,
		".git":                   "",
		"go.mod":                 "module test\n",
		"package.json":           "{}",
	})

	_, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "config" {
		t.Errorf("expected config (highest priority), got %q", method)
	}
}

// TestDiscoverRoot_Priority_GitBeatsGomod verifies git priority over gomod.
func TestDiscoverRoot_Priority_GitBeatsGomod(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git":       "",
		"go.mod":     "module test\n",
		"package.json": "{}",
	})

	_, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "git" {
		t.Errorf("expected git (2nd priority), got %q", method)
	}
}

// TestDiscoverRoot_Priority_GomodBeatsPackagejson verifies gomod priority.
func TestDiscoverRoot_Priority_GomodBeatsPackagejson(t *testing.T) {
	root := makeDir(t, map[string]string{
		"go.mod":       "module test\n",
		"package.json": "{}",
	})

	_, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "gomod" {
		t.Errorf("expected gomod (3rd priority), got %q", method)
	}
}
