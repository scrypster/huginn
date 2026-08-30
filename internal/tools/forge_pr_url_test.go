package tools

// G3/G6 PR-as-deliverable: gh_pr_create / glab_mr_create must return a
// parseable PR/MR URL, not just exit 0, so the model can report it and the
// web UI can render a PR card from the persisted tool call.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeForgeBinary writes an executable script (fakePath) that echoes
// stdoutBody to stdout and exits 0, regardless of its arguments — enough to
// exercise the tool's output-parsing path without a real gh/glab CLI or
// network/auth.
func writeFakeForgeBinary(t *testing.T, name, stdoutBody string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI script uses a shebang; not supported on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdoutBody + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return path
}

func TestGHPRCreateTool_ReturnsParseableURL(t *testing.T) {
	fakeGH := writeFakeForgeBinary(t, "gh", "https://github.com/scrypster/huginn/pull/123")
	tool := &GHPRCreateTool{ghBase: ghBase{GHPath: fakeGH, SandboxRoot: t.TempDir()}}

	result := tool.Execute(context.Background(), map[string]any{
		"title": "Add PR card",
		"body":  "Closes the deliverable loop.",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "https://github.com/scrypster/huginn/pull/123") {
		t.Fatalf("Output should contain the PR URL, got %q", result.Output)
	}
	if result.Metadata == nil {
		t.Fatal("expected Metadata to carry the parsed URL")
	}
	if url, _ := result.Metadata["url"].(string); url != "https://github.com/scrypster/huginn/pull/123" {
		t.Errorf("Metadata[url] = %q, want the PR URL", url)
	}
	if num, _ := result.Metadata["number"].(string); num != "123" {
		t.Errorf("Metadata[number] = %q, want %q", num, "123")
	}
}

func TestGHPRCreateTool_URLAmidOtherOutput(t *testing.T) {
	// Real `gh pr create` output can include a leading progress line before
	// the URL even on stdout in some versions — the parser must find the
	// URL regardless of surrounding text.
	fakeGH := writeFakeForgeBinary(t, "gh", "Creating pull request for feature-branch into main in scrypster/huginn\n\nhttps://github.com/scrypster/huginn/pull/456")
	tool := &GHPRCreateTool{ghBase: ghBase{GHPath: fakeGH, SandboxRoot: t.TempDir()}}

	result := tool.Execute(context.Background(), map[string]any{"title": "x", "body": "y"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if url, _ := result.Metadata["url"].(string); url != "https://github.com/scrypster/huginn/pull/456" {
		t.Errorf("Metadata[url] = %q, want the PR URL extracted from mixed output", url)
	}
}

func TestGHPRCreateTool_DescriptionHintsDefaultBranch(t *testing.T) {
	tool := &GHPRCreateTool{DefaultBranch: "develop"}
	if !strings.Contains(tool.Description(), "develop") {
		t.Errorf("Description() should hint the default branch, got %q", tool.Description())
	}
}

func TestGHPRCreateTool_DescriptionNoHintWhenUnknown(t *testing.T) {
	tool := &GHPRCreateTool{}
	if strings.Contains(tool.Description(), "defaults to") {
		t.Errorf("Description() should not claim a default branch when none is known, got %q", tool.Description())
	}
}

func TestGlabMRCreateTool_ReturnsParseableURL(t *testing.T) {
	fakeGlab := writeFakeForgeBinary(t, "glab", "https://gitlab.com/scrypster/huginn/-/merge_requests/789")
	tool := &GlabMRCreateTool{glBase: glBase{GlabPath: fakeGlab, SandboxRoot: t.TempDir()}}

	result := tool.Execute(context.Background(), map[string]any{"title": "Add MR card"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if url, _ := result.Metadata["url"].(string); url != "https://gitlab.com/scrypster/huginn/-/merge_requests/789" {
		t.Errorf("Metadata[url] = %q, want the MR URL", url)
	}
	if num, _ := result.Metadata["number"].(string); num != "789" {
		t.Errorf("Metadata[number] = %q, want %q", num, "789")
	}
}

func TestGlabMRCreateTool_DescriptionHintsDefaultBranch(t *testing.T) {
	tool := &GlabMRCreateTool{DefaultBranch: "main"}
	if !strings.Contains(tool.Description(), "main") {
		t.Errorf("Description() should hint the default branch, got %q", tool.Description())
	}
}

func TestExtractForgeURL_NoMatch(t *testing.T) {
	if got := extractForgeURL("no url here"); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestDetectDefaultBranch_NotARepo(t *testing.T) {
	if got := detectDefaultBranch(t.TempDir()); got != "" {
		t.Errorf("expected empty string for non-repo dir, got %q", got)
	}
}

func TestDetectDefaultBranch_EmptyRoot(t *testing.T) {
	if got := detectDefaultBranch(""); got != "" {
		t.Errorf("expected empty string for empty root, got %q", got)
	}
}
