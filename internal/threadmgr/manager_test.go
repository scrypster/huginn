package threadmgr_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestThreadManagerCreateAndGet(t *testing.T) {
	tm := threadmgr.New()

	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "implement OAuth",
		DependsOn: []string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if thread.ID == "" {
		t.Fatal("expected non-empty thread ID")
	}
	if thread.Status != threadmgr.StatusQueued {
		t.Errorf("expected StatusQueued, got %s", thread.Status)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("expected to find thread by ID")
	}
	if got.AgentID != "stacy" {
		t.Errorf("expected AgentID=stacy, got %s", got.AgentID)
	}
}

func TestThreadManagerCancel(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "implement OAuth",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(thread.ID, ctx, cancel)

	// Cancel the thread
	tm.Cancel(thread.ID)

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after cancel")
	}
	if got.Status != threadmgr.StatusCancelled {
		t.Errorf("expected StatusCancelled, got %s", got.Status)
	}
}

func TestThreadManagerListBySession(t *testing.T) {
	tm := threadmgr.New()
	_, _ = tm.Create(threadmgr.CreateParams{SessionID: "sess-A", AgentID: "stacy", Task: "task 1"})
	_, _ = tm.Create(threadmgr.CreateParams{SessionID: "sess-A", AgentID: "sam", Task: "task 2"})
	_, _ = tm.Create(threadmgr.CreateParams{SessionID: "sess-B", AgentID: "alex", Task: "task 3"})

	threads := tm.ListBySession("sess-A")
	if len(threads) != 2 {
		t.Errorf("expected 2 threads for sess-A, got %d", len(threads))
	}
	for _, th := range threads {
		if th.SessionID != "sess-A" {
			t.Errorf("unexpected session %s", th.SessionID)
		}
	}
}

func TestThreadManagerDependsOnResolution(t *testing.T) {
	tm := threadmgr.New()

	// Create upstream thread
	upstream, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "implement",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create downstream that depends on upstream (by agent name hint)
	downstream, err := tm.Create(threadmgr.CreateParams{
		SessionID:      "sess-1",
		AgentID:        "sam",
		Task:           "qa",
		DependsOnHints: []string{"stacy"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resolve hints → thread IDs
	resolved := tm.ResolveDependencies(downstream.ID)
	if len(resolved) != 1 || resolved[0] != upstream.ID {
		t.Errorf("expected resolved=[%s], got %v", upstream.ID, resolved)
	}

	// downstream should NOT be ready while upstream is queued
	if tm.IsReady(downstream.ID) {
		t.Error("downstream should not be ready while upstream is queued")
	}

	// Complete upstream
	tm.Complete(upstream.ID, threadmgr.FinishSummary{
		Summary: "done",
		Status:  "completed",
	})

	// Now downstream should be ready
	if !tm.IsReady(downstream.ID) {
		t.Error("downstream should be ready after upstream completes")
	}
}

func TestThreadTimestamp(t *testing.T) {
	tm := threadmgr.New()
	before := time.Now()
	thread, err := tm.Create(threadmgr.CreateParams{SessionID: "sess-1", AgentID: "stacy", Task: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	if thread.StartedAt.Before(before) || thread.StartedAt.After(after) {
		t.Errorf("thread StartedAt out of range: %v", thread.StartedAt)
	}
}

func TestThreadManagerComplete(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{SessionID: "sess-1", AgentID: "stacy", Task: "impl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summary := threadmgr.FinishSummary{
		Summary:       "Added /auth/login endpoint",
		FilesModified: []string{"api/auth.go"},
		KeyDecisions:  []string{"used JWT"},
		Status:        "completed",
	}
	tm.Complete(thread.ID, summary)

	got, _ := tm.Get(thread.ID)
	if got.Status != threadmgr.StatusDone {
		t.Errorf("expected StatusDone, got %s", got.Status)
	}
	if got.Summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if got.Summary.Summary != "Added /auth/login endpoint" {
		t.Errorf("wrong summary: %s", got.Summary.Summary)
	}
	if got.CompletedAt.IsZero() {
		t.Error("expected non-zero CompletedAt")
	}
}

func TestThreadManagerNoDependencies(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "alex",
		Task:      "standalone task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No dependencies = immediately ready
	if !tm.IsReady(thread.ID) {
		t.Error("thread with no dependencies should be ready immediately")
	}
}

func TestStart_IdempotentOnDoubleCall(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "stacy",
		Task:      "some task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First call: should succeed (StatusQueued → StatusThinking)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	first := tm.Start(thread.ID, ctx1, cancel1)
	if !first {
		t.Fatal("expected first Start() to return true")
	}

	// Second call: thread is now StatusThinking, so Start() must return false
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	second := tm.Start(thread.ID, ctx2, cancel2)
	if second {
		t.Fatal("expected second Start() to return false")
	}

	// Thread must still be StatusThinking after both calls
	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != threadmgr.StatusThinking {
		t.Errorf("expected StatusThinking, got %s", got.Status)
	}
}

func TestThreadManagerCancelledNotReady(t *testing.T) {
	tm := threadmgr.New()
	upstream, err := tm.Create(threadmgr.CreateParams{SessionID: "sess-1", AgentID: "stacy", Task: "impl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	downstream, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "sam",
		Task:      "qa",
		DependsOn: []string{upstream.ID},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tm.Cancel(upstream.ID)

	// Downstream should NOT become ready when upstream is cancelled (not done)
	if tm.IsReady(downstream.ID) {
		t.Error("downstream should not be ready when upstream is cancelled")
	}
}
