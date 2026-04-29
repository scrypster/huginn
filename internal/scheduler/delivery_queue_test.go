package scheduler

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func newTestDeliveryQueue(t *testing.T) *DeliveryQueue {
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
	store := NewDeliveryQueueStore(db)
	return NewDeliveryQueue(store, NewDelivererRegistry(nil), nil, nil)
}

func TestDeliveryQueue_Enqueue_CreatesEntry(t *testing.T) {
	q := newTestDeliveryQueue(t)
	n := &notification.Notification{ID: "n1", WorkflowID: "wf1", WorkflowRunID: "run1", Summary: "test"}
	target := NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"}
	if err := q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	entries, err := q.store.ListDue(time.Now().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].WorkflowID != "wf1" || entries[0].Channel != "webhook" {
		t.Errorf("unexpected entry: %+v", entries[0])
	}
}

func TestDeliveryQueue_Enqueue_Deduplicates(t *testing.T) {
	q := newTestDeliveryQueue(t)
	n := &notification.Notification{ID: "n1", WorkflowID: "wf1", Summary: "first"}
	target := NotificationDelivery{Type: "webhook", To: "https://hooks.slack.com/abc"}
	_ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)

	n2 := &notification.Notification{ID: "n2", WorkflowID: "wf1", Summary: "second"}
	_ = q.Enqueue(context.Background(), "wf1", "run2", "*/10 * * * *", n2, target)

	due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
	if len(due) != 1 {
		t.Fatalf("want 1 active entry after dedup, got %d", len(due))
	}
	var payload DeliveryQueuePayload
	_ = json.Unmarshal([]byte(due[0].Payload), &payload)
	if payload.Notification.Summary != "second" {
		t.Errorf("wrong entry kept, want 'second', got %q", payload.Notification.Summary)
	}
}

func TestDeliveryQueue_Enqueue_RetryWindowFromSchedule(t *testing.T) {
	q := newTestDeliveryQueue(t)
	n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
	target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
	_ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)
	due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
	if len(due) == 0 {
		t.Fatal("no entry")
	}
	if due[0].RetryWindowS < 400 || due[0].RetryWindowS > 500 {
		t.Errorf("RetryWindowS = %d, want ~480", due[0].RetryWindowS)
	}
}

func TestDeliveryQueue_Worker_DeliverSuccess(t *testing.T) {
	q := newTestDeliveryQueue(t)
	q.deliverers = &DelivererRegistry{m: map[string]Deliverer{
		"webhook": &mockDeliverer{status: "sent"},
	}}
	n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
	target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
	_ = q.Enqueue(context.Background(), "wf1", "run1", "*/10 * * * *", n, target)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	q.RunOnce(ctx)

	due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
	if len(due) != 0 {
		t.Errorf("entry still pending after successful delivery")
	}
	q.mu.Lock()
	id := q.lastEnqueuedID
	q.mu.Unlock()
	got, _ := q.store.Get(id)
	if got.Status != "delivered" {
		t.Errorf("want status=delivered, got %q", got.Status)
	}
}

func TestDeliveryQueue_Worker_CircuitOpensAfter5Failures(t *testing.T) {
	q := newTestDeliveryQueue(t)
	q.deliverers = &DelivererRegistry{m: map[string]Deliverer{
		"webhook": &mockDeliverer{status: "failed", errMsg: "connection refused"},
	}}
	n := &notification.Notification{ID: "n1", WorkflowID: "wf1"}
	target := NotificationDelivery{Type: "webhook", To: "https://x.com"}
	_ = q.Enqueue(context.Background(), "wf1", "run1", "", n, target)
	due, _ := q.store.ListDue(time.Now().Add(time.Minute), 10)
	if len(due) == 0 {
		t.Fatal("no entry")
	}
	entryID := due[0].ID
	endpoint := due[0].Endpoint

	ctx := context.Background()
	for i := 0; i < circuitBreakThreshold; i++ {
		q.attemptDelivery(ctx, due[0])
		due[0], _ = q.store.Get(entryID)
	}
	health, _ := q.store.GetHealth("wf1", endpoint)
	if health.CircuitState != "open" {
		t.Errorf("circuit should be open after %d failures, got %q", circuitBreakThreshold, health.CircuitState)
	}
}

func TestDeliveryQueue_BadgeActionableAndDismiss(t *testing.T) {
	q := newTestDeliveryQueue(t)
	now := time.Now().UTC()
	_ = q.store.Insert(DeliveryQueueEntry{
		ID:           "badge-1",
		WorkflowID:   "wf-1",
		RunID:        "run-1",
		Endpoint:     "https://hooks.slack.com/a",
		Channel:      "webhook",
		Payload:      `{}`,
		Status:       "failed",
		MaxAttempts:  5,
		RetryWindowS: 480,
		NextRetryAt:  now,
	})

	count, err := q.BadgeCount()
	if err != nil {
		t.Fatalf("BadgeCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("BadgeCount = %d, want 1", count)
	}

	rows, err := q.ListActionable(10)
	if err != nil {
		t.Fatalf("ListActionable: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "badge-1" {
		t.Fatalf("ListActionable mismatch: %+v", rows)
	}

	if err := q.Dismiss("badge-1"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	count, err = q.BadgeCount()
	if err != nil {
		t.Fatalf("BadgeCount after dismiss: %v", err)
	}
	if count != 0 {
		t.Errorf("BadgeCount after dismiss = %d, want 0", count)
	}
}

func TestDeliveryQueue_ForceRetry_ResetsFailedEntryAndClosesCircuit(t *testing.T) {
	q := newTestDeliveryQueue(t)
	now := time.Now().UTC()
	entry := DeliveryQueueEntry{
		ID:           "force-1",
		WorkflowID:   "wf-force",
		RunID:        "run-force",
		Endpoint:     "https://hooks.slack.com/force",
		Channel:      "webhook",
		Payload:      `{}`,
		Status:       "failed",
		AttemptCount: 5,
		MaxAttempts:  5,
		RetryWindowS: 120,
		NextRetryAt:  now,
	}
	if err := q.store.Insert(entry); err != nil {
		t.Fatalf("insert failed entry: %v", err)
	}
	opened := now.Add(-time.Minute)
	if err := q.store.UpsertHealth(EndpointHealth{
		WorkflowID:          entry.WorkflowID,
		Endpoint:            entry.Endpoint,
		ConsecutiveFailures: 9,
		CircuitState:        "open",
		OpenedAt:            &opened,
		LastProbeAt:         &opened,
	}); err != nil {
		t.Fatalf("UpsertHealth: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // make RunOnce(ctx) exit without mutating the reset entry
	if err := q.ForceRetry(ctx, entry.ID); err != nil {
		t.Fatalf("ForceRetry: %v", err)
	}

	got, err := q.store.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get entry: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("status after ForceRetry = %q, want pending", got.Status)
	}
	health, err := q.store.GetHealth(entry.WorkflowID, entry.Endpoint)
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if health.CircuitState != "closed" {
		t.Errorf("circuit_state after ForceRetry = %q, want closed", health.CircuitState)
	}
	if health.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures after ForceRetry = %d, want 0", health.ConsecutiveFailures)
	}
}

func TestDeliveryQueue_ForceRetry_NotFound(t *testing.T) {
	q := newTestDeliveryQueue(t)
	if err := q.ForceRetry(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected ForceRetry to fail for unknown entry")
	}
}

type mockDeliverer struct {
	status string
	errMsg string
}

func (m *mockDeliverer) Deliver(_ context.Context, _ *notification.Notification, _ NotificationDelivery) notification.DeliveryRecord {
	return notification.DeliveryRecord{Status: m.status, Error: m.errMsg}
}
