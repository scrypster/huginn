package notification

import "context"

// StoreInterface is the read/write contract for a notification store.
type StoreInterface interface {
	// Put writes a Notification and all its index keys atomically.
	Put(n *Notification) error

	// Get retrieves a single Notification by ID.
	Get(id string) (*Notification, error)

	// Transition moves a Notification to newStatus, updating index keys atomically.
	Transition(id string, newStatus Status) error

	// ListPending returns all pending notifications, newest first.
	ListPending() ([]*Notification, error)

	// ListPendingN returns up to limit pending notifications, newest first.
	// If limit <= 0 all pending notifications are returned.
	ListPendingN(limit int) ([]*Notification, error)

	// ListByRoutine returns all notifications for a routine, newest first.
	ListByRoutine(routineID string) ([]*Notification, error)

	// ListByRoutineN returns up to limit notifications for a routine, newest first.
	// If limit <= 0 all notifications are returned.
	ListByRoutineN(routineID string, limit int) ([]*Notification, error)

	// ListByWorkflow returns all notifications produced by a workflow, newest first.
	ListByWorkflow(workflowID string) ([]*Notification, error)

	// ListByWorkflowN returns up to limit notifications for a workflow, newest first.
	// If limit <= 0 all notifications are returned.
	ListByWorkflowN(workflowID string, limit int) ([]*Notification, error)

	// PendingCount returns the count of pending notifications.
	PendingCount() (int, error)

	// ExpireRun sets ExpiresAt = now for all notifications belonging to runID.
	ExpireRun(runID string) error

	// PruneExpired deletes all notifications whose ExpiresAt is set and in the past.
	// Returns the count of pruned notifications.
	// Respects ctx.Done() for cancellation.
	PruneExpired(ctx context.Context) (int, error)
}
