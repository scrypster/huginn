package threadmgr

import (
	"strings"
	"testing"
)

func TestTrySendInput_NotFound(t *testing.T) {
	tm := New()
	sent, found, reason := tm.TrySendInput("missing", "sess-1", "hello")
	if sent || found {
		t.Fatalf("expected not found, got sent=%v found=%v reason=%q", sent, found, reason)
	}
	if reason != "not_found" {
		t.Fatalf("expected reason not_found, got %q", reason)
	}
}

func TestTrySendInput_NotWaiting(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "helper",
		Task:      "do work",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, found, reason := tm.TrySendInput(thread.ID, "sess-1", "hello")
	if sent || !found {
		t.Fatalf("expected found+not-sent, got sent=%v found=%v reason=%q", sent, found, reason)
	}
	if reason != "not_waiting" {
		t.Fatalf("expected reason not_waiting, got %q", reason)
	}
}

func TestTrySendInput_BlockedAndDelivered(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "helper",
		Task:      "need help",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate a thread waiting for human input.
	tm.mu.Lock()
	tm.threads[thread.ID].Status = StatusBlocked
	tm.mu.Unlock()

	sent, found, reason := tm.TrySendInput(thread.ID, "sess-1", "answer")
	if !sent || !found || reason != "" {
		t.Fatalf("expected sent, got sent=%v found=%v reason=%q", sent, found, reason)
	}
	ch, ok := tm.GetInputCh(thread.ID)
	if !ok {
		t.Fatal("GetInputCh: expected thread channel")
	}
	select {
	case got := <-ch:
		if got != "answer" {
			t.Fatalf("expected delivered answer, got %q", got)
		}
	default:
		t.Fatal("expected input to be queued")
	}
}

func TestTrySendInput_BlockedBufferFull(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "helper",
		Task:      "need help",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tm.mu.Lock()
	tm.threads[thread.ID].Status = StatusBlocked
	ch := tm.threads[thread.ID].InputCh
	tm.mu.Unlock()

	ch <- "already buffered"
	sent, found, reason := tm.TrySendInput(thread.ID, "sess-1", "overflow")
	if sent || !found {
		t.Fatalf("expected found+not-sent, got sent=%v found=%v reason=%q", sent, found, reason)
	}
	if reason != "buffer_full" {
		t.Fatalf("expected reason buffer_full, got %q", reason)
	}
}

func TestTrySendInput_PublishesSiblingContext(t *testing.T) {
	tm := New()
	target, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alpha",
		Task:      "need help",
	})
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}
	sibling, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "beta",
		Task:      "parallel work",
	})
	if err != nil {
		t.Fatalf("Create sibling: %v", err)
	}

	tm.mu.Lock()
	tm.threads[target.ID].Status = StatusBlocked
	tm.mu.Unlock()

	sent, found, reason := tm.TrySendInput(target.ID, "sess-1", "Please include edge-case handling.")
	if !sent || !found || reason != "" {
		t.Fatalf("expected sent, got sent=%v found=%v reason=%q", sent, found, reason)
	}

	updates := tm.SiblingContext(sibling.ID, 10)
	if len(updates) == 0 {
		t.Fatalf("expected sibling context update, got none")
	}
	last := updates[len(updates)-1]
	if last.ThreadID != target.ID {
		t.Fatalf("expected update from %q, got %q", target.ID, last.ThreadID)
	}
	if !strings.HasPrefix(last.Content, "user guidance:") {
		t.Fatalf("unexpected sibling context content: %q", last.Content)
	}
}

func TestInjectReceipt_ReturnsTargetAndActiveSiblingCount(t *testing.T) {
	tm := New()
	target, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alpha",
		Task:      "need help",
	})
	if err != nil {
		t.Fatalf("Create target: %v", err)
	}
	activeSibling, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "beta",
		Task:      "parallel work",
	})
	if err != nil {
		t.Fatalf("Create active sibling: %v", err)
	}
	doneSibling, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "gamma",
		Task:      "finished work",
	})
	if err != nil {
		t.Fatalf("Create done sibling: %v", err)
	}
	otherSession, err := tm.Create(CreateParams{
		SessionID: "sess-2",
		AgentID:   "delta",
		Task:      "other session",
	})
	if err != nil {
		t.Fatalf("Create other session thread: %v", err)
	}
	tm.mu.Lock()
	tm.threads[target.ID].Status = StatusBlocked
	tm.threads[activeSibling.ID].Status = StatusThinking
	tm.threads[doneSibling.ID].Status = StatusDone
	tm.threads[otherSession.ID].Status = StatusThinking
	tm.mu.Unlock()

	deliveredTo, sharedCount, ok := tm.InjectReceipt(target.ID)
	if !ok {
		t.Fatalf("InjectReceipt expected ok=true")
	}
	if deliveredTo != "alpha" {
		t.Fatalf("InjectReceipt deliveredTo=%q, want %q", deliveredTo, "alpha")
	}
	if sharedCount != 1 {
		t.Fatalf("InjectReceipt sharedCount=%d, want %d", sharedCount, 1)
	}
}

func TestPublishSessionGuidance_PublishesToBusAndCountsActiveThreads(t *testing.T) {
	tm := New()
	activeOne, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alpha",
		Task:      "running work",
	})
	if err != nil {
		t.Fatalf("Create activeOne: %v", err)
	}
	activeTwo, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "beta",
		Task:      "blocked work",
	})
	if err != nil {
		t.Fatalf("Create activeTwo: %v", err)
	}
	doneThread, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "gamma",
		Task:      "done work",
	})
	if err != nil {
		t.Fatalf("Create doneThread: %v", err)
	}
	tm.mu.Lock()
	tm.threads[activeOne.ID].Status = StatusThinking
	tm.threads[activeTwo.ID].Status = StatusBlocked
	tm.threads[doneThread.ID].Status = StatusDone
	tm.mu.Unlock()

	active := tm.PublishSessionGuidance("sess-1", "operator", "Need faster turnaround.")
	if active != 2 {
		t.Fatalf("PublishSessionGuidance active=%d, want %d", active, 2)
	}
	updates := tm.SiblingContext(activeOne.ID, 10)
	if len(updates) == 0 {
		t.Fatalf("expected session guidance update, got none")
	}
	last := updates[len(updates)-1]
	if last.ThreadID != "session-guidance" {
		t.Fatalf("expected session-guidance thread id, got %q", last.ThreadID)
	}
	if !strings.Contains(last.Content, "Need faster turnaround.") {
		t.Fatalf("unexpected guidance content: %q", last.Content)
	}
}

func TestPublishSessionGuidanceTarget_DeliversOnlyToTargetAgent(t *testing.T) {
	tm := New()
	alpha, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "alpha",
		Task:      "alpha task",
	})
	if err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	beta, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "beta",
		Task:      "beta task",
	})
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}
	tm.mu.Lock()
	tm.threads[alpha.ID].Status = StatusThinking
	tm.threads[beta.ID].Status = StatusThinking
	tm.mu.Unlock()

	active := tm.PublishSessionGuidanceTarget("sess-1", "operator", "beta", "Only beta should see this")
	if active != 1 {
		t.Fatalf("PublishSessionGuidanceTarget active=%d, want %d", active, 1)
	}
	alphaUpdates := tm.SiblingContext(alpha.ID, 10)
	for _, up := range alphaUpdates {
		if strings.Contains(up.Content, "Only beta should see this") {
			t.Fatalf("alpha should not receive targeted guidance, got %q", up.Content)
		}
	}
	betaUpdates := tm.SiblingContext(beta.ID, 10)
	found := false
	for _, up := range betaUpdates {
		if strings.Contains(up.Content, "Only beta should see this") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected beta to receive targeted guidance")
	}
}
