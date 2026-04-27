package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDeliveryQueueStore(t *testing.T) *DeliveryQueueStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	if err := db.Migrate(Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewDeliveryQueueStore(db)
}

func TestDeliveryQueueStore_InsertAndGet(t *testing.T) {
	s := newTestDeliveryQueueStore(t)
	entry := DeliveryQueueEntry{
		ID:           "entry-1",
		WorkflowID:   "wf-1",
		RunID:        "run-1",
		Endpoint:     "https://hooks.slack.com/abc",
		Channel:      "webhook",
		Payload:      `{"notification":{},"target":{}}`,
		Status:       "pending",
		AttemptCount: 0,
		MaxAttempts:  5,
		RetryWindowS: 480,
		NextRetryAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := s.Get("entry-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.WorkflowID != "wf-1" || got.Status != "pending" {
		t.Errorf("unexpected entry: %+v", got)
	}
}

func TestDeliveryQueueStore_SupersedeAndInsert(t *testing.T) {
	s := newTestDeliveryQueueStore(t)
	base := DeliveryQueueEntry{
		ID: "old-1", WorkflowID: "wf-1", RunID: "run-1",
		Endpoint: "https://hooks.slack.com/abc", Channel: "webhook",
		Payload: `{}`, Status: "pending", MaxAttempts: 5, RetryWindowS: 480,
		NextRetryAt: time.Now().UTC(),
	}
	if err := s.Insert(base); err != nil {
		t.Fatalf("Insert old: %v", err)
	}
	newEntry := base
	newEntry.ID = "new-1"
	newEntry.RunID = "run-2"
	if err := s.SupersedeAndInsert(newEntry); err != nil {
		t.Fatalf("SupersedeAndInsert: %v", err)
	}
	old, _ := s.Get("old-1")
	if old.Status != "superseded" {
		t.Errorf("old entry not superseded, got status=%q", old.Status)
	}
	newGot, _ := s.Get("new-1")
	if newGot.Status != "pending" {
		t.Errorf("new entry wrong status=%q", newGot.Status)
	}
}

func TestDeliveryQueueStore_ListDue(t *testing.T) {
	s := newTestDeliveryQueueStore(t)
	past := time.Now().Add(-1 * time.Minute).UTC()
	future := time.Now().Add(1 * time.Hour).UTC()
	due := DeliveryQueueEntry{ID: "due-1", WorkflowID: "wf-1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: past}
	notDue := DeliveryQueueEntry{ID: "future-1", WorkflowID: "wf-1", RunID: "r2", Endpoint: "y", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: future}
	_ = s.Insert(due)
	_ = s.Insert(notDue)
	rows, err := s.ListDue(time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "due-1" {
		t.Errorf("ListDue returned %d rows, want 1 with id=due-1", len(rows))
	}
}

func TestDeliveryQueueStore_UpdateStatus(t *testing.T) {
	s := newTestDeliveryQueueStore(t)
	e := DeliveryQueueEntry{ID: "e1", WorkflowID: "w1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "pending", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: time.Now().UTC()}
	_ = s.Insert(e)
	next := time.Now().Add(5 * time.Minute).UTC()
	if err := s.UpdateAttempt("e1", "retrying", 1, "conn refused", &next); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	got, _ := s.Get("e1")
	if got.Status != "retrying" || got.AttemptCount != 1 {
		t.Errorf("unexpected after update: %+v", got)
	}
}

func TestDeliveryQueueStore_BadgeCount(t *testing.T) {
	s := newTestDeliveryQueueStore(t)
	now := time.Now().UTC()
	_ = s.Insert(DeliveryQueueEntry{ID: "f1", WorkflowID: "w1", RunID: "r1", Endpoint: "x", Channel: "webhook", Payload: "{}", Status: "failed", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: now})
	_ = s.Insert(DeliveryQueueEntry{ID: "f2", WorkflowID: "w1", RunID: "r2", Endpoint: "y", Channel: "email", Payload: "{}", Status: "delivered", MaxAttempts: 5, RetryWindowS: 480, NextRetryAt: now})
	count, err := s.BadgeCount()
	if err != nil {
		t.Fatalf("BadgeCount: %v", err)
	}
	if count != 1 {
		t.Errorf("BadgeCount = %d, want 1", count)
	}
}
