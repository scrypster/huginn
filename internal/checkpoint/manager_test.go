package checkpoint_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/checkpoint"
)

// fakeLocker is an in-test stand-in for *tools.FileLockManager, tracking
// how many times each path was locked so tests can assert restore actually
// takes the same locks writers do.
type fakeLocker struct {
	mu     sync.Mutex
	counts map[string]int
	delay  time.Duration // if set, Lock sleeps briefly while holding the mutex — for race exercising
}

func newFakeLocker() *fakeLocker { return &fakeLocker{counts: map[string]int{}} }

func (f *fakeLocker) Lock(path string) {
	f.mu.Lock()
	f.counts[path]++
}
func (f *fakeLocker) Unlock(path string) {
	f.mu.Unlock()
}

func newManager(t *testing.T, dir string) (*checkpoint.Manager, *fakeLocker) {
	t.Helper()
	locker := newFakeLocker()
	mgr, err := checkpoint.NewManager(context.Background(), t.TempDir(), dir, locker)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, locker
}

// newManagerWithHome is newManager but exposes the huginnHome directory the
// shadow store + ledger were rooted under, for tests that need to locate
// (or corrupt) the store on disk directly.
func newManagerWithHome(t *testing.T, dir string) (mgr *checkpoint.Manager, locker *fakeLocker, huginnHome string) {
	t.Helper()
	locker = newFakeLocker()
	huginnHome = t.TempDir()
	var err error
	mgr, err = checkpoint.NewManager(context.Background(), huginnHome, dir, locker)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, locker, huginnHome
}

func TestManager_BeginEndRun_TouchedPaths(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "thread-1", "coder", "edit hello.txt"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brand_new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := mgr.EndRun(ctx, "thread-1")
	if err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	if rec.Status != checkpoint.RunCompleted {
		t.Fatalf("Status = %v, want RunCompleted", rec.Status)
	}
	want := map[string]bool{"hello.txt": true, "brand_new.txt": true}
	if len(rec.TouchedPaths) != 2 {
		t.Fatalf("TouchedPaths = %v, want 2 entries", rec.TouchedPaths)
	}
	for _, p := range rec.TouchedPaths {
		if !want[p] {
			t.Errorf("unexpected touched path %q", p)
		}
	}
}

func TestManager_RevertRun_RoundTrip_ByteForByte(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, locker := newManager(t, dir)

	original, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.BeginRun(ctx, "thread-2", "coder", "mutate then more edits"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("first edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "thread-2"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	// More edits happen after checkpoint (simulating "checkpoint -> more
	// edits" from a later run or the same agent continuing).
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("second edit, should be undone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := mgr.RevertRun(ctx, "thread-2", checkpoint.RevertOptions{All: true})
	if err != nil {
		t.Fatalf("RevertRun: %v", err)
	}
	if len(result.Restored) != 1 || result.Restored[0] != "hello.txt" {
		t.Fatalf("Restored = %v, want [hello.txt]", result.Restored)
	}

	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("after revert, hello.txt = %q, want original %q", got, original)
	}
	// A1: RevertRun locks on the same absolute, symlink-resolved path
	// namespace write_file/edit_file's ResolveSandboxed produces — not the
	// bare repo-relative slash path from the shadow-git diff — so a shared
	// FileLockManager instance actually serializes the two. Assert against
	// that canonical form, not the relative "hello.txt".
	resolvedHello, evalErr := filepath.EvalSymlinks(filepath.Join(dir, "hello.txt"))
	if evalErr != nil {
		t.Fatalf("EvalSymlinks: %v", evalErr)
	}
	if locker.counts[resolvedHello] == 0 {
		t.Errorf("RevertRun did not take the file lock for %s (locked keys: %v)", resolvedHello, locker.counts)
	}
	if len(result.NotRestorable) == 0 {
		t.Error("RevertResult.NotRestorable should always disclose out-of-scope state")
	}
}

func TestManager_RevertRun_PreservesHandEditsByDefault(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "thread-3", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("run's edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "thread-3"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	// A human (or another run) hand-edits the file after this run finished.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hand edit, must survive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := mgr.RevertRun(ctx, "thread-3", checkpoint.RevertOptions{}) // default: preserve hand-edits
	if err != nil {
		t.Fatalf("RevertRun: %v", err)
	}
	if len(result.Restored) != 0 {
		t.Fatalf("Restored = %v, want none (file was hand-edited)", result.Restored)
	}
	if len(result.SkippedEdited) != 1 || result.SkippedEdited[0] != "hello.txt" {
		t.Fatalf("SkippedEdited = %v, want [hello.txt]", result.SkippedEdited)
	}

	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hand edit, must survive\n" {
		t.Fatalf("hand-edited file was overwritten: %q", got)
	}
}

func TestManager_RevertRun_PushedGuard(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "thread-4", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("pushed change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "thread-4"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	if err := mgr.MarkPushed(ctx, "thread-4", "https://github.com/example/repo/pull/1"); err != nil {
		t.Fatalf("MarkPushed: %v", err)
	}

	if _, err := mgr.RevertRun(ctx, "thread-4", checkpoint.RevertOptions{}); !errors.Is(err, checkpoint.ErrAlreadyPushed) {
		t.Fatalf("RevertRun on a pushed run = %v, want ErrAlreadyPushed", err)
	}

	result, err := mgr.RevertRun(ctx, "thread-4", checkpoint.RevertOptions{All: true, AllowAfterPush: true})
	if err != nil {
		t.Fatalf("RevertRun with AllowAfterPush: %v", err)
	}
	if len(result.Restored) != 1 {
		t.Fatalf("Restored = %v, want 1 entry", result.Restored)
	}
	if result.Warning == "" {
		t.Error("expected a Warning noting the pushed commit itself was not rewritten")
	}
}

func TestManager_BeginRun_CaptureFailure_SurfacesHonestly(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _, huginnHome := newManagerWithHome(t, dir)

	// Force a capture failure by making the shadow store's GIT_DIR
	// unwritable after Manager has already created it at construction. The
	// store now lives under huginnHome/checkpoints/<hash>/store.git (A5),
	// not inside the sandbox — locate it via glob rather than hard-coding
	// the hash.
	matches, globErr := filepath.Glob(filepath.Join(huginnHome, "checkpoints", "*", "store.git"))
	if globErr != nil || len(matches) != 1 {
		t.Fatalf("locate shadow store under %s: matches=%v err=%v", huginnHome, matches, globErr)
	}
	storeGitDir := matches[0]
	if err := os.Chmod(storeGitDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(storeGitDir, 0o755) })

	rec, err := mgr.BeginRun(ctx, "thread-5", "coder", "task")
	if err == nil {
		t.Fatal("BeginRun with an unwritable shadow store should return an error, got nil")
	}
	if rec.Status != checkpoint.RunCaptureFailed {
		t.Fatalf("Status = %v, want RunCaptureFailed — a failed capture must never look like a silent no-op success", rec.Status)
	}
	if rec.CaptureError == "" {
		t.Error("CaptureError should be set when Status is RunCaptureFailed")
	}

	// The ledger itself must also reflect the failure, not silently omit it.
	stored, err := mgr.Get(ctx, "thread-5")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != checkpoint.RunCaptureFailed {
		t.Fatalf("ledger Status = %v, want RunCaptureFailed", stored.Status)
	}

	// Revert must refuse rather than pretend there's something to restore.
	if _, err := mgr.RevertRun(ctx, "thread-5", checkpoint.RevertOptions{}); err == nil {
		t.Error("RevertRun on a capture-failed run should error, not silently succeed")
	}
}

func TestManager_GC_RetentionWindow(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	for i := 0; i < 5; i++ {
		id := "thread-gc-" + string(rune('a'+i))
		if _, err := mgr.BeginRun(ctx, id, "coder", "task"); err != nil {
			t.Fatalf("BeginRun(%s): %v", id, err)
		}
		if _, err := mgr.EndRun(ctx, id); err != nil {
			t.Fatalf("EndRun(%s): %v", id, err)
		}
	}

	result, err := mgr.GC(ctx, checkpoint.GCOptions{KeepRuns: 2})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if result.PrunedRuns != 3 {
		t.Fatalf("PrunedRuns = %d, want 3", result.PrunedRuns)
	}
	if result.KeptRuns != 2 {
		t.Fatalf("KeptRuns = %d, want 2", result.KeptRuns)
	}

	remaining, err := mgr.List(ctx, 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("List after GC = %d entries, want 2", len(remaining))
	}
}

func TestManager_DiffRun_ReflectsBashLikeChanges(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "thread-diff", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	// Simulate a bash-caused change (no tool-level diff metadata involved
	// at all — just a raw filesystem mutation), proving DiffRun/EndRun
	// don't depend on write_file/edit_file instrumentation.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("bash rm -rf style edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "thread-diff"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	diff, err := mgr.DiffRun(ctx, "thread-diff")
	if err != nil {
		t.Fatalf("DiffRun: %v", err)
	}
	if diff == "" {
		t.Fatal("DiffRun returned empty diff for a run that changed a file")
	}
	if !contains(diff, "hello.txt") {
		t.Fatalf("DiffRun output does not mention hello.txt:\n%s", diff)
	}
}

// TestManager_RevertRun_NonASCIIFilenames_ByteExact is A3's regression
// test: files with non-ASCII names (café.txt, 日本語.txt) and a file whose
// name contains a space must be damaged during a run and then restored
// byte-for-byte on revert. Before A3, `git diff --name-only` (no -z)
// C-quoted café.txt as the literal string "caf\303\251.txt"; every
// downstream `git show <commit>:<that literal>` failed with "does not
// exist", which fileAtCommit's old stderr-substring check misread as
// "file didn't exist at the pre-snapshot" — so RevertRun reported the file
// Deleted (a false success) while the damaged content stayed on disk.
func TestManager_RevertRun_NonASCIIFilenames_ByteExact(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	files := map[string]string{
		"café.txt":       "café original content\n",
		"日本語.txt":        "日本語 original content\n",
		"with space.txt": "space-named original content\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	// A separate BeginRun so these files are committed into the shadow
	// store's history before the run under test starts (mirroring "these
	// files already existed when the run began").
	if _, err := mgr.BeginRun(ctx, "seed-thread", "coder", "seed"); err != nil {
		t.Fatalf("BeginRun seed: %v", err)
	}
	if _, err := mgr.EndRun(ctx, "seed-thread"); err != nil {
		t.Fatalf("EndRun seed: %v", err)
	}

	if _, err := mgr.BeginRun(ctx, "unicode-thread", "coder", "damage non-ascii files"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("DAMAGED\n"), 0o644); err != nil {
			t.Fatalf("damage %s: %v", name, err)
		}
	}
	rec, err := mgr.EndRun(ctx, "unicode-thread")
	if err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	if len(rec.TouchedPaths) != len(files) {
		t.Fatalf("TouchedPaths = %v, want %d entries (one per damaged file)", rec.TouchedPaths, len(files))
	}

	result, err := mgr.RevertRun(ctx, "unicode-thread", checkpoint.RevertOptions{All: true})
	if err != nil {
		t.Fatalf("RevertRun: %v", err)
	}
	if len(result.Deleted) != 0 {
		t.Fatalf("Deleted = %v, want none — non-ASCII files must be recognized as EXISTING at the pre-snapshot and restored, not misreported as deleted", result.Deleted)
	}
	if len(result.Restored) != len(files) {
		t.Fatalf("Restored = %v, want %d entries", result.Restored, len(files))
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s after revert: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s after revert = %q, want byte-exact original %q", name, got, want)
		}
	}
}

// TestManager_RevertRun_DisclosesIgnoredFilesTouched is A8's regression
// test: a run that deletes a gitignored file leaves no trace in any
// snapshot (git add -A silently skips ignored paths) — RevertRun can't
// restore it, but the result MUST say so explicitly rather than silently
// omitting it, per the design doc's disclosure promise.
func TestManager_RevertRun_DisclosesIgnoredFilesTouched(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add .gitignore: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "--quiet", "-m", "add gitignore")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit .gitignore: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.env"), []byte("API_KEY=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.BeginRun(ctx, "ignored-thread", "coder", "task that deletes a gitignored file"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "secret.env")); err != nil {
		t.Fatal(err)
	}
	// Also touch a real tracked file so this run has something to revert
	// and RevertRun doesn't take the empty-touched-paths early return.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "ignored-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	result, err := mgr.RevertRun(ctx, "ignored-thread", checkpoint.RevertOptions{All: true})
	if err != nil {
		t.Fatalf("RevertRun: %v", err)
	}
	found := false
	for _, note := range result.NotRestorable {
		if strings.Contains(note, "secret.env") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("RevertResult.NotRestorable = %v, want a note disclosing secret.env was gitignored and unprotected", result.NotRestorable)
	}
}

// TestManager_RevertRun_PartialFailure_ReturnsPartialResult is A9's
// regression test: when one touched path fails to restore, RevertRun must
// still restore and report every OTHER path it can, returning the partial
// RevertResult alongside the error rather than discarding it.
func TestManager_RevertRun_PartialFailure_ReturnsPartialResult(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "partial-thread", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := mgr.EndRun(ctx, "partial-thread")
	if err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	if len(rec.TouchedPaths) != 2 {
		t.Fatalf("TouchedPaths = %v, want 2 entries", rec.TouchedPaths)
	}

	// Make second.txt's PARENT DIRECTORY unwritable so restoring second.txt
	// fails (can't recreate/mkdir into it) while hello.txt's restore, in a
	// writable directory, must still succeed.
	if err := os.Remove(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatal(err)
	}
	blockedDir := filepath.Join(dir, "blocked")
	// Replace the sandbox root's ability to create second.txt by making
	// second.txt itself an existing, read-only, non-writable directory (so
	// os.WriteFile onto that path fails, whichever the OS's exact error) —
	// simplest cross-platform way to force a single-path restore failure
	// without touching the whole sandbox.
	_ = blockedDir
	if err := os.Mkdir(filepath.Join(dir, "second.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "second.txt", "not-a-file-should-block-write"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := mgr.RevertRun(ctx, "partial-thread", checkpoint.RevertOptions{All: true})
	if err == nil {
		t.Fatal("RevertRun should report an error when one of two paths fails to restore")
	}
	if len(result.Restored) != 1 || result.Restored[0] != "hello.txt" {
		t.Fatalf("Restored = %v, want [hello.txt] restored despite second.txt failing (A9: partial results must not be discarded on error)", result.Restored)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %v, want exactly 1 entry for second.txt", result.Failed)
	}
	if _, ok := result.Failed["second.txt"]; !ok {
		t.Fatalf("Failed = %v, want a key for second.txt", result.Failed)
	}

	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("hello.txt after partial revert = %q, want restored to original %q", got, "hello\n")
	}
}

// TestManager_GC_ActuallyReclaimsObjects is A12's regression test: GC must
// genuinely shrink the shadow store's object count, not just delete ledger
// rows while every snapshot commit stays reachable forever via the
// `checkpoints` branch HEAD (verified: before A12, ObjectsBefore ==
// ObjectsAfter regardless of how many runs were pruned).
func TestManager_GC_ActuallyReclaimsObjects(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	// Enough runs, each writing a reasonably sized unique file, that pruned
	// snapshots represent a measurable number of objects.
	const n = 12
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("gc-thread-%02d", i)
		if _, err := mgr.BeginRun(ctx, id, "coder", "task"); err != nil {
			t.Fatalf("BeginRun(%s): %v", id, err)
		}
		content := strings.Repeat(fmt.Sprintf("run-%d-", i), 200)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("gc-file-%02d.txt", i)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := mgr.EndRun(ctx, id); err != nil {
			t.Fatalf("EndRun(%s): %v", id, err)
		}
	}

	result, err := mgr.GC(ctx, checkpoint.GCOptions{KeepRuns: 2})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if result.PrunedRuns == 0 {
		t.Fatal("GC pruned 0 runs, expected several")
	}
	if result.ObjectsAfter >= result.ObjectsBefore {
		t.Fatalf("GC claims ObjectsBefore=%d ObjectsAfter=%d — pruning %d run(s) reclaimed nothing (A12: the `checkpoints` branch HEAD keeps every snapshot reachable regardless of ref deletion unless it is also detached)",
			result.ObjectsBefore, result.ObjectsAfter, result.PrunedRuns)
	}
}

// TestManager_RevertRun_SetsRunRevertedStatus is A16's regression test:
// RunReverted was defined but never assigned anywhere — checkpoint_list
// would show a successfully-reverted run's status as "completed" forever.
func TestManager_RevertRun_SetsRunRevertedStatus(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "status-thread", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "status-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	if _, err := mgr.RevertRun(ctx, "status-thread", checkpoint.RevertOptions{All: true}); err != nil {
		t.Fatalf("RevertRun: %v", err)
	}

	rec, err := mgr.Get(ctx, "status-thread")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Status != checkpoint.RunReverted {
		t.Fatalf("status after successful revert = %v, want RunReverted", rec.Status)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
