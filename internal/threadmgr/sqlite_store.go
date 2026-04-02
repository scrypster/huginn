package threadmgr

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteThreadStore implements ThreadStore over the huginn SQLite database.
// It maps Thread struct fields to the existing threads table columns.
type SQLiteThreadStore struct {
	db *sqlitedb.DB

	// OnThreadReplyCount is an optional callback invoked when a new thread
	// increments the parent message's thread_reply_count. Used by the server
	// to broadcast badge updates to WS clients on page-load hydration.
	OnThreadReplyCount func(sessionID, parentMessageID string, newCount int64)
}

// NewSQLiteThreadStore creates a SQLiteThreadStore backed by db.
func NewSQLiteThreadStore(db *sqlitedb.DB) *SQLiteThreadStore {
	return &SQLiteThreadStore{db: db}
}

// Migrations returns an empty list — all schema is now in the base schema DDL.
func Migrations() []sqlitedb.Migration {
	return nil
}

// migrateThreadsParentMsgID adds the parent_msg_id column to the threads table
// so that threads can record the chat message that triggered them. Idempotent.
func migrateThreadsParentMsgID(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE threads ADD COLUMN parent_msg_id TEXT NOT NULL DEFAULT ''`)
	if err != nil && !sqlitedb.IsColumnExistsError(err) {
		return err
	}
	return nil
}

// migrateThreadsTimeoutNs adds the timeout_ns column to the threads table
// so that the per-thread Timeout (time.Duration) survives server restarts.
// Stored as nanoseconds (INTEGER). 0 means no timeout. Idempotent.
func migrateThreadsTimeoutNs(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE threads ADD COLUMN timeout_ns INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !sqlitedb.IsColumnExistsError(err) {
		return err
	}
	return nil
}

// threadStatusForDB maps a ThreadStatus to the value stored in the DB.
// The threads table CHECK constraint accepts: queued, thinking, tooling, done,
// blocked, cancelled, error, interrupted. We map StatusCancelled → "cancelled"
// and leave all others as their string value.
func threadStatusForDB(s ThreadStatus) string {
	return string(s)
}

// SaveThread upserts the full thread record (INSERT OR REPLACE) into the
// threads table. Uses parent_type='session' and parent_id=SessionID.
func (s *SQLiteThreadStore) SaveThread(ctx context.Context, t *Thread) error {
	if s == nil || s.db == nil {
		return nil
	}
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("threadmgr sqlite: SaveThread: database is closed")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var startedAt *string
	if !t.StartedAt.IsZero() {
		v := t.StartedAt.UTC().Format(time.RFC3339Nano)
		startedAt = &v
	}
	var completedAt *string
	if !t.CompletedAt.IsZero() {
		v := t.CompletedAt.UTC().Format(time.RFC3339Nano)
		completedAt = &v
	}

	// Marshal summary fields. Use nil for summary_status when no summary exists
	// because the DB CHECK constraint rejects empty strings for that column.
	var summaryText *string
	var summaryStatus *string
	filesModified := "[]"
	keyDecisions := "[]"
	artifacts := "[]"
	if t.Summary != nil {
		st := t.Summary.Summary
		ss := t.Summary.Status
		summaryText = &st
		summaryStatus = &ss
		if b, err := json.Marshal(t.Summary.FilesModified); err == nil {
			filesModified = string(b)
		}
		if b, err := json.Marshal(t.Summary.KeyDecisions); err == nil {
			keyDecisions = string(b)
		}
		if b, err := json.Marshal(t.Summary.Artifacts); err == nil {
			artifacts = string(b)
		}
	}

	createdAt := now
	if !t.CreatedAt.IsZero() {
		createdAt = t.CreatedAt.UTC().Format(time.RFC3339Nano)
	}

	status := threadStatusForDB(t.Status)
	// The DB CHECK constraint does not include "queued" as an insert-only state —
	// it does include it. Map any unrecognised status to "error" to be safe.
	switch status {
	case "queued", "thinking", "tooling", "done", "blocked", "cancelled", "error", "interrupted":
	default:
		status = "error"
	}

	// Use a transaction so the thread upsert and the thread_reply_count
	// increment on the parent message are atomic.
	tx, err := wdb.Begin()
	if err != nil {
		return fmt.Errorf("threadmgr sqlite: SaveThread %s: begin: %w", t.ID, err)
	}

	// Check if this thread already exists (to avoid double-incrementing
	// thread_reply_count on subsequent SaveThread calls / upserts).
	var exists bool
	if err := tx.QueryRow(`SELECT 1 FROM threads WHERE id = ?`, t.ID).Scan(new(int)); err == nil {
		exists = true
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO threads
			(id, parent_type, parent_id, agent_name, task, status,
			 parent_msg_id,
			 created_at, started_at, completed_at,
			 token_budget, tokens_used, cost_usd,
			 summary_text, summary_status,
			 files_modified, key_decisions, artifacts,
			 timeout_ns)
		VALUES (?, 'session', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0.0, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			parent_id      = excluded.parent_id,
			agent_name     = excluded.agent_name,
			task           = excluded.task,
			status         = excluded.status,
			parent_msg_id  = excluded.parent_msg_id,
			created_at     = excluded.created_at,
			started_at     = excluded.started_at,
			completed_at   = excluded.completed_at,
			token_budget   = excluded.token_budget,
			tokens_used    = excluded.tokens_used,
			summary_text   = excluded.summary_text,
			summary_status = excluded.summary_status,
			files_modified = excluded.files_modified,
			key_decisions  = excluded.key_decisions,
			artifacts      = excluded.artifacts,
			timeout_ns     = excluded.timeout_ns`,
		t.ID, t.SessionID, t.AgentID, t.Task, status,
		t.ParentMessageID,
		createdAt, startedAt, completedAt,
		t.TokenBudget, t.TokensUsed,
		summaryText, summaryStatus,
		filesModified, keyDecisions, artifacts,
		int64(t.Timeout),
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("threadmgr sqlite: SaveThread %s: %w", t.ID, err)
	}

	// On new thread creation (not upsert), increment thread_reply_count on
	// the parent message so the UI shows a thread badge after page refresh.
	// The badge is driven by `thread_reply_count > 0` in the container
	// threads query. Without this, badges only appear via live WS events
	// and vanish on reload.
	var newReplyCount int64
	if !exists && t.ParentMessageID != "" {
		if _, updateErr := tx.Exec(`
			UPDATE messages SET thread_reply_count = thread_reply_count + 1
			WHERE id = ?`, t.ParentMessageID); updateErr != nil {
			// Non-fatal: log but still commit the thread record.
			slog.Warn("threadmgr sqlite: thread_reply_count increment failed",
				"thread_id", t.ID, "parent_msg_id", t.ParentMessageID, "err", updateErr)
		} else {
			_ = tx.QueryRow(`SELECT thread_reply_count FROM messages WHERE id = ?`,
				t.ParentMessageID).Scan(&newReplyCount)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("threadmgr sqlite: SaveThread %s: commit: %w", t.ID, err)
	}

	// Notify listeners (e.g. WS hub) of the updated reply count after commit.
	if newReplyCount > 0 && s.OnThreadReplyCount != nil {
		s.OnThreadReplyCount(t.SessionID, t.ParentMessageID, newReplyCount)
	}

	return nil
}

// LoadThreads returns all threads for a given sessionID ordered by created_at ASC.
// Returns an empty slice when none are found. The returned Thread objects have their
// in-memory-only fields (cancel, InputCh) initialised with zero/nil values.
func (s *SQLiteThreadStore) LoadThreads(ctx context.Context, sessionID string) ([]*Thread, error) {
	if s == nil || s.db == nil {
		return []*Thread{}, nil
	}
	rdb := s.db.Read()
	if rdb == nil {
		return nil, fmt.Errorf("threadmgr sqlite: LoadThreads: database is closed")
	}

	rows, err := rdb.QueryContext(ctx, `
		SELECT id, agent_name, task, status,
		       COALESCE(parent_msg_id, '') AS parent_msg_id,
		       created_at,
		       COALESCE(started_at, '') AS started_at,
		       COALESCE(completed_at, '') AS completed_at,
		       token_budget, tokens_used,
		       COALESCE(summary_text, '') AS summary_text,
		       COALESCE(summary_status, '') AS summary_status,
		       COALESCE(files_modified, '[]') AS files_modified,
		       COALESCE(key_decisions, '[]') AS key_decisions,
		       COALESCE(artifacts, '[]') AS artifacts,
		       COALESCE(timeout_ns, 0) AS timeout_ns
		FROM threads
		WHERE parent_type = 'session' AND parent_id = ?
		ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("threadmgr sqlite: LoadThreads %s: %w", sessionID, err)
	}
	defer rows.Close()

	var out []*Thread
	for rows.Next() {
		t, err := scanThread(rows, sessionID)
		if err != nil {
			return nil, fmt.Errorf("threadmgr sqlite: LoadThreads scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("threadmgr sqlite: LoadThreads rows: %w", err)
	}
	if out == nil {
		out = []*Thread{}
	}
	return out, nil
}

// DeleteThread removes the thread record with the given ID.
// Returns nil if the thread does not exist (idempotent).
func (s *SQLiteThreadStore) DeleteThread(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("threadmgr sqlite: DeleteThread: database is closed")
	}
	_, err := wdb.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("threadmgr sqlite: DeleteThread %s: %w", id, err)
	}
	return nil
}

// UpdateThreadStatus updates the status column for the thread with the given ID.
func (s *SQLiteThreadStore) UpdateThreadStatus(ctx context.Context, id, status string) error {
	if s == nil || s.db == nil {
		return nil
	}
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("threadmgr sqlite: UpdateThreadStatus: database is closed")
	}
	_, err := wdb.ExecContext(ctx,
		`UPDATE threads SET status = ? WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("threadmgr sqlite: UpdateThreadStatus %s → %s: %w", id, status, err)
	}
	return nil
}

// scanThread reads one row from a SELECT on threads and populates a Thread.
func scanThread(rows *sql.Rows, sessionID string) (*Thread, error) {
	var (
		id, agentName, task, status string
		parentMsgID                  string
		createdAtStr                 string
		startedAtStr, completedAtStr string
		tokenBudget, tokensUsed      int
		summaryText, summaryStatus   string
		filesModifiedJSON            string
		keyDecisionsJSON             string
		artifactsJSON                string
		timeoutNs                    int64
	)

	if err := rows.Scan(
		&id, &agentName, &task, &status,
		&parentMsgID,
		&createdAtStr, &startedAtStr, &completedAtStr,
		&tokenBudget, &tokensUsed,
		&summaryText, &summaryStatus,
		&filesModifiedJSON, &keyDecisionsJSON, &artifactsJSON,
		&timeoutNs,
	); err != nil {
		return nil, err
	}

	t := &Thread{
		ID:              id,
		SessionID:       sessionID,
		AgentID:         agentName,
		Task:            task,
		Status:          ThreadStatus(status),
		ParentMessageID: parentMsgID,
		TokenBudget:     tokenBudget,
		TokensUsed:      tokensUsed,
		Timeout:         time.Duration(timeoutNs),
		InputCh:         make(chan string, 1),
	}

	if ts, err := parseTime(createdAtStr); err == nil {
		t.CreatedAt = ts
		t.StartedAt = ts // default StartedAt = CreatedAt
	}
	if startedAtStr != "" {
		if ts, err := parseTime(startedAtStr); err == nil {
			t.StartedAt = ts
		}
	}
	if completedAtStr != "" {
		if ts, err := parseTime(completedAtStr); err == nil {
			t.CompletedAt = ts
		}
	}

	// Rebuild FinishSummary only when summary data is present.
	if summaryText != "" || summaryStatus != "" {
		fs := &FinishSummary{
			Summary: summaryText,
			Status:  summaryStatus,
		}
		var files []string
		if err := json.Unmarshal([]byte(filesModifiedJSON), &files); err == nil {
			fs.FilesModified = files
		}
		var decisions []string
		if err := json.Unmarshal([]byte(keyDecisionsJSON), &decisions); err == nil {
			fs.KeyDecisions = decisions
		}
		var arts []string
		if err := json.Unmarshal([]byte(artifactsJSON), &arts); err == nil {
			fs.Artifacts = arts
		}
		t.Summary = fs
	}

	return t, nil
}

// parseTime tries RFC3339Nano then RFC3339 to parse a timestamp string.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// Compile-time assertion: *SQLiteThreadStore must satisfy ThreadStore.
var _ ThreadStore = (*SQLiteThreadStore)(nil)
