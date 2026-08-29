package main

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	huginnagent "github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// initVet wires the productized vet loop into tm's existing OnStatusChange
// hook — same extension point and same one-line-integration shape as
// initCheckpoints (init_checkpoint.go). No changes to threadmgr's spawn
// path are needed: threadmgr already calls Complete → fireStatusChange
// exactly once per thread, and Complete guards against a second StatusDone
// transition, so this hook fires at most once per thread — that guard,
// AttachVetResult's own idempotence check, and the fact that a vet pass
// never creates a Thread (see internal/agent.RunVetPass) together make
// recursion structurally impossible, not just discouraged.
//
// gitDiffTimeout bounds the best-effort `git diff` capture; vetBackend
// resolves the SAME backend a normal thread for that agent would use
// (falls back to defaultBackend when resolution is unset or fails), so the
// reviewer runs on the owning agent's own model — works on a 14b local
// model like any other run.
func initVet(sandboxRoot string, agentReg *agents.AgentRegistry, tm *threadmgr.ThreadManager, resolveBackend func(provider, endpoint, apiKey, model string) (backend.Backend, error), defaultBackend backend.Backend) func() {
	return tm.OnStatusChange(func(id string, status threadmgr.ThreadStatus) {
		if status != threadmgr.StatusDone || agentReg == nil {
			return
		}
		t, ok := tm.Get(id)
		if !ok || t.Summary == nil || len(t.Summary.FilesModified) == 0 {
			return // no file changes to review — nothing to vet
		}
		ag, found := agentReg.ByName(t.AgentID)
		if !found || !ag.VetWork {
			return // vet_work off for this agent (default unless strict-reviewer)
		}

		go func() {
			diff := captureDiff(sandboxRoot, t.Summary.FilesModified)

			b := defaultBackend
			if resolveBackend != nil && ag.Provider != "" {
				if resolved, err := resolveBackend(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID()); err == nil {
					b = resolved
				} else {
					slog.Warn("vet: backend resolution failed, using default", "thread_id", id, "agent", ag.Name, "err", err)
				}
			}

			result := huginnagent.RunVetPass(context.Background(), b, ag, t.Task, diff)
			if result.DidNotComplete() {
				slog.Info("vet: pass did not complete", "thread_id", id, "agent", ag.Name)
			} else {
				slog.Info("vet: pass complete", "thread_id", id, "agent", ag.Name, "label", result.Label)
			}
			tm.AttachVetResult(id, result.Label, result.Findings)
		}()
	})
}

// gitDiffTimeout bounds the best-effort diff capture so a slow/hung git
// process can never hold up thread completion.
const gitDiffTimeout = 5 * time.Second

// captureDiff runs `git -C sandboxRoot diff -- <files>` scoped to the
// thread's modified files. Returns "" on any failure (not a git repo, git
// missing, no changes left in the working tree) — RunVetPass degrades
// gracefully to reviewing the task description alone rather than failing.
func captureDiff(sandboxRoot string, files []string) string {
	if sandboxRoot == "" || len(files) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()
	args := append([]string{"-C", sandboxRoot, "diff", "--"}, files...)
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
