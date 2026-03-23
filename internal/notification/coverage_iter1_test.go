// coverage_iter1_test.go — additional coverage tests for notification package.
package notification_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// TestStore_Get_NotFound tests that Get returns an error for a nonexistent ID.
func TestStore_Get_NotFound(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	_, err := s.Get("nonexistent-notification-id")
	if err == nil {
		t.Fatal("expected error for nonexistent notification, got nil")
	}
}

// TestStore_Transition_NotFound tests that Transition returns an error for a nonexistent ID.
func TestStore_Transition_NotFound(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	err := s.Transition("nonexistent-id", notification.StatusDismissed)
	if err == nil {
		t.Fatal("expected error for nonexistent notification, got nil")
	}
}

// TestStore_PendingCount_Empty tests PendingCount returns 0 when no notifications exist.
func TestStore_PendingCount_Empty(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	count, err := s.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0, got %d", count)
	}
}

// TestStore_ExpireRun_NoMatches tests ExpireRun with a runID that has no notifications.
func TestStore_ExpireRun_NoMatches(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	// Should not error even when no notifications belong to this run.
	if err := s.ExpireRun("nonexistent-run-id"); err != nil {
		t.Fatalf("ExpireRun with no matches: %v", err)
	}
}

// TestStore_Put_WithWorkflowID tests that notifications with workflow IDs are indexed correctly.
func TestStore_Put_WithWorkflowID(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("routine1", "run1", notification.SeverityInfo)
	n.WorkflowID = "workflow-abc"
	n.WorkflowRunID = "wfrun-1"
	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Verify it appears in workflow listing.
	results, err := s.ListByWorkflow("workflow-abc")
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1, got %d", len(results))
	}
	if results[0].ID != n.ID {
		t.Errorf("ID mismatch: want %s, got %s", n.ID, results[0].ID)
	}

	// Verify it also appears in routine listing.
	byRoutine, err := s.ListByRoutine("routine1")
	if err != nil {
		t.Fatalf("ListByRoutine: %v", err)
	}
	if len(byRoutine) != 1 {
		t.Fatalf("want 1, got %d", len(byRoutine))
	}
}

// TestStore_ListPending_MultipleStatuses tests that ListPending only returns pending notifications.
func TestStore_ListPending_MultipleStatuses(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	// Add 2 pending, 1 dismissed.
	n1 := makeNotif("r1", "run1", notification.SeverityInfo)
	n2 := makeNotif("r1", "run1", notification.SeverityWarning)
	n3 := makeNotif("r1", "run1", notification.SeverityUrgent)
	for _, n := range []*notification.Notification{n1, n2, n3} {
		if err := s.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Transition(n3.ID, notification.StatusDismissed); err != nil {
		t.Fatal(err)
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("want 2 pending, got %d", len(pending))
	}
}
