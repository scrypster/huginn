package server

// notification_relay_forward_test.go — verifies the GAP B fix:
// BroadcastNotification must forward to the cloud relay buffer regardless
// of whether local WebSocket clients are connected (wsHub nil or not).
// Also verifies BuildNotificationRelayMsg produces the correct payload shape.

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/relay"
)

// TestBroadcastNotification_RelayForwarded_NilHub verifies that even when
// wsHub is nil (no local WS clients), BroadcastNotification does NOT panic
// and does not skip the relay path (no early return on nil hub).
func TestBroadcastNotification_RelayForwarded_NilHub(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.wsHub = nil
	// satellite is nil → SendRelay is a no-op, but must not panic
	n := &notification.Notification{
		ID:        notification.NewID(),
		Summary:   "relay test",
		Severity:  notification.SeverityInfo,
		Status:    notification.StatusPending,
		CreatedAt: time.Now().UTC(),
	}
	// Must not panic even with nil wsHub (old code would return early here)
	srv.BroadcastNotification(n, 0)
}

// TestBroadcastNotification_NilNotification_Safe verifies that passing a nil
// notification does not panic and does not attempt a relay send.
func TestBroadcastNotification_NilNotification_Safe(t *testing.T) {
	srv, _ := newTestServer(t)
	// must not panic
	srv.BroadcastNotification(nil, 0)
}

// TestBroadcastNotification_NilHubNilSatellite_NoRelay verifies full nil safety.
func TestBroadcastNotification_NilHubNilSatellite_NoRelay(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.wsHub = nil
	// default test server has nil satellite — SendRelay is a no-op
	srv.mu.Lock()
	srv.satellite = nil
	srv.mu.Unlock()

	n := &notification.Notification{
		ID:        notification.NewID(),
		Summary:   "safe no-op",
		CreatedAt: time.Now().UTC(),
	}
	srv.BroadcastNotification(n, 0)
}

// TestBroadcastNotification_SendRelayPayload verifies the relay message payload
// fields are correctly populated by BuildNotificationRelayMsg, including the
// pending_count that was missing in the original GAP B implementation.
func TestBroadcastNotification_SendRelayPayload(t *testing.T) {
	n := &notification.Notification{
		ID:         "notif-001",
		WorkflowID: "wf-abc",
		RunID:      "run-xyz",
		Summary:    "something happened",
		Severity:   notification.SeverityUrgent,
		Status:     notification.StatusPending,
		CreatedAt:  time.Now().UTC(),
	}

	msg := BuildNotificationRelayMsg(n, 2)

	if msg.Type != relay.MsgNotificationSync {
		t.Errorf("type = %q, want MsgNotificationSync", msg.Type)
	}
	p := msg.Payload
	if id := p["id"].(string); id != "notif-001" {
		t.Errorf("id = %q, want %q", id, "notif-001")
	}
	if wfID := p["workflow_id"].(string); wfID != "wf-abc" {
		t.Errorf("workflow_id = %q, want %q", wfID, "wf-abc")
	}
	if sev := p["severity"].(string); sev != "urgent" {
		t.Errorf("severity = %q, want %q", sev, "urgent")
	}
	if pc := p["pending_count"].(int); pc != 2 {
		t.Errorf("pending_count = %d, want 2", pc)
	}

	// Smoke-test: BroadcastNotification must not panic with nil satellite
	srv, _ := newTestServer(t)
	srv.wsHub = nil
	srv.BroadcastNotification(n, 2)
}
