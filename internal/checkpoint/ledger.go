package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// ledger persists RunRecords in a SQLite database that is entirely
// independent of Huginn's main ThreadStore schema (internal/sqlitedb/schema).
// DECISION 9 in the design doc suggests extending the shared ThreadStore
// schema; this implementation deliberately keeps its own small schema in
// its own file instead — see the "ambiguity" note in the final report for
// why (avoids a schema-migration collision with the rest of this wave's
// concurrent work; the ledger is purely additive bookkeeping, not part of
// thread identity, so co-locating it isn't required for correctness).
const ledgerSchema = `
CREATE TABLE IF NOT EXISTS runs (
	thread_id      TEXT PRIMARY KEY,
	agent_id       TEXT NOT NULL DEFAULT '',
	task_summary   TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL,
	pre_snapshot   TEXT NOT NULL DEFAULT '',
	post_snapshot  TEXT NOT NULL DEFAULT '',
	touched_paths  TEXT NOT NULL DEFAULT '[]',
	pushed         INTEGER NOT NULL DEFAULT 0,
	pr_url         TEXT NOT NULL DEFAULT '',
	capture_error  TEXT NOT NULL DEFAULT '',
	ignored_at_begin TEXT NOT NULL DEFAULT '[]',
	ignored_touched  TEXT NOT NULL DEFAULT '[]',
	created_at     TEXT NOT NULL,
	completed_at   TEXT
);
CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at);
`

type ledger struct {
	db *sqlitedb.DB
}

func newLedger(path string) (*ledger, error) {
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: open ledger db: %w", err)
	}
	if _, err := db.Write().Exec(ledgerSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("checkpoint: migrate ledger schema: %w", err)
	}
	// Additive migration for ledger DBs created before ignored_at_begin/
	// ignored_touched existed (A8) — CREATE TABLE IF NOT EXISTS above is a
	// no-op against an already-existing table, so these columns need an
	// explicit ALTER. Errors are ignored: "duplicate column" on a DB that
	// already has them is the expected steady-state outcome.
	_, _ = db.Write().Exec(`ALTER TABLE runs ADD COLUMN ignored_at_begin TEXT NOT NULL DEFAULT '[]'`)
	_, _ = db.Write().Exec(`ALTER TABLE runs ADD COLUMN ignored_touched TEXT NOT NULL DEFAULT '[]'`)
	return &ledger{db: db}, nil
}

func (l *ledger) Close() error {
	return l.db.Close()
}

func (l *ledger) Insert(ctx context.Context, r RunRecord) error {
	touched, err := json.Marshal(r.TouchedPaths)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal touched_paths: %w", err)
	}
	ignoredBegin, err := json.Marshal(r.IgnoredAtBegin)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal ignored_at_begin: %w", err)
	}
	ignoredTouched, err := json.Marshal(r.IgnoredTouched)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal ignored_touched: %w", err)
	}
	_, err = l.db.Write().ExecContext(ctx, `
		INSERT INTO runs (thread_id, agent_id, task_summary, status, pre_snapshot, post_snapshot, touched_paths, pushed, pr_url, capture_error, ignored_at_begin, ignored_touched, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			agent_id=excluded.agent_id, task_summary=excluded.task_summary, status=excluded.status,
			pre_snapshot=excluded.pre_snapshot, ignored_at_begin=excluded.ignored_at_begin, created_at=excluded.created_at
	`, r.ThreadID, r.AgentID, r.TaskSummary, string(r.Status), r.PreSnapshot, r.PostSnapshot,
		string(touched), boolToInt(r.Pushed), r.PRURL, r.CaptureError, string(ignoredBegin), string(ignoredTouched),
		r.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTime(r.CompletedAt))
	return err
}

func (l *ledger) Update(ctx context.Context, r RunRecord) error {
	touched, err := json.Marshal(r.TouchedPaths)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal touched_paths: %w", err)
	}
	ignoredTouched, err := json.Marshal(r.IgnoredTouched)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal ignored_touched: %w", err)
	}
	res, err := l.db.Write().ExecContext(ctx, `
		UPDATE runs SET status=?, post_snapshot=?, touched_paths=?, pushed=?, pr_url=?, capture_error=?, ignored_touched=?, completed_at=?
		WHERE thread_id=?
	`, string(r.Status), r.PostSnapshot, string(touched), boolToInt(r.Pushed), r.PRURL, r.CaptureError,
		string(ignoredTouched), nullableTime(r.CompletedAt), r.ThreadID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (l *ledger) Get(ctx context.Context, threadID string) (RunRecord, error) {
	row := l.db.Read().QueryRowContext(ctx, `
		SELECT thread_id, agent_id, task_summary, status, pre_snapshot, post_snapshot, touched_paths, pushed, pr_url, capture_error, ignored_at_begin, ignored_touched, created_at, completed_at
		FROM runs WHERE thread_id = ?
	`, threadID)
	return scanRun(row)
}

func (l *ledger) List(ctx context.Context, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := l.db.Read().QueryContext(ctx, `
		SELECT thread_id, agent_id, task_summary, status, pre_snapshot, post_snapshot, touched_paths, pushed, pr_url, capture_error, ignored_at_begin, ignored_touched, created_at, completed_at
		FROM runs ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteExcept removes every run row whose thread_id is not in keep.
// Returns the number of rows removed.
func (l *ledger) DeleteExcept(ctx context.Context, keep map[string]bool) (int, error) {
	rows, err := l.db.Read().QueryContext(ctx, `SELECT thread_id FROM runs`)
	if err != nil {
		return 0, err
	}
	var toDelete []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			toDelete = append(toDelete, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range toDelete {
		if _, err := l.db.Write().ExecContext(ctx, `DELETE FROM runs WHERE thread_id = ?`, id); err != nil {
			return 0, err
		}
	}
	return len(toDelete), nil
}

// rowScanner abstracts *sql.Row and *sql.Rows, both of which implement Scan.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(row rowScanner) (RunRecord, error) {
	var r RunRecord
	var status, touchedJSON, ignoredBeginJSON, ignoredTouchedJSON, createdAt string
	var completedAt sql.NullString
	var pushed int
	if err := row.Scan(&r.ThreadID, &r.AgentID, &r.TaskSummary, &status, &r.PreSnapshot, &r.PostSnapshot,
		&touchedJSON, &pushed, &r.PRURL, &r.CaptureError, &ignoredBeginJSON, &ignoredTouchedJSON, &createdAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunRecord{}, ErrRunNotFound
		}
		return RunRecord{}, err
	}
	r.Status = RunStatus(status)
	r.Pushed = pushed != 0
	if err := json.Unmarshal([]byte(touchedJSON), &r.TouchedPaths); err != nil {
		return RunRecord{}, fmt.Errorf("checkpoint: unmarshal touched_paths: %w", err)
	}
	if ignoredBeginJSON != "" {
		if err := json.Unmarshal([]byte(ignoredBeginJSON), &r.IgnoredAtBegin); err != nil {
			return RunRecord{}, fmt.Errorf("checkpoint: unmarshal ignored_at_begin: %w", err)
		}
	}
	if ignoredTouchedJSON != "" {
		if err := json.Unmarshal([]byte(ignoredTouchedJSON), &r.IgnoredTouched); err != nil {
			return RunRecord{}, fmt.Errorf("checkpoint: unmarshal ignored_touched: %w", err)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		r.CreatedAt = t
	}
	if completedAt.Valid && completedAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, completedAt.String); err == nil {
			r.CompletedAt = t
		}
	}
	return r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
