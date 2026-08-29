package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// storeDirName is the shadow git store's directory, relative to
// SandboxRoot. It lives under .huginn/, which the user's real .gitignore
// already excludes (see .gitignore:72 `.huginn/`) — so the shadow store
// never shows up in `git status` for the user's real repo and we never
// need to touch their .gitignore. DECISION 1.
const storeDirName = ".huginn/checkpoints/store.git"

// ledgerDBName is the run ledger's own SQLite file, a sibling of the shadow
// git store — also under .huginn/, also already gitignored.
const ledgerDBName = ".huginn/checkpoints/ledger.db"

// Store wraps a shadow git repository whose GIT_DIR is separate from the
// project's real .git and whose work-tree is the project's sandbox root.
// Because GIT_DIR differs, the shadow repo has its own index — nothing it
// does ever touches the user's real .git/index. DECISION 1.
type Store struct {
	gitDir   string // absolute path to the shadow GIT_DIR
	workTree string // absolute path to SandboxRoot
}

// NewStore creates (or opens) the shadow git store rooted at sandboxRoot.
func NewStore(ctx context.Context, sandboxRoot string) (*Store, error) {
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
	gitDir := filepath.Join(absRoot, storeDirName)
	s := &Store{gitDir: gitDir, workTree: absRoot}
	if err := s.ensureInit(ctx); err != nil {
		return nil, err
	}
	return s, nil
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
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+s.gitDir,
		"GIT_WORK_TREE="+s.workTree,
		// Isolate the shadow repo's identity from any repo-local git config
		// (commit hooks, signing, user.name) — checkpoints are an internal
		// bookkeeping mechanism, not user-attributed commits.
		"GIT_CONFIG_NOSYSTEM=1",
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
	if _, stderr, err := s.run(ctx, "init", "--quiet", "--initial-branch=checkpoints"); err != nil {
		return fmt.Errorf("checkpoint: init shadow store: %v: %s", err, strings.TrimSpace(stderr))
	}
	// Never GC-expire objects we still reference by hash even without a
	// ref pointing at them mid-commit — belt and suspenders around the
	// two-step add+commit below.
	if _, _, err := s.run(ctx, "config", "gc.auto", "0"); err != nil {
		return fmt.Errorf("checkpoint: configure shadow store: %w", err)
	}
	// Exclude the shadow store's own directory from what it snapshots, via
	// the shadow repo's OWN $GIT_DIR/info/exclude — never the user's real
	// .gitignore. This has to hold even for a sandbox whose real .gitignore
	// doesn't happen to list .huginn/ (most Huginn projects' does, see
	// .gitignore:72, but a snapshot store must not depend on that to avoid
	// snapshotting itself into a growing spiral).
	excludePath := filepath.Join(s.gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("checkpoint: create shadow store info dir: %w", err)
	}
	if err := os.WriteFile(excludePath, []byte("/.huginn/\n"), 0o644); err != nil {
		return fmt.Errorf("checkpoint: write shadow store exclude file: %w", err)
	}
	return nil
}

// snapshotPathspecs excludes the user's real .git (so it's recorded as an
// opaque gitlink, never walked for content). .huginn (the shadow store
// itself, and worktrees) is deliberately NOT pathspec-excluded here: it is
// already covered by the user's own .gitignore (see .gitignore:72
// `.huginn/`), and `git add -A` silently skips gitignored paths — but
// explicitly naming an ignored path as a pathspec (even negated) makes
// newer git refuse with "paths are ignored ... use -f", so we rely on the
// implicit .gitignore skip instead of restating it here.
var snapshotPathspecs = []string{".", ":(exclude).git"}

// Snapshot stages the current work tree (tracked + untracked-but-not-ignored
// files, honoring the user's own .gitignore since git reads it from the
// work tree regardless of GIT_DIR) and commits it in the shadow repo under
// the given ref, returning the resulting commit hash.
//
// An empty commit (nothing changed since the last snapshot on this ref) is
// allowed — a run that touches nothing still gets a valid pre/post pair.
func (s *Store) Snapshot(ctx context.Context, ref, message string) (hash string, err error) {
	addArgs := append([]string{"add", "-A", "--"}, snapshotPathspecs...)
	if _, stderr, err := s.run(ctx, addArgs...); err != nil {
		return "", fmt.Errorf("checkpoint: snapshot add: %v: %s", err, strings.TrimSpace(stderr))
	}
	commitArgs := []string{"commit", "--quiet", "--allow-empty", "-m", message}
	if _, stderr, err := s.run(ctx, commitArgs...); err != nil {
		return "", fmt.Errorf("checkpoint: snapshot commit: %v: %s", err, strings.TrimSpace(stderr))
	}
	stdout, stderr, err := s.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("checkpoint: resolve snapshot hash: %v: %s", err, strings.TrimSpace(stderr))
	}
	hash = strings.TrimSpace(stdout)
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

// PathsInTree returns whether path exists in the tree at commit, and if so
// the file's blob content.
func (s *Store) fileAtCommit(ctx context.Context, commit, path string) (content []byte, exists bool, err error) {
	stdout, stderr, err := s.runRaw(ctx, "show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		// git show exits non-zero both for "path does not exist at this
		// commit" and for real errors; grep the stderr for the specific
		// "does not exist"/"exists on disk, but not in" markers git uses.
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checkpoint: read %s at %s: %v: %s", path, commit, err, strings.TrimSpace(msg))
	}
	return stdout.Bytes(), true, nil
}

// runRaw is like run but returns the raw stdout buffer (not stringified) —
// used for binary-safe file content reads.
func (s *Store) runRaw(ctx context.Context, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.workTree
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+s.gitDir,
		"GIT_WORK_TREE="+s.workTree,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return &stdout, &stderr, err
}

// ChangedPaths returns the set of paths that differ between two shadow
// commits (git diff --name-only), the raw material for a run's touched-path
// set (DECISION 4/5) — computed from the actual filesystem diff rather than
// per-tool instrumentation, so it captures bash-caused changes too.
func (s *Store) ChangedPaths(ctx context.Context, from, to string) ([]string, error) {
	stdout, stderr, err := s.run(ctx, "diff", "--name-only", from, to, "--")
	if err != nil {
		return nil, fmt.Errorf("checkpoint: diff --name-only %s..%s: %v: %s", from, to, err, strings.TrimSpace(stderr))
	}
	var paths []string
	for _, line := range strings.Split(stdout, "\n") {
		if p := strings.TrimSpace(line); p != "" {
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

// RestorePath rewrites the working-tree file at path to match its content
// at commit, or removes it if it did not exist at commit. Returns which of
// the two happened.
func (s *Store) RestorePath(ctx context.Context, commit, path string) (restored bool, deleted bool, err error) {
	content, exists, err := s.fileAtCommit(ctx, commit, path)
	if err != nil {
		return false, false, err
	}
	absPath := filepath.Join(s.workTree, filepath.FromSlash(path))
	if !exists {
		if err := os.Remove(absPath); err != nil {
			if os.IsNotExist(err) {
				// Already gone — restoring "delete this" is a no-op success.
				return false, true, nil
			}
			return false, false, fmt.Errorf("checkpoint: remove %s: %w", path, err)
		}
		return false, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return false, false, fmt.Errorf("checkpoint: mkdir for %s: %w", path, err)
	}
	// Preserve the file's mode from the shadow tree where possible; default
	// to a sane 0644 when we can't determine it (git show doesn't return
	// mode via this path, so we keep the destination's existing mode if
	// present, otherwise 0644).
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(absPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(absPath, content, mode); err != nil {
		return false, false, fmt.Errorf("checkpoint: write %s: %w", path, err)
	}
	return true, false, nil
}

// GC prunes shadow-repo refs not in keepRefs and runs `git gc` so unreferenced
// objects (from pruned runs) are actually reclaimed on disk.
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
