package server

import (
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/relay"
)

// BuildNotificationRelayMsg constructs the MsgNotificationSync relay.Message
// from a notification and pending count. Centralises the payload shape so that
// BroadcastNotification, the workflow-completion handler in main.go, and tests
// all share one definition and the field set cannot drift.
func BuildNotificationRelayMsg(n *notification.Notification, pendingCount int) relay.Message {
	return relay.Message{
		Type: relay.MsgNotificationSync,
		Payload: map[string]any{
			"id":            n.ID,
			"workflow_id":   n.WorkflowID,
			"run_id":        n.RunID,
			"summary":       n.Summary,
			"severity":      string(n.Severity),
			"status":        string(n.Status),
			"created_at":    n.CreatedAt.UTC().Format(time.RFC3339),
			"pending_count": pendingCount,
		},
	}
}
