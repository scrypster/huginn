package notification_test

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/notification"
)

func openTestDB(t *testing.T) (*pebble.DB, func()) {
	t.Helper()
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return db, func() { db.Close() }
}

func makeNotif(routineID, runID string, sev notification.Severity) *notification.Notification {
	return &notification.Notification{
		ID:        notification.NewID(),
		RoutineID: routineID,
		RunID:     runID,
		Summary:      "test summary",
		Detail:       "test detail",
		Severity:     sev,
		Status:       notification.StatusPending,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func TestStorePut(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("autoA", "run1", notification.SeverityInfo)
	if err := s.Put(n); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if pending[0].ID != n.ID {
		t.Errorf("want ID %s, got %s", n.ID, pending[0].ID)
	}
}

func TestStoreGet(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("autoA", "run1", notification.SeverityWarning)
	if err := s.Put(n); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != n.Summary {
		t.Errorf("want summary %q, got %q", n.Summary, got.Summary)
	}
}

func TestStoreTransition(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("autoA", "run1", notification.SeverityWarning)
	if err := s.Put(n); err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(n.ID, notification.StatusDismissed); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("want 0 pending after dismiss, got %d", len(pending))
	}
	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != notification.StatusDismissed {
		t.Errorf("want status dismissed, got %s", got.Status)
	}
}

func TestStoreListByRoutine(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	for i := 0; i < 3; i++ {
		if err := s.Put(makeNotif("autoA", "runA", notification.SeverityInfo)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put(makeNotif("autoB", "runB", notification.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListByRoutine("autoA")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("want 3 for autoA, got %d", len(list))
	}
}

func TestStoreExpireRun(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("autoA", "run1", notification.SeverityInfo)
	if err := s.Put(n); err != nil {
		t.Fatal(err)
	}
	if err := s.ExpireRun("run1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt == nil {
		t.Error("want ExpiresAt set after ExpireRun, got nil")
	}
}

func TestStorePendingCount(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	for i := 0; i < 5; i++ {
		if err := s.Put(makeNotif("auto1", "run1", notification.SeverityInfo)); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.PendingCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("want pending count 5, got %d", count)
	}
}

func TestStore_ListByWorkflow(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	store := notification.NewStore(db)
	n := makeNotif("routineA", "run1", notification.SeverityInfo)
	n.WorkflowID = "wf1"
	n.WorkflowRunID = "run1"
	if err := store.Put(n); err != nil {
		t.Fatal(err)
	}
	results, err := store.ListByWorkflow("wf1")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1, got %d", len(results))
	}
	if results[0].WorkflowRunID != "run1" {
		t.Error("WorkflowRunID mismatch")
	}
}
