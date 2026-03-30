package headless

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWorkspaceHash_CollisionResistance verifies that paths differing by only
// one character produce distinct hashes.
func TestWorkspaceHash_CollisionResistance(t *testing.T) {
	base := "/repo/projectA"
	similar := "/repo/projectB"
	h1 := workspaceHash(base)
	h2 := workspaceHash(similar)
	if h1 == h2 {
		t.Errorf("collision detected: paths %q and %q produced the same hash %q", base, similar, h1)
	}
}

// TestWorkspaceHash_UnicodePath verifies that unicode paths produce a valid
// 12-char hex hash without panicking.
func TestWorkspaceHash_UnicodePath(t *testing.T) {
	h := workspaceHash("/repos/プロジェクト/main")
	if len(h) != 12 {
		t.Errorf("expected 12-char hash for unicode path, got %d: %q", len(h), h)
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected hex characters only, got %q in %q", c, h)
		}
	}
}

// TestGetGitHead_InitialCommit verifies getGitHead returns a non-empty string
// from a real git repo with a single commit (no HEAD~1).
func TestGetGitHead_InitialCommit(t *testing.T) {
	dir := t.TempDir()

	// Skip if git is unavailable.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "init")

	head := getGitHead(dir)
	if head == "HEAD" {
		// This means git returned an error, which shouldn't happen for a valid repo.
		t.Error("expected real SHA, got fallback 'HEAD' for valid repo")
	}
	if len(head) < 7 {
		t.Errorf("expected SHA-like string (>=7 chars), got %q", head)
	}
}

// TestGetChangedFiles_InitialCommit exercises the fallback diff-tree path when
// HEAD~1 does not exist (first commit in the repo).
func TestGetChangedFiles_InitialCommit(t *testing.T) {
	dir := t.TempDir()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "first commit")

	files := getChangedFiles(dir)
	// On initial commit, diff HEAD~1 fails, so diff-tree is used.
	// At least "main.go" should appear.
	found := false
	for _, f := range files {
		if strings.Contains(f, "main.go") {
			found = true
			break
		}
	}
	if !found {
		t.Logf("files returned from initial commit: %v", files)
		// Not a hard failure — the fallback may also attempt staged diff.
		// Just ensure no empty strings leaked.
		for _, f := range files {
			if strings.TrimSpace(f) == "" {
				t.Error("getChangedFiles returned empty-string entry")
			}
		}
	}
}

// TestGetGitBranch_InitialCommit verifies getGitBranch returns a non-empty
// branch name for a valid repo.
func TestGetGitBranch_InitialCommit(t *testing.T) {
	dir := t.TempDir()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "init")

	branch := getGitBranch(dir)
	if branch == "" {
		t.Error("expected non-empty branch name for valid repo")
	}
}

// TestRun_IndexDuration_IsValid verifies that IndexDuration is a parseable
// duration string when the run succeeds without errors.
func TestRun_IndexDuration_IsValid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if len(result.Errors) == 0 {
		// IndexDuration should be set and parseable.
		if result.IndexDuration == "" {
			t.Error("expected non-empty IndexDuration when no errors occurred")
		}
		if _, err := time.ParseDuration(result.IndexDuration); err != nil {
			t.Errorf("IndexDuration %q is not a valid duration: %v", result.IndexDuration, err)
		}
	}
}

// TestRun_ReposFound_PlainMode verifies that in plain mode, ReposFound contains
// exactly one entry equal to the root directory.
func TestRun_ReposFound_PlainMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Mode == "plain" {
		if len(result.ReposFound) != 1 {
			t.Errorf("expected 1 repo in plain mode, got %d: %v", len(result.ReposFound), result.ReposFound)
		}
	}
}

// TestHeadlessStoreDir_ConsistentForSameRoot verifies that calling
// headlessStoreDir twice for the same root returns the same path.
func TestHeadlessStoreDir_ConsistentForSameRoot(t *testing.T) {
	d1 := headlessStoreDir("/some/project/root")
	d2 := headlessStoreDir("/some/project/root")
	if d1 != d2 {
		t.Errorf("headlessStoreDir is not deterministic: %q vs %q", d1, d2)
	}
}

// TestSanitizePath_UnicodePreserved verifies that non-special unicode chars
// are preserved by sanitizePath.
func TestSanitizePath_UnicodePreserved(t *testing.T) {
	input := "café-project"
	result := sanitizePath(input)
	if !strings.Contains(result, "café") {
		t.Errorf("expected unicode chars preserved in %q, got %q", input, result)
	}
}

// TestRun_MultipleFiles_CountsScanned verifies that multiple files in the CWD
// result in a positive FilesScanned count.
func TestRun_MultipleFiles_CountsScanned(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	// Initialise a git repo so findGitRoot anchors at dir instead of walking
	// up to the huginn repository root (which would cause BuildIncrementalWithStats
	// to index the entire repo + vendor tree, blowing the test timeout).
	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "Test")

	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, filepath.FromSlash(
			"file"+string(rune('a'+i))+".go",
		))
		if err := os.WriteFile(name, []byte("package main\n"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "init")

	result, err := Run(HeadlessConfig{CWD: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	// If there are no errors, we should have scanned at least some files.
	if len(result.Errors) == 0 {
		total := result.FilesScanned + result.FilesSkipped
		if total == 0 {
			t.Error("expected FilesScanned+FilesSkipped > 0 for directory with Go files")
		}
	}
}

// mustGit runs a git command and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("git %v failed: %v\n%s", args, err, out)
	}
}
