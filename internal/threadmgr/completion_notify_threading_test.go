package threadmgr

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestCompletionNotifier_ThreadLookup_StampsParentMessageID verifies that when
// ThreadLookup is set and the thread has a non-empty ParentMessageID, Notify
// stamps it onto the FinishSummary before calling FollowUpFn.
func TestCompletionNotifier_ThreadLookup_StampsParentMessageID(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	// Create a thread with a non-empty ParentMessageID.
	thread := &Thread{
		ID:              "thread-1",
		ParentMessageID: "msg-42",
	}

	// Wire ThreadLookup to return the thread.
	threadLookup := func(threadID string) (*Thread, bool) {
		if threadID == "thread-1" {
			return thread, true
		}
		return nil, false
	}

	// Capture the summary passed to FollowUpFn in a channel.
	capturedSummary := make(chan *FinishSummary, 1)
	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		select {
		case capturedSummary <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: threadLookup,
		FollowUpFn:   followUpFn,
	}

	// Create a summary without ParentMessageID initially.
	summary := &FinishSummary{
		Summary: "Task completed",
		Status:  "completed",
		// ParentMessageID is empty at this point
	}

	n.Notify(context.Background(), "sess-1", "thread-1", "agent-1", summary)

	// Wait for FollowUpFn to run and capture the summary.
	var received *FinishSummary
	select {
	case received = <-capturedSummary:
	case <-time.After(1 * time.Second):
		t.Fatal("FollowUpFn did not run or did not send summary")
	}

	// Verify that the FollowUpFn received the summary with ParentMessageID stamped.
	if received.ParentMessageID != "msg-42" {
		t.Errorf("FollowUpFn received ParentMessageID %q, want \"msg-42\"", received.ParentMessageID)
	}

	// Verify broadcast still happened.
	if len(bc.events) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(bc.events))
	}
}

// TestCompletionNotifier_ThreadLookup_EmptyParentMessageID_NoStamp verifies that
// when the thread's ParentMessageID is empty, the summary should NOT get a
// ParentMessageID stamped, and FollowUpFn should receive empty ParentMessageID.
func TestCompletionNotifier_ThreadLookup_EmptyParentMessageID_NoStamp(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	// Create a thread with an empty ParentMessageID.
	thread := &Thread{
		ID:              "thread-2",
		ParentMessageID: "", // empty
	}

	threadLookup := func(threadID string) (*Thread, bool) {
		if threadID == "thread-2" {
			return thread, true
		}
		return nil, false
	}

	capturedSummary := make(chan *FinishSummary, 1)
	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		select {
		case capturedSummary <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: threadLookup,
		FollowUpFn:   followUpFn,
	}

	summary := &FinishSummary{
		Summary: "Task completed",
		Status:  "completed",
	}

	n.Notify(context.Background(), "sess-1", "thread-2", "agent-1", summary)

	var received *FinishSummary
	select {
	case received = <-capturedSummary:
	case <-time.After(1 * time.Second):
		t.Fatal("FollowUpFn did not run or did not send summary")
	}

	// Verify that ParentMessageID remained empty (not stamped).
	if received.ParentMessageID != "" {
		t.Errorf("FollowUpFn received ParentMessageID %q, want \"\"", received.ParentMessageID)
	}
}

// TestCompletionNotifier_NilThreadLookup_NoStamp verifies that when ThreadLookup
// is nil, Notify should still work (no panic) and FollowUpFn should receive the
// summary with whatever ParentMessageID was already on it.
func TestCompletionNotifier_NilThreadLookup_NoStamp(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	capturedSummary := make(chan *FinishSummary, 1)
	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		select {
		case capturedSummary <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: nil, // explicitly nil
		FollowUpFn:   followUpFn,
	}

	summary := &FinishSummary{
		Summary:         "Task completed",
		Status:          "completed",
		ParentMessageID: "original-parent-id",
	}

	// This should not panic.
	n.Notify(context.Background(), "sess-1", "thread-3", "agent-1", summary)

	var received *FinishSummary
	select {
	case received = <-capturedSummary:
	case <-time.After(1 * time.Second):
		t.Fatal("FollowUpFn did not run or did not send summary")
	}

	// Verify that the original ParentMessageID was preserved.
	if received.ParentMessageID != "original-parent-id" {
		t.Errorf("FollowUpFn received ParentMessageID %q, want \"original-parent-id\"", received.ParentMessageID)
	}

	// Verify broadcast still happened.
	if len(bc.events) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(bc.events))
	}
}

// TestCompletionNotifier_ThreadLookup_ThreadNotFound_NoStamp verifies that when
// ThreadLookup returns false (thread not found), FollowUpFn should receive the
// summary without ParentMessageID modification.
func TestCompletionNotifier_ThreadLookup_ThreadNotFound_NoStamp(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	threadLookup := func(threadID string) (*Thread, bool) {
		// Always return not found.
		return nil, false
	}

	capturedSummary := make(chan *FinishSummary, 1)
	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		select {
		case capturedSummary <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: threadLookup,
		FollowUpFn:   followUpFn,
	}

	summary := &FinishSummary{
		Summary:         "Task completed",
		Status:          "completed",
		ParentMessageID: "pre-existing-id",
	}

	n.Notify(context.Background(), "sess-1", "nonexistent-thread", "agent-1", summary)

	var received *FinishSummary
	select {
	case received = <-capturedSummary:
	case <-time.After(1 * time.Second):
		t.Fatal("FollowUpFn did not run or did not send summary")
	}

	// Verify that ParentMessageID was not modified (remains pre-existing).
	if received.ParentMessageID != "pre-existing-id" {
		t.Errorf("FollowUpFn received ParentMessageID %q, want \"pre-existing-id\"", received.ParentMessageID)
	}
}

// TestCompletionNotifier_FollowUpFn_ReceivesCopyOfSummary verifies that the
// FollowUpFn runs in a goroutine with a *copy* of the summary. Mutating the
// summary after Notify doesn't affect what FollowUpFn sees.
func TestCompletionNotifier_FollowUpFn_ReceivesCopyOfSummary(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	capturedSummary := make(chan *FinishSummary, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		defer wg.Done()
		select {
		case capturedSummary <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:  bc.fn(),
		FollowUpFn: followUpFn,
	}

	summary := &FinishSummary{
		Summary: "Original summary",
		Status:  "completed",
	}

	n.Notify(context.Background(), "sess-1", "thread-4", "agent-1", summary)

	// Immediately mutate the summary.
	summary.Summary = "MUTATED SUMMARY"
	summary.Status = "blocked"

	// Wait for FollowUpFn to finish.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("FollowUpFn did not complete")
	}

	var received *FinishSummary
	select {
	case received = <-capturedSummary:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("summary not received from FollowUpFn")
	}

	// Verify that the FollowUpFn received the *original* values, not the mutated ones.
	if received.Summary != "Original summary" {
		t.Errorf("FollowUpFn received Summary %q, want \"Original summary\"", received.Summary)
	}
	if received.Status != "completed" {
		t.Errorf("FollowUpFn received Status %q, want \"completed\"", received.Status)
	}
}

// TestCompletionNotifier_ThreadLookup_MultipleThreads ensures ThreadLookup
// correctly discriminates between different thread IDs.
func TestCompletionNotifier_ThreadLookup_MultipleThreads(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	threadA := &Thread{ID: "thread-a", ParentMessageID: "msg-100"}
	threadB := &Thread{ID: "thread-b", ParentMessageID: "msg-200"}

	threadLookup := func(threadID string) (*Thread, bool) {
		switch threadID {
		case "thread-a":
			return threadA, true
		case "thread-b":
			return threadB, true
		default:
			return nil, false
		}
	}

	capturedSummaries := make(chan *FinishSummary, 2)
	followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		select {
		case capturedSummaries <- summary:
		case <-time.After(100 * time.Millisecond):
			t.Error("FollowUpFn: timeout sending summary to channel")
		}
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: threadLookup,
		FollowUpFn:   followUpFn,
	}

	summaryA := &FinishSummary{Summary: "Task A completed", Status: "completed"}
	summaryB := &FinishSummary{Summary: "Task B completed", Status: "completed"}

	n.Notify(context.Background(), "sess-1", "thread-a", "agent-a", summaryA)
	n.Notify(context.Background(), "sess-1", "thread-b", "agent-b", summaryB)

	// Collect both summaries.
	received := make([]*FinishSummary, 2)
	for i := 0; i < 2; i++ {
		select {
		case s := <-capturedSummaries:
			received[i] = s
		case <-time.After(1 * time.Second):
			t.Fatalf("FollowUpFn %d did not run or did not send summary", i)
		}
	}

	// Verify correct threading for each.
	// Find which summary corresponds to which thread (order may vary due to goroutines).
	for _, s := range received {
		if s.Summary == "Task A completed" {
			if s.ParentMessageID != "msg-100" {
				t.Errorf("Task A: ParentMessageID %q, want \"msg-100\"", s.ParentMessageID)
			}
		} else if s.Summary == "Task B completed" {
			if s.ParentMessageID != "msg-200" {
				t.Errorf("Task B: ParentMessageID %q, want \"msg-200\"", s.ParentMessageID)
			}
		}
	}

	if len(bc.events) != 2 {
		t.Errorf("expected 2 broadcasts, got %d", len(bc.events))
	}
}

// TestCompletionNotifier_ThreadLookup_WithoutFollowUpFn verifies that ThreadLookup
// stamping still works even when FollowUpFn is nil (just no goroutine is spawned).
func TestCompletionNotifier_ThreadLookup_WithoutFollowUpFn(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	thread := &Thread{
		ID:              "thread-5",
		ParentMessageID: "msg-99",
	}

	threadLookup := func(threadID string) (*Thread, bool) {
		if threadID == "thread-5" {
			return thread, true
		}
		return nil, false
	}

	n := &CompletionNotifier{
		Broadcast:    bc.fn(),
		ThreadLookup: threadLookup,
		FollowUpFn:   nil, // explicitly nil
	}

	summary := &FinishSummary{
		Summary: "Task completed",
		Status:  "completed",
	}

	// This should not panic and should complete immediately (no goroutine).
	n.Notify(context.Background(), "sess-1", "thread-5", "agent-1", summary)

	// Verify that the summary was stamped even though FollowUpFn is nil.
	// (The stamping happens before the FollowUpFn check.)
	if summary.ParentMessageID != "msg-99" {
		t.Errorf("summary.ParentMessageID = %q, want \"msg-99\"", summary.ParentMessageID)
	}

	if len(bc.events) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(bc.events))
	}
}

// TestCompletionNotifier_FollowUpFn_PanicRecovery verifies that if FollowUpFn
// panics, the panic is recovered and doesn't crash the Notify call.
func TestCompletionNotifier_FollowUpFn_PanicRecovery(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	panicFollowUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
		panic("intentional panic in follow-up")
	}

	n := &CompletionNotifier{
		Broadcast:  bc.fn(),
		FollowUpFn: panicFollowUpFn,
	}

	summary := &FinishSummary{
		Summary: "Task completed",
		Status:  "completed",
	}

	// This should not panic despite FollowUpFn panicking (panic is recovered in goroutine).
	n.Notify(context.Background(), "sess-1", "thread-6", "agent-1", summary)

	// Give the goroutine time to run and recover.
	time.Sleep(100 * time.Millisecond)

	// Verify broadcast still happened.
	if len(bc.events) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(bc.events))
	}
}

// TestCompletionNotifier_ThreadLookup_StampsOnlyIfParentMessageIDNotEmpty
// verifies the logic: stamp only if lookup succeeds AND thread.ParentMessageID != "".
func TestCompletionNotifier_ThreadLookup_StampsOnlyIfParentMessageIDNotEmpty(t *testing.T) {
	tests := []struct {
		name                  string
		threadParentMessageID string
		lookupSuccess         bool
		expectedParentID      string
	}{
		{
			name:                  "lookup succeeds, thread has ParentMessageID",
			threadParentMessageID: "msg-123",
			lookupSuccess:         true,
			expectedParentID:      "msg-123",
		},
		{
			name:                  "lookup succeeds, thread ParentMessageID empty",
			threadParentMessageID: "",
			lookupSuccess:         true,
			// When thread.ParentMessageID is "", the code does NOT overwrite
			// the summary's existing value (guard: t.ParentMessageID != "").
			expectedParentID: "original",
		},
		{
			name:                  "lookup fails",
			threadParentMessageID: "msg-456",
			lookupSuccess:         false,
			expectedParentID:      "original",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := &notifyBroadcastRecorder{}

			thread := &Thread{
				ID:              "thread-test",
				ParentMessageID: tt.threadParentMessageID,
			}

			threadLookup := func(threadID string) (*Thread, bool) {
				if tt.lookupSuccess && threadID == "thread-test" {
					return thread, true
				}
				return nil, false
			}

			capturedSummary := make(chan *FinishSummary, 1)
			followUpFn := func(ctx context.Context, sessionID, agentID string, summary *FinishSummary) {
				select {
				case capturedSummary <- summary:
				case <-time.After(100 * time.Millisecond):
				}
			}

			n := &CompletionNotifier{
				Broadcast:    bc.fn(),
				ThreadLookup: threadLookup,
				FollowUpFn:   followUpFn,
			}

			summary := &FinishSummary{
				Summary:         "Task completed",
				Status:          "completed",
				ParentMessageID: "original",
			}

			n.Notify(context.Background(), "sess-1", "thread-test", "agent-1", summary)

			var received *FinishSummary
			select {
			case received = <-capturedSummary:
			case <-time.After(1 * time.Second):
				t.Fatal("FollowUpFn did not run")
			}

			if received.ParentMessageID != tt.expectedParentID {
				t.Errorf("ParentMessageID = %q, want %q", received.ParentMessageID, tt.expectedParentID)
			}
		})
	}
}
