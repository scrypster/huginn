// internal/scheduler/delivery_queue_store.go
package scheduler

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// DeliveryQueueStore provides CRUD access to the delivery_queue and
// endpoint_health SQLite tables.
type DeliveryQueueStore struct {
	db *sqlitedb.DB
}

// NewDeliveryQueueStore wraps an existing sqlitedb.DB connection.
func NewDeliveryQueueStore(db *sqlitedb.DB) *DeliveryQueueStore {
	return &DeliveryQueueStore{db: db}
}

// Insert writes a new queue entry. Does NOT supersede existing rows.
// Use SupersedeAndInsert for the normal enqueue path.
func (s *DeliveryQueueStore) Insert(e DeliveryQueueEntry) error {
	_, err := s.db.Write().Exec(`
        INSERT INTO delivery_queue
            (id, workflow_id, run_id, endpoint, channel, payload, status,
             attempt_count, max_attempts, retry_window_s, next_retry_at,
             created_at, last_attempt_at, last_error)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.WorkflowID, e.RunID, e.Endpoint, e.Channel, e.Payload, e.Status,
		e.AttemptCount, e.MaxAttempts, e.RetryWindowS,
		e.NextRetryAt.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		nullTime(e.LastAttemptAt),
		e.LastError,
	)
	return err
}

// SupersedeAndInsert marks any existing pending/retrying row for the same
// (workflow_id, endpoint) as superseded, then inserts the new entry.
func (s *DeliveryQueueStore) SupersedeAndInsert(e DeliveryQueueEntry) error {
	tx, err := s.db.Write().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	_, err = tx.Exec(`
        UPDATE delivery_queue SET status = 'superseded'
         WHERE workflow_id = ? AND endpoint = ? AND status IN ('pending','retrying')`,
		e.WorkflowID, e.Endpoint)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
        INSERT INTO delivery_queue
            (id, workflow_id, run_id, endpoint, channel, payload, status,
             attempt_count, max_attempts, retry_window_s, next_retry_at,
             created_at, last_attempt_at, last_error)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.WorkflowID, e.RunID, e.Endpoint, e.Channel, e.Payload, e.Status,
		e.AttemptCount, e.MaxAttempts, e.RetryWindowS,
		e.NextRetryAt.UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		nullTime(e.LastAttemptAt),
		e.LastError,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Get retrieves a single entry by ID.
func (s *DeliveryQueueStore) Get(id string) (DeliveryQueueEntry, error) {
	row := s.db.Read().QueryRow(`SELECT `+deliveryQueueCols+` FROM delivery_queue WHERE id = ?`, id)
	return scanDeliveryQueueEntry(row)
}

// ListDue returns entries with status pending/retrying where next_retry_at <= now,
// up to limit rows, ordered by next_retry_at ascending.
func (s *DeliveryQueueStore) ListDue(now time.Time, limit int) ([]DeliveryQueueEntry, error) {
	rows, err := s.db.Read().Query(`
        SELECT `+deliveryQueueCols+` FROM delivery_queue
         WHERE status IN ('pending','retrying') AND next_retry_at <= ?
         ORDER BY next_retry_at ASC LIMIT ?`,
		now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveryQueueEntries(rows)
}

// ListActionable returns entries with status=failed, up to limit rows.
func (s *DeliveryQueueStore) ListActionable(limit int) ([]DeliveryQueueEntry, error) {
	rows, err := s.db.Read().Query(`
        SELECT `+deliveryQueueCols+` FROM delivery_queue
         WHERE status = 'failed'
         ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeliveryQueueEntries(rows)
}

// UpdateAttempt updates status, attempt_count, last_error, and next_retry_at
// for an entry after an attempt.
func (s *DeliveryQueueStore) UpdateAttempt(id, status string, attemptCount int, lastError string, nextRetryAt *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var nextStr *string
	if nextRetryAt != nil {
		v := nextRetryAt.UTC().Format(time.RFC3339)
		nextStr = &v
	}
	_, err := s.db.Write().Exec(`
        UPDATE delivery_queue
           SET status = ?, attempt_count = ?, last_error = ?,
               next_retry_at = COALESCE(?, next_retry_at), last_attempt_at = ?
         WHERE id = ?`,
		status, attemptCount, lastError, nextStr, now, id)
	return err
}

// BadgeCount returns the count of distinct (workflow_id, endpoint) pairs
// with status=failed.
func (s *DeliveryQueueStore) BadgeCount() (int, error) {
	var count int
	err := s.db.Read().QueryRow(`
        SELECT COUNT(DISTINCT workflow_id || '|' || endpoint)
          FROM delivery_queue WHERE status = 'failed'`).Scan(&count)
	return count, err
}

// MarkDelivered sets status=delivered for an entry.
func (s *DeliveryQueueStore) MarkDelivered(id string) error {
	_, err := s.db.Write().Exec(`
        UPDATE delivery_queue
           SET status = 'delivered', last_error = '', last_attempt_at = ?
         WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// Dismiss deletes a failed entry from the queue (removes from badge count).
func (s *DeliveryQueueStore) Dismiss(id string) error {
	_, err := s.db.Write().Exec(`DELETE FROM delivery_queue WHERE id = ?`, id)
	return err
}

// GetHealth retrieves circuit breaker state for (workflowID, endpoint).
// Returns a zero-value EndpointHealth with CircuitState=closed if not found.
func (s *DeliveryQueueStore) GetHealth(workflowID, endpoint string) (EndpointHealth, error) {
	var h EndpointHealth
	var openedAt, lastProbeAt sql.NullString
	err := s.db.Read().QueryRow(`
        SELECT workflow_id, endpoint, consecutive_failures, circuit_state, opened_at, last_probe_at
          FROM endpoint_health WHERE workflow_id = ? AND endpoint = ?`,
		workflowID, endpoint).Scan(
		&h.WorkflowID, &h.Endpoint, &h.ConsecutiveFailures, &h.CircuitState,
		&openedAt, &lastProbeAt)
	if err == sql.ErrNoRows {
		return EndpointHealth{WorkflowID: workflowID, Endpoint: endpoint, CircuitState: "closed"}, nil
	}
	if err != nil {
		return h, err
	}
	if openedAt.Valid {
		t, _ := time.Parse(time.RFC3339, openedAt.String)
		h.OpenedAt = &t
	}
	if lastProbeAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastProbeAt.String)
		h.LastProbeAt = &t
	}
	return h, nil
}

// UpsertHealth writes circuit breaker state for (workflowID, endpoint).
func (s *DeliveryQueueStore) UpsertHealth(h EndpointHealth) error {
	_, err := s.db.Write().Exec(`
        INSERT INTO endpoint_health (workflow_id, endpoint, consecutive_failures, circuit_state, opened_at, last_probe_at)
        VALUES (?,?,?,?,?,?)
        ON CONFLICT(workflow_id, endpoint) DO UPDATE SET
            consecutive_failures = excluded.consecutive_failures,
            circuit_state        = excluded.circuit_state,
            opened_at            = excluded.opened_at,
            last_probe_at        = excluded.last_probe_at`,
		h.WorkflowID, h.Endpoint, h.ConsecutiveFailures, h.CircuitState,
		nullTime(h.OpenedAt), nullTime(h.LastProbeAt))
	return err
}

// --- helpers ---

const deliveryQueueCols = `id, workflow_id, run_id, endpoint, channel, payload, status,
    attempt_count, max_attempts, retry_window_s, next_retry_at,
    created_at, last_attempt_at, last_error`

func scanDeliveryQueueEntry(row *sql.Row) (DeliveryQueueEntry, error) {
	var e DeliveryQueueEntry
	var nextRetry, created string
	var lastAttempt sql.NullString
	err := row.Scan(
		&e.ID, &e.WorkflowID, &e.RunID, &e.Endpoint, &e.Channel, &e.Payload,
		&e.Status, &e.AttemptCount, &e.MaxAttempts, &e.RetryWindowS,
		&nextRetry, &created, &lastAttempt, &e.LastError)
	if err != nil {
		return e, fmt.Errorf("scan delivery queue entry: %w", err)
	}
	e.NextRetryAt, _ = time.Parse(time.RFC3339, nextRetry)
	e.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if lastAttempt.Valid {
		t, _ := time.Parse(time.RFC3339, lastAttempt.String)
		e.LastAttemptAt = &t
	}
	return e, nil
}

func scanDeliveryQueueEntries(rows *sql.Rows) ([]DeliveryQueueEntry, error) {
	var out []DeliveryQueueEntry
	for rows.Next() {
		var e DeliveryQueueEntry
		var nextRetry, created string
		var lastAttempt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.WorkflowID, &e.RunID, &e.Endpoint, &e.Channel, &e.Payload,
			&e.Status, &e.AttemptCount, &e.MaxAttempts, &e.RetryWindowS,
			&nextRetry, &created, &lastAttempt, &e.LastError); err != nil {
			return nil, err
		}
		e.NextRetryAt, _ = time.Parse(time.RFC3339, nextRetry)
		e.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if lastAttempt.Valid {
			t, _ := time.Parse(time.RFC3339, lastAttempt.String)
			e.LastAttemptAt = &t
		}
		out = append(out, e)
	}
	if out == nil {
		out = []DeliveryQueueEntry{}
	}
	return out, rows.Err()
}

func nullTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
