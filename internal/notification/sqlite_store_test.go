package notification_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openNotifTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeSQLiteNotif(id, routineID, runID string) *notification.Notification {
	return &notification.Notification{
		ID:          id,
		RoutineID:   routineID,
		RunID:       runID,
		Summary:     "Test summary for " + id,
		Detail:      "Test detail body",
		Severity:    notification.SeverityInfo,
		Status:      notification.StatusPending,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
}

func TestSQLiteNotifStore_Put_Get(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	n := makeSQLiteNotif(notification.NewID(), "routine-1", "run-1")
	n.WorkflowID = "workflow-1"
	n.WorkflowRunID = "wfrun-1"
	n.SatelliteID = "sat-1"
	n.SessionID = "sess-1"
	n.ProposedActions = []notification.ProposedAction{
		{
			ID:          "action-1",
			Label:       "Do something",
			ToolName:    "bash",
			ToolParams:  map[string]any{"cmd": "echo hello"},
			Destructive: false,
		},
	}

	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != n.ID {
		t.Errorf("ID = %q, want %q", got.ID, n.ID)
	}
	if got.RoutineID != n.RoutineID {
		t.Errorf("RoutineID = %q, want %q", got.RoutineID, n.RoutineID)
	}
	if got.RunID != n.RunID {
		t.Errorf("RunID = %q, want %q", got.RunID, n.RunID)
	}
	if got.WorkflowID != n.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", got.WorkflowID, n.WorkflowID)
	}
	if got.WorkflowRunID != n.WorkflowRunID {
		t.Errorf("WorkflowRunID = %q, want %q", got.WorkflowRunID, n.WorkflowRunID)
	}
	if got.SatelliteID != n.SatelliteID {
		t.Errorf("SatelliteID = %q, want %q", got.SatelliteID, n.SatelliteID)
	}
	if got.SessionID != n.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, n.SessionID)
	}
	if got.Summary != n.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, n.Summary)
	}
	if got.Detail != n.Detail {
		t.Errorf("Detail = %q, want %q", got.Detail, n.Detail)
	}
	if got.Severity != n.Severity {
		t.Errorf("Severity = %q, want %q", got.Severity, n.Severity)
	}
	if got.Status != n.Status {
		t.Errorf("Status = %q, want %q", got.Status, n.Status)
	}
	if len(got.ProposedActions) != 1 {
		t.Fatalf("ProposedActions len = %d, want 1", len(got.ProposedActions))
	}
	if got.ProposedActions[0].ID != "action-1" {
		t.Errorf("ProposedActions[0].ID = %q, want %q", got.ProposedActions[0].ID, "action-1")
	}
	if got.ProposedActions[0].Label != "Do something" {
		t.Errorf("ProposedActions[0].Label = %q, want %q", got.ProposedActions[0].Label, "Do something")
	}
}

func TestSQLiteNotifStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	_, err := s.Get("nonexistent-id")
	if err == nil {
		t.Fatal("Get nonexistent: expected error, got nil")
	}
}

func TestSQLiteNotifStore_Transition(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	n := makeSQLiteNotif(notification.NewID(), "routine-1", "run-1")
	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Transition(n.ID, notification.StatusDismissed); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get after Transition: %v", err)
	}
	if got.Status != notification.StatusDismissed {
		t.Errorf("Status = %q, want %q", got.Status, notification.StatusDismissed)
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("ListPending after dismiss = %d, want 0", len(pending))
	}
}

func TestSQLiteNotifStore_Transition_NotFound(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	err := s.Transition("nonexistent-id", notification.StatusSeen)
	if err == nil {
		t.Fatal("Transition nonexistent: expected error, got nil")
	}
}

func TestSQLiteNotifStore_ListPending(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	for i := 0; i < 3; i++ {
		n := makeSQLiteNotif(notification.NewID(), "routine-lp", "run-lp")
		if err := s.Put(n); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	dismissed := makeSQLiteNotif(notification.NewID(), "routine-lp", "run-lp")
	dismissed.Status = notification.StatusDismissed
	if err := s.Put(dismissed); err != nil {
		t.Fatalf("Put dismissed: %v", err)
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("ListPending = %d, want 3", len(pending))
	}
	for _, p := range pending {
		if p.Status != notification.StatusPending {
			t.Errorf("expected pending status, got %q", p.Status)
		}
	}
}

func TestSQLiteNotifStore_ListByRoutine(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	targetRoutine := "routine-target"
	otherRoutine := "routine-other"

	for i := 0; i < 2; i++ {
		n := makeSQLiteNotif(notification.NewID(), targetRoutine, "run-1")
		if err := s.Put(n); err != nil {
			t.Fatalf("Put target %d: %v", i, err)
		}
	}
	n := makeSQLiteNotif(notification.NewID(), otherRoutine, "run-2")
	if err := s.Put(n); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	list, err := s.ListByRoutine(targetRoutine)
	if err != nil {
		t.Fatalf("ListByRoutine: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListByRoutine = %d, want 2", len(list))
	}
	for _, item := range list {
		if item.RoutineID != targetRoutine {
			t.Errorf("RoutineID = %q, want %q", item.RoutineID, targetRoutine)
		}
	}
}

func TestSQLiteNotifStore_ListByWorkflow(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	targetWF := "workflow-target"
	otherWF := "workflow-other"

	for i := 0; i < 3; i++ {
		n := makeSQLiteNotif(notification.NewID(), "routine-wf", "run-wf")
		n.WorkflowID = targetWF
		if err := s.Put(n); err != nil {
			t.Fatalf("Put target %d: %v", i, err)
		}
	}
	n := makeSQLiteNotif(notification.NewID(), "routine-wf", "run-wf")
	n.WorkflowID = otherWF
	if err := s.Put(n); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	list, err := s.ListByWorkflow(targetWF)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("ListByWorkflow = %d, want 3", len(list))
	}
	for _, item := range list {
		if item.WorkflowID != targetWF {
			t.Errorf("WorkflowID = %q, want %q", item.WorkflowID, targetWF)
		}
	}
}

func TestSQLiteNotifStore_PendingCount(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	for i := 0; i < 5; i++ {
		n := makeSQLiteNotif(notification.NewID(), "routine-pc", "run-pc")
		if err := s.Put(n); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	// Add one seen — should not count
	seen := makeSQLiteNotif(notification.NewID(), "routine-pc", "run-pc")
	seen.Status = notification.StatusSeen
	if err := s.Put(seen); err != nil {
		t.Fatalf("Put seen: %v", err)
	}

	count, err := s.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 5 {
		t.Errorf("PendingCount = %d, want 5", count)
	}
}

func TestSQLiteNotifStore_ExpireRun(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	targetRun := "run-expire-target"

	for i := 0; i < 3; i++ {
		n := makeSQLiteNotif(notification.NewID(), "routine-er", targetRun)
		if err := s.Put(n); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	// Put one notification for a different run — should NOT be expired
	otherN := makeSQLiteNotif(notification.NewID(), "routine-er", "run-other")
	if err := s.Put(otherN); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	// Capture bounds with second-level granularity to account for RFC3339 truncation.
	before := time.Now().UTC().Truncate(time.Second)
	if err := s.ExpireRun(targetRun); err != nil {
		t.Fatalf("ExpireRun: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	list, err := s.ListByRoutine("routine-er")
	if err != nil {
		t.Fatalf("ListByRoutine: %v", err)
	}

	expiredCount := 0
	for _, n := range list {
		if n.RunID == targetRun {
			if n.ExpiresAt == nil {
				t.Errorf("notification %s: ExpiresAt is nil, want set", n.ID)
				continue
			}
			if n.ExpiresAt.Before(before) || n.ExpiresAt.After(after) {
				t.Errorf("notification %s: ExpiresAt = %v, want between %v and %v",
					n.ID, n.ExpiresAt, before, after)
			}
			expiredCount++
		} else {
			if n.ExpiresAt != nil {
				t.Errorf("other-run notification %s: ExpiresAt = %v, want nil", n.ID, n.ExpiresAt)
			}
		}
	}
	if expiredCount != 3 {
		t.Errorf("expired count = %d, want 3", expiredCount)
	}
}

func TestSQLiteNotifStore_ExpireRun_NoMatches(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	// No notifications exist — should not error
	if err := s.ExpireRun("run-does-not-exist"); err != nil {
		t.Fatalf("ExpireRun with no matches: %v", err)
	}
}

// TestSQLiteNotifStore_Deliveries verifies that DeliveryRecord entries survive a
// Put/Get round-trip. Before the fix, deliveries were silently dropped because
// the column did not exist in the schema and was not included in INSERT/SELECT.
func TestSQLiteNotifStore_Deliveries(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	sentAt := time.Now().UTC().Truncate(time.Second)
	n := makeSQLiteNotif(notification.NewID(), "routine-d", "run-d")
	n.Deliveries = []notification.DeliveryRecord{
		{Type: "inbox", Status: "sent", SentAt: sentAt},
		{Type: "space", Target: "space-42", Status: "failed", Error: "timeout", SentAt: sentAt},
	}

	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Deliveries) != 2 {
		t.Fatalf("Deliveries len = %d, want 2", len(got.Deliveries))
	}
	if got.Deliveries[0].Type != "inbox" {
		t.Errorf("Deliveries[0].Type = %q, want %q", got.Deliveries[0].Type, "inbox")
	}
	if got.Deliveries[0].Status != "sent" {
		t.Errorf("Deliveries[0].Status = %q, want %q", got.Deliveries[0].Status, "sent")
	}
	if got.Deliveries[1].Type != "space" {
		t.Errorf("Deliveries[1].Type = %q, want %q", got.Deliveries[1].Type, "space")
	}
	if got.Deliveries[1].Target != "space-42" {
		t.Errorf("Deliveries[1].Target = %q, want %q", got.Deliveries[1].Target, "space-42")
	}
	if got.Deliveries[1].Error != "timeout" {
		t.Errorf("Deliveries[1].Error = %q, want %q", got.Deliveries[1].Error, "timeout")
	}
}

// TestSQLiteNotifStore_StepFields verifies that StepPosition and StepName survive
// a Put/Get round-trip. Before the fix, these fields were silently dropped because
// the columns did not exist in the schema and were not included in INSERT/SELECT.
func TestSQLiteNotifStore_StepFields(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	pos := 3
	n := makeSQLiteNotif(notification.NewID(), "routine-sf", "run-sf")
	n.WorkflowID = "wf-step-test"
	n.StepPosition = &pos
	n.StepName = "analyze-data"

	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StepPosition == nil {
		t.Fatal("StepPosition is nil after Get, want *3")
	}
	if *got.StepPosition != pos {
		t.Errorf("StepPosition = %d, want %d", *got.StepPosition, pos)
	}
	if got.StepName != "analyze-data" {
		t.Errorf("StepName = %q, want %q", got.StepName, "analyze-data")
	}
}

// TestSQLiteNotifStore_StepPosition_NilRoundTrip verifies that a notification
// with no StepPosition (nil pointer) round-trips correctly — StepPosition must
// remain nil (not become a pointer to zero).
func TestSQLiteNotifStore_StepPosition_NilRoundTrip(t *testing.T) {
	t.Parallel()
	db := openNotifTestDB(t)
	s := notification.NewSQLiteNotificationStore(db)

	n := makeSQLiteNotif(notification.NewID(), "routine-nil", "run-nil")
	// StepPosition and StepName intentionally left at zero values.

	if err := s.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StepPosition != nil {
		t.Errorf("StepPosition = %v, want nil", got.StepPosition)
	}
	if got.StepName != "" {
		t.Errorf("StepName = %q, want empty", got.StepName)
	}
}
