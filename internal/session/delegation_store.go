// internal/session/delegation_store.go
package session

import (
	"database/sql"
	"fmt"
	"time"
)

// DelegationRecord represents a single agent delegation persisted in SQLite.
//
// StartedAt uses time.Time (not *time.Time) because the underlying delegations
// table has `started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
// which means the column can never be NULL without rebuilding the table in SQLite.
type DelegationRecord struct {
	ID        string
	SessionID string
	ThreadID  string
	FromAgent string
	ToAgent   string
	Task      string
	Status    string     // "pending" | "in_progress" | "completed" | "failed"
	Result    string
	CreatedAt time.Time
	StartedAt time.Time  // NOT nullable — see schema note above
	CompletedAt *time.Time
}

// DelegationStore defines persistence operations for agent delegations.
// SQLiteSessionStore implements this interface; it is accessed via type assertion
// so that non-SQLite stores remain compatible without stub methods.
type DelegationStore interface {
	// InsertDelegation persists a new delegation record.
	InsertDelegation(d DelegationRecord) error

	// GetDelegation retrieves a delegation by its primary key.
	GetDelegation(id string) (*DelegationRecord, error)

	// FindDelegationByThread looks up the delegation whose thread_id matches.
	// Returns sql.ErrNoRows (wrapped) when no matching record exists.
	// The UNIQUE index uq_delegations_thread guarantees at most one result.
	FindDelegationByThread(threadID string) (*DelegationRecord, error)

	// ListDelegationsBySession returns delegations for a session ordered by
	// created_at DESC. Both limit and offset must be non-negative; limit is
	// clamped to [1, 200] by callers before invocation.
	ListDelegationsBySession(sessionID string, limit, offset int) ([]DelegationRecord, error)

	// UpdateDelegationStatus atomically sets status, result, and optional
	// startedAt/completedAt timestamps for the delegation with the given id.
	// Pass nil for timestamp pointers to leave those columns unchanged.
	UpdateDelegationStatus(id, status, result string, startedAt *time.Time, completedAt *time.Time) error

	// ReconcileOrphanDelegations marks any delegation whose status is
	// "pending" or "in_progress" as "failed" with a standard reason string.
	// Called once at server startup so delegations orphaned by a crash or
	// restart don't remain stuck in a terminal-pending state indefinitely.
	ReconcileOrphanDelegations() error
}

// scanDelegation reads one row from rows into a DelegationRecord.
func scanDelegation(rows *sql.Rows) (*DelegationRecord, error) {
	var d DelegationRecord
	var createdAtStr, startedAtStr string
	var completedAtStr sql.NullString

	if err := rows.Scan(
		&d.ID, &d.SessionID, &d.ThreadID, &d.FromAgent, &d.ToAgent,
		&d.Task, &d.Status, &d.Result,
		&createdAtStr, &startedAtStr, &completedAtStr,
	); err != nil {
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
		d.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, startedAtStr); err == nil {
		d.StartedAt = t
	}
	if completedAtStr.Valid && completedAtStr.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, completedAtStr.String); err == nil {
			d.CompletedAt = &t
		}
	}
	return &d, nil
}

// InsertDelegation persists a new delegation record to the database.
func (s *SQLiteSessionStore) InsertDelegation(d DelegationRecord) error {
	db := s.db.Write()
	_, err := db.Exec(`
		INSERT INTO delegations
			(id, session_id, thread_id, from_agent, to_agent, task, status, result, created_at, started_at, completed_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.SessionID, d.ThreadID, d.FromAgent, d.ToAgent,
		d.Task, d.Status, d.Result,
		d.CreatedAt.UTC().Format(time.RFC3339Nano),
		d.StartedAt.UTC().Format(time.RFC3339Nano),
		nil, // completed_at starts NULL
	)
	if err != nil {
		return fmt.Errorf("session: InsertDelegation %s: %w", d.ID, err)
	}
	return nil
}

// GetDelegation retrieves a delegation by primary key.
func (s *SQLiteSessionStore) GetDelegation(id string) (*DelegationRecord, error) {
	db := s.db.Read()
	rows, err := db.Query(`
		SELECT id, session_id, thread_id, from_agent, to_agent, task, status, result,
		       created_at, started_at, completed_at
		FROM delegations WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("session: GetDelegation %s: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("session: GetDelegation %s: %w", id, err)
		}
		return nil, fmt.Errorf("session: GetDelegation %s: %w", id, sql.ErrNoRows)
	}
	return scanDelegation(rows)
}

// FindDelegationByThread retrieves the delegation whose thread_id equals the given value.
// The UNIQUE index uq_delegations_thread guarantees at most one match.
func (s *SQLiteSessionStore) FindDelegationByThread(threadID string) (*DelegationRecord, error) {
	db := s.db.Read()
	rows, err := db.Query(`
		SELECT id, session_id, thread_id, from_agent, to_agent, task, status, result,
		       created_at, started_at, completed_at
		FROM delegations WHERE thread_id = ?`, threadID)
	if err != nil {
		return nil, fmt.Errorf("session: FindDelegationByThread %s: %w", threadID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("session: FindDelegationByThread %s: %w", threadID, err)
		}
		return nil, fmt.Errorf("session: FindDelegationByThread %s: %w", threadID, sql.ErrNoRows)
	}
	return scanDelegation(rows)
}

// ListDelegationsBySession returns up to limit delegations for sessionID, newest first.
func (s *SQLiteSessionStore) ListDelegationsBySession(sessionID string, limit, offset int) ([]DelegationRecord, error) {
	db := s.db.Read()
	rows, err := db.Query(`
		SELECT id, session_id, thread_id, from_agent, to_agent, task, status, result,
		       created_at, started_at, completed_at
		FROM delegations
		WHERE session_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session: ListDelegationsBySession %s: %w", sessionID, err)
	}
	defer rows.Close()

	var out []DelegationRecord
	for rows.Next() {
		d, err := scanDelegation(rows)
		if err != nil {
			return nil, fmt.Errorf("session: ListDelegationsBySession scan: %w", err)
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: ListDelegationsBySession %s: %w", sessionID, err)
	}
	return out, nil
}

// UpdateDelegationStatus atomically updates status, result, and optional timestamps.
// Pass nil for startedAt or completedAt to skip updating those columns.
func (s *SQLiteSessionStore) UpdateDelegationStatus(id, status, result string, startedAt *time.Time, completedAt *time.Time) error {
	db := s.db.Write()

	var startedAtStr any
	if startedAt != nil {
		startedAtStr = startedAt.UTC().Format(time.RFC3339Nano)
	}
	var completedAtStr any
	if completedAt != nil {
		completedAtStr = completedAt.UTC().Format(time.RFC3339Nano)
	}

	_, err := db.Exec(`
		UPDATE delegations SET
			status       = ?,
			result       = ?,
			started_at   = COALESCE(?, started_at),
			completed_at = COALESCE(?, completed_at)
		WHERE id = ?`,
		status, result, startedAtStr, completedAtStr, id,
	)
	if err != nil {
		return fmt.Errorf("session: UpdateDelegationStatus %s: %w", id, err)
	}
	return nil
}

// ReconcileOrphanDelegations marks all non-terminal delegations as failed.
// Called once at server startup to clean up records interrupted by a crash.
func (s *SQLiteSessionStore) ReconcileOrphanDelegations() error {
	db := s.db.Write()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`
		UPDATE delegations SET
			status       = 'failed',
			result       = 'interrupted by server restart',
			completed_at = ?
		WHERE status IN ('pending', 'in_progress')`, now)
	if err != nil {
		return fmt.Errorf("session: ReconcileOrphanDelegations: %w", err)
	}
	return nil
}
