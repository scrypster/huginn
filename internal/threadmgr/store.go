package threadmgr

import "context"

// ThreadStore persists thread lifecycle state to durable storage.
// All methods must be safe for concurrent use. Implementations should treat
// a nil receiver as a no-op (in-memory-only mode) to keep callers simple.
type ThreadStore interface {
	// SaveThread upserts the full thread record (INSERT OR REPLACE).
	// Called after Create() to ensure the thread is durably recorded.
	SaveThread(ctx context.Context, t *Thread) error

	// LoadThreads returns all threads for a given sessionID, ordered by
	// created_at ASC. Returns an empty slice (never nil) when none exist.
	LoadThreads(ctx context.Context, sessionID string) ([]*Thread, error)

	// DeleteThread removes the thread record by ID.
	// Returns nil if the thread does not exist (idempotent).
	DeleteThread(ctx context.Context, id string) error

	// UpdateThreadStatus updates the status column for the thread with the
	// given ID. status must be one of the ThreadStatus constants.
	UpdateThreadStatus(ctx context.Context, id, status string) error
}
