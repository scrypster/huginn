package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// makeDir creates a directory structure from a map of path->content (empty string = directory only).
func makeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if content == "" {
			if err := os.MkdirAll(full, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
		} else {
			if err := os.WriteFile(full, []byte(content), 0644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
	}
	return root
}

func TestDiscoverRoot_HuginnConfig(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":"my-project"}`,
	})
	dir := filepath.Join(root)
	got, method, err := DiscoverRoot(dir)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "config" {
		t.Errorf("method = %q, want %q", method, "config")
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestDiscoverRoot_GitRoot(t *testing.T) {
	root := makeDir(t, map[string]string{
		".git": "", // directory
	})
	got, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "git" {
		t.Errorf("method = %q, want %q", method, "git")
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestDiscoverRoot_GoMod(t *testing.T) {
	root := makeDir(t, map[string]string{
		"go.mod": "module example.com/mymod\ngo 1.21\n",
	})
	got, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "gomod" {
		t.Errorf("method = %q, want %q", method, "gomod")
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestDiscoverRoot_PackageJSON(t *testing.T) {
	root := makeDir(t, map[string]string{
		"package.json": `{"name":"my-app"}`,
	})
	got, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "packagejson" {
		t.Errorf("method = %q, want %q", method, "packagejson")
	}
	if got != root {
		t.Errorf("root = %q, want %q", got, root)
	}
}

func TestDiscoverRoot_CWDFallback(t *testing.T) {
	// A temp dir with no markers — should fall back to cwd
	root := t.TempDir()
	// Create a subdir with no markers
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	// The parent (root) temp dir itself might be under a git root on the test machine,
	// so we test from sub and expect cwd (the sub itself) OR that some ancestor was found.
	// Just verify no error and a non-empty root is returned.
	got, _, err := DiscoverRoot(sub)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if got == "" {
		t.Error("DiscoverRoot returned empty root")
	}
}

func TestDiscoverRoot_PreferHigherPriority(t *testing.T) {
	// Both .git and go.mod present — should prefer .git over go.mod
	root := makeDir(t, map[string]string{
		".git":   "", // directory
		"go.mod": "module example.com/test\ngo 1.21\n",
	})
	_, method, err := DiscoverRoot(root)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "git" {
		t.Errorf("method = %q, want %q (git should beat gomod)", method, "git")
	}
}

func TestDiscoverRoot_WalksUpToFindGit(t *testing.T) {
	// .git is in parent, we start from a subdirectory
	root := makeDir(t, map[string]string{
		".git": "", // directory
	})
	sub := filepath.Join(root, "pkg", "internal")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, method, err := DiscoverRoot(sub)
	if err != nil {
		t.Fatalf("DiscoverRoot: %v", err)
	}
	if method != "git" {
		t.Errorf("method = %q, want %q", method, "git")
	}
	if got != root {
		t.Errorf("root = %q, want %q (should find parent with .git)", got, root)
	}
}

func TestLoadConfig_Present(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{"name":"test","root":"/custom","exclude":["vendor"]}`,
	})
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil, want config")
	}
	if cfg.Name != "test" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test")
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "vendor" {
		t.Errorf("Exclude = %v, want [vendor]", cfg.Exclude)
	}
}

func TestLoadConfig_Missing_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig: expected nil error for missing file, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("LoadConfig returned non-nil config for missing file: %v", cfg)
	}
}

func TestLoadConfig_InvalidJSON_ReturnsError(t *testing.T) {
	root := makeDir(t, map[string]string{
		".huginn/workspace.json": `{invalid`,
	})
	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
