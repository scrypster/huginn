package notification

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

const (
	pfxByID       = "notifications/id/"
	pfxByStatus   = "notifications/status/"
	pfxByRoutine  = "notifications/routine/"
	pfxByRun      = "notifications/run/"
	pfxByWorkflow = "notifications/workflow/"
)

// Store manages Notification records in Pebble KV with multi-index prefix scans.
type Store struct {
	db *pebble.DB
}

// NewStore creates a Store backed by the given Pebble DB.
func NewStore(db *pebble.DB) *Store {
	return &Store{db: db}
}

// Put writes a Notification and all its index keys atomically.
func (s *Store) Put(n *Notification) error {
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("notification: marshal: %w", err)
	}
	b := s.db.NewBatch()
	defer b.Close()
	b.Set([]byte(pfxByID+n.ID), data, nil)
	b.Set([]byte(pfxByStatus+string(n.Status)+"/"+n.ID), []byte(n.ID), nil)
	b.Set([]byte(pfxByRoutine+n.RoutineID+"/"+n.ID), []byte(n.ID), nil)
	b.Set([]byte(pfxByRun+n.RunID+"/"+n.ID), []byte(n.ID), nil)
	if n.WorkflowID != "" {
		b.Set([]byte(pfxByWorkflow+n.WorkflowID+"/"+n.ID), []byte(n.ID), nil)
	}
	return b.Commit(pebble.Sync)
}

// Get retrieves a single Notification by ID.
func (s *Store) Get(id string) (*Notification, error) {
	data, closer, err := s.db.Get([]byte(pfxByID + id))
	if err != nil {
		return nil, fmt.Errorf("notification: get %s: %w", id, err)
	}
	defer closer.Close()
	var n Notification
	if err := json.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("notification: unmarshal %s: %w", id, err)
	}
	return &n, nil
}

// Transition moves a Notification to newStatus, updating index keys atomically.
func (s *Store) Transition(id string, newStatus Status) error {
	n, err := s.Get(id)
	if err != nil {
		return err
	}
	oldStatus := n.Status
	n.Status = newStatus
	n.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("notification: marshal: %w", err)
	}
	b := s.db.NewBatch()
	defer b.Close()
	b.Set([]byte(pfxByID+id), data, nil)
	b.Delete([]byte(pfxByStatus+string(oldStatus)+"/"+id), nil)
	b.Set([]byte(pfxByStatus+string(newStatus)+"/"+id), []byte(id), nil)
	return b.Commit(pebble.Sync)
}

// ListPending returns all pending notifications, newest first.
func (s *Store) ListPending() ([]*Notification, error) {
	return s.listByPrefix(pfxByStatus + string(StatusPending) + "/")
}

// ListByRoutine returns all notifications for a routine, newest first.
func (s *Store) ListByRoutine(routineID string) ([]*Notification, error) {
	return s.listByPrefix(pfxByRoutine + routineID + "/")
}

// ListByWorkflow returns all notifications produced by a workflow, newest first.
func (s *Store) ListByWorkflow(workflowID string) ([]*Notification, error) {
	return s.listByPrefix(pfxByWorkflow + workflowID + "/")
}

// PendingCount returns the count of pending notifications.
func (s *Store) PendingCount() (int, error) {
	ids, err := s.scanIDs(pfxByStatus + string(StatusPending) + "/")
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// ExpireRun sets ExpiresAt = now for all notifications belonging to runID.
// All updates are applied in a single atomic batch.
func (s *Store) ExpireRun(runID string) error {
	ids, err := s.scanIDs(pfxByRun + runID + "/")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	b := s.db.NewBatch()
	defer b.Close()
	for _, id := range ids {
		n, err := s.Get(id)
		if err != nil {
			continue
		}
		n.ExpiresAt = &now
		n.UpdatedAt = now
		data, err := json.Marshal(n)
		if err != nil {
			continue
		}
		b.Set([]byte(pfxByID+id), data, nil)
	}
	return b.Commit(pebble.Sync)
}

// listByPrefix does a prefix scan on index keys, loads canonical records,
// and returns them newest-first (IDs are time-sortable ascending).
func (s *Store) listByPrefix(prefix string) ([]*Notification, error) {
	ids, err := s.scanIDs(prefix)
	if err != nil {
		return nil, err
	}
	// Reverse to get newest first (IDs sort ascending by creation time).
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	out := make([]*Notification, 0, len(ids))
	for _, id := range ids {
		n, err := s.Get(id)
		if err != nil {
			continue // skip corrupt/missing records
		}
		out = append(out, n)
	}
	return out, nil
}

// scanIDs returns all notification IDs under a prefix in ascending order.
func (s *Store) scanIDs(prefix string) ([]string, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: keyUpperBound([]byte(prefix)),
	})
	if err != nil {
		return nil, fmt.Errorf("notification: iter: %w", err)
	}
	defer iter.Close()
	var ids []string
	for iter.First(); iter.Valid(); iter.Next() {
		ids = append(ids, string(iter.Value()))
	}
	return ids, iter.Error()
}

// Compile-time assertion: *Store must satisfy StoreInterface.
var _ StoreInterface = (*Store)(nil)

// keyUpperBound returns the smallest key greater than all keys with the given prefix.
func keyUpperBound(prefix []byte) []byte {
	out := make([]byte, len(prefix))
	copy(out, prefix)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			return out[:i+1]
		}
	}
	return nil // all 0xFF — no upper bound needed
}
