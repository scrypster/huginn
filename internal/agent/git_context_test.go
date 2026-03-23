package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

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
