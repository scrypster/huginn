package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests that do NOT require gh in PATH.

func TestGHTools_Names(t *testing.T) {
	tools := []Tool{
		&GHPRListTool{},
		&GHPRViewTool{},
		&GHPRDiffTool{},
		&GHPRCreateTool{},
		&GHIssueListTool{},
		&GHIssueViewTool{},
		&GHIssueCreateTool{},
	}
	wantNames := []string{
		"gh_pr_list", "gh_pr_view", "gh_pr_diff", "gh_pr_create",
		"gh_issue_list", "gh_issue_view", "gh_issue_create",
	}
	for i, tool := range tools {
		if tool.Name() != wantNames[i] {
			t.Errorf("tool[%d].Name() = %q, want %q", i, tool.Name(), wantNames[i])
		}
	}
}

func TestGHTools_Permissions(t *testing.T) {
	reads := []Tool{&GHPRListTool{}, &GHPRViewTool{}, &GHPRDiffTool{}, &GHIssueListTool{}, &GHIssueViewTool{}}
	for _, tool := range reads {
		if tool.Permission() != PermRead {
			t.Errorf("%s should be PermRead", tool.Name())
		}
	}
	writes := []Tool{&GHPRCreateTool{}, &GHIssueCreateTool{}}
	for _, tool := range writes {
		if tool.Permission() != PermWrite {
			t.Errorf("%s should be PermWrite", tool.Name())
		}
	}
}

func TestGHPRViewTool_MissingNumber(t *testing.T) {
	tool := &GHPRViewTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
	if !strings.Contains(result.Error, "number") {
		t.Errorf("error should mention 'number', got %q", result.Error)
	}
}

func TestGHPRDiffTool_MissingNumber(t *testing.T) {
	tool := &GHPRDiffTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
}

func TestGHPRCreateTool_MissingTitle(t *testing.T) {
	tool := &GHPRCreateTool{}
	result := tool.Execute(context.Background(), map[string]any{"body": "some body"})
	if !result.IsError {
		t.Error("expected error for missing title")
	}
	if !strings.Contains(result.Error, "title") {
		t.Errorf("error should mention 'title', got %q", result.Error)
	}
}

func TestGHIssueViewTool_MissingNumber(t *testing.T) {
	tool := &GHIssueViewTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
}

func TestGHIssueCreateTool_MissingTitle(t *testing.T) {
	tool := &GHIssueCreateTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing title")
	}
}

func TestIntArg_Float64(t *testing.T) {
	args := map[string]any{"n": float64(42)}
	got, ok := intArg(args, "n")
	if !ok || got != 42 {
		t.Errorf("intArg float64: got (%d, %v), want (42, true)", got, ok)
	}
}

func TestIntArg_Int(t *testing.T) {
	args := map[string]any{"n": 7}
	got, ok := intArg(args, "n")
	if !ok || got != 7 {
		t.Errorf("intArg int: got (%d, %v), want (7, true)", got, ok)
	}
}

func TestIntArg_Missing(t *testing.T) {
	_, ok := intArg(map[string]any{}, "n")
	if ok {
		t.Error("intArg missing: expected false")
	}
}

func TestRegisterGitHubTools_SkipsWhenGHNotInPath(t *testing.T) {
	// We can't guarantee gh is absent on the test machine, so we test the
	// ghAvailable() function behavior by verifying RegisterGitHubTools does
	// not panic regardless of gh availability.
	reg := NewRegistry()
	// Should not panic.
	RegisterGitHubTools(reg, "/tmp")
	// If gh is in PATH, tools should be registered; if not, registry stays empty.
	// Both outcomes are valid — we just verify no panic.
}

func TestRegisterGitHubTools_CountsTools(t *testing.T) {
	binDir := t.TempDir()
	fakeGH := filepath.Join(binDir, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Inject the fake gh into PATH
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := NewRegistry()
	RegisterGitHubTools(reg, "/tmp")

	registered := reg.All()
	// GHPRListTool, GHPRViewTool, GHPRDiffTool, GHPRCreateTool, GHPRChecksTool,
	// GHPRCommentTool, GHRunViewFailedTool, GHIssueListTool, GHIssueViewTool,
	// GHIssueCreateTool. Deliberately no pr-merge tool: humans merge.
	expectedCount := 10
	if len(registered) != expectedCount {
		t.Errorf("expected %d tools to be registered when gh is in PATH, got %d", expectedCount, len(registered))
	}
}

func TestRegisterGitHubTools_ZeroWhenGHAbsent(t *testing.T) {
	// Set PATH to an empty directory so gh cannot be found
	t.Setenv("PATH", t.TempDir())

	reg := NewRegistry()
	RegisterGitHubTools(reg, "/tmp")

	if len(reg.All()) != 0 {
		t.Errorf("expected 0 tools when gh not in PATH, got %d", len(reg.All()))
	}
}

// Integration tests — only run if gh is in PATH.

func TestGHPRListTool_Integration(t *testing.T) {
	if !ghAvailable() {
		t.Skip("gh not in PATH")
	}
	tool := &GHPRListTool{}
	result := tool.Execute(context.Background(), map[string]any{"state": "open", "limit": float64(5)})
	// Even if there are no PRs the result should be valid JSON array.
	if result.IsError {
		// gh may error if not authenticated, no remotes, or GH_TOKEN not set (CI).
		if strings.Contains(result.Error, "authentication") ||
			strings.Contains(result.Error, "not logged") ||
			strings.Contains(result.Error, "no git remotes") ||
			strings.Contains(result.Error, "GH_TOKEN") {
			t.Skip("gh not usable in this repo context")
		}
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestGHIssueListTool_Integration(t *testing.T) {
	if !ghAvailable() {
		t.Skip("gh not in PATH")
	}
	tool := &GHIssueListTool{}
	result := tool.Execute(context.Background(), map[string]any{"state": "open"})
	if result.IsError {
		if strings.Contains(result.Error, "authentication") ||
			strings.Contains(result.Error, "not logged") ||
			strings.Contains(result.Error, "no git remotes") ||
			strings.Contains(result.Error, "GH_TOKEN") {
			t.Skip("gh not usable in this repo context")
		}
		t.Errorf("unexpected error: %s", result.Error)
	}
}

// Hermetic tests — do NOT require gh in PATH.

// fakeGHBinary creates a fake gh binary in a temp directory that outputs the given stdout and stderr,
// then exits with the given code. Returns the absolute path to the fake binary.
func fakeGHBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "gh")

	// Create a shell script that mimics gh behavior
	script := fmt.Sprintf(`#!/bin/sh
echo %q >&1
echo %q >&2
exit %d
`, stdout, stderr, exitCode)

	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake gh binary: %v", err)
	}
	return binPath
}

func TestGHPRListTool_Execute_HappyPath(t *testing.T) {
	// Create a fake gh binary that returns valid JSON.
	jsonOutput := `[{"number":1,"title":"Fix bug","state":"open","url":"https://github.com/owner/repo/pull/1","headRefName":"fix-bug","author":{"login":"alice"}}]`
	ghPath := fakeGHBinary(t, jsonOutput, "", 0)

	tool := NewGHPRListTool(ghPath)
	result := tool.Execute(context.Background(), map[string]any{"state": "open", "limit": float64(30)})

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Fix bug") {
		t.Errorf("expected output to contain 'Fix bug', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "github.com") {
		t.Errorf("expected output to contain JSON URL, got: %s", result.Output)
	}
}

func TestGHPRListTool_Execute_StderrNotInOutput(t *testing.T) {
	// Create a fake gh binary that exits 0, writes JSON to stdout, but also writes to stderr.
	jsonOutput := `[{"number":2,"title":"Add feature","state":"open","url":"https://github.com/owner/repo/pull/2","headRefName":"feature","author":{"login":"bob"}}]`
	stderrOutput := "warning: rate limit approaching"
	ghPath := fakeGHBinary(t, jsonOutput, stderrOutput, 0)

	tool := NewGHPRListTool(ghPath)
	result := tool.Execute(context.Background(), map[string]any{"state": "open", "limit": float64(30)})

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if strings.Contains(result.Output, "warning: rate limit") {
		t.Errorf("stderr should NOT appear in output, but got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Add feature") {
		t.Errorf("expected output to contain 'Add feature', got: %s", result.Output)
	}
}

func TestGHPRListTool_Execute_CommandFailure(t *testing.T) {
	// Create a fake gh binary that exits 1 and writes an error message to stderr.
	stderrOutput := "authentication error: token is invalid or expired"
	ghPath := fakeGHBinary(t, "", stderrOutput, 1)

	tool := NewGHPRListTool(ghPath)
	result := tool.Execute(context.Background(), map[string]any{"state": "open", "limit": float64(30)})

	if !result.IsError {
		t.Errorf("expected error, but got success")
	}
	if !strings.Contains(result.Error, "authentication error") {
		t.Errorf("expected error to contain stderr message 'authentication error', got: %s", result.Error)
	}
	if !strings.Contains(result.Error, "gh pr list") {
		t.Errorf("expected error to mention 'gh pr list', got: %s", result.Error)
	}
}

// fakeGHBinaryEchoArgs creates a fake gh binary that prints its own working
// directory (line 1) and its argv (line 2), then exits with exitCode.
func fakeGHBinaryEchoArgs(t *testing.T, exitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "gh")
	script := fmt.Sprintf("#!/bin/sh\npwd\necho \"$@\"\nexit %d\n", exitCode)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake gh binary: %v", err)
	}
	return binPath
}

// G3: all gh_* tools must run in SandboxRoot (github.go previously never set
// cmd.Dir, so gh always ran in the process's own cwd instead of the project
// the agent was operating on).
func TestGHPRListTool_RunsInSandboxRoot(t *testing.T) {
	ghPath := fakeGHBinaryEchoArgs(t, 0)
	sandbox := t.TempDir()
	resolvedSandbox, err := filepath.EvalSymlinks(sandbox)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	tool := &GHPRListTool{ghBase: ghBase{GHPath: ghPath, SandboxRoot: sandbox}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	pwdLine := strings.SplitN(result.Output, "\n", 2)[0]
	resolvedPwd, err := filepath.EvalSymlinks(strings.TrimSpace(pwdLine))
	if err != nil {
		t.Fatalf("EvalSymlinks(pwd output): %v", err)
	}
	if resolvedPwd != resolvedSandbox {
		t.Errorf("gh ran in %q, want SandboxRoot %q", resolvedPwd, resolvedSandbox)
	}
}

// G3: gh_pr_checks — exit code 8 ("still running") must surface as a
// distinct "pending" state, not a tool error.
func TestGHPRChecksTool_ExitCode8_IsPendingNotError(t *testing.T) {
	ghPath := fakeGHBinary(t, "some-check\tpending\t0s\thttps://example.com", "", ghPRChecksPendingExitCode)
	tool := &GHPRChecksTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(42)})
	if result.IsError {
		t.Errorf("exit code 8 (pending) must not surface as an error, got: %s", result.Error)
	}
	if status, _ := result.Metadata["status"].(string); status != "pending" {
		t.Errorf("expected metadata status=pending, got %v", result.Metadata["status"])
	}
	if !strings.Contains(result.Output, "pending") {
		t.Errorf("expected 'pending' in output, got: %s", result.Output)
	}
}

func TestGHPRChecksTool_ExitCode0_IsPassed(t *testing.T) {
	ghPath := fakeGHBinary(t, "some-check\tpass\t1s\thttps://example.com", "", 0)
	tool := &GHPRChecksTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(42)})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if status, _ := result.Metadata["status"].(string); status != "passed" {
		t.Errorf("expected metadata status=passed, got %v", result.Metadata["status"])
	}
}

func TestGHPRChecksTool_ExitCode1_IsRealError(t *testing.T) {
	// A non-8, non-zero exit (e.g. 1 = checks failed) must remain a real error,
	// distinct from the pending state.
	ghPath := fakeGHBinary(t, "some-check\tfail\t1s\thttps://example.com", "checks failed", 1)
	tool := &GHPRChecksTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(42)})
	if !result.IsError {
		t.Error("expected a real error for exit code 1 (checks failed)")
	}
}

// G3: gh_run_view_failed must wrap `gh run view --log-failed` ONLY — never
// the full --log, which can be enormous.
func TestGHRunViewFailedTool_UsesLogFailedOnly(t *testing.T) {
	ghPath := fakeGHBinaryEchoArgs(t, 0)
	tool := &GHRunViewFailedTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"run_id": "12345"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	argsLine := strings.SplitN(result.Output, "\n", 2)
	if len(argsLine) < 2 {
		t.Fatalf("expected two output lines, got: %q", result.Output)
	}
	gotArgs := argsLine[1]
	if !strings.Contains(gotArgs, "--log-failed") {
		t.Errorf("expected --log-failed in args, got: %q", gotArgs)
	}
	if strings.Contains(gotArgs, "--log ") || strings.HasSuffix(strings.TrimSpace(gotArgs), "--log") {
		t.Errorf("must never pass the full --log flag, got: %q", gotArgs)
	}
	if !strings.Contains(gotArgs, "12345") {
		t.Errorf("expected run_id in args, got: %q", gotArgs)
	}
}

func TestGHRunViewFailedTool_NoRunID_StillUsesLogFailed(t *testing.T) {
	ghPath := fakeGHBinaryEchoArgs(t, 0)
	tool := &GHRunViewFailedTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	argsLine := strings.SplitN(result.Output, "\n", 2)
	if len(argsLine) < 2 || !strings.Contains(argsLine[1], "--log-failed") {
		t.Errorf("expected --log-failed even with no run_id, got: %q", result.Output)
	}
}

// G3: gh_pr_comment.
func TestGHPRCommentTool_MissingNumber(t *testing.T) {
	tool := &GHPRCommentTool{}
	result := tool.Execute(context.Background(), map[string]any{"body": "hi"})
	if !result.IsError {
		t.Error("expected error for missing number")
	}
}

func TestGHPRCommentTool_MissingBody(t *testing.T) {
	tool := &GHPRCommentTool{}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(1)})
	if !result.IsError {
		t.Error("expected error for missing body")
	}
}

func TestGHPRCommentTool_Execute_HappyPath(t *testing.T) {
	ghPath := fakeGHBinary(t, "https://github.com/owner/repo/pull/1#issuecomment-1", "", 0)
	tool := &GHPRCommentTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(1), "body": "nice work"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "issuecomment") {
		t.Errorf("expected comment URL in output, got: %s", result.Output)
	}
}

func TestGHPRCommentTool_Execute_CommandFailure(t *testing.T) {
	ghPath := fakeGHBinary(t, "", "pull request not found", 1)
	tool := &GHPRCommentTool{ghBase: ghBase{GHPath: ghPath}}
	result := tool.Execute(context.Background(), map[string]any{"number": float64(999), "body": "hi"})
	if !result.IsError {
		t.Error("expected error")
	}
	if !strings.Contains(result.Error, "pull request not found") {
		t.Errorf("expected stderr in error, got: %s", result.Error)
	}
}

func TestGHTools_NewTools_NamesAndPermissions(t *testing.T) {
	if (&GHPRChecksTool{}).Name() != "gh_pr_checks" {
		t.Error("expected gh_pr_checks")
	}
	if (&GHPRChecksTool{}).Permission() != PermRead {
		t.Error("gh_pr_checks should be PermRead")
	}
	if (&GHRunViewFailedTool{}).Name() != "gh_run_view_failed" {
		t.Error("expected gh_run_view_failed")
	}
	if (&GHRunViewFailedTool{}).Permission() != PermRead {
		t.Error("gh_run_view_failed should be PermRead")
	}
	if (&GHPRCommentTool{}).Name() != "gh_pr_comment" {
		t.Error("expected gh_pr_comment")
	}
	if (&GHPRCommentTool{}).Permission() != PermWrite {
		t.Error("gh_pr_comment should be PermWrite")
	}
}
