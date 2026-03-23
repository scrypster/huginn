package spaces

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Workstream represents a named project that groups sessions and artifacts.
type Workstream struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkstreamStore provides CRUD operations for workstreams backed by SQLite.
type WorkstreamStore struct {
	db *sqlitedb.DB
}

// NewWorkstreamStore returns a WorkstreamStore using the provided DB.
// The workstreams and workstream_sessions tables must already exist (created by
// ApplySchema or the Workstream migration).
func NewWorkstreamStore(db *sqlitedb.DB) *WorkstreamStore {
	return &WorkstreamStore{db: db}
}

// WorkstreamMigrations returns an empty list — all schema is now in the base schema DDL.
func WorkstreamMigrations() []sqlitedb.Migration {
	return nil
}

// migrateWorkstreamsV1 creates the workstreams and workstream_sessions tables
// if they do not already exist.
func migrateWorkstreamsV1(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS workstreams (
			id          TEXT NOT NULL PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT,
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`,
		// session_id is stored as a plain TEXT reference without a foreign key
		// constraint so that workstreams can be migrated independently of the
		// sessions schema version. Application code validates session IDs.
		`CREATE TABLE IF NOT EXISTS workstream_sessions (
			workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
			session_id    TEXT NOT NULL,
			tagged_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			PRIMARY KEY (workstream_id, session_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workstream_sessions_ws
			ON workstream_sessions (workstream_id, tagged_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workstream_sessions_sess
			ON workstream_sessions (session_id)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// Create inserts a new workstream and returns it with the generated ID set.
// The INSERT and subsequent SELECT are wrapped in a single write transaction
// so that a partial failure cannot leave a phantom ID in the table.
func (ws *WorkstreamStore) Create(ctx context.Context, name, description string) (*Workstream, error) {
	if name == "" {
		return nil, fmt.Errorf("workstream: name is required")
	}
	id := newID() // reuse the spaces package hex ID generator
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := ws.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("workstream: create begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO workstreams (id, name, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, name, description, now, now,
	); err != nil {
		return nil, fmt.Errorf("workstream: create insert: %w", err)
	}

	w, err := getWithTx(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("workstream: create read-back: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("workstream: create commit: %w", err)
	}
	return w, nil
}

// List returns all workstreams ordered by created_at DESC.
func (ws *WorkstreamStore) List(ctx context.Context) ([]*Workstream, error) {
	rows, err := ws.db.Read().QueryContext(ctx,
		`SELECT id, name, COALESCE(description,''), created_at, updated_at
		 FROM workstreams ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("workstream: list: %w", err)
	}
	defer rows.Close()

	var result []*Workstream
	for rows.Next() {
		w, err := scanWorkstream(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workstream: list rows: %w", err)
	}
	if result == nil {
		result = []*Workstream{}
	}
	return result, nil
}

// Get returns a single workstream by ID. Returns an error if not found.
func (ws *WorkstreamStore) Get(ctx context.Context, id string) (*Workstream, error) {
	row := ws.db.Read().QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description,''), created_at, updated_at
		 FROM workstreams WHERE id = ?`, id,
	)
	w, err := scanWorkstreamRow(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workstream: not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("workstream: get %s: %w", id, err)
	}
	return w, nil
}

// Delete removes a workstream and its session associations (CASCADE).
func (ws *WorkstreamStore) Delete(ctx context.Context, id string) error {
	res, err := ws.db.Write().ExecContext(ctx,
		`DELETE FROM workstreams WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("workstream: delete %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workstream: not found: %s", id)
	}
	return nil
}

// Update modifies the name and/or description of an existing workstream.
// Both fields are required; pass the existing value to leave a field unchanged.
// Returns the updated workstream on success.
func (ws *WorkstreamStore) Update(ctx context.Context, id, name, description string) (*Workstream, error) {
	if id == "" {
		return nil, fmt.Errorf("workstream: id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("workstream: name is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := ws.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("workstream: update begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.ExecContext(ctx,
		`UPDATE workstreams SET name = ?, description = ?, updated_at = ?
		 WHERE id = ?`,
		name, description, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("workstream: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("workstream: not found: %s", id)
	}

	w, err := getWithTx(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("workstream: update read-back: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("workstream: update commit: %w", err)
	}
	return w, nil
}

// TagSession associates a session with a workstream. Idempotent.
func (ws *WorkstreamStore) TagSession(ctx context.Context, workstreamID, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := ws.db.Write().ExecContext(ctx,
		`INSERT OR IGNORE INTO workstream_sessions (workstream_id, session_id, tagged_at)
		 VALUES (?, ?, ?)`,
		workstreamID, sessionID, now,
	); err != nil {
		return fmt.Errorf("workstream: tag session: %w", err)
	}
	return nil
}

// ListSessions returns session IDs associated with a workstream, newest first.
func (ws *WorkstreamStore) ListSessions(ctx context.Context, workstreamID string) ([]string, error) {
	rows, err := ws.db.Read().QueryContext(ctx,
		`SELECT session_id FROM workstream_sessions
		 WHERE workstream_id = ? ORDER BY tagged_at DESC`,
		workstreamID,
	)
	if err != nil {
		return nil, fmt.Errorf("workstream: list sessions: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("workstream: scan session id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workstream: list sessions rows: %w", err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

// getWithTx reads a single workstream within an existing transaction.
// Used by Create to avoid mixing the write Tx with the read *sql.DB handle.
func getWithTx(ctx context.Context, tx *sql.Tx, id string) (*Workstream, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description,''), created_at, updated_at
		 FROM workstreams WHERE id = ?`, id,
	)
	return scanWorkstreamRow(row)
}

// scanWorkstream scans a workstream from sql.Rows.
func scanWorkstream(rows *sql.Rows) (*Workstream, error) {
	var w Workstream
	var createdAt, updatedAt string
	if err := rows.Scan(&w.ID, &w.Name, &w.Description, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("workstream: scan: %w", err)
	}
	w.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &w, nil
}

// scanWorkstreamRow scans a workstream from *sql.Row.
func scanWorkstreamRow(row *sql.Row) (*Workstream, error) {
	var w Workstream
	var createdAt, updatedAt string
	if err := row.Scan(&w.ID, &w.Name, &w.Description, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	w.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &w, nil
}
