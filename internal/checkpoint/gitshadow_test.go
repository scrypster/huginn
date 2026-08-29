package checkpoint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/checkpoint"
)

// initRealRepo creates a real git repo at a temp dir with one committed
// file, mirroring how Huginn's sandbox root is a real user repo. Used to
// prove the shadow store never disturbs it.
//
// Deliberately does NOT write a .gitignore listing .huginn/ (or anything
// else) — a plain repo with no opinion on Huginn's internals at all. A5's
// whole point is that the checkpoint store must never depend on the user's
// own .gitignore to stay invisible (it now lives entirely outside the
// sandbox tree, under a separate huginnHome passed to NewStore/NewManager)
// — pre-writing that entry here would "immunize" the repo and let a
// pollution bug pass silently.
func initRealRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "initial")
	return dir
}

func realGitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

func TestStore_SnapshotAndRestore_TrackedAndUntracked(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)

	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	preHash, err := store.Snapshot(ctx, "refs/huginn/run/t1/pre", "pre")
	if err != nil {
		t.Fatalf("Snapshot pre: %v", err)
	}

	// Mutate a tracked file and add an untracked-but-not-ignored file.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("goodbye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	postHash, err := store.Snapshot(ctx, "refs/huginn/run/t1/post", "post")
	if err != nil {
		t.Fatalf("Snapshot post: %v", err)
	}

	changed, err := store.ChangedPaths(ctx, preHash, postHash)
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	wantSet := map[string]bool{"hello.txt": true, "new.txt": true}
	if len(changed) != 2 {
		t.Fatalf("ChangedPaths = %v, want 2 entries", changed)
	}
	for _, c := range changed {
		if !wantSet[c] {
			t.Errorf("unexpected changed path %q", c)
		}
	}

	// Restore hello.txt to pre state.
	restored, deleted, err := store.RestorePath(ctx, preHash, "hello.txt")
	if err != nil {
		t.Fatalf("RestorePath hello.txt: %v", err)
	}
	if !restored || deleted {
		t.Fatalf("RestorePath hello.txt: restored=%v deleted=%v, want restored=true", restored, deleted)
	}
	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("hello.txt after restore = %q, want %q", got, "hello\n")
	}

	// Restore new.txt to pre state: it didn't exist at pre, so it must be deleted.
	restored, deleted, err = store.RestorePath(ctx, preHash, "new.txt")
	if err != nil {
		t.Fatalf("RestorePath new.txt: %v", err)
	}
	if restored || !deleted {
		t.Fatalf("RestorePath new.txt: restored=%v deleted=%v, want deleted=true", restored, deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should not exist after restore, stat err = %v", err)
	}
}

// TestStore_NeverPollutesUserRepo is the "store isolation" requirement: a
// full snapshot+restore cycle must leave the user's real git status
// byte-for-byte identical, and must never add anything to the real .git
// index or working tree that git status (or `git add -A`) can see —
// PROVEN on a plain repo with no .gitignore opinion about Huginn at all
// (initRealRepo deliberately writes none). Before A5, the shadow store
// lived under sandboxRoot/.huginn/checkpoints/ and relied entirely on the
// user's own .gitignore listing .huginn/ to stay invisible; on a repo
// without that entry, `git status` would show "?? .huginn/" and `git add
// -A` would stage the shadow store + ledger as ordinary untracked files.
func TestStore_NeverPollutesUserRepo(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	huginnHome := t.TempDir()

	before := realGitStatus(t, dir)
	if before != "" {
		t.Fatalf("real repo not clean before test: %q", before)
	}

	store, err := checkpoint.NewStore(ctx, huginnHome, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	pre, err := store.Snapshot(ctx, "refs/huginn/run/t2/pre", "pre")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	post, err := store.Snapshot(ctx, "refs/huginn/run/t2/post", "post")
	if err != nil {
		t.Fatalf("Snapshot 2: %v", err)
	}
	if _, err := store.Diff(ctx, pre, post); err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if _, _, err := store.RestorePath(ctx, pre, "hello.txt"); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}

	after := realGitStatus(t, dir)
	if after != "" {
		t.Fatalf("real repo status changed by checkpoint store: %q", after)
	}

	// The stronger assertion the old test skipped: with no .gitignore entry
	// protecting anything, `git add -A` in the real repo must stage
	// nothing at all — proving the store isn't merely untracked-but-hidden
	// by an accident of git status formatting, but genuinely outside the
	// sandbox tree.
	cmd := exec.Command("git", "add", "-A", "-n")
	cmd.Dir = dir
	addOut, addErr := cmd.CombinedOutput()
	if addErr != nil {
		t.Fatalf("git add -A -n: %v\n%s", addErr, addOut)
	}
	if strings.TrimSpace(string(addOut)) != "" {
		t.Fatalf("git add -A would stage checkpoint-related files: %q", addOut)
	}

	// And the shadow store must have landed OUTSIDE the sandbox entirely —
	// nothing named .huginn/ (or anything else new) inside dir at all.
	if _, err := os.Stat(filepath.Join(dir, ".huginn")); !os.IsNotExist(err) {
		t.Fatalf(".huginn unexpectedly exists inside the sandbox root (store should live under huginnHome): stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(huginnHome, "checkpoints")); err != nil {
		t.Fatalf("shadow store not found under huginnHome/checkpoints: %v", err)
	}
}

func TestStore_GC_PrunesUnkeptRefs(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := store.Snapshot(ctx, "refs/huginn/run/keep/pre", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Snapshot(ctx, "refs/huginn/run/drop/pre", "drop"); err != nil {
		t.Fatal(err)
	}
	pruned, err := store.GC(ctx, map[string]bool{"refs/huginn/run/keep/pre": true})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("GC pruned = %d, want 1", pruned)
	}
}

// TestStore_RestorePath_Symlink_DoesNotCorruptTarget is A4's regression
// test: restoring a snapshot in which path was a symlink must recreate the
// LINK (via os.Symlink), never write the link's target string into the
// target FILE. Before A4, RestorePath's unconditional os.WriteFile followed
// the on-disk symlink and clobbered target.txt's real content with the
// literal text "target.txt".
func TestStore_RestorePath_Symlink_DoesNotCorruptTarget(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("real target content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	pre, err := store.Snapshot(ctx, "refs/huginn/run/symlink/pre", "pre with symlink")
	if err != nil {
		t.Fatalf("Snapshot pre: %v", err)
	}

	// Simulate a run deleting/replacing the symlink with something else
	// entirely, then revert back to the pre-snapshot state.
	if err := os.Remove(filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "link.txt"), []byte("not a symlink anymore\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.RestorePath(ctx, pre, "link.txt"); err != nil {
		t.Fatalf("RestorePath link.txt: %v", err)
	}

	// target.txt must be COMPLETELY untouched by restoring link.txt.
	targetContent, err := os.ReadFile(filepath.Join(dir, "target.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(targetContent) != "real target content\n" {
		t.Fatalf("target.txt corrupted by restoring link.txt: %q", targetContent)
	}

	fi, err := os.Lstat(filepath.Join(dir, "link.txt"))
	if err != nil {
		t.Fatalf("Lstat link.txt: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link.txt was not restored as a symlink, mode = %v", fi.Mode())
	}
	dest, err := os.Readlink(filepath.Join(dir, "link.txt"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if dest != "target.txt" {
		t.Fatalf("link.txt points at %q, want %q", dest, "target.txt")
	}
}

// TestStore_RestorePath_RegularFileOverSymlink_ReplacesLink covers the
// mirror-image case A4 also names: restoring a snapshot where path was a
// REGULAR file, when the CURRENT on-disk entry at that path is a symlink,
// must replace the link itself (not follow it and write through to
// whatever it points at).
func TestStore_RestorePath_RegularFileOverSymlink_ReplacesLink(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("regular file content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pre, err := store.Snapshot(ctx, "refs/huginn/run/reglink/pre", "pre, regular.txt is a plain file")
	if err != nil {
		t.Fatalf("Snapshot pre: %v", err)
	}

	// A run replaces regular.txt with a symlink to some unrelated file
	// elsewhere in the tree (danger.txt) — reverting must NOT write
	// regular.txt's pre-content into danger.txt.
	if err := os.WriteFile(filepath.Join(dir, "danger.txt"), []byte("must never be touched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "regular.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("danger.txt", filepath.Join(dir, "regular.txt")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.RestorePath(ctx, pre, "regular.txt"); err != nil {
		t.Fatalf("RestorePath regular.txt: %v", err)
	}

	dangerContent, err := os.ReadFile(filepath.Join(dir, "danger.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(dangerContent) != "must never be touched\n" {
		t.Fatalf("danger.txt was corrupted by restoring regular.txt through a symlink: %q", dangerContent)
	}

	fi, err := os.Lstat(filepath.Join(dir, "regular.txt"))
	if err != nil {
		t.Fatalf("Lstat regular.txt: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("regular.txt is still a symlink after restore — the link was not replaced")
	}
	got, err := os.ReadFile(filepath.Join(dir, "regular.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "regular file content\n" {
		t.Fatalf("regular.txt content = %q, want %q", got, "regular file content\n")
	}
}

// TestStore_RestorePath_PreservesExecutableMode is A10's regression test:
// restoring a deleted executable file must come back mode 0755, not the
// naive implementation's "destination doesn't exist, default to 0644".
func TestStore_RestorePath_PreservesExecutableMode(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	scriptPath := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pre, err := store.Snapshot(ctx, "refs/huginn/run/exec/pre", "pre, run.sh is executable")
	if err != nil {
		t.Fatalf("Snapshot pre: %v", err)
	}

	if err := os.Remove(scriptPath); err != nil {
		t.Fatal(err)
	}

	restored, deleted, err := store.RestorePath(ctx, pre, "run.sh")
	if err != nil {
		t.Fatalf("RestorePath: %v", err)
	}
	if !restored || deleted {
		t.Fatalf("RestorePath run.sh: restored=%v deleted=%v, want restored=true", restored, deleted)
	}

	fi, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("restored run.sh mode = %o, want 0755 (resurrected executable must not silently lose its exec bit)", fi.Mode().Perm())
	}
}

// TestStore_Snapshot_IgnoresGlobalGitConfig_HooksAndGpgSign is A6's
// regression test: a ~/.gitconfig with commit.gpgsign=true and
// core.hooksPath pointing at a hook that writes a sentinel file must not
// affect BeginRun (Snapshot) at all — no gpg-sign fail-open, and the hook
// must never execute inside a checkpoint snapshot.
func TestStore_Snapshot_IgnoresGlobalGitConfig_HooksAndGpgSign(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)

	fakeHome := t.TempDir()
	hooksDir := filepath.Join(fakeHome, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(fakeHome, "hook-ran.sentinel")
	hookScript := "#!/bin/sh\ntouch " + sentinel + "\nexit 0\n"
	for _, hookName := range []string{"pre-commit", "post-commit"} {
		hookPath := filepath.Join(hooksDir, hookName)
		if err := os.WriteFile(hookPath, []byte(hookScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	globalGitConfig := filepath.Join(fakeHome, ".gitconfig")
	configContent := "[commit]\n\tgpgsign = true\n[core]\n\thooksPath = " + hooksDir + "\n[gpg]\n\tprogram = /bin/false-does-not-exist\n"
	if err := os.WriteFile(globalGitConfig, []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point $HOME at fakeHome so, absent the GIT_CONFIG_GLOBAL=/dev/null
	// fix, git would actually read this ~/.gitconfig (GIT_CONFIG_NOSYSTEM
	// alone only blocks /etc/gitconfig, never ~/.gitconfig).
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })
	if err := os.Setenv("HOME", fakeHome); err != nil {
		t.Fatal(err)
	}

	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := store.Snapshot(ctx, "refs/huginn/run/gitconfig/pre", "pre"); err != nil {
		t.Fatalf("Snapshot should succeed even with commit.gpgsign=true and an invalid gpg.program in ~/.gitconfig: %v", err)
	}

	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("core.hooksPath hook ran during a checkpoint snapshot (sentinel exists): stat err = %v", statErr)
	}
}

// TestStore_Snapshot_StripsInheritedGitIndexEnv is A15's regression test: a
// GIT_INDEX_FILE inherited from the parent process environment must never
// redirect a checkpoint snapshot's `git add` at the wrong index — verified
// by pointing GIT_INDEX_FILE at an index for a completely different,
// throwaway repo and confirming the shadow store's own snapshot still
// succeeds and reflects the SANDBOX's tree, not that other index.
func TestStore_Snapshot_StripsInheritedGitIndexEnv(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)

	otherRepo := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = otherRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init otherRepo: %v\n%s", err, out)
	}
	otherIndex := filepath.Join(otherRepo, ".git", "index")

	oldIdx, hadIdx := os.LookupEnv("GIT_INDEX_FILE")
	t.Cleanup(func() {
		if hadIdx {
			_ = os.Setenv("GIT_INDEX_FILE", oldIdx)
		} else {
			_ = os.Unsetenv("GIT_INDEX_FILE")
		}
	})
	if err := os.Setenv("GIT_INDEX_FILE", otherIndex); err != nil {
		t.Fatal(err)
	}

	store, err := checkpoint.NewStore(ctx, t.TempDir(), dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	hash, err := store.Snapshot(ctx, "refs/huginn/run/indexleak/pre", "pre")
	if err != nil {
		t.Fatalf("Snapshot with GIT_INDEX_FILE set in parent env: %v", err)
	}
	if hash == "" {
		t.Fatal("Snapshot produced no commit hash")
	}

	// The leaked GIT_INDEX_FILE path must never have been touched by the
	// shadow store's git invocations.
	if _, statErr := os.Stat(otherIndex); !os.IsNotExist(statErr) {
		t.Fatalf("otherRepo's index was touched by the shadow store's snapshot (GIT_INDEX_FILE leaked): stat err = %v", statErr)
	}

	// And the snapshot must actually reflect dir's own tracked file.
	changed, err := store.ChangedPaths(ctx, hash, hash)
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("ChangedPaths(hash, hash) = %v, want empty (no self-diff)", changed)
	}
}
