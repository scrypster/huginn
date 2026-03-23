package threadmgr_test

// hardening_iter1_test.go — tests added during Hardening Iteration 1.
// Covers bugs found and fixed in manager.go, preview.go, summary_cache.go,
// leases.go, and the spawn/cancel path.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// ─── Bug 1: Complete on a cancelled thread must remain cancelled ────────────

func TestComplete_AfterCancel_StatusStaysCancelled(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "do work",
	})

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(thread.ID, ctx, cancel)

	// Cancel the thread first.
	tm.Cancel(thread.ID)

	// Now attempt to complete it — should be a no-op.
	tm.Complete(thread.ID, threadmgr.FinishSummary{
		Summary: "should not overwrite",
		Status:  "completed",
	})

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != threadmgr.StatusCancelled {
		t.Errorf("expected StatusCancelled after Complete on cancelled thread, got %s", got.Status)
	}
	if got.Summary != nil {
		t.Error("expected nil Summary: Complete should not have stored a summary")
	}
}

// ─── Bug 2: Cancel after Complete must remain done ──────────────────────────

func TestCancel_AfterComplete_StatusStaysDone(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "sam",
		Task:      "review PR",
	})

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(thread.ID, ctx, cancel)
	tm.Complete(thread.ID, threadmgr.FinishSummary{
		Summary: "all good",
		Status:  "completed",
	})

	// Cancelling a Done thread should be a no-op.
	tm.Cancel(thread.ID)

	got, _ := tm.Get(thread.ID)
	if got.Status != threadmgr.StatusDone {
		t.Errorf("expected StatusDone after Cancel on done thread, got %s", got.Status)
	}
}

// ─── Bug 3: ResolveDependencies must not produce duplicate IDs ──────────────

func TestResolveDependencies_NoDuplicatesOnExplicitPlusHint(t *testing.T) {
	tm := threadmgr.New()

	upstream, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "impl",
	})

	// downstream lists the upstream thread both explicitly AND via a hint.
	downstream, _ := tm.Create(threadmgr.CreateParams{
		SessionID:      "sess-1",
		AgentID:        "sam",
		Task:           "qa",
		DependsOn:      []string{upstream.ID}, // explicit
		DependsOnHints: []string{"stacy"},     // hint resolves to the same thread
	})

	resolved := tm.ResolveDependencies(downstream.ID)
	// The upstream.ID must appear exactly once.
	seen := make(map[string]int)
	for _, id := range resolved {
		seen[id]++
	}
	if seen[upstream.ID] != 1 {
		t.Errorf("expected upstream.ID to appear exactly once, got %d: %v", seen[upstream.ID], resolved)
	}
}

func TestResolveDependencies_CalledTwice_StillNoDuplicates(t *testing.T) {
	tm := threadmgr.New()
	upstream, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "alex",
		Task:      "build",
	})
	downstream, _ := tm.Create(threadmgr.CreateParams{
		SessionID:      "sess-1",
		AgentID:        "sam",
		Task:           "test",
		DependsOnHints: []string{"alex"},
	})

	// First call resolves and clears hints.
	first := tm.ResolveDependencies(downstream.ID)
	// Second call should be a no-op (hints cleared), returning same slice.
	second := tm.ResolveDependencies(downstream.ID)

	if len(first) != 1 || len(second) != 1 {
		t.Errorf("expected 1 dep each call, got %d and %d", len(first), len(second))
	}
	if first[0] != second[0] {
		t.Errorf("dep IDs diverged: %q vs %q", first[0], second[0])
	}
	if first[0] != upstream.ID {
		t.Errorf("expected upstream.ID %q, got %q", upstream.ID, first[0])
	}
}

// ─── Bug 4: DelegationPreviewGate double-Approve for same key ───────────────

func TestDelegationPreview_ConcurrentApprove_SecondReturnsFalse(t *testing.T) {
	gate := threadmgr.NewDelegationPreviewGate(true)

	ready1 := make(chan struct{})
	result1 := make(chan bool, 1)

	// Start first Approve (will block waiting for Ack).
	go func() {
		approved := gate.Approve(
			context.Background(), "sess-x", "t-dup", "Stacy", "task", "",
			func(_, _ string, _ map[string]any) { close(ready1) },
		)
		result1 <- approved
	}()

	// Wait until first Approve is registered.
	<-ready1

	// Second Approve for the same key should return false immediately
	// rather than overwriting the first goroutine's channel.
	secondResult := make(chan bool, 1)
	go func() {
		approved := gate.Approve(
			context.Background(), "sess-x", "t-dup", "Stacy", "task", "", nil,
		)
		secondResult <- approved
	}()

	select {
	case approved := <-secondResult:
		if approved {
			t.Error("second concurrent Approve should return false, not true")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second Approve did not return promptly")
	}

	// Unblock the first goroutine.
	gate.Ack("sess-x", "t-dup", true)
	select {
	case approved := <-result1:
		if !approved {
			t.Error("first Approve should have been approved via Ack")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first Approve did not return after Ack")
	}
}

// ─── Bug 5: Cancel releases file leases ─────────────────────────────────────

func TestCancel_ReleasesLeases(t *testing.T) {
	tm := threadmgr.New()

	t1, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "write main.go",
	})
	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(t1.ID, ctx, cancel)

	// Acquire a lease for thread t1.
	conflicts, err := tm.AcquireLeases(t1.ID, []string{"main.go"})
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("unexpected conflict acquiring lease: %v %v", err, conflicts)
	}

	// Cancel t1 — leases should be automatically released.
	tm.Cancel(t1.ID)

	// A different thread should now be able to acquire the same file.
	t2, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "sam",
		Task:      "also write main.go",
	})
	conflicts2, err2 := tm.AcquireLeases(t2.ID, []string{"main.go"})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if len(conflicts2) > 0 {
		t.Errorf("lease was not released on Cancel: conflicts=%v", conflicts2)
	}
}

// ─── Bug 6: Complete releases file leases ───────────────────────────────────

func TestComplete_ReleasesLeases(t *testing.T) {
	tm := threadmgr.New()

	t1, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "write db.go",
	})
	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(t1.ID, ctx, cancel)

	_, _ = tm.AcquireLeases(t1.ID, []string{"db.go"})
	tm.Complete(t1.ID, threadmgr.FinishSummary{Status: "completed", Summary: "done"})

	t2, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "sam",
		Task:      "update db.go",
	})
	conflicts, err := tm.AcquireLeases(t2.ID, []string{"db.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) > 0 {
		t.Errorf("lease was not released on Complete: conflicts=%v", conflicts)
	}
}

// ─── SummaryCache: Store with same msgCount is idempotent ───────────────────

func TestSummaryCache_SameMsgCount_DoesNotOverwrite(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 5, "original summary")
	c.Store("sess-1", 5, "newer but same count — should be rejected")

	got, ok := c.Get("sess-1")
	if !ok {
		t.Fatal("expected cached summary")
	}
	if got != "original summary" {
		t.Errorf("Store with same msgCount should not overwrite: got %q", got)
	}
}

func TestSummaryCache_OlderCount_DoesNotOverwrite(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 10, "new summary")
	c.Store("sess-1", 5, "old summary — should not overwrite")

	got, _ := c.Get("sess-1")
	if got != "new summary" {
		t.Errorf("Store with older msgCount should not overwrite: got %q", got)
	}
}

func TestSummaryCache_NewerCount_Overwrites(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 5, "first")
	c.Store("sess-1", 10, "second — should win")

	got, _ := c.Get("sess-1")
	if got != "second — should win" {
		t.Errorf("Store with newer msgCount should overwrite: got %q", got)
	}
}

func TestSummaryCache_Invalidate_ClearsEntry(t *testing.T) {
	c := threadmgr.NewSummaryCache()
	c.Store("sess-1", 5, "something")
	c.Invalidate("sess-1")

	_, ok := c.Get("sess-1")
	if ok {
		t.Error("expected Invalidate to clear the entry")
	}
}

// ─── GetInputCh ──────────────────────────────────────────────────────────────

func TestGetInputCh_ReturnsChannelForKnownThread(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "ask user",
	})
	ch, ok := tm.GetInputCh(thread.ID)
	if !ok {
		t.Fatal("expected GetInputCh to succeed for known thread")
	}
	if ch == nil {
		t.Error("expected non-nil channel")
	}
}

func TestGetInputCh_ReturnsFalseForUnknownThread(t *testing.T) {
	tm := threadmgr.New()
	ch, ok := tm.GetInputCh("nonexistent-thread")
	if ok || ch != nil {
		t.Error("expected GetInputCh to fail for unknown thread")
	}
}

// ─── Concurrent safety ───────────────────────────────────────────────────────

func TestThreadManager_ConcurrentCreateAndGet(t *testing.T) {
	// Set limit high enough for this stress test.
	tm := threadmgr.New()
	tm.MaxThreadsPerSession = 200
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := threadmgr.CreateParams{
				SessionID: "sess-concurrent",
				AgentID:   "agent",
				Task:      "concurrent task",
			}
			thread, err := tm.Create(p)
			if err != nil {
				return // limit hit (should not happen with limit=200)
			}
			tm.Get(thread.ID)
		}(i)
	}
	wg.Wait()
	threads := tm.ListBySession("sess-concurrent")
	if len(threads) != 50 {
		t.Errorf("expected 50 threads, got %d", len(threads))
	}
}
