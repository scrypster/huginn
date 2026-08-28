package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// git_commit stages with `git add -u`, which never picks up untracked files.
// That is the right default (it keeps scratch files and secrets out of a
// commit), but it means a brand-new file the agent just created is NOT in the
// commit. Reporting a bare "committed abc1234" in that situation reads as
// total success and silently drops the agent's own new work — so the result
// must name what was left out.
func TestGitCommitTool_DisclosesUntrackedFilesLeftOut(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# modified\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// A file the agent just created — untracked, therefore not committed.
	if err := os.WriteFile(filepath.Join(dir, "brand_new_feature.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &GitCommitTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"message": "test commit"})
	if result.IsError {
		t.Fatalf("unexpected error on commit: %s", result.Error)
	}

	if !strings.Contains(result.Output, "brand_new_feature.go") {
		t.Errorf("commit result must name the untracked file it left out, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "NOT COMMITTED") {
		t.Errorf("commit result must flag that files were left out, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "paths") {
		t.Errorf("commit result should tell the caller how to include the file (`paths`), got: %q", result.Output)
	}
}

// The clean case must stay clean: no untracked files means no warning noise.
func TestGitCommitTool_NoUntrackedWarningWhenNothingLeftOut(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# modified\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &GitCommitTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"message": "test commit"})
	if result.IsError {
		t.Fatalf("unexpected error on commit: %s", result.Error)
	}
	if strings.Contains(result.Output, "NOT COMMITTED") {
		t.Errorf("no untracked files, so no omission warning expected, got: %q", result.Output)
	}
}

// When the new file IS named in `paths`, it gets committed and must not then
// be reported as left out.
func TestGitCommitTool_NamedNewFileIsNotReportedAsOmitted(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "brand_new_feature.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tool := &GitCommitTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"message": "add feature",
		"paths":   []any{"brand_new_feature.go"},
	})
	if result.IsError {
		t.Fatalf("unexpected error on commit: %s", result.Error)
	}
	if strings.Contains(result.Output, "NOT COMMITTED") {
		t.Errorf("named new file was committed, so nothing should be reported omitted, got: %q", result.Output)
	}

	out, _, err := runGit(context.Background(), dir, "show", "--stat", "HEAD")
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	if !strings.Contains(out, "brand_new_feature.go") {
		t.Errorf("expected the named new file in the commit, got: %s", out)
	}
}

// The schema is what the model reads before deciding whether to pass `paths`.
// It previously claimed omitting paths "stages all", which is now false and
// is exactly what would lead a model to lose its own new files.
func TestGitCommitTool_SchemaDescribesUntrackedBehaviourHonestly(t *testing.T) {
	tool := &GitCommitTool{SandboxRoot: t.TempDir()}
	desc := tool.Schema().Function.Parameters.Properties["paths"].Description
	if strings.Contains(desc, "stages all if omitted") {
		t.Errorf("schema still claims omitting paths stages all; it does not (git add -u), got: %q", desc)
	}
	if !strings.Contains(desc, "untracked") {
		t.Errorf("schema must tell the caller untracked files need naming, got: %q", desc)
	}
}
