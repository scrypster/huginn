package notification

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

	// ListByRoutine returns all notifications for a routine, newest first.
	ListByRoutine(routineID string) ([]*Notification, error)

	// ListByWorkflow returns all notifications produced by a workflow, newest first.
	ListByWorkflow(workflowID string) ([]*Notification, error)

	// PendingCount returns the count of pending notifications.
	PendingCount() (int, error)

	// ExpireRun sets ExpiresAt = now for all notifications belonging to runID.
	ExpireRun(runID string) error
}
