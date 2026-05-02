package threadmgr_test

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestDelegationPreview_Disabled_SpawnsImmediately(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(false)
	approved := preview.Approve(context.Background(), "sess-1", "t-1", "Stacy", "build login", "", nil)
	if !approved {
		t.Error("expected immediate approval when preview disabled")
	}
}

func TestDelegationPreview_Enabled_ApprovedByAck(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(true)

	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.Approve(
			context.Background(), "sess-1", "t-2", "Stacy", "build login", "",
			func(_, _ string, _ map[string]any) { close(ready) },
		)
		resultCh <- approved
	}()

	<-ready // Approve has registered the channel AND called broadcastFn
	preview.Ack("sess-1", "t-2", true)

	select {
	case result := <-resultCh:
		if !result {
			t.Error("expected approval after Ack(true)")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Approve to return")
	}
}

func TestDelegationPreview_Enabled_DeniedByAck(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(true)

	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.Approve(
			context.Background(), "sess-1", "t-3", "Stacy", "build login", "",
			func(_, _ string, _ map[string]any) { close(ready) },
		)
		resultCh <- approved
	}()

	<-ready
	preview.Ack("sess-1", "t-3", false)

	select {
	case result := <-resultCh:
		if result {
			t.Error("expected denial after Ack(false)")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Approve to return")
	}
}

func TestDelegationPreview_Enabled_ContextCancelled_ReturnsFalse(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(true)

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.Approve(
			ctx, "sess-1", "t-4", "Stacy", "build login", "",
			func(_, _ string, _ map[string]any) { close(ready) },
		)
		resultCh <- approved
	}()

	<-ready
	cancel()

	select {
	case result := <-resultCh:
		if result {
			t.Error("expected false when context cancelled")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Approve to return")
	}
}

func TestDelegationPreview_Enabled_BroadcastFnCalled(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(true)

	broadcastCalled := make(chan map[string]any, 1)
	broadcastFn := func(sessionID, msgType string, payload map[string]any) {
		if msgType == "delegation_preview" {
			broadcastCalled <- payload
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		preview.Approve(ctx, "sess-1", "t-5", "Stacy", "build login", "", broadcastFn)
	}()

	select {
	case payload := <-broadcastCalled:
		if payload["thread_id"] != "t-5" {
			t.Errorf("expected thread_id=t-5, got %v", payload["thread_id"])
		}
		if payload["agent_id"] != "Stacy" {
			t.Errorf("expected agent_id=Stacy, got %v", payload["agent_id"])
		}
		if payload["agent"] != "Stacy" {
			t.Errorf("expected agent=Stacy, got %v", payload["agent"])
		}
		if payload["mode"] != "manual" {
			t.Errorf("expected mode=manual, got %v", payload["mode"])
		}
		if payload["expires_in_seconds"] != 30 {
			t.Errorf("expected expires_in_seconds=30, got %v", payload["expires_in_seconds"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestDelegationPreview_Ack_NoOpWhenNoPending(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGate(true)
	// Should not panic
	preview.Ack("sess-1", "nonexistent", true)
}

// ─── parentMessageID in delegation_preview broadcast ────────────────────────

func TestApprove_ParentMessageID_IncludedInPayload(t *testing.T) {
	gate := threadmgr.NewDelegationPreviewGate(true)

	payloadCh := make(chan map[string]any, 1)
	ready := make(chan struct{})

	go func() {
		gate.Approve(context.Background(), "sess-pm", "t-pm1", "agent", "task", "msg-42",
			func(_, _ string, p map[string]any) {
				payloadCh <- p
				close(ready)
			},
		)
	}()

	select {
	case <-ready:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("broadcast not fired")
	}
	gate.Ack("sess-pm", "t-pm1", true)

	payload := <-payloadCh
	val, exists := payload["parent_message_id"]
	if !exists {
		t.Error("expected parent_message_id in delegation_preview payload")
	}
	if val != "msg-42" {
		t.Errorf("parent_message_id = %v, want %q", val, "msg-42")
	}
}

func TestApprove_EmptyParentMessageID_OmittedFromPayload(t *testing.T) {
	gate := threadmgr.NewDelegationPreviewGate(true)

	payloadCh := make(chan map[string]any, 1)
	ready := make(chan struct{})

	go func() {
		gate.Approve(context.Background(), "sess-pm", "t-pm2", "agent", "task", "", // empty parentMessageID
			func(_, _ string, p map[string]any) {
				payloadCh <- p
				close(ready)
			},
		)
	}()

	select {
	case <-ready:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("broadcast not fired")
	}
	gate.Ack("sess-pm", "t-pm2", true)

	payload := <-payloadCh
	if _, exists := payload["parent_message_id"]; exists {
		t.Error("parent_message_id should NOT be in payload when empty")
	}
}

func TestDelegationPreview_Config_Off_SpawnsImmediately(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("off", 2*time.Second)
	approved := preview.Approve(context.Background(), "sess-off", "t-off", "Stacy", "build login", "", nil)
	if !approved {
		t.Error("expected immediate approval in off mode")
	}
}

func TestDelegationPreview_Config_Auto_SpawnsImmediately(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("auto", 2*time.Second)
	approved := preview.Approve(context.Background(), "sess-auto", "t-auto", "Stacy", "build login", "", nil)
	if !approved {
		t.Error("expected immediate approval in auto mode")
	}
}

func TestDelegationPreview_Config_Conditional_BenignTaskSkipsApproval(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("conditional", 2*time.Second)
	broadcastCalled := false
	approved := preview.Approve(
		context.Background(),
		"sess-cond", "t-cond1", "Stacy", "list top 3 Go testing tips", "",
		func(_, _ string, _ map[string]any) { broadcastCalled = true },
	)
	if !approved {
		t.Error("expected conditional mode to auto-approve benign task")
	}
	if broadcastCalled {
		t.Error("benign conditional task should not emit delegation_preview")
	}
}

func TestDelegationPreview_Config_Conditional_RiskyTaskRequiresApproval(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("conditional", 2*time.Second)
	ready := make(chan struct{})
	resultCh := make(chan bool, 1)
	go func() {
		approved := preview.Approve(
			context.Background(),
			"sess-cond", "t-cond2", "Stacy", "delete production rows from billing table", "",
			func(_, _ string, payload map[string]any) {
				if payload["mode"] != "conditional" {
					t.Errorf("expected mode=conditional, got %v", payload["mode"])
				}
				close(ready)
			},
		)
		resultCh <- approved
	}()
	<-ready
	preview.Ack("sess-cond", "t-cond2", true)
	select {
	case result := <-resultCh:
		if !result {
			t.Error("expected risky conditional task to approve after Ack(true)")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for conditional approval result")
	}
}

func TestDelegationPreview_Config_CustomTimeout_AutoApprovesAfterTimeout(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("manual", 25*time.Millisecond)
	start := time.Now()
	approved := preview.Approve(context.Background(), "sess-timeout", "t-timeout", "Stacy", "build login", "", nil)
	if !approved {
		t.Fatal("expected auto-approval after timeout")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("expected approve to wait for configured timeout, waited only %v", elapsed)
	}
}

func TestDelegationPreview_Config_CustomTimeout_BroadcastsTimeoutEvent(t *testing.T) {
	preview := threadmgr.NewDelegationPreviewGateWithConfig("manual", 25*time.Millisecond)
	eventCh := make(chan map[string]any, 1)
	approved := preview.Approve(
		context.Background(),
		"sess-timeout", "t-timeout-evt", "Stacy", "build login", "msg-123",
		func(_ string, msgType string, payload map[string]any) {
			if msgType == "delegation_preview_timeout" {
				eventCh <- payload
			}
		},
	)
	if !approved {
		t.Fatal("expected timeout path to auto-approve")
	}
	select {
	case payload := <-eventCh:
		if payload["thread_id"] != "t-timeout-evt" {
			t.Errorf("expected timeout event thread_id=t-timeout-evt, got %v", payload["thread_id"])
		}
		if payload["agent_id"] != "Stacy" {
			t.Errorf("expected timeout event agent_id=Stacy, got %v", payload["agent_id"])
		}
		if payload["timeout_seconds"] != 1 {
			t.Errorf("expected timeout_seconds=1 for sub-second timeout floor, got %v", payload["timeout_seconds"])
		}
		if payload["parent_message_id"] != "msg-123" {
			t.Errorf("expected timeout event parent_message_id=msg-123, got %v", payload["parent_message_id"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected delegation_preview_timeout event")
	}
}
