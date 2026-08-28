package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests that do NOT require glab in PATH.

func TestGlabTools_Names(t *testing.T) {
	tools := []Tool{
		&GlabMRCreateTool{},
		&GlabMRChecksTool{},
		&GlabCIViewFailedTool{},
		&GlabMRCommentTool{},
	}
	wantNames := []string{
		"glab_mr_create", "glab_mr_checks", "glab_ci_view_failed", "glab_mr_comment",
	}
	for i, tool := range tools {
		if tool.Name() != wantNames[i] {
			t.Errorf("tool[%d].Name() = %q, want %q", i, tool.Name(), wantNames[i])
		}
	}
}

func TestGlabTools_Permissions(t *testing.T) {
	reads := []Tool{&GlabMRChecksTool{}, &GlabCIViewFailedTool{}}
	for _, tool := range reads {
		if tool.Permission() != PermRead {
			t.Errorf("%s should be PermRead", tool.Name())
		}
	}
	writes := []Tool{&GlabMRCreateTool{}, &GlabMRCommentTool{}}
	for _, tool := range writes {
		if tool.Permission() != PermWrite {
			t.Errorf("%s should be PermWrite", tool.Name())
		}
	}
}

func TestGlabMRCreateTool_MissingTitle(t *testing.T) {
	tool := &GlabMRCreateTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing title")
	}
	if !strings.Contains(result.Error, "title") {
		t.Errorf("error should mention 'title', got %q", result.Error)
	}
}

func TestGlabMRCommentTool_MissingMR(t *testing.T) {
	tool := &GlabMRCommentTool{}
	result := tool.Execute(context.Background(), map[string]any{"body": "hi"})
	if !result.IsError {
		t.Error("expected error for missing mr")
	}
	if !strings.Contains(result.Error, "mr") {
		t.Errorf("error should mention 'mr', got %q", result.Error)
	}
}

func TestGlabMRCommentTool_MissingBody(t *testing.T) {
	tool := &GlabMRCommentTool{}
	result := tool.Execute(context.Background(), map[string]any{"mr": float64(1)})
	if !result.IsError {
		t.Error("expected error for missing body")
	}
	if !strings.Contains(result.Error, "body") {
		t.Errorf("error should mention 'body', got %q", result.Error)
	}
}

func TestRegisterGitLabTools_SkipsWhenGlabNotInPath(t *testing.T) {
	reg := NewRegistry()
	// Should not panic regardless of glab availability on the test machine.
	RegisterGitLabTools(reg, "/tmp")
}

func TestRegisterGitLabTools_CountsTools(t *testing.T) {
	binDir := t.TempDir()
	fakeGlab := filepath.Join(binDir, "glab")
	if err := os.WriteFile(fakeGlab, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := NewRegistry()
	RegisterGitLabTools(reg, "/tmp")

	// GlabMRCreateTool, GlabMRChecksTool, GlabCIViewFailedTool, GlabMRCommentTool.
	// Deliberately no mr-merge tool, matching gh: humans merge.
	expectedCount := 4
	if len(reg.All()) != expectedCount {
		t.Errorf("expected %d tools to be registered when glab is in PATH, got %d", expectedCount, len(reg.All()))
	}
}

func TestRegisterGitLabTools_ZeroWhenGlabAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	reg := NewRegistry()
	RegisterGitLabTools(reg, "/tmp")

	if len(reg.All()) != 0 {
		t.Errorf("expected 0 tools when glab not in PATH, got %d", len(reg.All()))
	}
}

func TestGitLabCLIToolNames_MatchesRegistered(t *testing.T) {
	binDir := t.TempDir()
	fakeGlab := filepath.Join(binDir, "glab")
	if err := os.WriteFile(fakeGlab, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := NewRegistry()
	RegisterGitLabTools(reg, "/tmp")

	names := GitLabCLIToolNames()
	if len(names) != len(reg.All()) {
		t.Fatalf("GitLabCLIToolNames() has %d entries, registry has %d tools", len(names), len(reg.All()))
	}
	for _, name := range names {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("GitLabCLIToolNames() lists %q but it is not registered", name)
		}
	}
}

// --- Hermetic tests via fake glab binary ---

// fakeGlabBinary creates a fake glab binary in a temp directory that outputs
// the given stdout/stderr, then exits with the given code.
func fakeGlabBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "glab")
	script := fmt.Sprintf(`#!/bin/sh
echo %q >&1
echo %q >&2
exit %d
`, stdout, stderr, exitCode)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake glab binary: %v", err)
	}
	return binPath
}

// fakeGlabBinaryEchoArgs creates a fake glab binary that prints its own
// working directory (line 1) and its argv (line 2), then exits with exitCode.
func fakeGlabBinaryEchoArgs(t *testing.T, exitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "glab")
	script := fmt.Sprintf("#!/bin/sh\npwd\necho \"$@\"\nexit %d\n", exitCode)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake glab binary: %v", err)
	}
	return binPath
}

func TestGlabMRCreateTool_Execute_HappyPath(t *testing.T) {
	glabPath := fakeGlabBinary(t, "https://gitlab.com/owner/repo/-/merge_requests/1", "", 0)
	tool := NewGlabMRCreateTool(glabPath)
	result := tool.Execute(context.Background(), map[string]any{"title": "Fix bug"})
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "merge_requests") {
		t.Errorf("expected output to contain MR URL, got: %s", result.Output)
	}
}

func TestGlabMRCreateTool_Execute_CommandFailure(t *testing.T) {
	glabPath := fakeGlabBinary(t, "", "authentication error: token invalid", 1)
	tool := NewGlabMRCreateTool(glabPath)
	result := tool.Execute(context.Background(), map[string]any{"title": "Fix bug"})
	if !result.IsError {
		t.Error("expected error, got success")
	}
	if !strings.Contains(result.Error, "authentication error") {
		t.Errorf("expected error to mention stderr, got: %s", result.Error)
	}
}

// G3-equivalent: all glab_* tools must run in SandboxRoot, same discipline
// as gh_* (see ghBase.command / TestGHPRListTool_RunsInSandboxRoot).
func TestGlabMRCreateTool_RunsInSandboxRoot(t *testing.T) {
	glabPath := fakeGlabBinaryEchoArgs(t, 0)
	sandbox := t.TempDir()
	resolvedSandbox, err := filepath.EvalSymlinks(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	tool := &GlabMRCreateTool{glBase: glBase{GlabPath: glabPath, SandboxRoot: sandbox}}
	result := tool.Execute(context.Background(), map[string]any{"title": "t"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) == 0 {
		t.Fatal("expected output")
	}
	gotDir, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != resolvedSandbox {
		t.Errorf("cmd.Dir = %q, want %q", gotDir, resolvedSandbox)
	}
}

func TestGlabMRChecksTool_Execute_HappyPath(t *testing.T) {
	glabPath := fakeGlabBinary(t, "Pipeline Status: success\nStage: build passed", "", 0)
	tool := &GlabMRChecksTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Metadata["status"] != "passed" {
		t.Errorf("expected status=passed, got %v", result.Metadata["status"])
	}
	if result.Metadata["status_is_heuristic"] != true {
		t.Error("expected status_is_heuristic=true — glab has no exit-8-style contract like gh")
	}
}

func TestGlabMRChecksTool_Execute_FailedPipeline(t *testing.T) {
	glabPath := fakeGlabBinary(t, "Pipeline Status: failed\nStage: test failed", "", 0)
	tool := &GlabMRChecksTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Metadata["status"] != "failed" {
		t.Errorf("expected status=failed, got %v", result.Metadata["status"])
	}
}

func TestGlabMRChecksTool_Execute_RunningPipeline_ExitsZero(t *testing.T) {
	// Unlike gh pr checks (exit code 8 for pending), glab exits 0 here too —
	// this test pins that glab_mr_checks does NOT treat exit code as a
	// pending signal; it relies purely on text classification.
	glabPath := fakeGlabBinary(t, "Pipeline Status: running\nStage: build running", "", 0)
	tool := &GlabMRChecksTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Metadata["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", result.Metadata["status"])
	}
}

func TestGlabMRChecksTool_Execute_CommandFailure_IsRealError(t *testing.T) {
	glabPath := fakeGlabBinary(t, "", "no merge request found for branch", 1)
	tool := &GlabMRChecksTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected a real error for a non-zero glab exit, no tri-state pending here")
	}
}

func TestClassifyGlabPipelineStatus(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"passed", "Pipeline Status: success", "passed"},
		{"passed-word", "the pipeline passed", "passed"},
		{"failed", "Pipeline Status: failed", "failed"},
		{"failure-word", "job failure detected", "failed"},
		{"running", "Pipeline Status: running", "pending"},
		{"pending-word", "status: pending", "pending"},
		{"waiting", "waiting_for_resource", "pending"},
		{"unknown", "some unrecognized text", "unknown"},
		{"empty", "", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyGlabPipelineStatus(tc.in)
			if got != tc.want {
				t.Errorf("classifyGlabPipelineStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGlabMRCommentTool_Execute_HappyPath(t *testing.T) {
	glabPath := fakeGlabBinary(t, "note created", "", 0)
	tool := &GlabMRCommentTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{"mr": float64(3), "body": "LGTM"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "note created") {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestGlabMRCommentTool_Execute_CommandFailure(t *testing.T) {
	glabPath := fakeGlabBinary(t, "", "merge request not found", 1)
	tool := &GlabMRCommentTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{"mr": float64(3), "body": "LGTM"})
	if !result.IsError {
		t.Error("expected error")
	}
	if !strings.Contains(result.Error, "merge request not found") {
		t.Errorf("expected stderr in error, got: %s", result.Error)
	}
}

func TestGlabCIViewFailedTool_NoFailedJobsFound_FallsBackToRawOutput(t *testing.T) {
	glabPath := fakeGlabBinary(t, `{"pipeline":{"status":"success"}}`, "", 0)
	tool := &GlabCIViewFailedTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "success") {
		t.Errorf("expected raw fallback output, got: %s", result.Output)
	}
}

func TestGlabCIViewFailedTool_CommandFailure(t *testing.T) {
	glabPath := fakeGlabBinary(t, "", "pipeline not found", 1)
	tool := &GlabCIViewFailedTool{glBase: glBase{GlabPath: glabPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error")
	}
}

func TestFindFailedJobIDs_SimpleShape(t *testing.T) {
	raw := `{"jobs":[{"id":101,"status":"failed","name":"test"},{"id":102,"status":"success","name":"build"}]}`
	ids := findFailedJobIDs(raw)
	if len(ids) != 1 || ids[0] != 101 {
		t.Errorf("got %v, want [101]", ids)
	}
}

func TestFindFailedJobIDs_NestedShape(t *testing.T) {
	raw := `{"pipeline":{"jobs":{"nodes":[{"id":5,"status":"FAILED"},{"id":6,"status":"success"}]}}}`
	ids := findFailedJobIDs(raw)
	if len(ids) != 1 || ids[0] != 5 {
		t.Errorf("got %v, want [5] (case-insensitive FAILED status)", ids)
	}
}

func TestFindFailedJobIDs_NoMatches(t *testing.T) {
	raw := `{"jobs":[{"id":1,"status":"success"}]}`
	ids := findFailedJobIDs(raw)
	if len(ids) != 0 {
		t.Errorf("got %v, want none", ids)
	}
}

func TestFindFailedJobIDs_InvalidJSON(t *testing.T) {
	ids := findFailedJobIDs("not json at all")
	if ids != nil {
		t.Errorf("got %v, want nil for invalid JSON", ids)
	}
}

func TestGlabCIViewFailedTool_FetchesTraceForFailedJobs(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "glab")
	script := `#!/bin/sh
if [ "$1" = "ci" ] && [ "$2" = "get" ]; then
  echo '{"jobs":[{"id":42,"status":"failed","name":"test"}]}'
  exit 0
fi
if [ "$1" = "ci" ] && [ "$2" = "trace" ]; then
  echo "log for job $3"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	tool := &GlabCIViewFailedTool{glBase: glBase{GlabPath: binPath}}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "job 42") {
		t.Errorf("expected trace output for job 42, got: %s", result.Output)
	}
}

func TestFindFailedJobIDs_MultipleFailedJobs(t *testing.T) {
	raw := `[{"id":1,"status":"failed"},{"id":2,"status":"failed"},{"id":3,"status":"failed"}]`
	ids := findFailedJobIDs(raw)
	if len(ids) != 3 {
		t.Errorf("got %v, want 3 ids", ids)
	}
}
