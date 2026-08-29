package checkpoint

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Locker is the subset of internal/tools.FileLockManager's API the
// checkpoint manager needs. Defined here (not imported from internal/tools)
// to avoid an import cycle: internal/tools imports internal/checkpoint to
// expose checkpoint_* tools, so this package cannot import internal/tools.
// *tools.FileLockManager already satisfies this interface structurally.
//
// Restore takes the same per-path locks writes/edits do (Lock before
// mutating a path, Unlock after) — this is how RevertRun avoids a torn
// write racing a concurrent edit_file/write_file call on the same path
// (Sharp edge 1 in the design doc).
type Locker interface {
	Lock(path string)
	Unlock(path string)
}

type noopLocker struct{}

func (noopLocker) Lock(string)   {}
func (noopLocker) Unlock(string) {}

// Manager is the top-level checkpoint API: a shadow-git snapshot Store plus
// a run ledger, coordinated so a caller (a tool, an HTTP handler, or the
// ThreadManager wiring in wire.go) only ever needs BeginRun / EndRun /
// RevertRun / DiffRun / List / GC / MarkPushed.
type Manager struct {
	store  *Store
	ledger *ledger
	locker Locker

	// runMu serializes BeginRun/EndRun/RevertRun against each other so two
	// concurrent runs can't interleave a shadow-repo add+commit (the shadow
	// git index is shared process-wide, same reasoning as git.go's use of
	// runGit against a single real repo).
	runMu sync.Mutex
}

// NewManager creates a checkpoint Manager for sandboxRoot, with its shadow
// store and ledger rooted under huginnHome (never inside sandboxRoot — see
// gitshadow.go's storeSubdir doc, A5). locker may be nil, in which case
// restores are not serialized against other file writers (fine for tests /
// single-writer use; production wiring MUST pass the exact same
// *tools.FileLockManager instance used by write_file/edit_file — see
// canonicalLockPath below for why the key namespace must also match).
func NewManager(ctx context.Context, huginnHome, sandboxRoot string, locker Locker) (*Manager, error) {
	store, err := NewStore(ctx, huginnHome, sandboxRoot)
	if err != nil {
		return nil, err
	}
	ldg, err := newLedger(store.LedgerPath())
	if err != nil {
		return nil, err
	}
	if locker == nil {
		locker = noopLocker{}
	}
	return &Manager{store: store, ledger: ldg, locker: locker}, nil
}

// Close releases the ledger's database handle. The shadow git store has no
// open handles to release (each call shells out and exits).
func (m *Manager) Close() error {
	return m.ledger.Close()
}

func preRef(threadID string) string  { return "refs/huginn/run/" + threadID + "/pre" }
func postRef(threadID string) string { return "refs/huginn/run/" + threadID + "/post" }

// canonicalLockPath maps a repo-relative slash path (as returned by
// Store.ChangedPaths / stored in RunRecord.TouchedPaths) to the SAME
// absolute-path key namespace internal/tools.ResolveSandboxed produces for
// write_file/edit_file's own FileLock.Lock calls (A1). Without this, even a
// shared *tools.FileLockManager instance never actually serializes anything
// against RevertRun: write_file locks the absolute, symlink-resolved path
// it resolves the request to, while RevertRun previously locked the bare
// relative path straight from the shadow-git diff ("hello.txt" vs
// "/abs/sandbox/hello.txt") — two keys that never collide.
//
// This mirrors ResolveSandboxed's EvalSymlinks-then-fall-back-to-parent
// logic (internal/tools/sandbox.go) rather than importing it, to avoid an
// internal/checkpoint <-> internal/tools import cycle (internal/tools
// already imports internal/checkpoint to expose the checkpoint_* tools).
func (m *Manager) canonicalLockPath(relPath string) string {
	joined := filepath.Join(m.store.WorkTree(), filepath.FromSlash(relPath))
	if resolved, err := filepath.EvalSymlinks(joined); err == nil {
		return resolved
	}
	if resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(joined)); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(joined))
	}
	return joined
}

// BeginRun captures a pre-run snapshot of the working tree and records a new
// active ledger entry for threadID. Called once per run, at the moment a
// thread transitions from queued to running (DECISION 2).
//
// A capture failure is never a silent no-op: it is recorded as
// RunCaptureFailed with CaptureError set, and returned as an error, so a
// caller that ignores the error still leaves an honest ledger trail instead
// of a run that looks "protected" but isn't.
func (m *Manager) BeginRun(ctx context.Context, threadID, agentID, taskSummary string) (RunRecord, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	now := time.Now()
	hash, snapErr := m.store.Snapshot(ctx, preRef(threadID), "pre-run: "+threadID)
	r := RunRecord{
		ThreadID:    threadID,
		AgentID:     agentID,
		TaskSummary: truncateSummary(taskSummary),
		CreatedAt:   now,
	}
	if snapErr != nil {
		r.Status = RunCaptureFailed
		r.CaptureError = snapErr.Error()
		_ = m.ledger.Insert(ctx, r) // best-effort: still surface the real error below
		return r, fmt.Errorf("checkpoint: BeginRun(%s): %w", threadID, snapErr)
	}
	r.Status = RunActive
	r.PreSnapshot = hash
	// Best-effort: a failure to list ignored paths must never block
	// BeginRun (the pre-snapshot itself already succeeded) — it only
	// degrades the honesty disclosure in RevertResult later.
	r.IgnoredAtBegin, _ = m.store.IgnoredPaths(ctx)
	if err := m.ledger.Insert(ctx, r); err != nil {
		return r, fmt.Errorf("checkpoint: BeginRun(%s): record ledger: %w", threadID, err)
	}
	return r, nil
}

const maxTaskSummaryLen = 500

func truncateSummary(s string) string {
	if len(s) <= maxTaskSummaryLen {
		return s
	}
	return s[:maxTaskSummaryLen] + "…"
}

// EndRun captures a post-run snapshot, computes the run's touched paths from
// the shadow-git diff between pre and post (capturing bash side effects too,
// not just instrumented write/edit calls), and finalizes the ledger entry.
//
// If threadID has no BeginRun record (e.g. checkpointing was wired up after
// the run already started), EndRun returns ErrRunNotFound — callers should
// treat that as "nothing to finalize", not as a fatal error.
func (m *Manager) EndRun(ctx context.Context, threadID string) (RunRecord, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	r, err := m.ledger.Get(ctx, threadID)
	if err != nil {
		return RunRecord{}, err
	}
	if r.Status == RunCaptureFailed {
		// Nothing to compare against — leave the failure recorded as-is.
		return r, fmt.Errorf("checkpoint: EndRun(%s): pre-run capture had failed, nothing to finalize", threadID)
	}
	if r.Status != RunActive {
		// Already finalized (e.g. a duplicate terminal-status hook fire) —
		// return the existing record rather than re-snapshotting.
		return r, nil
	}

	hash, snapErr := m.store.Snapshot(ctx, postRef(threadID), "post-run: "+threadID)
	if snapErr != nil {
		r.Status = RunCaptureFailed
		r.CaptureError = snapErr.Error()
		r.CompletedAt = time.Now()
		_ = m.ledger.Update(ctx, r)
		return r, fmt.Errorf("checkpoint: EndRun(%s): %w", threadID, snapErr)
	}

	touched, diffErr := m.store.ChangedPaths(ctx, r.PreSnapshot, hash)
	if diffErr != nil {
		// The snapshot itself succeeded; we just couldn't compute the
		// touched-path summary. Don't fail the whole run over it — record
		// what we have and note the gap in CaptureError without flipping
		// Status to failed (the safety net — the snapshot — is intact).
		r.CaptureError = "post-snapshot ok, but touched-paths diff failed: " + diffErr.Error()
	}

	r.Status = RunCompleted
	r.PostSnapshot = hash
	r.TouchedPaths = touched
	r.CompletedAt = time.Now()
	// A8: which gitignored-at-begin paths are gone by end-of-run — the
	// checkpoint system's honest blind spot, since git add -A never
	// snapshotted them in the first place. Best-effort, same reasoning as
	// BeginRun's IgnoredAtBegin capture.
	if len(r.IgnoredAtBegin) > 0 {
		nowIgnored, ignErr := m.store.IgnoredPaths(ctx)
		if ignErr == nil {
			stillIgnored := make(map[string]bool, len(nowIgnored))
			for _, p := range nowIgnored {
				stillIgnored[p] = true
			}
			for _, p := range r.IgnoredAtBegin {
				if !stillIgnored[p] {
					r.IgnoredTouched = append(r.IgnoredTouched, p)
				}
			}
		}
	}
	if err := m.ledger.Update(ctx, r); err != nil {
		return r, fmt.Errorf("checkpoint: EndRun(%s): record ledger: %w", threadID, err)
	}
	return r, nil
}

// MarkPushed flags a run as pushed (or PR-opened), gating RevertRun's
// pushed-guard (Sharp edge 3).
func (m *Manager) MarkPushed(ctx context.Context, threadID, prURL string) error {
	r, err := m.ledger.Get(ctx, threadID)
	if err != nil {
		return err
	}
	r.Pushed = true
	r.PRURL = prURL
	return m.ledger.Update(ctx, r)
}

// MarkAllUnsyncedPushed flags every RunCompleted run not already marked
// Pushed as pushed, with prURL attached to each. This is the production
// caller MarkPushed was missing entirely (A2): a git_push tool call has no
// reliable way to know which in-flight runs' file changes ended up in the
// pushed commit (that would require correlating TouchedPaths against the
// pushed commit's diff, which the shadow store — a separate git history —
// can't do without duplicating real-git plumbing). The coarse, honest
// alternative: on a successful push, treat every completed-but-unsynced run
// as now "pushed" — conservative in the safe direction, since the
// pushed-guard's failure mode (blocking an unnecessary revert until
// allow_after_push is passed) is far less costly than the guard silently
// never firing at all (the previous behavior, MarkPushed with zero
// production callers).
func (m *Manager) MarkAllUnsyncedPushed(ctx context.Context, prURL string) (int, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	all, err := m.ledger.List(ctx, 1<<30)
	if err != nil {
		return 0, fmt.Errorf("checkpoint: MarkAllUnsyncedPushed: list runs: %w", err)
	}
	marked := 0
	for _, r := range all {
		if r.Status != RunCompleted || r.Pushed {
			continue
		}
		r.Pushed = true
		r.PRURL = prURL
		if err := m.ledger.Update(ctx, r); err != nil {
			return marked, fmt.Errorf("checkpoint: MarkAllUnsyncedPushed: update %s: %w", r.ThreadID, err)
		}
		marked++
	}
	return marked, nil
}

// Get returns the ledger record for threadID.
func (m *Manager) Get(ctx context.Context, threadID string) (RunRecord, error) {
	return m.ledger.Get(ctx, threadID)
}

// List returns the most recent runs, newest first — the "runs you can undo"
// surface (DECISION 9).
func (m *Manager) List(ctx context.Context, limit int) ([]RunRecord, error) {
	return m.ledger.List(ctx, limit)
}

// DiffRun returns the unified diff for a run: everything that changed
// between its pre- and post-run snapshots, straight from the shadow git
// store — the same diff-card material every edit/write tool call already
// produces, but for the whole run at once (DECISION 5).
func (m *Manager) DiffRun(ctx context.Context, threadID string) (string, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	r, err := m.ledger.Get(ctx, threadID)
	if err != nil {
		return "", err
	}
	to := r.PostSnapshot
	if to == "" {
		// Run still active (or ended without a post-snapshot) — diff
		// against the live working tree by taking a throwaway snapshot.
		to, err = m.store.Snapshot(ctx, "", "diff-run: "+threadID)
		if err != nil {
			return "", fmt.Errorf("checkpoint: DiffRun(%s): snapshot current tree: %w", threadID, err)
		}
	}
	return m.store.Diff(ctx, r.PreSnapshot, to)
}

// RevertRun restores threadID's touched files to their pre-run state.
//
// Default behavior is hand-edit-preserving (DECISION 4): for each path this
// run touched, RevertRun re-reads the pre-snapshot and the current working
// tree; if the current content still matches the run's own post-snapshot
// (i.e. nobody has edited it since), it is restored. If it no longer
// matches — a human or another run edited it after this run finished — it
// is left alone and reported in SkippedEdited, unless opts.All is set.
//
// Every mutated path is Lock()/Unlock()'d through the same Locker used by
// write_file/edit_file, so a restore can never race a concurrent write to
// the same path (Sharp edge 1).
func (m *Manager) RevertRun(ctx context.Context, threadID string, opts RevertOptions) (RevertResult, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	r, err := m.ledger.Get(ctx, threadID)
	if err != nil {
		return RevertResult{}, err
	}
	if r.Status == RunCaptureFailed {
		return RevertResult{}, fmt.Errorf("checkpoint: RevertRun(%s): this run's snapshot capture failed (%s) — there is nothing to restore to", threadID, r.CaptureError)
	}
	if r.PreSnapshot == "" {
		return RevertResult{}, fmt.Errorf("checkpoint: RevertRun(%s): no pre-run snapshot recorded", threadID)
	}
	if r.Pushed && !opts.AllowAfterPush {
		return RevertResult{}, ErrAlreadyPushed
	}

	paths := r.TouchedPaths
	if len(opts.OnlyPaths) > 0 {
		allowed := make(map[string]bool, len(opts.OnlyPaths))
		for _, p := range opts.OnlyPaths {
			allowed[filepath.ToSlash(p)] = true
		}
		filtered := paths[:0:0]
		for _, p := range paths {
			if allowed[p] {
				filtered = append(filtered, p)
			}
		}
		paths = filtered
	}

	result := RevertResult{}
	if len(paths) == 0 {
		// A7: never let this read as a success headline — a run with no
		// touched paths at all commonly means it ran in a worktree (whose
		// changes this wave still doesn't capture) or touched only
		// gitignored files, not that it "changed nothing". Callers (see
		// checkpoint_tools.go) must lead with this Warning, gated on
		// NothingCaptured, rather than printing "Reverted run X."
		result.Warning = "Nothing captured for this run — it may have run in a worktree or touched only ignored files; nothing was restored."
		result.NothingCaptured = true
		return result, nil
	}

	postHash := r.PostSnapshot
	// Take one throwaway snapshot of the live tree up front (rather than
	// per path) so hand-edit detection below is a single git operation,
	// not one per touched file.
	var liveHash string
	var changedSinceRun map[string]bool
	if !opts.All && postHash != "" {
		liveHash, err = m.store.Snapshot(ctx, "", "revert-compare: "+threadID)
		if err != nil {
			return result, fmt.Errorf("checkpoint: RevertRun(%s): snapshot live tree: %w", threadID, err)
		}
		changed, err := m.store.ChangedPaths(ctx, postHash, liveHash)
		if err != nil {
			return result, fmt.Errorf("checkpoint: RevertRun(%s): diff live tree: %w", threadID, err)
		}
		changedSinceRun = make(map[string]bool, len(changed))
		for _, c := range changed {
			changedSinceRun[c] = true
		}
	}

	// A9: continue collecting results across all paths even when one fails
	// to restore, rather than aborting the whole run's revert on the first
	// error. A caller that discards a partial result on error (the old
	// behavior) has no way to tell a human "12 of 13 files restored, 1
	// failed: X" — it just reports total failure and leaves the 12 restores
	// that DID happen undisclosed.
	var restoreErrs []string
	for _, p := range paths {
		lockKey := m.canonicalLockPath(p)
		m.locker.Lock(lockKey)
		restored, deleted, skip, restoreErr := m.revertOnePath(ctx, r.PreSnapshot, p, changedSinceRun[p])
		m.locker.Unlock(lockKey)
		if restoreErr != nil {
			if result.Failed == nil {
				result.Failed = make(map[string]string, 1)
			}
			result.Failed[p] = restoreErr.Error()
			restoreErrs = append(restoreErrs, fmt.Sprintf("%s: %v", p, restoreErr))
			continue
		}
		switch {
		case skip:
			result.SkippedEdited = append(result.SkippedEdited, p)
		case deleted:
			result.Deleted = append(result.Deleted, p)
		case restored:
			result.Restored = append(result.Restored, p)
		}
	}

	sort.Strings(result.Restored)
	sort.Strings(result.Deleted)
	sort.Strings(result.SkippedEdited)

	// DECISION-doc-mandated honesty: name what's out of scope regardless of
	// how the restore went. Files-only undo — DB rows, package installs,
	// network calls, and any state outside the sandboxed working tree are
	// never touched (Sharp edges 2 and 5).
	result.NotRestorable = []string{
		"rows this run wrote to Huginn's own SQLite (thread/message state)",
		"any application database or external state the run modified",
		"network calls, package installs, or other bash side effects with no filesystem trace",
	}
	if len(r.IgnoredTouched) > 0 {
		result.NotRestorable = append(result.NotRestorable,
			fmt.Sprintf("gitignored file(s) this run may have deleted or changed, never snapshotted or protected: %s", strings.Join(r.IgnoredTouched, ", ")))
	}

	var warn strings.Builder
	warn.WriteString("Files only: this reverts the working tree, not databases, network calls, or other side effects.")
	if len(result.SkippedEdited) > 0 {
		fmt.Fprintf(&warn, " %d file(s) were hand-edited after this run and were left alone (pass All to override).", len(result.SkippedEdited))
	}
	if len(result.Failed) > 0 {
		fmt.Fprintf(&warn, " %d file(s) FAILED to restore: %s.", len(result.Failed), strings.Join(sortedKeys(result.Failed), ", "))
	}
	if r.Pushed {
		fmt.Fprintf(&warn, " This run was already pushed (%s) — local files were restored, but the pushed commit was NOT rewritten; open a revert PR to undo it remotely.", strings.TrimSpace(r.PRURL))
	}
	result.Warning = warn.String()

	// A16: a successful (even if partially failed) revert attempt updates
	// the run's status so checkpoint_list reflects reality — previously
	// RunReverted was defined but never set anywhere, so every reverted run
	// still showed status=completed forever. Best-effort: a failure here
	// must not turn a real restore into a reported error.
	if len(result.Restored) > 0 || len(result.Deleted) > 0 {
		r.Status = RunReverted
		_ = m.ledger.Update(ctx, r)
	}

	if len(restoreErrs) > 0 {
		return result, fmt.Errorf("checkpoint: RevertRun(%s): %d of %d path(s) failed to restore: %s", threadID, len(restoreErrs), len(paths), strings.Join(restoreErrs, "; "))
	}
	return result, nil
}

// sortedKeys returns the sorted keys of a string-keyed map, for stable,
// deterministic warning text.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// revertOnePath restores a single path, honoring hand-edit preservation:
// handEdited is true when the path changed between this run's post-snapshot
// and the live tree (computed once, up front, by the caller).
func (m *Manager) revertOnePath(ctx context.Context, preHash, path string, handEdited bool) (restored, deleted, skip bool, err error) {
	if handEdited {
		return false, false, true, nil
	}
	restored, deleted, err = m.store.RestorePath(ctx, preHash, path)
	return restored, deleted, false, err
}

// GC prunes ledger rows and shadow-git refs beyond the retention window,
// then reclaims the freed shadow-store disk with `git gc` (Sharp edge 4).
func (m *Manager) GC(ctx context.Context, opts GCOptions) (GCResult, error) {
	// A12: GC previously took no lock at all, so it could race a concurrent
	// RevertRun/BeginRun/EndRun's shadow-git add+commit (the shadow index is
	// shared process-wide, same reasoning as runMu's doc comment above).
	m.runMu.Lock()
	defer m.runMu.Unlock()

	keepRuns := opts.KeepRuns
	if keepRuns <= 0 {
		keepRuns = DefaultKeepRuns
	}

	all, err := m.ledger.List(ctx, 1<<30)
	if err != nil {
		return GCResult{}, fmt.Errorf("checkpoint: GC: list runs: %w", err)
	}

	before, _ := m.store.ObjectCount(ctx)

	keep := make(map[string]bool)
	kept := 0
	for _, r := range all {
		age := time.Since(r.CreatedAt)
		withinAge := opts.MaxAge <= 0 || age <= opts.MaxAge
		if kept < keepRuns && withinAge {
			keep[r.ThreadID] = true
			kept++
		}
	}

	pruned, err := m.ledger.DeleteExcept(ctx, keep)
	if err != nil {
		return GCResult{}, fmt.Errorf("checkpoint: GC: prune ledger: %w", err)
	}

	keepRefs := make(map[string]bool, len(keep)*2)
	for id := range keep {
		keepRefs[preRef(id)] = true
		keepRefs[postRef(id)] = true
	}
	if _, err := m.store.GC(ctx, keepRefs); err != nil {
		return GCResult{}, fmt.Errorf("checkpoint: GC: shadow store gc: %w", err)
	}

	after, _ := m.store.ObjectCount(ctx)

	return GCResult{
		PrunedRuns:    pruned,
		KeptRuns:      kept,
		ObjectsBefore: before,
		ObjectsAfter:  after,
	}, nil
}
