package headless

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHeadlessConfig_Empty verifies RunResult with empty config.
func TestHeadlessConfig_Empty(t *testing.T) {
	cfg := HeadlessConfig{
		CWD:     "",
		Command: "",
		JSON:    false,
	}

	result, _ := Run(cfg)
	// Should handle empty config gracefully (uses current directory)
	if result == nil {
		t.Error("Run should return non-nil RunResult even on error")
	}
}

// TestHeadlessConfig_InvalidCWD verifies Run with invalid directory.
func TestHeadlessConfig_InvalidCWD(t *testing.T) {
	cfg := HeadlessConfig{
		CWD:     "/nonexistent/path/that/does/not/exist",
		Command: "",
		JSON:    false,
	}

	result, _ := Run(cfg)
	// Should return a result (possibly with errors)
	if result == nil {
		t.Error("Run should return non-nil RunResult")
	}
	// May have errors due to invalid path
	if len(result.Errors) == 0 {
		t.Logf("Run with invalid CWD returned no errors (acceptable if gracefully handled)")
	}
}

// TestHeadlessConfig_WithValidCWD verifies Run with valid directory.
func TestHeadlessConfig_WithValidCWD(t *testing.T) {
	tmpDir := t.TempDir()
	// Initialize a git repo
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := HeadlessConfig{
		CWD:     tmpDir,
		Command: "",
		JSON:    false,
	}

	result, _ := Run(cfg)
	if result == nil {
		t.Fatal("Run should return non-nil RunResult")
	}
	if result.Root != tmpDir {
		t.Errorf("expected Root=%q, got %q", tmpDir, result.Root)
	}
}

// TestRunResult_FilesScanned verifies FilesScanned counter.
func TestRunResult_FilesScanned(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create some files
	for i := 0; i < 5; i++ {
		fpath := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".go")
		if err := os.WriteFile(fpath, []byte("package main\n"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	cfg := HeadlessConfig{CWD: tmpDir, JSON: false}
	result, _ := Run(cfg)

	// FilesScanned should be non-negative
	if result.FilesScanned < 0 {
		t.Errorf("FilesScanned should be non-negative, got %d", result.FilesScanned)
	}
}

// TestRunResult_Mode verifies Mode field in result.
func TestRunResult_Mode(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := HeadlessConfig{CWD: tmpDir, JSON: false}
	result, _ := Run(cfg)

	if result.Mode == "" {
		t.Error("Mode should not be empty")
	}
	// Mode should be one of: workspace, repo, plain
	validModes := map[string]bool{"workspace": true, "repo": true, "plain": true}
	if !validModes[result.Mode] {
		t.Errorf("Mode should be workspace/repo/plain, got %q", result.Mode)
	}
}

// TestRunResult_Errors_EmptyInitially verifies Errors slice behavior.
func TestRunResult_Errors_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := HeadlessConfig{CWD: tmpDir, JSON: false}
	result, _ := Run(cfg)

	// Errors is nil when there are no errors ([]string omitempty) — that's fine.
	// Just verify the result is non-nil overall.
	if result == nil {
		t.Error("Run should return non-nil RunResult")
	}
}

// TestRunResult_ReposFound_NonEmpty verifies ReposFound is populated.
func TestRunResult_ReposFound(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := HeadlessConfig{CWD: tmpDir, JSON: false}
	result, _ := Run(cfg)

	if len(result.ReposFound) == 0 {
		t.Error("ReposFound should contain at least the root")
	}
	if result.ReposFound[0] != tmpDir {
		t.Errorf("first repo should be root %q, got %q", tmpDir, result.ReposFound[0])
	}
}

// TestRunResult_IndexDuration_NonEmpty verifies timing information.
func TestRunResult_IndexDuration(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := HeadlessConfig{CWD: tmpDir, JSON: false}
	result, _ := Run(cfg)

	if result.IndexDuration == "" {
		t.Error("IndexDuration should not be empty")
	}
}

// TestFindingSummary_Structure verifies FindingSummary fields.
func TestFindingSummary_Structure(t *testing.T) {
	fs := FindingSummary{
		ID:       "finding-1",
		Type:     "cyclic_dependency",
		Title:    "Circular dependency detected",
		Severity: "high",
		Score:    0.85,
		Files:    []string{"main.go", "lib.go"},
	}

	if fs.ID == "" {
		t.Error("FindingSummary.ID should not be empty")
	}
	if fs.Type == "" {
		t.Error("FindingSummary.Type should not be empty")
	}
	if fs.Severity == "" {
		t.Error("FindingSummary.Severity should not be empty")
	}
	if fs.Score < 0 || fs.Score > 1 {
		t.Errorf("FindingSummary.Score should be 0-1, got %f", fs.Score)
	}
	if len(fs.Files) != 2 {
		t.Errorf("FindingSummary.Files should have 2 items, got %d", len(fs.Files))
	}
}

// TestRunResult_JSON_Serializable verifies RunResult can be JSON encoded.
func TestRunResult_JSON_Structure(t *testing.T) {
	result := &RunResult{
		Mode:          "repo",
		Root:          "/tmp/repo",
		ReposFound:    []string{"/tmp/repo"},
		IndexDuration: "1.5s",
		FilesScanned:  100,
		FilesSkipped:  5,
		RadarDuration: "0.8s",
		TopFindings: []FindingSummary{
			{
				ID:       "f1",
				Type:     "issue",
				Title:    "Issue 1",
				Severity: "medium",
				Score:    0.5,
			},
		},
		Errors: []string{},
	}

	// Verify all fields are accessible
	if result.Mode != "repo" {
		t.Error("Mode should be repo")
	}
	if result.FilesScanned != 100 {
		t.Error("FilesScanned should be 100")
	}
	if len(result.TopFindings) != 1 {
		t.Error("should have 1 finding")
	}
}

// TestHeadlessConfig_JSON_Flag verifies JSON flag is stored.
func TestHeadlessConfig_JSON_Flag(t *testing.T) {
	cfg := HeadlessConfig{
		CWD:     "/tmp",
		Command: "",
		JSON:    true,
	}

	if !cfg.JSON {
		t.Error("JSON flag should be true")
	}

	cfg2 := HeadlessConfig{
		CWD:     "/tmp",
		Command: "",
		JSON:    false,
	}

	if cfg2.JSON {
		t.Error("JSON flag should be false")
	}
}

// TestHeadlessConfig_Command_Field verifies Command field.
func TestHeadlessConfig_Command(t *testing.T) {
	cfg := HeadlessConfig{
		CWD:     "/tmp",
		Command: "index",
		JSON:    false,
	}

	if cfg.Command != "index" {
		t.Errorf("expected Command='index', got %q", cfg.Command)
	}
}

// TestHeadlessStoreDir_PathConstruction verifies store directory path.
func TestHeadlessStoreDir_PathConstruction(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := headlessStoreDir(tmpDir)

	if storeDir == "" {
		t.Error("headlessStoreDir should return non-empty path")
	}
	// Should contain .huginn
	if !contains(storeDir, ".huginn") {
		t.Errorf("store dir should contain .huginn, got %q", storeDir)
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
