package tools

import (
	"context"
	"strings"
	"testing"
)

// git_push interpolates `remote` into an argument position:
//
//	git push -u <remote> <branch>
//
// There is no shell involved (exec.Command takes an arg array), so the only
// injection surface is git's OWN option parsing: a value beginning with "-"
// is read as a flag. git push accepts --receive-pack=<cmd> / --exec=<cmd>,
// which git runs for a local-path remote — so an unvalidated remote name is
// a command-execution primitive, not just a malformed argument.
func TestGitPushTool_RejectsDashLeadingRemote(t *testing.T) {
	dir := initGitRepo(t)
	tool := &GitPushTool{SandboxRoot: dir}

	for _, remote := range []string{
		"--receive-pack=touch /tmp/huginn-pwned",
		"--exec=/bin/sh",
		"-u",
	} {
		result := tool.Execute(context.Background(), map[string]any{"remote": remote})
		if !result.IsError {
			t.Fatalf("remote %q must be rejected, got success: %q", remote, result.Output)
		}
		if !strings.Contains(result.Error, "invalid remote name") {
			t.Errorf("remote %q: expected an invalid-remote-name error, got: %q", remote, result.Error)
		}
	}
}

// A normal remote name must still be accepted (the guard must not reject
// legitimate input). The push itself fails here — the temp repo has no such
// remote — but it must fail for that reason, not the validation guard.
func TestGitPushTool_AllowsOrdinaryRemoteName(t *testing.T) {
	dir := initGitRepo(t)
	tool := &GitPushTool{SandboxRoot: dir}

	result := tool.Execute(context.Background(), map[string]any{"remote": "origin"})
	if result.IsError && strings.Contains(result.Error, "invalid remote name") {
		t.Errorf("ordinary remote name must not trip the dash guard, got: %q", result.Error)
	}
}

// Same argument-position hazard for `gh run view <run_id> --log-failed`.
func TestGHRunViewFailedTool_RejectsDashLeadingRunID(t *testing.T) {
	tool := &GHRunViewFailedTool{ghBase: ghBase{GHPath: noGH}}

	result := tool.Execute(context.Background(), map[string]any{"run_id": "--help"})
	if !result.IsError {
		t.Fatalf("dash-leading run_id must be rejected, got success: %q", result.Output)
	}
	if !strings.Contains(result.Error, "invalid run_id") {
		t.Errorf("expected an invalid-run_id error, got: %q", result.Error)
	}
}
