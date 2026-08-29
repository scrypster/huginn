package checkpoint_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	mgr, err := checkpoint.NewManager(context.Background(), dir, locker)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, locker
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
	if locker.counts["hello.txt"] == 0 {
		t.Error("RevertRun did not take the file lock for hello.txt")
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
	mgr, _ := newManager(t, dir)

	// Force a capture failure by making the shadow store's GIT_DIR
	// unwritable after Manager has already created it at construction.
	storeGitDir := filepath.Join(dir, ".huginn", "checkpoints", "store.git")
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
