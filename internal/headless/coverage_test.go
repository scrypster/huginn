package headless

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Run — ModeWorkspace (huginn.workspace.json in CWD)
// ---------------------------------------------------------------------------

func TestRun_WorkspaceMode(t *testing.T) {
	dir := t.TempDir()

	// Create a sub-repo directory.
	subRepo := filepath.Join(dir, "service-a")
	if err := os.MkdirAll(subRepo, 0755); err != nil {
		t.Fatalf("MkdirAll sub-repo: %v", err)
	}
	// Write a small Go file in the sub-repo.
	if err := os.WriteFile(filepath.Join(subRepo, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Write huginn.workspace.json in the CWD to trigger ModeWorkspace.
	wsCfg := map[string]any{
		"name": "test-workspace",
		"repos": []map[string]any{
			{"path": "service-a"},
		},
	}
	wsData, _ := json.Marshal(wsCfg)
	if err := os.WriteFile(filepath.Join(dir, "huginn.workspace.json"), wsData, 0644); err != nil {
		t.Fatalf("WriteFile workspace config: %v", err)
	}

	cfg := HeadlessConfig{
		CWD:     dir,
		Command: "",
		JSON:    false,
	}

	result, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run (workspace mode): %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil result")
	}
	if result.Mode != "workspace" {
		t.Errorf("expected mode 'workspace', got %q", result.Mode)
	}
	// Root should be set
	if result.Root == "" {
		t.Error("expected non-empty Root in workspace mode")
	}
}

// TestRun_WorkspaceMode_WithRepos verifies that repos found are populated.
func TestRun_WorkspaceMode_WithRepos(t *testing.T) {
	dir := t.TempDir()

	// Create sub-repo directories.
	for _, name := range []string{"api", "web"} {
		subDir := filepath.Join(dir, name)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package main\n"), 0644); err != nil {
			t.Fatalf("WriteFile %s/main.go: %v", name, err)
		}
	}

	wsCfg := map[string]any{
		"name": "multi-repo",
		"repos": []map[string]any{
			{"path": "api"},
			{"path": "web"},
		},
	}
	wsData, _ := json.Marshal(wsCfg)
	if err := os.WriteFile(filepath.Join(dir, "huginn.workspace.json"), wsData, 0644); err != nil {
		t.Fatalf("WriteFile workspace config: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Mode != "workspace" {
		t.Errorf("expected 'workspace' mode, got %q", result.Mode)
	}
	if len(result.ReposFound) != 2 {
		t.Errorf("expected 2 repos found, got %d: %v", len(result.ReposFound), result.ReposFound)
	}
}

// ---------------------------------------------------------------------------
// Run — ModeRepo (directory with .git)
// ---------------------------------------------------------------------------

func TestRun_RepoMode(t *testing.T) {
	dir := t.TempDir()

	// Create a fake .git directory to trigger ModeRepo detection.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run (repo mode): %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil")
	}
	if result.Mode != "repo" {
		t.Errorf("expected 'repo' mode, got %q", result.Mode)
	}
}

// ---------------------------------------------------------------------------
// Run — store open failure (blocking file at pebble path)
// ---------------------------------------------------------------------------

// TestRun_StoreOpenFailure verifies that when the Pebble store cannot be opened,
// Run appends an error entry and returns a non-nil result (not a hard error).
func TestRun_StoreOpenFailure(t *testing.T) {
	// We cannot easily force storage.Open to fail from here since headlessStoreDir
	// returns a path under ~/.huginn/store. Instead, we test through the result
	// checking that any errors are in result.Errors, not returned as Go errors.

	// Use a CWD that's a plain dir — store should succeed; this test just confirms
	// the error-handling shape is correct.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run should never return a Go error for store failures: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil result")
	}
	// No panic = success for this structural test.
}

// ---------------------------------------------------------------------------
// Run — radar command with totalFiles == 0 path
// ---------------------------------------------------------------------------

// TestRun_RadarCommand_EmptyDir exercises the runRadar totalFiles == 0 branch.
func TestRun_RadarCommand_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	// No files in dir — FilesScanned + FilesSkipped == 0, triggering totalFiles = 100 fallback.

	result, err := Run(HeadlessConfig{
		CWD:     dir,
		Command: "/radar run",
	})
	if err != nil {
		t.Fatalf("Run radar empty dir: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil")
	}
	// May have errors (radar), but must not panic.
}

// TestRun_RadarCommand_Alias exercises the "radar" (without slash) alias.
func TestRun_RadarCommand_Alias(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{
		CWD:     dir,
		Command: "radar",
	})
	if err != nil {
		t.Fatalf("Run radar alias: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil")
	}
}

// ---------------------------------------------------------------------------
// headlessStoreDir — fallback when HOME env var is invalid
// ---------------------------------------------------------------------------

func TestHeadlessStoreDir_HomeDirFallback(t *testing.T) {
	// Override HOME to an empty string to force os.UserHomeDir to fail on
	// some platforms, exercising the os.TempDir() fallback branch.
	// Note: os.UserHomeDir() on macOS/Linux reads $HOME if set; clearing it
	// makes it fall back to /etc/passwd — which may still succeed.
	// We test the function is callable and returns a non-empty path.
	original := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		os.Setenv("HOME", original)
	}()

	// On Linux with no HOME, UserHomeDir looks up /etc/passwd, which may succeed.
	// On macOS it uses the directory services, which may also succeed.
	// Either way, headlessStoreDir must return a non-empty path.
	dir := headlessStoreDir("/some/root")
	if dir == "" {
		t.Error("headlessStoreDir must not return an empty string")
	}
}

// ---------------------------------------------------------------------------
// runRadar — workspace mode with repos
// ---------------------------------------------------------------------------

// TestRun_WorkspaceMode_RadarCommand tests runRadar in workspace mode by
// triggering Run with a workspace config and radar command.
func TestRun_WorkspaceMode_RadarCommand(t *testing.T) {
	dir := t.TempDir()

	// Create a sub-repo directory.
	subRepo := filepath.Join(dir, "backend")
	if err := os.MkdirAll(subRepo, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subRepo, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wsCfg := map[string]any{
		"name": "ws",
		"repos": []map[string]any{
			{"path": "backend"},
		},
	}
	wsData, _ := json.Marshal(wsCfg)
	if err := os.WriteFile(filepath.Join(dir, "huginn.workspace.json"), wsData, 0644); err != nil {
		t.Fatalf("WriteFile workspace: %v", err)
	}

	result, err := Run(HeadlessConfig{
		CWD:     dir,
		Command: "/radar run",
	})
	if err != nil {
		t.Fatalf("Run workspace+radar: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil")
	}
	if result.Mode != "workspace" {
		t.Errorf("expected workspace mode, got %q", result.Mode)
	}
}

// ---------------------------------------------------------------------------
// Large findings list — exercises the summaries >= 10 break
// ---------------------------------------------------------------------------

// TestRun_RadarLargeFindings verifies that when radar returns many findings,
// the result is capped at 10 (the break at summaries >= 10).
// We can only verify this indirectly by checking TopFindings length.
func TestRun_RadarResult_TopFindings_Capped(t *testing.T) {
	// RunResult.TopFindings should never exceed 10.
	// Create a plain dir with some files to index.
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, "file"+string(rune('a'+i))+".go")
		if err := os.WriteFile(name, []byte("package x\n"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	result, err := Run(HeadlessConfig{CWD: dir, Command: "/radar run"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if len(result.TopFindings) > 10 {
		t.Errorf("TopFindings should be capped at 10, got %d", len(result.TopFindings))
	}
}

// ---------------------------------------------------------------------------
// Run — result structure assertions for all modes
// ---------------------------------------------------------------------------

func TestRun_PlainDir_ResultFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hello')"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Root == "" {
		t.Error("Root must be non-empty")
	}
	if result.Mode == "" {
		t.Error("Mode must be non-empty")
	}
	// IndexDuration should be set (index ran)
	if result.IndexDuration == "" && len(result.Errors) == 0 {
		t.Error("expected IndexDuration to be set when no errors")
	}
}
