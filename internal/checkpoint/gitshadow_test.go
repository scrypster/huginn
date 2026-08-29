package checkpoint_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/checkpoint"
)

// initRealRepo creates a real git repo at a temp dir with one committed
// file, mirroring how Huginn's sandbox root is a real user repo. Used to
// prove the shadow store never disturbs it.
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
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".huginn/\n"), 0o644); err != nil {
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

	store, err := checkpoint.NewStore(ctx, dir)
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
// index or working tree that git status can see.
func TestStore_NeverPollutesUserRepo(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)

	before := realGitStatus(t, dir)
	if before != "" {
		t.Fatalf("real repo not clean before test: %q", before)
	}

	store, err := checkpoint.NewStore(ctx, dir)
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

	// The .gitignore in the real repo already excludes .huginn/ — assert
	// the shadow store actually landed there, so isolation isn't accidental.
	if _, err := os.Stat(filepath.Join(dir, ".huginn", "checkpoints", "store.git")); err != nil {
		t.Fatalf("shadow store not found under .huginn/checkpoints: %v", err)
	}
}

func TestStore_GC_PrunesUnkeptRefs(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	store, err := checkpoint.NewStore(ctx, dir)
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
