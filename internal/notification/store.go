package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

	var previous *Notification
	if existing, err := s.Get(n.ID); err == nil {
		previous = existing
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return fmt.Errorf("notification: read existing %s: %w", n.ID, err)
	}

	b := s.db.NewBatch()
	defer b.Close()
	if previous != nil {
		deleteIndexKeys(b, previous)
	}
	b.Set([]byte(pfxByID+n.ID), data, nil)
	setIndexKeys(b, n)
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
	if err := ValidateTransition(n.Status, newStatus); err != nil {
		return fmt.Errorf("notification: transition %s: %w", id, err)
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

// ListPendingN returns up to limit pending notifications, newest first.
// If limit <= 0 all pending notifications are returned.
func (s *Store) ListPendingN(limit int) ([]*Notification, error) {
	return s.listByPrefixN(pfxByStatus+string(StatusPending)+"/", limit)
}

// ListByRoutine returns all notifications for a routine, newest first.
func (s *Store) ListByRoutine(routineID string) ([]*Notification, error) {
	return s.listByPrefix(pfxByRoutine + routineID + "/")
}

// ListByWorkflow returns all notifications produced by a workflow, newest first.
func (s *Store) ListByWorkflow(workflowID string) ([]*Notification, error) {
	return s.listByPrefix(pfxByWorkflow + workflowID + "/")
}

// ListByRoutineN returns up to limit notifications for a routine, newest first.
// If limit <= 0 all notifications are returned.
func (s *Store) ListByRoutineN(routineID string, limit int) ([]*Notification, error) {
	return s.listByPrefixN(pfxByRoutine+routineID+"/", limit)
}

// ListByWorkflowN returns up to limit notifications for a workflow, newest first.
// If limit <= 0 all notifications are returned.
func (s *Store) ListByWorkflowN(workflowID string, limit int) ([]*Notification, error) {
	return s.listByPrefixN(pfxByWorkflow+workflowID+"/", limit)
}

// PendingCount returns the count of pending notifications.
// Uses a prefix-iterator count to avoid allocating a full ID slice.
func (s *Store) PendingCount() (int, error) {
	return s.countByPrefix(pfxByStatus + string(StatusPending) + "/")
}

// countByPrefix counts how many index keys exist under a prefix
// without loading their values into memory.
func (s *Store) countByPrefix(prefix string) (int, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: keyUpperBound([]byte(prefix)),
	})
	if err != nil {
		return 0, fmt.Errorf("notification: iter: %w", err)
	}
	defer iter.Close()
	n := 0
	for iter.First(); iter.Valid(); iter.Next() {
		n++
	}
	return n, iter.Error()
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
	// Use a single snapshot for all reads so we open one consistent reader
	// instead of N separate point-lookup handles.
	snap := s.db.NewSnapshot()
	defer snap.Close()
	b := s.db.NewBatch()
	defer b.Close()
	for _, id := range ids {
		data, closer, err := snap.Get([]byte(pfxByID + id))
		if err != nil {
			slog.Warn("notification: expire run: failed to read notification", "id", id, "err", err)
			continue
		}
		var n Notification
		unmarshalErr := json.Unmarshal(data, &n)
		closer.Close()
		if unmarshalErr != nil {
			slog.Warn("notification: expire run: corrupt record, skipping", "id", id, "err", unmarshalErr)
			continue
		}
		n.ExpiresAt = &now
		n.UpdatedAt = now
		updated, err := json.Marshal(&n)
		if err != nil {
			slog.Warn("notification: expire run: failed to marshal updated notification", "id", id, "err", err)
			continue
		}
		b.Set([]byte(pfxByID+id), updated, nil)
	}
	return b.Commit(pebble.Sync)
}

// listByPrefix does a prefix scan on index keys, loads canonical records,
// and returns them newest-first (IDs are time-sortable ascending).
func (s *Store) listByPrefix(prefix string) ([]*Notification, error) {
	return s.listByPrefixN(prefix, 0)
}

// listByPrefixN is like listByPrefix but stops after collecting limit IDs.
// If limit <= 0, all matching records are returned.
func (s *Store) listByPrefixN(prefix string, limit int) ([]*Notification, error) {
	ids, err := s.scanIDsN(prefix, limit)
	if err != nil {
		return nil, err
	}
	// When limit==0: ids are ascending (oldest first); reverse for newest-first output.
	// When limit>0: ids are already descending (newest first) from reverse scan.
	if limit <= 0 {
		for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
			ids[i], ids[j] = ids[j], ids[i]
		}
	}
	out := make([]*Notification, 0, len(ids))
	for _, id := range ids {
		n, err := s.Get(id)
		if err != nil {
			slog.Warn("notification: skipping corrupt or missing record", "id", id, "err", err)
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// scanIDs returns all notification IDs under a prefix in ascending order.
func (s *Store) scanIDs(prefix string) ([]string, error) {
	return s.scanIDsN(prefix, 0)
}

// scanIDsN returns notification IDs under a prefix.
// When limit <= 0, scans ascending (oldest first) and returns all IDs.
// When limit > 0, scans descending (newest first) and returns at most limit IDs.
func (s *Store) scanIDsN(prefix string, limit int) ([]string, error) {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: keyUpperBound([]byte(prefix)),
	})
	if err != nil {
		return nil, fmt.Errorf("notification: iter: %w", err)
	}
	defer iter.Close()

	if limit <= 0 {
		// No limit: scan ascending (existing behavior preserved for scanIDs delegate).
		var ids []string
		for iter.First(); iter.Valid(); iter.Next() {
			ids = append(ids, string(iter.Value()))
		}
		return ids, iter.Error()
	}

	// With limit: scan descending so we collect the newest N IDs first.
	// listByPrefixN will NOT reverse this result, so output is newest-first.
	ids := make([]string, 0, limit)
	for ok := iter.Last(); ok && len(ids) < limit; ok = iter.Prev() {
		ids = append(ids, string(iter.Value()))
	}
	return ids, iter.Error()
}

// PruneExpired deletes all notifications whose ExpiresAt is set and is in the past.
// It scans all primary ID-keyed records in batches of up to 100, deleting expired
// ones without holding a write lock across the entire scan. Respects ctx.Done().
// Returns the count of pruned notifications.
func (s *Store) PruneExpired(ctx context.Context) (int, error) {
	const batchSize = 100
	now := time.Now().UTC()
	pruned := 0

	// Collect all notification IDs by scanning the primary prefix.
	// Unlike the index prefixes (where value == ID), the pfxByID prefix stores
	// JSON as the value; the ID is the key suffix after the prefix.
	pfxBytes := []byte(pfxByID)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: pfxBytes,
		UpperBound: keyUpperBound(pfxBytes),
	})
	if err != nil {
		return 0, fmt.Errorf("notification: prune iter: %w", err)
	}
	var allIDs []string
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		id := key[len(pfxByID):]
		allIDs = append(allIDs, id)
	}
	if iterErr := iter.Error(); iterErr != nil {
		iter.Close()
		return 0, fmt.Errorf("notification: prune scan: %w", iterErr)
	}
	iter.Close()

	// Process in batches to avoid holding a write lock across the entire scan.
	for len(allIDs) > 0 {
		// Check for cancellation at the start of each batch.
		select {
		case <-ctx.Done():
			return pruned, ctx.Err()
		default:
		}

		end := batchSize
		if end > len(allIDs) {
			end = len(allIDs)
		}
		batch := allIDs[:end]
		allIDs = allIDs[end:]

		snap := s.db.NewSnapshot()
		type expiredEntry struct {
			n *Notification
		}
		toDelete := make([]expiredEntry, 0, len(batch))

		for _, id := range batch {
			data, closer, err := snap.Get([]byte(pfxByID + id))
			if err != nil {
				slog.Warn("notification: prune expired: failed to read notification", "id", id, "err", err)
				continue
			}
			var n Notification
			unmarshalErr := json.Unmarshal(data, &n)
			closer.Close()
			if unmarshalErr != nil {
				slog.Warn("notification: prune expired: corrupt record, skipping", "id", id, "err", unmarshalErr)
				continue
			}
			if n.ExpiresAt != nil && !n.ExpiresAt.IsZero() && n.ExpiresAt.Before(now) {
				toDelete = append(toDelete, expiredEntry{n: &n})
			}
		}
		snap.Close()

		if len(toDelete) == 0 {
			continue
		}

		// Delete all index keys for each expired notification in one atomic batch.
		wb := s.db.NewBatch()
		for _, e := range toDelete {
			n := e.n
			wb.Delete([]byte(pfxByID+n.ID), nil)
			wb.Delete([]byte(pfxByStatus+string(n.Status)+"/"+n.ID), nil)
			wb.Delete([]byte(pfxByRoutine+n.RoutineID+"/"+n.ID), nil)
			wb.Delete([]byte(pfxByRun+n.RunID+"/"+n.ID), nil)
			if n.WorkflowID != "" {
				wb.Delete([]byte(pfxByWorkflow+n.WorkflowID+"/"+n.ID), nil)
			}
		}
		if err := wb.Commit(pebble.Sync); err != nil {
			wb.Close()
			return pruned, fmt.Errorf("notification: prune delete batch: %w", err)
		}
		wb.Close()
		pruned += len(toDelete)
	}

	return pruned, nil
}

// StartPruner launches a background goroutine that calls PruneExpired on the
// given interval until ctx is cancelled. Log output is written at INFO level.
// The goroutine exits cleanly when ctx is done.
func (s *Store) StartPruner(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := s.PruneExpired(ctx)
				if err != nil && ctx.Err() == nil {
					slog.Warn("notification: prune expired failed", "err", err)
				} else if n > 0 {
					slog.Info("notification: pruned expired notifications", "count", n)
				}
			}
		}
	}()
}

// Compile-time assertion: *Store must satisfy StoreInterface.
var _ StoreInterface = (*Store)(nil)

func setIndexKeys(b *pebble.Batch, n *Notification) {
	b.Set([]byte(pfxByStatus+string(n.Status)+"/"+n.ID), []byte(n.ID), nil)
	b.Set([]byte(pfxByRoutine+n.RoutineID+"/"+n.ID), []byte(n.ID), nil)
	b.Set([]byte(pfxByRun+n.RunID+"/"+n.ID), []byte(n.ID), nil)
	if n.WorkflowID != "" {
		b.Set([]byte(pfxByWorkflow+n.WorkflowID+"/"+n.ID), []byte(n.ID), nil)
	}
}

func deleteIndexKeys(b *pebble.Batch, n *Notification) {
	b.Delete([]byte(pfxByStatus+string(n.Status)+"/"+n.ID), nil)
	b.Delete([]byte(pfxByRoutine+n.RoutineID+"/"+n.ID), nil)
	b.Delete([]byte(pfxByRun+n.RunID+"/"+n.ID), nil)
	if n.WorkflowID != "" {
		b.Delete([]byte(pfxByWorkflow+n.WorkflowID+"/"+n.ID), nil)
	}
}

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
