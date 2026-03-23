package headless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- workspaceHash ---

func TestWorkspaceHash_Length(t *testing.T) {
	h := workspaceHash("/some/path")
	if len(h) != 12 {
		t.Errorf("expected 12-char hash, got %d: %q", len(h), h)
	}
}

func TestWorkspaceHash_Deterministic(t *testing.T) {
	h1 := workspaceHash("/some/path")
	h2 := workspaceHash("/some/path")
	if h1 != h2 {
		t.Errorf("workspaceHash must be deterministic: %q vs %q", h1, h2)
	}
}

func TestWorkspaceHash_DifferentInputs(t *testing.T) {
	h1 := workspaceHash("/repo/a")
	h2 := workspaceHash("/repo/b")
	if h1 == h2 {
		t.Errorf("expected different hashes for different inputs, both got %q", h1)
	}
}

func TestWorkspaceHash_EmptyString(t *testing.T) {
	h := workspaceHash("")
	if len(h) != 12 {
		t.Errorf("expected 12-char hash for empty string, got %d: %q", len(h), h)
	}
}

func TestWorkspaceHash_HexCharsOnly(t *testing.T) {
	h := workspaceHash("/usr/local/bin")
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected hex characters only, got %q in %q", c, h)
		}
	}
}

// --- sanitizePath ---

func TestSanitizePath_NoSpecialChars(t *testing.T) {
	result := sanitizePath("normalpath")
	if result != "normalpath" {
		t.Errorf("expected unchanged path, got %q", result)
	}
}

func TestSanitizePath_Slashes(t *testing.T) {
	result := sanitizePath("/home/user/project")
	if strings.Contains(result, "/") {
		t.Errorf("expected slashes replaced, got %q", result)
	}
}

func TestSanitizePath_Backslash(t *testing.T) {
	result := sanitizePath("C:\\Windows\\System32")
	if strings.Contains(result, "\\") {
		t.Errorf("expected backslashes replaced, got %q", result)
	}
}

func TestSanitizePath_AllSpecialChars(t *testing.T) {
	specials := []rune{'/', '\\', ':', '*', '?', '"', '<', '>', '|'}
	for _, c := range specials {
		input := string(c)
		result := sanitizePath(input)
		if result != "_" {
			t.Errorf("expected %q to be replaced with '_', got %q", string(c), result)
		}
	}
}

func TestSanitizePath_EmptyString(t *testing.T) {
	result := sanitizePath("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSanitizePath_MixedChars(t *testing.T) {
	result := sanitizePath("/home/user:project")
	// slashes and colon become underscores, letters stay
	if strings.ContainsAny(result, "/:\\") {
		t.Errorf("special chars not replaced in %q", result)
	}
	if !strings.Contains(result, "home") {
		t.Errorf("expected 'home' to be preserved in %q", result)
	}
}

// --- headlessStoreDir ---

func TestHeadlessStoreDir_UnderHomeHuginn(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	dir := headlessStoreDir("/some/repo/root")
	expected := filepath.Join(home, ".huginn", "store")
	if !strings.HasPrefix(dir, expected) {
		t.Errorf("expected store dir under %q, got %q", expected, dir)
	}
}

func TestHeadlessStoreDir_MaxNameLength(t *testing.T) {
	// With a very long root path, the store name must be capped at 64 chars.
	longPath := "/" + strings.Repeat("a", 200)
	dir := headlessStoreDir(longPath)
	name := filepath.Base(dir)
	if len(name) > 64 {
		t.Errorf("store dir name exceeds 64 chars: %d (%q)", len(name), name)
	}
}

func TestHeadlessStoreDir_ShortPath(t *testing.T) {
	// Short paths should not be truncated.
	dir := headlessStoreDir("/short")
	name := filepath.Base(dir)
	if len(name) == 0 {
		t.Error("store dir name must not be empty for short path")
	}
}

func TestHeadlessStoreDir_DifferentRoots(t *testing.T) {
	d1 := headlessStoreDir("/repo/a")
	d2 := headlessStoreDir("/repo/b")
	if d1 == d2 {
		t.Errorf("different roots should yield different store dirs: %q vs %q", d1, d2)
	}
}

// --- getGitHead / getGitBranch fallback when not a git repo ---

func TestGetGitHead_NonGitDir_ReturnsFallback(t *testing.T) {
	dir := t.TempDir()
	result := getGitHead(dir)
	if result == "" {
		t.Error("expected non-empty fallback from getGitHead in non-git dir")
	}
	// The fallback is "HEAD"
	if result != "HEAD" {
		// Could also be a real SHA if the temp dir happens to be inside a git repo.
		// Just ensure non-empty.
		if len(result) == 0 {
			t.Error("getGitHead returned empty string")
		}
	}
}

func TestGetGitBranch_NonGitDir_ReturnsFallback(t *testing.T) {
	dir := t.TempDir()
	result := getGitBranch(dir)
	if result == "" {
		t.Error("expected non-empty fallback from getGitBranch in non-git dir")
	}
}

// --- getChangedFiles ---

func TestGetChangedFiles_NonGitDir_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	files := getChangedFiles(dir)
	// Must return a non-nil slice (may be empty).
	if files == nil {
		t.Error("expected non-nil slice from getChangedFiles for non-git dir")
	}
}

func TestGetChangedFiles_NoEmptyStrings(t *testing.T) {
	dir := t.TempDir()
	files := getChangedFiles(dir)
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			t.Error("getChangedFiles must not return empty-string entries")
		}
	}
}

// --- FindingSummary / RunResult struct fields ---

func TestFindingSummary_JSONFields(t *testing.T) {
	fs := FindingSummary{
		ID:       "f1",
		Type:     "high-impact",
		Title:    "Some Finding",
		Severity: "high",
		Score:    9.5,
		Files:    []string{"main.go"},
	}
	if fs.ID != "f1" {
		t.Errorf("unexpected ID: %q", fs.ID)
	}
	if fs.Score != 9.5 {
		t.Errorf("unexpected Score: %v", fs.Score)
	}
}

func TestRunResult_DefaultFields(t *testing.T) {
	r := &RunResult{}
	if r.Mode != "" {
		t.Error("expected empty Mode on zero value")
	}
	if r.FilesScanned != 0 {
		t.Error("expected 0 FilesScanned on zero value")
	}
	if r.Errors != nil {
		t.Error("expected nil Errors on zero value")
	}
}

// --- HeadlessConfig ---

func TestHeadlessConfig_Fields(t *testing.T) {
	cfg := HeadlessConfig{
		CWD:     "/tmp",
		Command: "/radar run",
		JSON:    true,
	}
	if cfg.CWD != "/tmp" {
		t.Errorf("unexpected CWD: %q", cfg.CWD)
	}
	if !cfg.JSON {
		t.Error("expected JSON=true")
	}
}

// --- Run: smoke test with temp directory (plain mode, no git) ---

func TestRun_PlainDir_NoError(t *testing.T) {
	dir := t.TempDir()
	// Write a trivial Go file so indexing has something to scan.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := HeadlessConfig{
		CWD:     dir,
		Command: "",
		JSON:    false,
	}

	result, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil result")
	}
	// Mode should be "plain" for a non-git, non-workspace directory.
	if result.Mode == "" {
		t.Error("expected non-empty Mode in RunResult")
	}
	if result.Root == "" {
		t.Error("expected non-empty Root in RunResult")
	}
}

func TestRun_EmptyCWD_UsesCurrentDir(t *testing.T) {
	// CWD="" means os.Getwd(); just verify it doesn't error out.
	cfg := HeadlessConfig{
		CWD:     "",
		Command: "",
		JSON:    false,
	}

	result, err := Run(cfg)
	// It's acceptable to get errors (store or index) but Run itself must not panic.
	if err != nil {
		t.Fatalf("Run with empty CWD returned hard error: %v", err)
	}
	if result == nil {
		t.Fatal("Run with empty CWD returned nil result")
	}
}

func TestRun_RadarCommand_PlainDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := HeadlessConfig{
		CWD:     dir,
		Command: "/radar run",
		JSON:    false,
	}

	result, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run (radar) returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Run (radar) returned nil result")
	}
	// Radar may add an error entry (e.g. store DB nil) but must not panic.
}
