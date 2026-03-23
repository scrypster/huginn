package threadmgr_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestAuditLog_CreateEntry verifies that Create() appends an audit entry.
func TestAuditLog_CreateEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID:     "sess-audit",
		AgentID:       "stacy",
		Task:          "write tests",
		CreatedByUser: "primary-agent",
		CreatedReason: "QA needed",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	log := tm.AuditLog()
	if len(log) == 0 {
		t.Fatal("expected at least one audit entry after Create")
	}
	entry := log[len(log)-1]
	if entry.Action != "created" {
		t.Errorf("expected action 'created', got %q", entry.Action)
	}
	if entry.ThreadID != thread.ID {
		t.Errorf("expected thread_id %q, got %q", thread.ID, entry.ThreadID)
	}
	if entry.Actor != "primary-agent" {
		t.Errorf("expected actor 'primary-agent', got %q", entry.Actor)
	}
	if entry.At.IsZero() {
		t.Error("audit entry At must not be zero")
	}
}

// TestAuditLog_CompleteEntry verifies that Complete() appends an audit entry.
func TestAuditLog_CompleteEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-audit-done", AgentID: "sam", Task: "impl",
	})
	tm.Complete(thread.ID, threadmgr.FinishSummary{
		Summary: "done",
		Status:  "completed",
	})
	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "completed" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'completed' audit entry after Complete()")
	}
}

// TestAuditLog_ErrorEntry verifies that Complete() with status=error logs "error".
func TestAuditLog_ErrorEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-audit-err", AgentID: "bot", Task: "fail",
	})
	tm.Complete(thread.ID, threadmgr.FinishSummary{
		Summary: "LLM error",
		Status:  "error",
	})
	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'error' audit entry for error completion")
	}
}

// TestAuditLog_CancelEntry verifies that Cancel() appends an audit entry.
func TestAuditLog_CancelEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-audit-cancel", AgentID: "bot", Task: "do nothing",
	})
	tm.Cancel(thread.ID)
	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "cancelled" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'cancelled' audit entry after Cancel()")
	}
}

// TestAuditLog_ArchiveEntry verifies that Archive() appends an audit entry.
func TestAuditLog_ArchiveEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-audit-arch", AgentID: "bot", Task: "do stuff",
	})
	tm.Archive(thread.ID, "session ended")
	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "archived" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'archived' audit entry after Archive()")
	}
}

// TestAuditLog_BoundedRingBuffer verifies that the audit log is bounded to
// maxAuditEntries by writing more than 1000 entries and checking no panic/OOM.
func TestAuditLog_BoundedRingBuffer(t *testing.T) {
	tm := threadmgr.New()
	// Create 1100 threads — each Create emits one audit entry.
	for i := 0; i < 1100; i++ {
		tm.Create(threadmgr.CreateParams{
			SessionID: "sess-big",
			AgentID:   "bot",
			Task:      "task",
		})
	}
	log := tm.AuditLog()
	const maxAudit = 1000
	if len(log) > maxAudit {
		t.Errorf("audit log exceeds %d entries: got %d", maxAudit, len(log))
	}
}

// TestAuditLog_TimeoutEntry verifies that completed-with-timeout maps to "timeout".
func TestAuditLog_TimeoutEntry(t *testing.T) {
	tm := threadmgr.New()
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-timeout", AgentID: "bot", Task: "timeout task",
	})
	tm.Complete(thread.ID, threadmgr.FinishSummary{
		Summary: "max turns reached",
		Status:  "completed-with-timeout",
	})
	log := tm.AuditLog()
	var found bool
	for _, e := range log {
		if e.ThreadID == thread.ID && e.Action == "timeout" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'timeout' audit entry for completed-with-timeout status")
	}
}
