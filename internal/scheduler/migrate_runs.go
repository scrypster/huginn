package scheduler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

const runsMigrationName = "M4_workflow_runs"
const runsMigrateBatchSize = 1000

// MigrateFromJSONL reads per-workflow JSONL files from baseDir, inserts all
// WorkflowRun records into SQLite in batches, records the migration in
// _migrations, and renames baseDir to baseDir+".bak".
//
// Idempotent: if M4_workflow_runs is already in _migrations, returns nil
// immediately.
// If baseDir does not exist, records the migration with 0 rows and returns nil.
func MigrateFromJSONL(baseDir string, db *sqlitedb.DB) error {
	// 1. Idempotency check.
	done, err := runsMigrationDone(db, runsMigrationName)
	if err != nil {
		return fmt.Errorf("migrate workflow runs: check: %w", err)
	}
	if done {
		return nil
	}

	// 2. Missing dir → treat as fresh install with no historical runs.
	if _, err := os.Stat(baseDir); errors.Is(err, os.ErrNotExist) {
		return runsRecordMigration(db, runsMigrationName, 0, baseDir)
	}

	// 3. Walk all *.jsonl files in baseDir.
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("migrate workflow runs: readdir %s: %w", baseDir, err)
	}

	totalRows := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		// Derive workflowID from filename: strip ".jsonl" suffix.
		workflowID := strings.TrimSuffix(name, ".jsonl")
		filePath := filepath.Join(baseDir, name)

		n, err := migrateRunsFile(filePath, workflowID, db)
		if err != nil {
			return fmt.Errorf("migrate workflow runs: file %s: %w", filePath, err)
		}
		totalRows += n
	}

	// 4. Record migration in _migrations.
	if err := runsRecordMigration(db, runsMigrationName, totalRows, baseDir); err != nil {
		return fmt.Errorf("migrate workflow runs: record migration: %w", err)
	}

	// 5. Rename baseDir → baseDir+".bak" (non-fatal).
	bakDir := baseDir + ".bak"
	if err := os.Rename(baseDir, bakDir); err != nil {
		fmt.Fprintf(os.Stderr, "migrate workflow runs: rename %s: %v\n", baseDir, err)
	}

	return nil
}

// migrateRunsFile reads all WorkflowRun records from a single JSONL file and
// inserts them into workflow_runs in batches of runsMigrateBatchSize.
// Returns the number of rows successfully inserted.
func migrateRunsFile(filePath, workflowID string, db *sqlitedb.DB) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var batch []*WorkflowRun
	total := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := insertRunsBatch(batch, workflowID, db)
		if err != nil {
			return err
		}
		total += n
		batch = batch[:0]
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var run WorkflowRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			// Skip malformed lines — don't abort the entire migration.
			fmt.Fprintf(os.Stderr, "migrate workflow runs: skip malformed line in %s: %v\n", filePath, err)
			continue
		}
		batch = append(batch, &run)
		if len(batch) >= runsMigrateBatchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return total, fmt.Errorf("scan: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

// insertRunsBatch inserts a slice of WorkflowRun records for a given
// workflowID into workflow_runs within a single transaction.
// Uses INSERT OR IGNORE so duplicate IDs are silently skipped.
// Returns the number of rows processed (attempted, not necessarily inserted).
func insertRunsBatch(runs []*WorkflowRun, workflowID string, db *sqlitedb.DB) (int, error) {
	tx, err := db.Write().Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO workflow_runs
			(id, workflow_id, status, steps, error, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, run := range runs {
		stepsJSON := []byte("[]")
		if run.Steps != nil {
			stepsJSON, err = json.Marshal(run.Steps)
			if err != nil {
				tx.Rollback()
				return 0, fmt.Errorf("marshal steps for run %q: %w", run.ID, err)
			}
		}

		startedAt := run.StartedAt.UTC().Format(time.RFC3339)

		var completedAt *string
		if run.CompletedAt != nil {
			s := run.CompletedAt.UTC().Format(time.RFC3339)
			completedAt = &s
		}

		// Use the workflowID from the filename as the authoritative value;
		// fall back to the field on the struct if the filename-derived one is empty.
		wfID := workflowID
		if wfID == "" {
			wfID = run.WorkflowID
		}

		if _, err := stmt.Exec(
			run.ID,
			wfID,
			string(run.Status),
			string(stepsJSON),
			run.Error,
			startedAt,
			completedAt,
		); err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("insert run %q: %w", run.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(runs), nil
}

// runsMigrationDone reports whether name already exists in _migrations.
func runsMigrationDone(db *sqlitedb.DB, name string) (bool, error) {
	var count int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

// runsRecordMigration writes a row to _migrations.
func runsRecordMigration(db *sqlitedb.DB, name string, count int, sourcePath string) error {
	_, err := db.Write().Exec(
		`INSERT OR IGNORE INTO _migrations (name, record_count, source_path) VALUES (?, ?, ?)`,
		name, count, sourcePath,
	)
	return err
}
