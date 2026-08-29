// Package checkpoint implements Huginn's Run Checkpoints system: a
// shadow-git snapshot store plus a run ledger that lets a human undo an
// agent run's file changes without ever rewriting the user's real git
// history.
//
// Design reference: DESIGN-COMPETITIVE-2026-08-29.md, PART 4. The numbered
// DECISIONS there map onto this package roughly as:
//
//	DECISION 1 (shadow git store, not the real .git)      -> gitshadow.go (Store)
//	DECISION 2 (snapshot-before-run, keyed to the thread)  -> ../../init_checkpoint.go (wireThreadManagerCheckpoints), manager.go (BeginRun)
//	DECISION 3 (snapshot before destructive ops)           -> manager.go (Snapshot); see BeginRun/EndRun doc comments
//	DECISION 4 (revert a whole run, hand-edit preserving)  -> manager.go (RevertRun)
//	DECISION 5 (diff-of-a-run via shadow git)              -> manager.go (DiffRun), gitshadow.go (Diff)
//	DECISION 6 (merge-back-as-deliverable)                 -> out of scope here; belongs to the worktree/PR accept-reject
//	                                                           flow, not this package; see report
//	DECISION 7 (per-run worktree by default)               -> out of scope here; existing worktree.go is not touched
//	                                                           this wave; see report
//	DECISION 8 (couple conversation rewind to file rewind) -> deferred; RevertResult.Warning names the gap explicitly
//	                                                           on every revert; see report
//	DECISION 9 (run ledger, jj-style op log)                -> ledger.go
package checkpoint

import (
	"errors"
	"time"
)

// RunStatus is the lifecycle state of a run in the ledger.
type RunStatus string

const (
	// RunActive is set once BeginRun's pre-snapshot has been captured and
	// before EndRun runs.
	RunActive RunStatus = "active"
	// RunCompleted is set once EndRun successfully captured a post-snapshot.
	RunCompleted RunStatus = "completed"
	// RunCaptureFailed means a snapshot (pre or post) could not be taken.
	// This status must NEVER be silently swallowed — a run whose capture
	// failed is not protected and callers must be told so explicitly. This
	// is the "honesty requirement": a failed capture surfaces as failed,
	// never as a silent no-op that reads as "protected".
	RunCaptureFailed RunStatus = "capture_failed"
	// RunReverted means checkpoint_revert_run successfully restored (fully
	// or partially) this run's pre-snapshot state.
	RunReverted RunStatus = "reverted"
)

// ErrRunNotFound is returned when a threadID has no ledger entry.
var ErrRunNotFound = errors.New("checkpoint: no run recorded for this thread")

// ErrAlreadyPushed is returned by RevertRun when the run's ledger record is
// flagged pushed and the caller did not pass AllowAfterPush — undo after a
// push must never silently rewrite local history out from under a pushed
// commit (Sharp edge 3 in the design doc).
var ErrAlreadyPushed = errors.New("checkpoint: this run was already pushed (or has an open PR); local revert is refused — see RevertOptions.AllowAfterPush for the revert-forward path")

// RunRecord is one row of the run ledger: everything needed to show a human
// "runs you can undo" and to act on one.
type RunRecord struct {
	ThreadID     string
	AgentID      string
	TaskSummary  string
	Status       RunStatus
	PreSnapshot  string // shadow-git commit hash captured at run start
	PostSnapshot string // shadow-git commit hash captured at run end ("" until EndRun)
	TouchedPaths []string
	Pushed       bool
	PRURL        string
	CreatedAt    time.Time
	CompletedAt  time.Time
	// CaptureError is set (and Status == RunCaptureFailed) when a snapshot
	// attempt failed. Never empty when Status == RunCaptureFailed.
	CaptureError string
}

// RevertOptions controls how RevertRun restores files.
type RevertOptions struct {
	// All forces a full restore to the pre-snapshot regardless of what
	// files the run touched, overwriting any hand-edits made since
	// (Hermes' --all). Default (false) is hand-edit-preserving: only paths
	// this run itself touched are restored.
	All bool
	// OnlyPaths, if non-empty, restricts the restore to this subset of the
	// run's touched paths (Hermes' "/rollback <N> <file>").
	OnlyPaths []string
	// AllowAfterPush permits reverting a run flagged Pushed. When true and
	// the run was pushed, RevertRun performs a "revert-forward" restore:
	// it still only touches the local working tree (never rewrites the
	// pushed commit), and the result is annotated so the caller knows a
	// forward-revert (new commit) is still needed to undo the remote side.
	AllowAfterPush bool
}

// RevertResult reports exactly what RevertRun did and did not do — the
// honesty requirement from the design doc, modeled on git.go's
// untrackedFilesNote pattern.
type RevertResult struct {
	Restored      []string // paths whose content was rewritten to match the pre-snapshot
	Deleted       []string // paths that did not exist at the pre-snapshot and were removed
	SkippedEdited []string // paths touched by this run but also hand-edited afterward — preserved, not restored (unless All)
	NotRestorable []string // paths the design doc says are out of scope: DB rows, untracked-but-ignored files, side effects
	Warning       string   // human-readable summary of anything the caller must know (pushed run, bash side effects, etc.)
}

// GCOptions controls checkpoint_gc retention.
type GCOptions struct {
	// KeepRuns retains ledger/snapshot state for at most this many most
	// recent runs (0 = use DefaultKeepRuns).
	KeepRuns int
	// MaxAge prunes runs whose CreatedAt is older than this (0 = no age
	// cutoff).
	MaxAge time.Duration
}

// DefaultKeepRuns is the retention window applied by GC when
// GCOptions.KeepRuns is 0.
const DefaultKeepRuns = 50

// GCResult reports what GC actually removed.
type GCResult struct {
	PrunedRuns    int
	KeptRuns      int
	ObjectsBefore int
	ObjectsAfter  int
}
