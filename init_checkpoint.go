package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/scrypster/huginn/internal/checkpoint"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

// initCheckpoints builds a checkpoint.Manager rooted at sandboxRoot,
// registers the checkpoint_* tool belt on toolReg, and wires automatic
// pre/post-run snapshots into tm via tm's existing OnStatusChange hook
// (internal/threadmgr/manager.go — no changes to threadmgr needed).
//
// Deliberately NOT called from init_tools.go (another agent owns that file
// this wave) or from anywhere in main.go/builtin.go — this is the single
// entry point an orchestrator setup path should call once per sandbox.
//
// ONE-LINE INTEGRATION HOOK for the orchestrator: wherever
// tools.RegisterBuiltins/RegisterGitTools/RegisterWorktreeTools are called
// for a given (toolReg, sandboxRoot) alongside the *threadmgr.ThreadManager
// for that session, add:
//
//	ckptMgr, unwireCheckpoints, err := initCheckpoints(ctx, sandboxRoot, toolReg, tm)
//
// and mount its REST surface (optional, only if the HTTP server for that
// sandbox is available at wiring time):
//
//	mux.Handle("/api/v1/checkpoints/", http.StripPrefix("/api/v1/checkpoints", checkpoint.HTTPHandler(ckptMgr)))
//
// unwireCheckpoints deregisters the ThreadManager hook and closes the
// ledger DB — call it on sandbox/session teardown.
func initCheckpoints(ctx context.Context, sandboxRoot string, toolReg *tools.Registry, tm *threadmgr.ThreadManager) (mgr *checkpoint.Manager, teardown func(), err error) {
	flm := tools.NewFileLockManager()
	mgr, err = checkpoint.NewManager(ctx, sandboxRoot, flm)
	if err != nil {
		return nil, nil, err
	}

	tools.RegisterCheckpointTools(toolReg, mgr)

	unwire := wireThreadManagerCheckpoints(tm, mgr)

	teardown = func() {
		unwire()
		if cerr := mgr.Close(); cerr != nil {
			slog.Error("checkpoint: close manager failed", "error", cerr)
		}
	}
	return mgr, teardown, nil
}

// wireThreadManagerCheckpoints hooks automatic pre/post-run snapshots into
// tm's OnStatusChange callback (DECISION 2 in DESIGN-COMPETITIVE-2026-08-29.md
// PART 4). Lives here rather than in internal/checkpoint to avoid an import
// cycle: internal/threadmgr transitively imports internal/tools (via
// internal/agents), and internal/tools imports internal/checkpoint to
// expose the checkpoint_* belt — so internal/checkpoint cannot import
// internal/threadmgr directly. package main already depends on both, so
// this is the natural (and only cycle-free) place for the glue.
//
// A thread's first transition into StatusThinking (queued -> thinking, only
// ever fired once by ThreadManager.Start) triggers BeginRun. Any transition
// into a terminal status (done/error/cancelled) triggers EndRun.
func wireThreadManagerCheckpoints(tm *threadmgr.ThreadManager, mgr *checkpoint.Manager) func() {
	return tm.OnStatusChange(func(id string, status threadmgr.ThreadStatus) {
		bgCtx := context.Background()
		switch status {
		case threadmgr.StatusThinking:
			t, ok := tm.Get(id)
			if !ok {
				return
			}
			if _, err := mgr.BeginRun(bgCtx, id, t.AgentID, t.Task); err != nil {
				slog.Error("checkpoint: BeginRun failed", "thread_id", id, "agent", t.AgentID, "error", err)
			}
		case threadmgr.StatusDone, threadmgr.StatusError, threadmgr.StatusCancelled:
			if _, err := mgr.EndRun(bgCtx, id); err != nil {
				if err == checkpoint.ErrRunNotFound {
					// No BeginRun for this thread (checkpointing wired up
					// mid-session, or the thread never reached
					// StatusThinking) — nothing to finalize, not an error.
					return
				}
				slog.Error("checkpoint: EndRun failed", "thread_id", id, "status", status, "error", err)
			}
		}
	})
}

// mountCheckpointRoutes is a convenience the orchestrator setup path can
// call once an http.ServeMux and Manager both exist, per the ONE-LINE hook
// documented on initCheckpoints above.
func mountCheckpointRoutes(mux *http.ServeMux, mgr *checkpoint.Manager) {
	mux.Handle("/api/v1/checkpoints/", http.StripPrefix("/api/v1/checkpoints", checkpoint.HTTPHandler(mgr)))
}
