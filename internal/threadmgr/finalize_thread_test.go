package threadmgr_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestFinalizeThread_MarksTerminal verifies that finalizeThread transitions a
// non-terminal thread to a terminal state.
func TestFinalizeThread_MarksTerminal(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-fin",
		AgentID:   "bot",
		Task:      "do stuff",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Thread is queued (non-terminal). Call FinalizeThread.
	tm.FinalizeThread(thread.ID, "error", "deliberate test failure")

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after finalize")
	}
	if got.Status != threadmgr.StatusError {
		t.Errorf("expected StatusError after finalize with 'error', got %s", got.Status)
	}
}

// TestFinalizeThread_Idempotent verifies that calling FinalizeThread twice does
// not overwrite an already-terminal status.
func TestFinalizeThread_Idempotent(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-fin-idem",
		AgentID:   "bot",
		Task:      "do stuff",
	})

	// First finalize → cancelled.
	tm.FinalizeThread(thread.ID, "cancelled", "user cancelled")
	// Second finalize → try to set done.
	tm.FinalizeThread(thread.ID, "done", "")

	got, _ := tm.Get(thread.ID)
	if got.Status != threadmgr.StatusCancelled {
		t.Errorf("expected status to remain StatusCancelled, got %s", got.Status)
	}
}

// TestFinalizeThread_EmitsAuditEntry verifies that FinalizeThread appends to
// the audit log.
func TestFinalizeThread_EmitsAuditEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-fin-audit",
		AgentID:   "bot",
		Task:      "task",
	})
	tm.FinalizeThread(thread.ID, "error", "timeout exceeded")

	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'error' audit entry from FinalizeThread")
	}
}

// TestFinalizeThread_DoneStatus verifies that "done" status maps to StatusDone.
func TestFinalizeThread_DoneStatus(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-fin-done",
		AgentID:   "bot",
		Task:      "task",
	})
	tm.FinalizeThread(thread.ID, "done", "normal completion")
	got, _ := tm.Get(thread.ID)
	if got.Status != threadmgr.StatusDone {
		t.Errorf("expected StatusDone, got %s", got.Status)
	}
}
