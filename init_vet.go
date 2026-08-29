package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
			// recover() so a panic in diff capture, backend resolution, or
			// the reviewer pass itself can never crash the process — this
			// goroutine is fire-and-forget off the OnStatusChange hook, with
			// nothing downstream watching it. context.Background() (not a
			// server-lifecycle ctx) is deliberate: no such ctx is plumbed
			// into this wiring call in main.go, and the pass is already
			// bounded independently — RunVetPass wraps it in its own
			// context.WithTimeout(ctx, VetTimeout), so an unbounded parent
			// context can never let it run past VetTimeout regardless.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("vet: reviewer goroutine panicked", "thread_id", id, "agent", ag.Name, "panic", r)
				}
			}()

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
			} else if result.NotVetted() {
				slog.Info("vet: skipped — no diff captured", "thread_id", id, "agent", ag.Name)
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

// untrackedContentLineCap bounds how much of a newly-created (untracked)
// file gets embedded in the review input — enough for a real review,
// small enough to never blow up the reviewer's context on a huge file.
const untrackedContentLineCap = 200

// captureDiff builds the review input for the thread's modified files,
// combining three sources so a real change is never silently invisible to
// the reviewer:
//
//  1. `git diff HEAD -- <file>` — catches ordinary working-tree edits AND
//     anything the agent `git add`ed but didn't commit (plain `git diff`
//     alone misses staged content).
//  2. For a file `git diff HEAD` has nothing to say about, check whether
//     it's tracked. If not, it's a file the run CREATED — plain `git diff`
//     is structurally blind to untracked files, so embed the file's own
//     content (bounded to untrackedContentLineCap lines) instead of a diff.
//  3. If it IS tracked but still has nothing to show, the agent most
//     likely COMMITTED the change — fall back to `git show HEAD -- <file>`,
//     the diff introduced by the most recent commit touching that path.
//
// Returns "" only when every file yields nothing from all three sources —
// RunVetPass's honesty backstop treats that as "not vetted" rather than
// letting a PASS verdict be printed over a review that never saw any code.
func captureDiff(sandboxRoot string, files []string) string {
	if sandboxRoot == "" || len(files) == 0 {
		return ""
	}
	var parts []string
	for _, f := range files {
		if part := captureFileDiff(sandboxRoot, f); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func captureFileDiff(sandboxRoot, file string) string {
	if diff := runGit(sandboxRoot, "diff", "HEAD", "--", file); diff != "" {
		return diff
	}
	if !isTrackedFile(sandboxRoot, file) {
		return untrackedFileContent(sandboxRoot, file)
	}
	if diff := runGit(sandboxRoot, "show", "HEAD", "--", file); diff != "" {
		return diff
	}
	return ""
}

func runGit(sandboxRoot string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()
	full := append([]string{"-C", sandboxRoot}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func isTrackedFile(sandboxRoot, file string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", sandboxRoot, "ls-files", "--error-unmatch", "--", file)
	return cmd.Run() == nil
}

// untrackedFileContent embeds the raw content of a newly-created file
// (bounded to untrackedContentLineCap lines) so the reviewer sees what the
// run actually wrote instead of an empty diff — `git diff` never shows
// untracked files at all.
func untrackedFileContent(sandboxRoot, file string) string {
	full := filepath.Join(sandboxRoot, file)
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	truncated := len(lines) > untrackedContentLineCap
	if truncated {
		lines = lines[:untrackedContentLineCap]
	}
	content := strings.Join(lines, "\n")
	header := fmt.Sprintf("--- new file: %s ---", file)
	if truncated {
		header += fmt.Sprintf(" (showing first %d lines)", untrackedContentLineCap)
	}
	return header + "\n" + content
}
