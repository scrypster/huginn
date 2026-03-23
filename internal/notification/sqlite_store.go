package notification

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteNotificationStore implements StoreInterface backed by a SQLite database.
type SQLiteNotificationStore struct {
	db *sqlitedb.DB
}

// Compile-time assertion.
var _ StoreInterface = (*SQLiteNotificationStore)(nil)

// NewSQLiteNotificationStore creates a new SQLiteNotificationStore.
func NewSQLiteNotificationStore(db *sqlitedb.DB) *SQLiteNotificationStore {
	return &SQLiteNotificationStore{db: db}
}

// Put writes a Notification and all its index keys atomically.
func (s *SQLiteNotificationStore) Put(n *Notification) error {
	actions := n.ProposedActions
	if actions == nil {
		actions = []ProposedAction{}
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("notification: marshal proposed_actions: %w", err)
	}

	deliveries := n.Deliveries
	if deliveries == nil {
		deliveries = []DeliveryRecord{}
	}
	deliveriesJSON, err := json.Marshal(deliveries)
	if err != nil {
		return fmt.Errorf("notification: marshal deliveries: %w", err)
	}

	createdAt := n.CreatedAt.UTC().Format(time.RFC3339)
	if n.CreatedAt.IsZero() {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	updatedAt := n.UpdatedAt.UTC().Format(time.RFC3339)
	if n.UpdatedAt.IsZero() {
		updatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	var expiresAt *string
	if n.ExpiresAt != nil && !n.ExpiresAt.IsZero() {
		s := n.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}

	severity := string(n.Severity)
	if severity == "" {
		severity = string(SeverityInfo)
	}
	status := string(n.Status)
	if status == "" {
		status = string(StatusPending)
	}

	_, err = s.db.Write().Exec(`
		INSERT OR REPLACE INTO notifications
			(id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
			 summary, detail, severity, status, session_id, proposed_actions,
			 step_position, step_name, deliveries,
			 created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.RoutineID, n.RunID, n.SatelliteID, n.WorkflowID, n.WorkflowRunID,
		n.Summary, n.Detail, severity, status, n.SessionID, string(actionsJSON),
		n.StepPosition, n.StepName, string(deliveriesJSON),
		createdAt, updatedAt, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("notification: put %q: %w", n.ID, err)
	}
	return nil
}

// Get retrieves a single Notification by ID.
func (s *SQLiteNotificationStore) Get(id string) (*Notification, error) {
	row := s.db.Read().QueryRow(`
		SELECT id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
		       summary, detail, severity, status, session_id, proposed_actions,
		       step_position, step_name, deliveries,
		       created_at, updated_at, expires_at
		FROM notifications WHERE id = ?`, id)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("notification: get %q: not found", id)
		}
		return nil, fmt.Errorf("notification: get %q: %w", id, err)
	}
	return n, nil
}

// Transition moves a Notification to newStatus, updating updated_at.
func (s *SQLiteNotificationStore) Transition(id string, newStatus Status) error {
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Write().Exec(`
		UPDATE notifications SET status = ?, updated_at = ? WHERE id = ?`,
		string(newStatus), updatedAt, id)
	if err != nil {
		return fmt.Errorf("notification: transition %q: %w", id, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("notification: transition %q rows affected: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("notification: transition %q: not found", id)
	}
	return nil
}

// ListPending returns all pending notifications, newest first.
func (s *SQLiteNotificationStore) ListPending() ([]*Notification, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
		       summary, detail, severity, status, session_id, proposed_actions,
		       step_position, step_name, deliveries,
		       created_at, updated_at, expires_at
		FROM notifications WHERE status = ? ORDER BY id DESC`,
		string(StatusPending))
	if err != nil {
		return nil, fmt.Errorf("notification: list pending: %w", err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

// ListByRoutine returns all notifications for a routine, newest first.
func (s *SQLiteNotificationStore) ListByRoutine(routineID string) ([]*Notification, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
		       summary, detail, severity, status, session_id, proposed_actions,
		       step_position, step_name, deliveries,
		       created_at, updated_at, expires_at
		FROM notifications WHERE routine_id = ? ORDER BY id DESC`, routineID)
	if err != nil {
		return nil, fmt.Errorf("notification: list by routine %q: %w", routineID, err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

// ListByWorkflow returns all notifications produced by a workflow, newest first.
func (s *SQLiteNotificationStore) ListByWorkflow(workflowID string) ([]*Notification, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
		       summary, detail, severity, status, session_id, proposed_actions,
		       step_position, step_name, deliveries,
		       created_at, updated_at, expires_at
		FROM notifications WHERE workflow_id = ? ORDER BY id DESC`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("notification: list by workflow %q: %w", workflowID, err)
	}
	defer rows.Close()
	return scanNotifications(rows)
}

// PendingCount returns the count of pending notifications.
func (s *SQLiteNotificationStore) PendingCount() (int, error) {
	var count int
	err := s.db.Read().QueryRow(`
		SELECT COUNT(*) FROM notifications WHERE status = ?`,
		string(StatusPending)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notification: pending count: %w", err)
	}
	return count, nil
}

// ExpireRun sets ExpiresAt = now for all notifications belonging to runID.
func (s *SQLiteNotificationStore) ExpireRun(runID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Write().Exec(`
		UPDATE notifications SET expires_at = ?, updated_at = ? WHERE run_id = ?`,
		now, now, runID)
	if err != nil {
		return fmt.Errorf("notification: expire run %q: %w", runID, err)
	}
	return nil
}

// scanNotification reads a single Notification from a sql.Row.
func scanNotification(row *sql.Row) (*Notification, error) {
	var n Notification
	var severityStr, statusStr, actionsJSON, deliveriesJSON, stepName string
	var createdAtStr, updatedAtStr string
	var expiresAtStr *string
	var stepPosition *int

	err := row.Scan(
		&n.ID, &n.RoutineID, &n.RunID, &n.SatelliteID,
		&n.WorkflowID, &n.WorkflowRunID,
		&n.Summary, &n.Detail,
		&severityStr, &statusStr,
		&n.SessionID, &actionsJSON,
		&stepPosition, &stepName, &deliveriesJSON,
		&createdAtStr, &updatedAtStr, &expiresAtStr,
	)
	if err != nil {
		return nil, err
	}

	return hydrateNotification(&n, severityStr, statusStr, actionsJSON, deliveriesJSON, stepPosition, stepName, createdAtStr, updatedAtStr, expiresAtStr)
}

// scanNotifications reads all Notifications from sql.Rows.
func scanNotifications(rows *sql.Rows) ([]*Notification, error) {
	var out []*Notification
	for rows.Next() {
		var n Notification
		var severityStr, statusStr, actionsJSON, deliveriesJSON, stepName string
		var createdAtStr, updatedAtStr string
		var expiresAtStr *string
		var stepPosition *int

		if err := rows.Scan(
			&n.ID, &n.RoutineID, &n.RunID, &n.SatelliteID,
			&n.WorkflowID, &n.WorkflowRunID,
			&n.Summary, &n.Detail,
			&severityStr, &statusStr,
			&n.SessionID, &actionsJSON,
			&stepPosition, &stepName, &deliveriesJSON,
			&createdAtStr, &updatedAtStr, &expiresAtStr,
		); err != nil {
			return nil, err
		}

		hydrated, err := hydrateNotification(&n, severityStr, statusStr, actionsJSON, deliveriesJSON, stepPosition, stepName, createdAtStr, updatedAtStr, expiresAtStr)
		if err != nil {
			return nil, err
		}
		out = append(out, hydrated)
	}
	return out, rows.Err()
}

// hydrateNotification fills in typed fields from raw string column values.
func hydrateNotification(n *Notification, severityStr, statusStr, actionsJSON, deliveriesJSON string, stepPosition *int, stepName, createdAtStr, updatedAtStr string, expiresAtStr *string) (*Notification, error) {
	n.Severity = Severity(severityStr)
	n.Status = Status(statusStr)
	n.StepPosition = stepPosition
	n.StepName = stepName

	if actionsJSON != "" && actionsJSON != "[]" {
		if err := json.Unmarshal([]byte(actionsJSON), &n.ProposedActions); err != nil {
			n.ProposedActions = nil
		}
	}

	if deliveriesJSON != "" && deliveriesJSON != "[]" {
		if err := json.Unmarshal([]byte(deliveriesJSON), &n.Deliveries); err != nil {
			n.Deliveries = nil
		}
	}

	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		n.CreatedAt = t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
		n.UpdatedAt = t.UTC()
	}

	if expiresAtStr != nil {
		if t, err := time.Parse(time.RFC3339, *expiresAtStr); err == nil {
			t = t.UTC()
			n.ExpiresAt = &t
		}
	}

	return n, nil
}
