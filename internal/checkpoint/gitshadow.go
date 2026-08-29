package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// checkpointsBranch names the shadow repo's initial branch purely to
// satisfy `git init`'s --initial-branch requirement. It is NEVER actually
// committed onto (see Snapshot's use of write-tree + commit-tree with no
// parent, deliberately bypassing `git commit`, which would advance
// whatever branch HEAD resolves to) — A12's fix. HEAD stays permanently
// unborn.
const checkpointsBranch = "checkpoints"

// storeSubdir/ledgerFileName name the shadow GIT_DIR and ledger db files
// within a per-sandbox directory under the Huginn home
// (~/.huginn/checkpoints/<hash-of-sandbox-path>/, see checkpointDirFor).
//
// DECISION 1 originally put these under the sandbox's own .huginn/
// directory, relying on the user's .gitignore to hide them. That pollutes
// any repo whose .gitignore doesn't already list .huginn/ (a plain `git
// add -A` in the user's real repo would stage the shadow store and ledger
// as ordinary untracked files). Storing them entirely outside the sandbox
// tree — under the user's Huginn home instead — means the user's real git
// never sees them at all, regardless of their .gitignore.
const storeSubdir = "store.git"
const ledgerFileName = "ledger.db"

// Store wraps a shadow git repository whose GIT_DIR lives outside the
// project's sandbox tree entirely (under huginnHome/checkpoints/<hash>/) and
// whose work-tree is the project's sandbox root. Because GIT_DIR differs
// from the sandbox's own .git, and lives outside the sandbox altogether, the
// shadow repo never touches — or is visible to — the user's real git state.
type Store struct {
	gitDir   string // absolute path to the shadow GIT_DIR (outside the sandbox tree)
	workTree string // absolute path to SandboxRoot
}

// checkpointDirFor returns the per-sandbox directory under huginnHome that
// holds this sandbox's shadow store + ledger, keyed by a hash of the
// sandbox's resolved absolute path so distinct sandboxes never collide.
func checkpointDirFor(huginnHome, absSandboxRoot string) string {
	sum := sha256.Sum256([]byte(absSandboxRoot))
	key := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(huginnHome, "checkpoints", key)
}

// NewStore creates (or opens) the shadow git store for sandboxRoot, rooted
// under huginnHome (never inside sandboxRoot — see storeSubdir doc above).
func NewStore(ctx context.Context, huginnHome, sandboxRoot string) (*Store, error) {
	absRoot, err := filepath.Abs(sandboxRoot)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: resolve sandbox root: %w", err)
	}
	// EvalSymlinks so /tmp -> /private/tmp (macOS) doesn't produce a
	// mismatched work-tree vs the paths tools report — same reasoning as
	// git.go's GitCommitTool.Execute path resolution.
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absHome, err := filepath.Abs(huginnHome)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: resolve huginn home: %w", err)
	}
	checkpointDir := checkpointDirFor(absHome, absRoot)
	gitDir := filepath.Join(checkpointDir, storeSubdir)
	s := &Store{gitDir: gitDir, workTree: absRoot}
	if err := s.ensureInit(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// LedgerPath returns the absolute path to this store's sibling ledger.db,
// for NewManager to open.
func (s *Store) LedgerPath() string {
	return filepath.Join(filepath.Dir(s.gitDir), ledgerFileName)
}

// WorkTree returns the shadow store's work tree — the sandbox root, fully
// resolved (symlinks evaluated). Exposed so callers computing lock keys
// against the same paths RestorePath acts on (see manager.go
// canonicalLockPath) agree on the same absolute-path namespace write_file
// and edit_file resolve their own lock keys against.
func (s *Store) WorkTree() string { return s.workTree }

// gitEnvStripKeys are GIT_* environment variables that, if inherited from
// the parent process, would redirect the shadow repo's git invocations at
// someone else's index/object store/namespace — e.g. a caller that shells
// out to git against the user's real repo with GIT_INDEX_FILE set for its
// own purposes would otherwise leak that into every checkpoint git call
// too. Stripped from the child env before setting our own GIT_DIR/
// GIT_WORK_TREE (verified: an inherited GIT_INDEX_FILE staged objects into
// the wrong index without this).
var gitEnvStripKeys = []string{
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_NAMESPACE",
}

// sanitizedEnviron returns os.Environ() with gitEnvStripKeys removed.
func sanitizedEnviron() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		strip := false
		for _, k := range gitEnvStripKeys {
			if strings.HasPrefix(kv, k+"=") {
				strip = true
				break
			}
		}
		if !strip {
			out = append(out, kv)
		}
	}
	return out
}

// baseEnv returns the env every shadow-git invocation shares: GIT_DIR/
// GIT_WORK_TREE pointed at the shadow store (never the user's real .git),
// with any inherited git-redirect vars stripped, and BOTH global and system
// git config isolated. GIT_CONFIG_NOSYSTEM alone only blocks /etc/gitconfig
// — a user's ~/.gitconfig (commit.gpgsign=true, core.hooksPath pointing at
// a repo hook) still applies to any git invocation that doesn't also block
// it, which would make BeginRun fail-open on an unrelated gpg prompt or
// silently execute an unrelated hook inside a snapshot. GIT_CONFIG_GLOBAL
// (git >= 2.32) redirects the "global" config file itself, so setting it to
// /dev/null makes ~/.gitconfig invisible to every command below regardless
// of $HOME.
func (s *Store) baseEnv() []string {
	return append(sanitizedEnviron(),
		"GIT_DIR="+s.gitDir,
		"GIT_WORK_TREE="+s.workTree,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
}

// run executes a git command against the shadow repo (GIT_DIR/GIT_WORK_TREE
// pointed at the shadow store, never at the user's real .git). Mirrors the
// runGit helper in internal/tools/worktree.go, duplicated here rather than
// imported to avoid an internal/tools <-> internal/checkpoint import cycle
// (internal/tools imports this package to expose checkpoint_* tools).
func (s *Store) run(ctx context.Context, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.workTree
	cmd.Env = append(s.baseEnv(),
		// Isolate the shadow repo's identity from any repo-local git config
		// (commit hooks, signing, user.name) — checkpoints are an internal
		// bookkeeping mechanism, not user-attributed commits.
		"GIT_AUTHOR_NAME=huginn-checkpoint",
		"GIT_AUTHOR_EMAIL=checkpoint@huginn.local",
		"GIT_COMMITTER_NAME=huginn-checkpoint",
		"GIT_COMMITTER_EMAIL=checkpoint@huginn.local",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (s *Store) ensureInit(ctx context.Context) error {
	if _, err := os.Stat(s.gitDir); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.gitDir, 0o755); err != nil {
		return fmt.Errorf("checkpoint: create shadow store dir: %w", err)
	}
	if _, stderr, err := s.run(ctx, "init", "--quiet", "--initial-branch="+checkpointsBranch); err != nil {
		return fmt.Errorf("checkpoint: init shadow store: %v: %s", err, strings.TrimSpace(stderr))
	}
	// Never GC-expire objects we still reference by hash even without a
	// ref pointing at them mid-commit — belt and suspenders around the
	// two-step add+commit below.
	if _, _, err := s.run(ctx, "config", "gc.auto", "0"); err != nil {
		return fmt.Errorf("checkpoint: configure shadow store: %w", err)
	}
	return nil
}

// snapshotPathspecs excludes the user's real .git (so it's recorded as an
// opaque gitlink, never walked for content).
var snapshotPathspecs = []string{".", ":(exclude).git"}

// Snapshot stages the current work tree (tracked + untracked-but-not-ignored
// files, honoring the user's own .gitignore since git reads it from the
// work tree regardless of GIT_DIR) and commits it in the shadow repo under
// the given ref, returning the resulting commit hash.
//
// Deliberately uses write-tree + commit-tree with NO parent (an orphan
// commit) rather than `git commit`, which would advance whatever HEAD
// resolves to — A12's fix. `git commit` chaining every snapshot onto the
// PREVIOUS snapshot's commit meant every run's pre/post refs were points
// along one single linear ancestor chain: even after GC deleted every
// other run's named ref, keeping just the most recent run's ref kept
// EVERY earlier run's commit reachable too, via plain parent ancestry —
// ref deletion was a complete no-op for reclaiming disk (verified: 48
// objects before, 49 after, for a 12-run/keep-2 GC). Orphan commits make
// each snapshot an independent root: deleting its ref is what actually
// makes it (and any blob/tree not shared with a still-kept snapshot)
// unreachable.
//
// An "empty" snapshot (tree identical to the last one on this ref) is
// still allowed — a run that touches nothing still gets a valid pre/post
// pair; write-tree naturally produces the same tree hash for identical
// content, and commit-tree still produces a distinct commit (different
// message/timestamp), so this never fails the way `git commit` without
// --allow-empty would.
func (s *Store) Snapshot(ctx context.Context, ref, message string) (hash string, err error) {
	addArgs := append([]string{"add", "-A", "--"}, snapshotPathspecs...)
	if _, stderr, err := s.run(ctx, addArgs...); err != nil {
		return "", fmt.Errorf("checkpoint: snapshot add: %v: %s", err, strings.TrimSpace(stderr))
	}
	treeOut, stderr, err := s.run(ctx, "write-tree")
	if err != nil {
		return "", fmt.Errorf("checkpoint: snapshot write-tree: %v: %s", err, strings.TrimSpace(stderr))
	}
	treeHash := strings.TrimSpace(treeOut)
	if treeHash == "" {
		return "", errors.New("checkpoint: snapshot write-tree produced no tree hash")
	}
	// No -p/parent argument: an intentional orphan commit, see doc comment.
	// commit-tree is plumbing and never runs hooks (so --no-verify has
	// nothing to skip here, unlike Snapshot's old `git commit` call), but
	// --no-gpg-sign is still needed: commit-tree does honor commit.gpgsign.
	commitOut, stderr, err := s.run(ctx, "commit-tree", "--no-gpg-sign", treeHash, "-m", message)
	if err != nil {
		return "", fmt.Errorf("checkpoint: snapshot commit-tree: %v: %s", err, strings.TrimSpace(stderr))
	}
	hash = strings.TrimSpace(commitOut)
	if hash == "" {
		return "", errors.New("checkpoint: snapshot produced no commit hash")
	}
	if ref != "" {
		if _, stderr, err := s.run(ctx, "update-ref", ref, hash); err != nil {
			return "", fmt.Errorf("checkpoint: update ref %s: %v: %s", ref, err, strings.TrimSpace(stderr))
		}
	}
	return hash, nil
}

// symlinkMode is the git tree-entry mode for a symbolic link.
const symlinkMode = "120000"

// executableMode is the git tree-entry mode for an executable regular file.
const executableMode = "100755"

// treeEntry looks up path's git mode at commit via `git ls-tree`, the
// authoritative source for "does this path exist at this commit, and if so
// what mode" — unlike grepping `git show`'s stderr for "does not exist"
// (the old approach), this can't be fooled by a shell-quoted non-ASCII
// filename that never reaches `git show` correctly in the first place.
func (s *Store) treeEntry(ctx context.Context, commit, path string) (mode string, exists bool, err error) {
	stdout, stderr, err := s.runRawZ(ctx, "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return "", false, fmt.Errorf("checkpoint: ls-tree %s %s: %v: %s", commit, path, err, strings.TrimSpace(stderr))
	}
	entry := strings.TrimRight(stdout, "\x00")
	if entry == "" {
		return "", false, nil
	}
	// Format: "<mode> <type> <hash>\t<path>"
	tabIdx := strings.IndexByte(entry, '\t')
	if tabIdx < 0 {
		return "", false, fmt.Errorf("checkpoint: unexpected ls-tree output for %s at %s: %q", path, commit, entry)
	}
	fields := strings.Fields(entry[:tabIdx])
	if len(fields) < 1 {
		return "", false, fmt.Errorf("checkpoint: unexpected ls-tree output for %s at %s: %q", path, commit, entry)
	}
	return fields[0], true, nil
}

// fileAtCommit returns path's mode and raw blob content at commit, and
// whether it existed at all. Existence is determined via treeEntry (ls-tree
// against the exact path string we already have from ChangedPaths' -z
// output — see DECISION note there), not by pattern-matching `git show`'s
// stderr, which silently misreports non-ASCII paths as "does not exist"
// when they are C-quoted by git's default quotepath behavior.
func (s *Store) fileAtCommit(ctx context.Context, commit, path string) (content []byte, mode string, exists bool, err error) {
	mode, exists, err = s.treeEntry(ctx, commit, path)
	if err != nil {
		return nil, "", false, err
	}
	if !exists {
		return nil, "", false, nil
	}
	stdout, stderr, err := s.runRawBytes(ctx, "show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		return nil, "", false, fmt.Errorf("checkpoint: read %s at %s: %v: %s", path, commit, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), mode, true, nil
}

// runRawZ is like run but for -z (NUL-terminated) output, returned as a
// plain string (safe: shadow-git commands here never emit non-UTF8-safe
// binary framing outside blob content, which callers read via
// runRawBytes instead).
func (s *Store) runRawZ(ctx context.Context, args ...string) (string, string, error) {
	stdout, stderr, err := s.runRawBytes(ctx, args...)
	return stdout.String(), stderr.String(), err
}

// runRawBytes is like run but returns the raw stdout/stderr buffers (not
// stringified) — used for binary-safe file content reads.
func (s *Store) runRawBytes(ctx context.Context, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.workTree
	cmd.Env = s.baseEnv()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return &stdout, &stderr, err
}

// ChangedPaths returns the set of paths that differ between two shadow
// commits (git diff --name-only -z), the raw material for a run's
// touched-path set (DECISION 4/5) — computed from the actual filesystem
// diff rather than per-tool instrumentation, so it captures bash-caused
// changes too.
//
// -z is load-bearing, not cosmetic: without it, git's default quotepath
// behavior C-quotes any path containing non-ASCII bytes (café.txt becomes
// the literal 11-byte string "caf\303\251.txt"), which then fails every
// downstream `git show <commit>:<path>` lookup with "does not exist" —
// silently misreporting a damaged non-ASCII file as successfully reverted.
// -z guarantees raw, unquoted, NUL-separated paths regardless of
// core.quotepath, so this is the single fix point for that whole class of
// bug (see fileAtCommit/treeEntry, which consume these paths verbatim).
func (s *Store) ChangedPaths(ctx context.Context, from, to string) ([]string, error) {
	stdout, stderr, err := s.runRawZ(ctx, "diff", "--name-only", "-z", from, to, "--")
	if err != nil {
		return nil, fmt.Errorf("checkpoint: diff --name-only %s..%s: %v: %s", from, to, err, strings.TrimSpace(stderr))
	}
	var paths []string
	for _, p := range strings.Split(stdout, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Diff returns the unified diff between two shadow commits — the source of
// truth for checkpoint_diff_run (DECISION 5), since it captures every
// change to the tree (including bash side effects), not just the changes
// individual write/edit tool calls happened to report.
func (s *Store) Diff(ctx context.Context, from, to string) (string, error) {
	stdout, stderr, err := s.run(ctx, "diff", "--no-color", from, to, "--")
	if err != nil {
		return "", fmt.Errorf("checkpoint: diff %s..%s: %v: %s", from, to, err, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

// IgnoredPaths lists paths in the work tree currently excluded by the
// user's own .gitignore (git reads .gitignore from the work tree regardless
// of GIT_DIR, so this reflects the SANDBOX's real ignore rules, not the
// shadow store's). Used to honestly disclose the checkpoint system's
// blind spot: git add -A silently skips ignored paths, so a run that
// deletes or corrupts a gitignored file leaves no trace in any snapshot —
// RevertRun can restore nothing for it, and must say so (see
// RunRecord.IgnoredAtBegin / RevertResult.NotRestorable, manager.go).
func (s *Store) IgnoredPaths(ctx context.Context) ([]string, error) {
	stdout, stderr, err := s.runRawZ(ctx, "status", "--porcelain=v1", "-z", "--ignored=matching")
	if err != nil {
		return nil, fmt.Errorf("checkpoint: status --ignored: %v: %s", err, strings.TrimSpace(stderr))
	}
	var out []string
	for _, entry := range strings.Split(stdout, "\x00") {
		if len(entry) < 4 || !strings.HasPrefix(entry, "!!") {
			continue
		}
		out = append(out, entry[3:])
	}
	return out, nil
}

// RestorePath rewrites the working-tree entry at path to match its content
// (and mode) at commit, or removes it if it did not exist at commit.
// Returns which of the two happened.
//
// Two failure modes this guards against (both verified reproducible against
// the naive os.WriteFile-only implementation):
//
//  1. Symlink write-through: if the CURRENT on-disk entry at path is a
//     symlink, os.WriteFile follows it and overwrites whatever it points
//     at — restoring a regular file over a symlink would silently corrupt
//     the symlink's target file instead of replacing the link. Guarded by
//     an Lstat+Remove of any existing symlink before writing.
//  2. Symlink target corruption: if the SNAPSHOT entry itself is a symlink
//     (mode 120000), its blob content is the link's target path text, not
//     file content — os.WriteFile on that content into path would create a
//     regular file containing the target string instead of recreating the
//     link. Guarded by checking mode == symlinkMode and using os.Symlink.
func (s *Store) RestorePath(ctx context.Context, commit, path string) (restored bool, deleted bool, err error) {
	content, mode, exists, err := s.fileAtCommit(ctx, commit, path)
	if err != nil {
		return false, false, err
	}
	absPath := filepath.Join(s.workTree, filepath.FromSlash(path))
	if !exists {
		if rmErr := removeAny(absPath); rmErr != nil {
			if os.IsNotExist(rmErr) {
				// Already gone — restoring "delete this" is a no-op success.
				return false, true, nil
			}
			return false, false, fmt.Errorf("checkpoint: remove %s: %w", path, rmErr)
		}
		return false, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return false, false, fmt.Errorf("checkpoint: mkdir for %s: %w", path, err)
	}
	// Remove any existing entry at absPath first — critical when it is
	// currently a symlink (so we don't write through it) and harmless
	// otherwise (os.WriteFile below would just truncate+rewrite a plain
	// file in place, but Remove+recreate keeps both branches uniform and
	// correct for the symlink-target restore case too).
	if fi, statErr := os.Lstat(absPath); statErr == nil {
		if fi.Mode()&os.ModeSymlink != 0 || mode == symlinkMode {
			if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
				return false, false, fmt.Errorf("checkpoint: remove existing entry at %s: %w", path, rmErr)
			}
		}
	}
	if mode == symlinkMode {
		target := string(content)
		if err := os.Symlink(target, absPath); err != nil {
			return false, false, fmt.Errorf("checkpoint: symlink %s -> %s: %w", path, target, err)
		}
		return true, false, nil
	}
	// Preserve the file's mode from the shadow tree (git only distinguishes
	// 100644/100755/120000 — restore the executable bit accordingly, never
	// the destination's current/default mode, so reviving a deleted
	// executable doesn't silently come back non-executable).
	perm := os.FileMode(0o644)
	if mode == executableMode {
		perm = 0o755
	}
	if err := os.WriteFile(absPath, content, perm); err != nil {
		return false, false, fmt.Errorf("checkpoint: write %s: %w", path, err)
	}
	return true, false, nil
}

// removeAny removes path whether it is a regular file, directory, or
// symlink, without following a symlink (os.Remove already doesn't follow
// symlinks, but os.RemoveAll on a symlinked directory entry would — this
// makes that guarantee explicit at the call site).
func removeAny(path string) error {
	return os.Remove(path)
}

// GC prunes shadow-repo refs not in keepRefs and runs `git gc` so
// unreferenced objects are actually reclaimed on disk.
//
// This only actually reclaims anything because Snapshot (see its doc
// comment) commits each snapshot as an ORPHAN commit via write-tree +
// commit-tree, never `git commit` — with plain `git commit`, every
// snapshot's parent is the previous snapshot on the same HEAD branch, so
// keeping even the single most-recently-created run's ref keeps EVERY
// earlier run's commit reachable too via plain ancestry, no matter how
// many other refs get deleted below (verified: a 12-run/keep-2 GC left 49
// of 48 objects — i.e. reclaimed nothing at all). Orphan commits make each
// snapshot an independent reachability root, so deleting its ref is what
// actually lets `git gc --prune=now` collect it.
//
// The shadow repo's own INDEX is a second, easy-to-miss reachability root:
// every blob currently staged there (i.e. every file in the live working
// tree, since Snapshot always `git add -A`s before committing) stays
// reachable regardless of which refs exist — verified separately, and
// fixed here by resetting the index (`read-tree --empty`) before gc. Safe
// and self-healing: the next Snapshot's `git add -A` re-stages the live
// tree from scratch regardless of the index's prior state.
func (s *Store) GC(ctx context.Context, keepRefs map[string]bool) (prunedRefs int, err error) {
	stdout, stderr, err := s.run(ctx, "for-each-ref", "--format=%(refname)", "refs/huginn/")
	if err != nil {
		return 0, fmt.Errorf("checkpoint: list refs: %v: %s", err, strings.TrimSpace(stderr))
	}
	for _, ref := range strings.Split(stdout, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || keepRefs[ref] {
			continue
		}
		if _, stderr, err := s.run(ctx, "update-ref", "-d", ref); err != nil {
			return prunedRefs, fmt.Errorf("checkpoint: delete ref %s: %v: %s", ref, err, strings.TrimSpace(stderr))
		}
		prunedRefs++
	}
	if _, stderr, err := s.run(ctx, "read-tree", "--empty"); err != nil {
		return prunedRefs, fmt.Errorf("checkpoint: reset shadow index: %v: %s", err, strings.TrimSpace(stderr))
	}
	if _, _, err := s.run(ctx, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return prunedRefs, fmt.Errorf("checkpoint: expire reflog: %w", err)
	}
	if _, stderr, err := s.run(ctx, "gc", "--prune=now", "--quiet"); err != nil {
		return prunedRefs, fmt.Errorf("checkpoint: gc: %v: %s", err, strings.TrimSpace(stderr))
	}
	return prunedRefs, nil
}

// ObjectCount returns the number of loose+packed objects in the shadow
// store, used to report GC effectiveness.
func (s *Store) ObjectCount(ctx context.Context) (int, error) {
	stdout, stderr, err := s.run(ctx, "count-objects", "-v")
	if err != nil {
		return 0, fmt.Errorf("checkpoint: count-objects: %v: %s", err, strings.TrimSpace(stderr))
	}
	total := 0
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "count:") && !strings.HasPrefix(line, "in-pack:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(fields[1], "%d", &n); err == nil {
			total += n
		}
	}
	return total, nil
}
