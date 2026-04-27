// internal/scheduler/delivery_queue.go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/scrypster/huginn/internal/notification"
)

// DeliveryQueue manages the durable retry queue for failed webhook/email
// deliveries. It is safe for concurrent use.
type DeliveryQueue struct {
	store          *DeliveryQueueStore
	deliverers     *DelivererRegistry
	notifStore     notification.StoreInterface
	broadcastFn    WorkflowBroadcastFunc
	mu             sync.Mutex
	lastEnqueuedID string // for test inspection only
}

// NewDeliveryQueue constructs a DeliveryQueue.
func NewDeliveryQueue(
	store *DeliveryQueueStore,
	deliverers *DelivererRegistry,
	notifStore notification.StoreInterface,
	broadcastFn WorkflowBroadcastFunc,
) *DeliveryQueue {
	return &DeliveryQueue{
		store:       store,
		deliverers:  deliverers,
		notifStore:  notifStore,
		broadcastFn: broadcastFn,
	}
}

// Enqueue adds a failed delivery to the durable queue. Any existing
// pending/retrying entry for the same (workflowID, target endpoint) is
// superseded first (dedup). schedule is the workflow's cron expression.
func (q *DeliveryQueue) Enqueue(
	ctx context.Context,
	workflowID, runID, schedule string,
	n *notification.Notification,
	target NotificationDelivery,
) error {
	payload := DeliveryQueuePayload{
		Notification: *n,
		Target:       target,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("delivery queue: marshal payload: %w", err)
	}
	retryWindow := ComputeRetryWindow(schedule)
	entry := DeliveryQueueEntry{
		ID:           uuid.New().String(),
		WorkflowID:   workflowID,
		RunID:        runID,
		Endpoint:     endpointKey(target),
		Channel:      target.Type,
		Payload:      string(payloadJSON),
		Status:       "pending",
		AttemptCount: 0,
		MaxAttempts:  5,
		RetryWindowS: retryWindow,
		NextRetryAt:  time.Now().UTC(), // attempt 0: fire on next poll tick
	}
	if err := q.store.SupersedeAndInsert(entry); err != nil {
		return fmt.Errorf("delivery queue: enqueue: %w", err)
	}
	slog.Info("delivery queue: enqueued entry",
		"workflow_id", workflowID, "run_id", runID,
		"channel", target.Type, "endpoint", entry.Endpoint,
		"retry_window_s", retryWindow)
	q.mu.Lock()
	q.lastEnqueuedID = entry.ID
	q.mu.Unlock()
	return nil
}

// BadgeCount returns the number of distinct (workflow_id, endpoint) pairs
// with permanently failed deliveries.
func (q *DeliveryQueue) BadgeCount() (int, error) {
	return q.store.BadgeCount()
}

// ListActionable returns entries needing user attention (status=failed).
func (q *DeliveryQueue) ListActionable(limit int) ([]DeliveryQueueEntry, error) {
	return q.store.ListActionable(limit)
}

// Dismiss removes a failed entry from the queue.
func (q *DeliveryQueue) Dismiss(id string) error {
	if err := q.store.Dismiss(id); err != nil {
		return err
	}
	q.broadcastBadge()
	return nil
}

// ForceRetry resets a failed entry to pending with next_retry_at=now,
// closes its circuit breaker if open, then triggers an immediate worker sweep.
func (q *DeliveryQueue) ForceRetry(ctx context.Context, id string) error {
	entry, err := q.store.Get(id)
	if err != nil {
		return fmt.Errorf("entry not found: %w", err)
	}
	next := time.Now().UTC()
	if err := q.store.UpdateAttempt(entry.ID, "pending", entry.AttemptCount, "", &next); err != nil {
		return fmt.Errorf("reset entry: %w", err)
	}
	health, _ := q.store.GetHealth(entry.WorkflowID, entry.Endpoint)
	if health.CircuitState == "open" {
		health.CircuitState = "closed"
		health.ConsecutiveFailures = 0
		_ = q.store.UpsertHealth(health)
	}
	go q.RunOnce(ctx)
	return nil
}

// StartWorker launches the background retry goroutine. Call once at startup.
func (q *DeliveryQueue) StartWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		q.RunOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				q.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce processes all due entries once. Exported so ForceRetry can trigger
// an immediate sweep.
func (q *DeliveryQueue) RunOnce(ctx context.Context) {
	entries, err := q.store.ListDue(time.Now().UTC(), 50)
	if err != nil {
		slog.Error("delivery queue: list due entries", "err", err)
		return
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		q.attemptDelivery(ctx, e)
	}
}

// attemptDelivery runs one delivery attempt for the given entry.
func (q *DeliveryQueue) attemptDelivery(ctx context.Context, e DeliveryQueueEntry) {
	health, err := q.store.GetHealth(e.WorkflowID, e.Endpoint)
	if err != nil {
		slog.Error("delivery queue: get health", "err", err)
		return
	}
	if health.CircuitState == "open" {
		if health.LastProbeAt != nil && time.Since(*health.LastProbeAt) < time.Duration(e.RetryWindowS)*time.Second {
			return // too soon to probe; allow the first probe immediately when LastProbeAt is nil
		}
		slog.Info("delivery queue: circuit open — probing", "workflow_id", e.WorkflowID, "endpoint", e.Endpoint)
	}

	var payload DeliveryQueuePayload
	if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
		slog.Error("delivery queue: decode payload", "id", e.ID, "err", err)
		return
	}

	d := q.deliverers.get(e.Channel)
	if d == nil {
		slog.Warn("delivery queue: no deliverer for channel", "channel", e.Channel)
		return
	}
	rec := d.Deliver(ctx, &payload.Notification, payload.Target)
	now := time.Now().UTC()
	e.AttemptCount++

	if rec.Status == "sent" {
		if err := q.store.MarkDelivered(e.ID); err != nil {
			slog.Error("delivery queue: mark delivered", "id", e.ID, "err", err)
		}
		health.ConsecutiveFailures = 0
		health.CircuitState = "closed"
		health.OpenedAt = nil
		_ = q.store.UpsertHealth(health)
		slog.Info("delivery queue: delivered", "id", e.ID, "workflow_id", e.WorkflowID)
		q.broadcastBadge()
		return
	}

	health.ConsecutiveFailures++
	health.LastProbeAt = &now
	if health.CircuitState != "open" && health.ConsecutiveFailures >= circuitBreakThreshold {
		health.CircuitState = "open"
		health.OpenedAt = &now
		slog.Warn("delivery queue: circuit opened",
			"workflow_id", e.WorkflowID, "endpoint", e.Endpoint,
			"consecutive_failures", health.ConsecutiveFailures)
	}
	_ = q.store.UpsertHealth(health)

	if e.AttemptCount >= e.MaxAttempts {
		_ = q.store.UpdateAttempt(e.ID, "failed", e.AttemptCount, rec.Error, nil)
		slog.Warn("delivery queue: exhausted", "id", e.ID, "workflow_id", e.WorkflowID, "last_error", rec.Error)
		q.escalate(e, payload, rec.Error)
		q.broadcastBadge()
		return
	}

	delay := nextRetryDelay(e.RetryWindowS, e.AttemptCount)
	next := now.Add(delay)
	_ = q.store.UpdateAttempt(e.ID, "retrying", e.AttemptCount, rec.Error, &next)
	slog.Info("delivery queue: scheduled retry",
		"id", e.ID, "attempt", e.AttemptCount, "next_retry_at", next)
}

// escalate fires an inbox notification when all retries are exhausted.
func (q *DeliveryQueue) escalate(e DeliveryQueueEntry, payload DeliveryQueuePayload, lastError string) {
	if q.notifStore == nil {
		return
	}
	endpointDisplay := e.Endpoint
	if len(endpointDisplay) > 50 {
		endpointDisplay = endpointDisplay[:47] + "..."
	}
	now := time.Now().UTC()
	n := notification.Notification{
		ID:            fmt.Sprintf("dlq-escalation-%s", e.ID),
		WorkflowID:    e.WorkflowID,
		WorkflowRunID: e.RunID,
		Summary:       fmt.Sprintf("Delivery to %s permanently failed", endpointDisplay),
		Detail:        fmt.Sprintf("Workflow run %s could not deliver to %s after %d attempts. Last error: %s", e.RunID, e.Endpoint, e.AttemptCount, lastError),
		Severity:      notification.SeverityUrgent,
		Status:        notification.StatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := q.notifStore.Put(&n); err != nil {
		slog.Error("delivery queue: escalation notification failed", "err", err)
	}
}

// broadcastBadge emits a delivery_badge_update WS event with the current count.
func (q *DeliveryQueue) broadcastBadge() {
	if q.broadcastFn == nil {
		return
	}
	count, err := q.store.BadgeCount()
	if err != nil {
		slog.Warn("delivery queue: badge count error", "err", err)
		return
	}
	q.broadcastFn("delivery_badge_update", map[string]any{"count": count})
}
