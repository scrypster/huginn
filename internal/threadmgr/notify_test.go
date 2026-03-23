package threadmgr

import (
	"sync"
	"testing"
)

func TestOnStatusChange_Complete(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Start the thread so it can be completed.
	tm.Start(thread.ID, nil, func() {})

	var mu sync.Mutex
	var calls []struct {
		id     string
		status ThreadStatus
	}
	tm.OnStatusChange(func(id string, status ThreadStatus) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, struct {
			id     string
			status ThreadStatus
		}{id, status})
	})

	tm.Complete(thread.ID, FinishSummary{Summary: "done", Status: "completed"})

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(calls))
	}
	if calls[0].id != thread.ID {
		t.Errorf("expected thread ID %q, got %q", thread.ID, calls[0].id)
	}
	if calls[0].status != StatusDone {
		t.Errorf("expected StatusDone, got %s", calls[0].status)
	}
}

func TestOnStatusChange_Cancel(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.Start(thread.ID, nil, func() {})

	var mu sync.Mutex
	var calls []ThreadStatus
	tm.OnStatusChange(func(id string, status ThreadStatus) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, status)
	})

	tm.Cancel(thread.ID)

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(calls))
	}
	if calls[0] != StatusCancelled {
		t.Errorf("expected StatusCancelled, got %s", calls[0])
	}
}

func TestOnStatusChange_SetBlocked(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.Start(thread.ID, nil, func() {})

	var mu sync.Mutex
	var calls []ThreadStatus
	tm.OnStatusChange(func(id string, status ThreadStatus) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, status)
	})

	tm.setBlocked(thread.ID, "need help")

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(calls))
	}
	if calls[0] != StatusBlocked {
		t.Errorf("expected StatusBlocked, got %s", calls[0])
	}
}

func TestOnStatusChange_NilCallback(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.Start(thread.ID, nil, func() {})

	// No callback set — should not panic.
	tm.Complete(thread.ID, FinishSummary{Summary: "done", Status: "completed"})
}

func TestOnStatusChange_ClearCallback(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.Start(thread.ID, nil, func() {})

	callCount := 0
	tm.OnStatusChange(func(id string, status ThreadStatus) {
		callCount++
	})

	// Clear the callback.
	tm.OnStatusChange(nil)

	tm.Complete(thread.ID, FinishSummary{Summary: "done", Status: "completed"})
	if callCount != 0 {
		t.Errorf("expected 0 calls after clearing callback, got %d", callCount)
	}
}

func TestOnStatusChange_TerminalNoop(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "agent-a",
		Task:      "test task",
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.Start(thread.ID, nil, func() {})
	tm.Complete(thread.ID, FinishSummary{Summary: "done", Status: "completed"})

	callCount := 0
	tm.OnStatusChange(func(id string, status ThreadStatus) {
		callCount++
	})

	// Completing an already-done thread should be a no-op.
	tm.Complete(thread.ID, FinishSummary{Summary: "again", Status: "completed"})
	tm.Cancel(thread.ID)

	if callCount != 0 {
		t.Errorf("expected 0 calls for terminal thread, got %d", callCount)
	}
}
