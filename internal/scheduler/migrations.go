package scheduler

import (
	"database/sql"
	"strings"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Migrations returns the scheduler's pending DDL migrations. Each runs inside
// a transaction. The framework records applied migrations in `_migrations`
// so they execute exactly once per database file.
func Migrations() []sqlitedb.Migration {
	return []sqlitedb.Migration{
		{
			Name: "scheduler_v2_workflow_runs_add_replay_columns",
			Up:   migrateWorkflowRunsV2AddReplayColumns,
		},
	}
}

// migrateWorkflowRunsV2AddReplayColumns adds the trigger_inputs and
// workflow_snapshot columns introduced in Phase 6 (run analytics). Both are
// JSON-typed TEXT columns with default '{}' so existing rows stay readable.
//
// SQLite supports ADD COLUMN but not IF NOT EXISTS for it, so we tolerate
// "duplicate column" errors — that's the case where a fresh-DB install
// already created the columns via the base schema and the migration runs
// only to record the version marker.
func migrateWorkflowRunsV2AddReplayColumns(tx *sql.Tx) error {
	addCol := func(name, ddl string) error {
		_, err := tx.Exec(ddl)
		if err == nil {
			return nil
		}
		// SQLite returns: "duplicate column name: trigger_inputs". Swallow
		// that exact case so the migration is idempotent on fresh installs
		// where the base schema already provisioned the column.
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	if err := addCol("trigger_inputs",
		`ALTER TABLE workflow_runs ADD COLUMN trigger_inputs TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err := addCol("workflow_snapshot",
		`ALTER TABLE workflow_runs ADD COLUMN workflow_snapshot TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	return nil
}

// migrateWorkflowRunsV1 recreates the workflow_runs table with 'partial' added
// to the status CHECK constraint.
//
// The original table was created with:
//
//	CHECK (status IN ('running', 'complete', 'failed', 'cancelled'))
//
// SQLite does not support ALTER TABLE DROP CONSTRAINT or ALTER TABLE ALTER COLUMN,
// so the only way to change a CHECK constraint on an existing table is to:
//  1. Rename the old table.
//  2. Create the new table with the updated constraint.
//  3. Copy all rows from the old table into the new table.
//  4. Drop the old table.
//
// This migration runs inside a transaction so it is atomic: either all steps
// succeed or the entire migration is rolled back and retried on next startup.
//
// The migration is idempotent via the _migrations check in sqlitedb.Migrate —
// if "scheduler_v1_workflow_runs_add_partial_status" is already recorded, this
// function is never called.
func migrateWorkflowRunsV1(tx *sql.Tx) error {
	stmts := []string{
		// Step 1: rename the old table.
		`ALTER TABLE workflow_runs RENAME TO workflow_runs_old`,
		// Step 2: create the new table with 'partial' in the status CHECK.
		`CREATE TABLE workflow_runs (
			id              TEXT    NOT NULL PRIMARY KEY,
			tenant_id       TEXT    NOT NULL DEFAULT '',
			workflow_id     TEXT    NOT NULL,
			status          TEXT    NOT NULL DEFAULT 'running'
			                    CHECK (status IN ('running', 'complete', 'partial', 'failed', 'cancelled')),
			steps           TEXT    NOT NULL DEFAULT '[]',
			error           TEXT    NOT NULL DEFAULT '',
			started_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
			completed_at    TEXT
		)`,
		// Step 3: copy all rows.
		`INSERT INTO workflow_runs
			(id, tenant_id, workflow_id, status, steps, error, started_at, completed_at)
		SELECT id, tenant_id, workflow_id, status, steps, error, started_at, completed_at
		FROM workflow_runs_old`,
		// Step 4: recreate the indexes.
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow
			ON workflow_runs (tenant_id, workflow_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_runs_status
			ON workflow_runs (tenant_id, status)
			WHERE status = 'running'`,
		// Step 5: drop the old table.
		`DROP TABLE workflow_runs_old`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
