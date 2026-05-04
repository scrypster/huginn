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
		Summary:   "test summary",
		Detail:    "test detail",
		Severity:  sev,
		Status:    notification.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
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

func TestStore_ListPendingN_RespectsLimit(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	const total = 10
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		n := makeNotif("routineA", "run1", notification.SeverityInfo)
		if err := s.Put(n); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	// Newest-first means lexicographically largest ULID first (ULID sorts by time).
	// Find the max ID across all inserted notifications — that is what the store
	// should return as got[0] regardless of insertion order within a millisecond.
	maxID := ids[0]
	for _, id := range ids[1:] {
		if id > maxID {
			maxID = id
		}
	}

	got, err := s.ListPendingN(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5, got %d", len(got))
	}
	// The first result should be the lexicographically largest (newest) notification.
	if got[0].ID != maxID {
		t.Errorf("want first result to be newest ID %s, got %s", maxID, got[0].ID)
	}

	// limit=0 should return all
	all, err := s.ListPendingN(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != total {
		t.Fatalf("want %d with limit=0, got %d", total, len(all))
	}
	// limit=0 result should also be newest-first
	if all[0].ID != maxID {
		t.Errorf("want first result (no limit) to be newest ID %s, got %s", maxID, all[0].ID)
	}
}

func TestStore_PutOverwrite_RemovesStaleIndexes(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	s := notification.NewStore(db)

	n := makeNotif("routine-old", "run-old", notification.SeverityInfo)
	n.ID = "notif-stale-index"
	n.WorkflowID = "workflow-old"
	if err := s.Put(n); err != nil {
		t.Fatalf("Put old: %v", err)
	}

	n.RoutineID = "routine-new"
	n.RunID = "run-new"
	n.WorkflowID = "workflow-new"
	n.Status = notification.StatusSeen
	n.UpdatedAt = time.Now().UTC()
	if err := s.Put(n); err != nil {
		t.Fatalf("Put new: %v", err)
	}

	oldRoutine, err := s.ListByRoutine("routine-old")
	if err != nil {
		t.Fatalf("ListByRoutine(old): %v", err)
	}
	if len(oldRoutine) != 0 {
		t.Fatalf("old routine index should be empty, got %d entries", len(oldRoutine))
	}

	oldWorkflow, err := s.ListByWorkflow("workflow-old")
	if err != nil {
		t.Fatalf("ListByWorkflow(old): %v", err)
	}
	if len(oldWorkflow) != 0 {
		t.Fatalf("old workflow index should be empty, got %d entries", len(oldWorkflow))
	}

	pending, err := s.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending should be empty after overwrite to seen, got %d", len(pending))
	}

	got, err := s.Get(n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RoutineID != "routine-new" || got.WorkflowID != "workflow-new" || got.Status != notification.StatusSeen {
		t.Fatalf("unexpected canonical record after overwrite: %+v", got)
	}
}
