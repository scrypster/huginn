package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/scrypster/huginn/internal/search"
)

func TestContextBuilder_SetSkillsFragment_AppearsInBuildOutput(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	fragment := "You specialize in Go concurrency patterns."
	cb.SetSkillsFragment(fragment)

	result := cb.Build("some query", "test-model")
	if !strings.Contains(result, fragment) {
		t.Errorf("Build() output does not contain skills fragment.\nGot: %q", result)
	}
}

func TestContextBuilder_NoSkillsFragment_BuildOutputUnchanged(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.Build("query", "test-model")
	if strings.Contains(result, "## Skills & Workspace Rules") {
		t.Errorf("Build() output contains skills section when no fragment was set.\nGot: %q", result)
	}
}

func TestContextBuilder_SkillsFragmentSection_HasHeader(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSkillsFragment("some skill content")

	result := cb.Build("query", "test-model")
	if !strings.Contains(result, "## Skills & Workspace Rules") {
		t.Errorf("Build() output missing section header.\nGot: %q", result)
	}
}

func TestContextBuilder_SkillsFragment_UpdatedBetweenCalls(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSkillsFragment("first fragment")
	cb.SetSkillsFragment("second fragment")

	result := cb.Build("query", "test-model")
	if strings.Contains(result, "first fragment") {
		t.Errorf("Build() still contains old fragment after update.\nGot: %q", result)
	}
	if !strings.Contains(result, "second fragment") {
		t.Errorf("Build() does not contain updated fragment.\nGot: %q", result)
	}
}

func TestContextBuilder_EmptySkillsFragment_NoSection(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSkillsFragment("")

	result := cb.Build("query", "test-model")
	if strings.Contains(result, "## Skills & Workspace Rules") {
		t.Errorf("Build() contains skills section for empty fragment.\nGot: %q", result)
	}
}

// TestContextBuilder_WithGitRoot_IncludesGitSection verifies that git context is injected when gitRoot is set.
func TestFormatSearchResults_EmptyChunks_ReturnsEmpty(t *testing.T) {
	result := formatSearchResults(nil, 10000)
	if result != "" {
		t.Errorf("expected empty string for nil chunks, got %q", result)
	}
}

func TestFormatSearchResults_ChunksTooLarge_ReturnsEmpty(t *testing.T) {
	chunks := []search.Chunk{
		{Path: "big.go", StartLine: 1, Content: strings.Repeat("x", 10000)},
	}
	// Budget is tiny (10 bytes) — no chunk can fit
	result := formatSearchResults(chunks, 10)
	if result != "" {
		t.Errorf("expected empty string when no chunks fit, got %q", result)
	}
}

func TestFormatSearchResults_NormalChunks(t *testing.T) {
	chunks := []search.Chunk{
		{Path: "foo.go", StartLine: 5, Content: "func Foo() {}"},
	}
	result := formatSearchResults(chunks, 10000)
	if !strings.Contains(result, "## Repository Context") {
		t.Error("expected header in result")
	}
	if !strings.Contains(result, "foo.go") {
		t.Error("expected file path in result")
	}
}

func TestContextBuilder_WithGitRoot_IncludesGitSection(t *testing.T) {
	// Create a temp git repo
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	// Create and commit a test file
	testFile := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(testFile, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	if _, err := w.Add("f.txt"); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}
	if _, err := w.Commit("init", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Build context with git root
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetGitRoot(dir)
	result := cb.Build("query", "test-model")

	if !strings.Contains(result, "## Git Context") {
		t.Error("expected ## Git Context in Build output")
	}
	if !strings.Contains(result, "Branch:") {
		t.Error("expected Branch: in git context")
	}
}
