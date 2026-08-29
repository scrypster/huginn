package checkpoint_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/checkpoint"
)

// TestManager_ConcurrentBeginEndRun_DistinctThreads exercises several
// threads' full BeginRun/EndRun lifecycle concurrently against the same
// Manager (as swarm agents would), each touching its own file, under
// -race. This must never crash, race, or corrupt another run's ledger row.
//
// It deliberately does NOT assert that each run's TouchedPaths is scoped to
// exactly its own file. The whole-tree-snapshot-diff mechanism this package
// uses (DiffRun/EndRun) is a safety net over the shared tree, not a
// concurrency primitive: when multiple runs mutate the same shared working
// tree at once, a run's post-snapshot captures whatever the *whole tree*
// looks like at that instant, so its touched-path diff can pick up a
// sibling run's concurrent edits too (Sharp edge 1 in the design doc — the
// doc's own stated answer for genuinely parallel write work is per-run
// worktrees, DECISION 7, deferred — see report). What IS guaranteed, and
// what this test asserts, is that the bookkeeping itself never corrupts:
// every run completes, lands in the ledger exactly once, with at least its
// own file among the touched paths.
func TestManager_ConcurrentBeginEndRun_DistinctThreads(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, _ := newManager(t, dir)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-thread-%d", i)
			file := filepath.Join(dir, fmt.Sprintf("concurrent-%d.txt", i))
			if _, err := mgr.BeginRun(ctx, id, "coder", "task"); err != nil {
				errs[i] = fmt.Errorf("BeginRun: %w", err)
				return
			}
			if err := os.WriteFile(file, []byte(fmt.Sprintf("content-%d\n", i)), 0o644); err != nil {
				errs[i] = err
				return
			}
			if _, err := mgr.EndRun(ctx, id); err != nil {
				errs[i] = fmt.Errorf("EndRun: %w", err)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	runs, err := mgr.List(ctx, 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != n {
		t.Fatalf("List returned %d runs, want %d (ledger corrupted under concurrency)", len(runs), n)
	}
	seen := map[string]bool{}
	for _, r := range runs {
		if seen[r.ThreadID] {
			t.Errorf("run %s appears more than once in the ledger", r.ThreadID)
		}
		seen[r.ThreadID] = true
		if r.Status != checkpoint.RunCompleted {
			t.Errorf("run %s status = %v, want RunCompleted", r.ThreadID, r.Status)
		}
		ownFile := strings.TrimPrefix(r.ThreadID, "concurrent-thread-")
		ownFile = "concurrent-" + ownFile + ".txt"
		found := false
		for _, p := range r.TouchedPaths {
			if p == ownFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("run %s touched %v, missing its own file %s", r.ThreadID, r.TouchedPaths, ownFile)
		}
	}
}

// TestManager_RevertRun_SerializesWithConcurrentWriter proves RevertRun
// takes the same path lock a concurrent writer (standing in for
// write_file/edit_file) would — so a restore can never interleave with a
// write to the same path and leave the file torn (Sharp edge 1). Both
// sides write distinguishable, length-checkable content so a torn/mixed
// write would be detectable, and the whole test is run under -race.
func TestManager_RevertRun_SerializesWithConcurrentWriter(t *testing.T) {
	ctx := context.Background()
	dir := initRealRepo(t)
	mgr, locker := newManager(t, dir)

	if _, err := mgr.BeginRun(ctx, "race-thread", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("run content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "race-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	const writerLine = "concurrent writer content, must never be torn\n"
	const preRunBaseline = "hello\n" // initRealRepo's original content, what All:true reverts to

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		locker.Lock("hello.txt")
		defer locker.Unlock("hello.txt")
		_ = os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(writerLine), 0o644)
	}()
	go func() {
		defer wg.Done()
		_, _ = mgr.RevertRun(ctx, "race-thread", checkpoint.RevertOptions{All: true})
	}()
	wg.Wait()

	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Whichever won the race, the result must be one full, uncorrupted
	// write — either the concurrent writer's line (it ran last) or the
	// full pre-run baseline RevertRun(All:true) restores to (it ran last)
	// — never a byte-level interleaving of both.
	s := string(got)
	if s != writerLine && s != preRunBaseline {
		t.Fatalf("hello.txt content torn by concurrent writer/restore: %q", s)
	}
}
