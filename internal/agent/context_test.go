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

// TestContextBuilder_WithGitRoot_IncludesProjectInstructions verifies that
// .huginn.md project instructions are injected into every Build() output via
// the git root, so every prompt path (web chat, delegated threads, scheduled
// agents) receives them consistently instead of only the one call site that
// used to load them directly.
func TestContextBuilder_WithGitRoot_IncludesProjectInstructions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte("Always use tabs."), 0644); err != nil {
		t.Fatalf("write .huginn.md: %v", err)
	}

	cb := NewContextBuilder(nil, nil, nil)
	cb.SetGitRoot(dir)
	result := cb.Build("query", "test-model")

	if !strings.Contains(result, "## Project Instructions") {
		t.Error("expected ## Project Instructions section in Build output")
	}
	if !strings.Contains(result, "Always use tabs.") {
		t.Error("expected project instructions content in Build output")
	}
}

func TestContextBuilder_NoGitRoot_NoProjectInstructionsSection(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.Build("query", "test-model")

	if strings.Contains(result, "## Project Instructions") {
		t.Error("expected no ## Project Instructions section without a git root")
	}
}

// TestContextBuilder_WorkspaceRootFallback_NoGitRoot verifies that a plain,
// non-git workspace still gets .huginn.md project instructions when only
// SetWorkspaceRoot (not SetGitRoot) is wired — the fallback path for callers
// that never resolve a git root.
func TestContextBuilder_WorkspaceRootFallback_NoGitRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".huginn.md"), []byte("Plain dir rule."), 0644); err != nil {
		t.Fatalf("write .huginn.md: %v", err)
	}

	cb := NewContextBuilder(nil, nil, nil)
	cb.SetWorkspaceRoot(dir) // no SetGitRoot call
	result := cb.Build("query", "test-model")

	if !strings.Contains(result, "## Project Instructions") {
		t.Error("expected ## Project Instructions section via workspaceRoot fallback")
	}
	if !strings.Contains(result, "Plain dir rule.") {
		t.Error("expected project instructions content via workspaceRoot fallback")
	}
	// No git context section, since no git root was set — only the
	// workspace-root fallback is used for project instructions, not git info.
	if strings.Contains(result, "## Git Context") {
		t.Error("expected no git context section when only workspaceRoot is set")
	}
}

// TestContextBuilder_GitRootTakesPrecedenceOverWorkspaceRoot verifies gitRoot
// wins when both are set to different directories with different files.
func TestContextBuilder_GitRootTakesPrecedenceOverWorkspaceRoot(t *testing.T) {
	gitDir := t.TempDir()
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gitDir, ".huginn.md"), []byte("From git root."), 0644); err != nil {
		t.Fatalf("write .huginn.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".huginn.md"), []byte("From workspace root."), 0644); err != nil {
		t.Fatalf("write .huginn.md: %v", err)
	}

	cb := NewContextBuilder(nil, nil, nil)
	cb.SetWorkspaceRoot(wsDir)
	cb.SetGitRoot(gitDir)
	result := cb.Build("query", "test-model")

	if !strings.Contains(result, "From git root.") {
		t.Error("expected gitRoot's instructions to win")
	}
	if strings.Contains(result, "From workspace root.") {
		t.Error("did not expect workspaceRoot's instructions when gitRoot is set")
	}
}
