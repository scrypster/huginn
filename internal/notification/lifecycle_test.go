package notification_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

func TestValidateTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    notification.Status
		to      notification.Status
		wantErr bool
	}{
		{name: "pending_to_seen", from: notification.StatusPending, to: notification.StatusSeen},
		{name: "pending_to_approved", from: notification.StatusPending, to: notification.StatusApproved},
		{name: "seen_to_dismissed", from: notification.StatusSeen, to: notification.StatusDismissed},
		{name: "approved_to_executed", from: notification.StatusApproved, to: notification.StatusExecuted},
		{name: "approved_to_failed", from: notification.StatusApproved, to: notification.StatusFailed},
		{name: "same_status_is_noop", from: notification.StatusSeen, to: notification.StatusSeen},
		{name: "invalid_jump", from: notification.StatusPending, to: notification.StatusExecuted, wantErr: true},
		{name: "executed_to_pending_invalid", from: notification.StatusExecuted, to: notification.StatusPending, wantErr: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := notification.ValidateTransition(tc.from, tc.to)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.name, err)
			}
		})
	}
}

func TestResolveApproveAction(t *testing.T) {
	t.Parallel()
	n := &notification.Notification{
		ID: "notif-1",
		ProposedActions: []notification.ProposedAction{
			{ID: "a-1", Label: "One"},
			{ID: "a-2", Label: "Two"},
		},
	}
	if _, err := notification.ResolveApproveAction(n, "a-2"); err != nil {
		t.Fatalf("ResolveApproveAction valid id: %v", err)
	}
	if _, err := notification.ResolveApproveAction(n, "missing"); err == nil {
		t.Fatal("ResolveApproveAction missing id: expected error")
	}
	if _, err := notification.ResolveApproveAction(n, ""); err == nil {
		t.Fatal("ResolveApproveAction empty id with multiple actions: expected error")
	}

	single := &notification.Notification{
		ID:              "notif-2",
		ProposedActions: []notification.ProposedAction{{ID: "only", Label: "Only"}},
	}
	if _, err := notification.ResolveApproveAction(single, ""); err != nil {
		t.Fatalf("ResolveApproveAction single action with empty id: %v", err)
	}
}
