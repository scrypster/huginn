package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteWorkflowRunStore implements WorkflowRunStoreInterface backed by SQLite.
type SQLiteWorkflowRunStore struct {
	db *sqlitedb.DB
}

// NewSQLiteWorkflowRunStore creates a new SQLiteWorkflowRunStore.
func NewSQLiteWorkflowRunStore(db *sqlitedb.DB) *SQLiteWorkflowRunStore {
	return &SQLiteWorkflowRunStore{db: db}
}

// Compile-time assertion.
var _ WorkflowRunStoreInterface = (*SQLiteWorkflowRunStore)(nil)

// Append inserts a WorkflowRun into the workflow_runs table.
// Uses INSERT OR IGNORE so re-inserting the same run ID is a no-op.
func (s *SQLiteWorkflowRunStore) Append(workflowID string, run *WorkflowRun) error {
	stepsJSON := []byte("[]")
	if run.Steps != nil {
		var err error
		stepsJSON, err = json.Marshal(run.Steps)
		if err != nil {
			return fmt.Errorf("sqlite run store: marshal steps: %w", err)
		}
	}

	startedAt := run.StartedAt.UTC().Format(time.RFC3339)

	var completedAt *string
	if run.CompletedAt != nil {
		s := run.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &s
	}

	_, err := s.db.Write().Exec(`
		INSERT OR IGNORE INTO workflow_runs
			(id, workflow_id, status, steps, error, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		workflowID,
		string(run.Status),
		string(stepsJSON),
		run.Error,
		startedAt,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite run store: append %q: %w", run.ID, err)
	}
	return nil
}

// Get returns a single WorkflowRun by workflow ID and run ID.
// Returns (nil, nil) when no matching run is found.
func (s *SQLiteWorkflowRunStore) Get(workflowID, runID string) (*WorkflowRun, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, workflow_id, status, steps, error, started_at, completed_at
		FROM workflow_runs
		WHERE workflow_id = ? AND id = ?
		LIMIT 1`,
		workflowID, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite run store: get %q/%q: %w", workflowID, runID, err)
	}
	defer rows.Close()

	runs, err := scanWorkflowRuns(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite run store: scan %q/%q: %w", workflowID, runID, err)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return runs[0], nil
}

// List returns the n most recent WorkflowRuns for workflowID, newest first.
// Returns an empty slice (not nil) when no runs exist.
func (s *SQLiteWorkflowRunStore) List(workflowID string, n int) ([]*WorkflowRun, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, workflow_id, status, steps, error, started_at, completed_at
		FROM workflow_runs
		WHERE workflow_id = ?
		ORDER BY started_at DESC
		LIMIT ?`,
		workflowID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite run store: list %q: %w", workflowID, err)
	}
	defer rows.Close()

	runs, err := scanWorkflowRuns(rows)
	if err != nil {
		return nil, fmt.Errorf("sqlite run store: scan %q: %w", workflowID, err)
	}
	return runs, nil
}

// scanWorkflowRuns reads all WorkflowRun records from sql.Rows.
func scanWorkflowRuns(rows *sql.Rows) ([]*WorkflowRun, error) {
	var out []*WorkflowRun
	for rows.Next() {
		var run WorkflowRun
		var statusStr, stepsJSON, startedAtStr string
		var completedAtStr *string

		if err := rows.Scan(
			&run.ID,
			&run.WorkflowID,
			&statusStr,
			&stepsJSON,
			&run.Error,
			&startedAtStr,
			&completedAtStr,
		); err != nil {
			return nil, err
		}

		run.Status = WorkflowRunStatus(statusStr)

		if err := json.Unmarshal([]byte(stepsJSON), &run.Steps); err != nil {
			run.Steps = nil
		}

		if t, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			run.StartedAt = t.UTC()
		}

		if completedAtStr != nil {
			if t, err := time.Parse(time.RFC3339, *completedAtStr); err == nil {
				tc := t.UTC()
				run.CompletedAt = &tc
			}
		}

		out = append(out, &run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []*WorkflowRun{}
	}
	return out, nil
}
