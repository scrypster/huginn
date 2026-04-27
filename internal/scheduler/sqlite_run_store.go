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

	// Phase 6: trigger_inputs and workflow_snapshot. Empty values default to
	// '{}' so SELECT can always Scan into a string. We tolerate nil maps and
	// nil snapshots — older runs without these fields stay readable.
	triggerJSON := []byte("{}")
	if len(run.TriggerInputs) > 0 {
		var err error
		triggerJSON, err = json.Marshal(run.TriggerInputs)
		if err != nil {
			return fmt.Errorf("sqlite run store: marshal trigger_inputs: %w", err)
		}
	}
	snapshotJSON := []byte("{}")
	if run.WorkflowSnapshot != nil {
		var err error
		snapshotJSON, err = json.Marshal(run.WorkflowSnapshot)
		if err != nil {
			return fmt.Errorf("sqlite run store: marshal workflow_snapshot: %w", err)
		}
	}

	_, err := s.db.Write().Exec(`
		INSERT OR IGNORE INTO workflow_runs
			(id, workflow_id, status, steps, error, started_at, completed_at,
			 trigger_inputs, workflow_snapshot)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		workflowID,
		string(run.Status),
		string(stepsJSON),
		run.Error,
		startedAt,
		completedAt,
		string(triggerJSON),
		string(snapshotJSON),
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
		SELECT id, workflow_id, status, steps, error, started_at, completed_at,
		       trigger_inputs, workflow_snapshot
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
		SELECT id, workflow_id, status, steps, error, started_at, completed_at,
		       trigger_inputs, workflow_snapshot
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
		var triggerJSON, snapshotJSON string

		if err := rows.Scan(
			&run.ID,
			&run.WorkflowID,
			&statusStr,
			&stepsJSON,
			&run.Error,
			&startedAtStr,
			&completedAtStr,
			&triggerJSON,
			&snapshotJSON,
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

		// Phase 6: best-effort decode of trigger_inputs / workflow_snapshot.
		// Old rows default to "{}" via the migration so Unmarshal succeeds
		// and the resulting empty map is normalised back to nil to match
		// the in-memory shape produced by a fresh run with no inputs.
		if triggerJSON != "" {
			var inputs map[string]string
			if err := json.Unmarshal([]byte(triggerJSON), &inputs); err == nil && len(inputs) > 0 {
				run.TriggerInputs = inputs
			}
		}
		if snapshotJSON != "" && snapshotJSON != "{}" {
			var snap Workflow
			if err := json.Unmarshal([]byte(snapshotJSON), &snap); err == nil && snap.ID != "" {
				run.WorkflowSnapshot = &snap
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
