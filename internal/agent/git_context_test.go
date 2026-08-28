package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// runGitCmd is a small test helper that shells out to native git, failing the
// test on error. Used to set up remotes/upstreams that go-git alone can't
// easily wire (needed for the FABLE-ADD ahead/behind/default-branch tests).
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestBuildGitContext_NotGitRepo_ReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	result := buildGitContext(tmp)
	if result != "" {
		t.Errorf("expected empty for non-git dir, got %q", result)
	}
}

func TestBuildGitContext_GitRepo_HasBranch(t *testing.T) {
	tmp := t.TempDir()
	r, _ := git.PlainInit(tmp, false)
	w, _ := r.Worktree()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("x"), 0644)
	w.Add("f.txt")
	w.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()}})

	result := buildGitContext(tmp)
	if result == "" {
		t.Error("expected non-empty result for git repo")
	}
	if !strings.Contains(result, "Branch:") {
		t.Errorf("expected Branch: in output, got %q", result)
	}
}

// FABLE-ADD: enrich the git context block with upstream, ahead/behind
// counts, and default branch — compact and structured, no dumps.

func TestBuildGitContext_NoUpstream_NoUpstreamLine(t *testing.T) {
	tmp := t.TempDir()
	r, _ := git.PlainInit(tmp, false)
	w, _ := r.Worktree()
	os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("x"), 0644)
	w.Add("f.txt")
	w.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()}})

	result := buildGitContext(tmp)
	if strings.Contains(result, "Upstream:") {
		t.Errorf("expected no Upstream: line without a configured upstream, got %q", result)
	}
}

func TestBuildGitContext_WithUpstream_ShowsAheadBehind(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	remote := t.TempDir()
	runGitCmd(t, remote, "init", "--bare")

	local := t.TempDir()
	runGitCmd(t, local, "init")
	runGitCmd(t, local, "config", "user.email", "t@t.com")
	runGitCmd(t, local, "config", "user.name", "T")
	os.WriteFile(filepath.Join(local, "f.txt"), []byte("x"), 0644)
	runGitCmd(t, local, "add", ".")
	runGitCmd(t, local, "commit", "-m", "init")
	runGitCmd(t, local, "remote", "add", "origin", remote)
	runGitCmd(t, local, "push", "-u", "origin", "HEAD")

	// One local-only commit — should show as "ahead 1".
	os.WriteFile(filepath.Join(local, "g.txt"), []byte("y"), 0644)
	runGitCmd(t, local, "add", ".")
	runGitCmd(t, local, "commit", "-m", "second")

	result := buildGitContext(local)
	if !strings.Contains(result, "Upstream:") {
		t.Fatalf("expected Upstream: line, got %q", result)
	}
	if !strings.Contains(result, "ahead 1") {
		t.Errorf("expected 'ahead 1' in output, got %q", result)
	}
	if !strings.Contains(result, "behind 0") {
		t.Errorf("expected 'behind 0' in output, got %q", result)
	}
}

func TestBuildGitContext_DefaultBranch_FromOriginHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	remote := t.TempDir()
	runGitCmd(t, remote, "init", "--bare")

	local := t.TempDir()
	runGitCmd(t, local, "init")
	runGitCmd(t, local, "config", "user.email", "t@t.com")
	runGitCmd(t, local, "config", "user.name", "T")
	os.WriteFile(filepath.Join(local, "f.txt"), []byte("x"), 0644)
	runGitCmd(t, local, "add", ".")
	runGitCmd(t, local, "commit", "-m", "init")
	runGitCmd(t, local, "remote", "add", "origin", remote)
	runGitCmd(t, local, "push", "-u", "origin", "HEAD")
	// Simulate what `git remote set-head origin -a` would set up after a clone.
	runGitCmd(t, local, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/master")

	result := buildGitContext(local)
	if !strings.Contains(result, "Default branch:") {
		t.Fatalf("expected Default branch: line, got %q", result)
	}
	if !strings.Contains(result, "master") {
		t.Errorf("expected default branch 'master' in output, got %q", result)
	}
}
