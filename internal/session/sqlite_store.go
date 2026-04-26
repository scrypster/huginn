// internal/session/sqlite_store.go
package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteSessionStore implements session CRUD methods backed by SQLite.
type SQLiteSessionStore struct {
	db *sqlitedb.DB

	// OnThreadReply is an optional callback invoked after a thread reply
	// increments the parent's thread_reply_count. Called with (sessionID,
	// parentMessageID, newReplyCount). Safe to set once before first use.
	OnThreadReply func(sessionID, parentMessageID string, newCount int64)
}

// NewSQLiteSessionStore creates a new SQLiteSessionStore.
func NewSQLiteSessionStore(db *sqlitedb.DB) *SQLiteSessionStore {
	return &SQLiteSessionStore{db: db}
}

// New creates a new in-memory Session (not persisted until SaveManifest is called).
func (s *SQLiteSessionStore) New(title, workspaceRoot, model string) *Session {
	id := NewID()
	now := time.Now().UTC()
	var wname string
	if workspaceRoot == "" {
		wname = ""
	} else {
		wname = filepath.Base(workspaceRoot)
	}
	return &Session{
		ID: id,
		Manifest: Manifest{
			ID:            id,
			SessionID:     id,
			Title:         title,
			Model:         model,
			WorkspaceRoot: workspaceRoot,
			WorkspaceName: wname,
			Status:        "active",
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
}

// SaveManifest upserts the session manifest into the sessions table and keeps
// the sessions_fts full-text index in sync.
// The manifest's UpdatedAt field is used as-is (caller controls it).
func (s *SQLiteSessionStore) SaveManifest(sess *Session) error {
	sess.mu.Lock()
	m := sess.Manifest
	sess.mu.Unlock()

	var spaceID *string
	if m.SpaceID != "" {
		spaceID = &m.SpaceID
	}

	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("session sqlite: database is closed")
	}

	// Wrap the session UPSERT + FTS DELETE + FTS INSERT in a single transaction
	// so that a crash between operations cannot leave the FTS index inconsistent
	// with the sessions table (e.g. session exists but is unsearchable).
	tx, err := wdb.Begin()
	if err != nil {
		return fmt.Errorf("session sqlite: begin tx for manifest %s: %w", m.ID, err)
	}
	defer tx.Rollback() //nolint:errcheck — rollback is a safety net; commit error is returned below

	if _, err := tx.Exec(`
		INSERT INTO sessions
			(id, title, model, agent, created_at, updated_at, message_count,
			 last_message_id, workspace_root, workspace_name, status, version,
			 source, routine_id, run_id, space_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, model=excluded.model, agent=excluded.agent,
			updated_at=excluded.updated_at, message_count=excluded.message_count,
			last_message_id=excluded.last_message_id,
			workspace_root=excluded.workspace_root, workspace_name=excluded.workspace_name,
			status=excluded.status, version=excluded.version,
			source=excluded.source, routine_id=excluded.routine_id, run_id=excluded.run_id,
			space_id=excluded.space_id`,
		m.ID, m.Title, m.Model, m.Agent,
		m.CreatedAt.UTC().Format(time.RFC3339),
		m.UpdatedAt.UTC().Format(time.RFC3339),
		m.MessageCount, m.LastMessageID,
		m.WorkspaceRoot, m.WorkspaceName,
		statusOrDefault(m.Status), versionOrDefault(m.Version),
		m.Source, m.RoutineID, m.RunID, spaceID,
	); err != nil {
		return fmt.Errorf("session sqlite: save manifest %s: %w", m.ID, err)
	}

	// Keep sessions_fts in sync within the same transaction. FTS5 virtual tables
	// do not support UNIQUE constraints on non-rowid columns, so INSERT OR REPLACE
	// creates a duplicate row. Explicit DELETE then INSERT guarantees a true upsert.
	if _, err := tx.Exec(`DELETE FROM sessions_fts WHERE session_id = ?`, m.ID); err != nil {
		return fmt.Errorf("session sqlite: fts delete manifest %s: %w", m.ID, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO sessions_fts(session_id, space_id, title) VALUES (?, ?, ?)`,
		m.ID, spaceID, m.Title,
	); err != nil {
		return fmt.Errorf("session sqlite: fts insert manifest %s: %w", m.ID, err)
	}

	return tx.Commit()
}

// Exists returns true if the session exists in the database.
// If the query fails, it returns false rather than silently treating an error as absence.
func (s *SQLiteSessionStore) Exists(id string) bool {
	rdb := s.db.Read()
	if rdb == nil {
		return false
	}
	var count int
	if err := rdb.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// Load reads the session manifest from SQLite and returns a *Session.
func (s *SQLiteSessionStore) Load(id string) (*Session, error) {
	m, err := s.loadManifestRow(id)
	if err != nil {
		return nil, fmt.Errorf("session.Load: %w", err)
	}
	sess := &Session{ID: m.ID, Manifest: *m}
	// Initialize seq from the current max in the DB so that subsequent
	// Append calls use the correct next sequence number. Without this,
	// every Load returns seq=0 and the first Append after a fresh Load
	// tries seq=1 again — the UNIQUE (container_id, seq) constraint causes
	// INSERT OR IGNORE to silently drop all messages after the first turn.
	var maxSeq int64
	_ = s.db.Read().QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE container_id = ?`, id,
	).Scan(&maxSeq)
	atomic.StoreInt64(&sess.seq, maxSeq)
	return sess, nil
}

// LoadOrReconstruct is equivalent to Load for the SQLite store.
// There is no JSONL to reconstruct from; the DB row is authoritative.
func (s *SQLiteSessionStore) LoadOrReconstruct(id string) (*Session, error) {
	return s.Load(id)
}

// Delete removes the session and its related messages and threads from SQLite.
// Also removes the session's FTS row from sessions_fts.
func (s *SQLiteSessionStore) Delete(id string) error {
	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("session sqlite: delete %s: begin: %w", id, err)
	}
	for _, q := range []string{
		`DELETE FROM messages WHERE container_id = ?`,
		`DELETE FROM thread_deps WHERE thread_id IN (SELECT id FROM threads WHERE parent_id = ?)`,
		`DELETE FROM threads WHERE parent_id = ?`,
		`DELETE FROM sessions WHERE id = ?`,
		`DELETE FROM sessions_fts WHERE session_id = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			tx.Rollback()
			return fmt.Errorf("session sqlite: delete %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// ArchiveSession sets the session status to "archived" in SQLite.
// The session and all its messages remain in the database; they are just
// excluded from normal List() results. Use Delete() for permanent removal.
func (s *SQLiteSessionStore) ArchiveSession(id string) error {
	_, err := s.db.Write().Exec(
		`UPDATE sessions SET status = 'archived', updated_at = ? WHERE id = ?`,
		timeNow(), id,
	)
	if err != nil {
		return fmt.Errorf("session sqlite: archive %s: %w", id, err)
	}
	return nil
}

// ListFiltered returns session manifests sorted by updated_at DESC.
// When filter.IncludeArchived is false (the default), archived sessions are
// excluded. Pass SessionFilter{IncludeArchived: true} to include them.
func (s *SQLiteSessionStore) ListFiltered(filter SessionFilter) ([]Manifest, error) {
	var q string
	if filter.IncludeArchived {
		q = `SELECT id, title, model, agent, created_at, updated_at, message_count,
		       last_message_id, workspace_root, workspace_name, status, version,
		       source, routine_id, run_id, space_id
		FROM sessions ORDER BY updated_at DESC`
	} else {
		q = `SELECT id, title, model, agent, created_at, updated_at, message_count,
		       last_message_id, workspace_root, workspace_name, status, version,
		       source, routine_id, run_id, space_id
		FROM sessions WHERE status != 'archived' ORDER BY updated_at DESC`
	}
	rows, err := s.db.Read().Query(q)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: list filtered: %w", err)
	}
	defer rows.Close()
	var out []Manifest
	for rows.Next() {
		m, err := scanManifestRow(rows)
		if err != nil {
			return nil, fmt.Errorf("session sqlite: list filtered scan: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session sqlite: list filtered rows: %w", err)
	}
	if out == nil {
		out = []Manifest{}
	}
	return out, nil
}

// List returns all session manifests sorted by updated_at DESC (newest first).
// NOTE: List() returns ALL sessions including archived ones for backward
// compatibility. Use ListFiltered(SessionFilter{}) to exclude archived sessions.
func (s *SQLiteSessionStore) List() ([]Manifest, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, title, model, agent, created_at, updated_at, message_count,
		       last_message_id, workspace_root, workspace_name, status, version,
		       source, routine_id, run_id, space_id
		FROM sessions
		ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: list: %w", err)
	}
	defer rows.Close()

	var out []Manifest
	for rows.Next() {
		m, err := scanManifestRow(rows)
		if err != nil {
			return nil, fmt.Errorf("session sqlite: list scan: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session sqlite: list rows: %w", err)
	}
	if out == nil {
		out = []Manifest{}
	}
	return out, nil
}

// Append adds a message to the session in SQLite and increments MessageCount on the in-memory session.
func (s *SQLiteSessionStore) Append(sess *Session, msg SessionMessage) error {
	if len(msg.Content) > maxMessageContentBytes {
		return fmt.Errorf("session: message content exceeds %d byte limit (%d bytes)", maxMessageContentBytes, len(msg.Content))
	}
	if msg.ID == "" {
		msg.ID = NewID()
	}
	if msg.Ts.IsZero() {
		msg.Ts = time.Now().UTC()
	}

	// Assign monotonic seq using atomic increment on the session.
	seq := atomic.AddInt64(&sess.seq, 1)

	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("session sqlite: database is closed")
	}

	var parentMsgID *string
	if msg.ParentMessageID != "" {
		parentMsgID = &msg.ParentMessageID
	}

	// Use an explicit transaction so the INSERT and the reply-count UPDATE are
	// grouped for clarity and future-proofing. The UPDATE is non-fatal: if it
	// fails we log and commit anyway so the message is never lost.
	tx, err := wdb.Begin()
	if err != nil {
		return fmt.Errorf("session sqlite: begin tx for message %s: %w", msg.ID, err)
	}

	var toolCallsJSON *string
	if len(msg.ToolCalls) > 0 {
		b, jsonErr := json.Marshal(msg.ToolCalls)
		if jsonErr == nil {
			s := string(b)
			toolCallsJSON = &s
		}
	}

	_, err = tx.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content,
			 agent, tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model, parent_message_id,
			 tool_calls_json)
		VALUES (?, 'session', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, sess.ID, seq,
		msg.Ts.UTC().Format(time.RFC3339Nano),
		roleOrDefault(msg.Role), msg.Content,
		msg.Agent, msg.ToolName, msg.ToolCallID,
		msg.Type,
		msg.PromptTok, msg.CompTok, msg.CostUSD, msg.ModelName,
		parentMsgID,
		toolCallsJSON,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("session sqlite: append message %s: %w", msg.ID, err)
	}

	// If this is a thread reply, increment the parent's thread_reply_count.
	var newReplyCount int64
	if msg.ParentMessageID != "" {
		if _, updateErr := tx.Exec(`
			UPDATE messages SET thread_reply_count = thread_reply_count + 1
			WHERE id = ?`, msg.ParentMessageID); updateErr != nil {
			// Non-fatal: log visibly but still commit the message.
			slog.Warn("session: thread_reply_count increment failed",
				"parent_id", msg.ParentMessageID, "err", updateErr)
		} else {
			// Query the new count to pass to the hook.
			_ = tx.QueryRow(`SELECT thread_reply_count FROM messages WHERE id = ?`,
				msg.ParentMessageID).Scan(&newReplyCount)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session sqlite: commit message %s: %w", msg.ID, err)
	}

	// Notify listeners (e.g. WS hub) of the updated reply count.
	if msg.ParentMessageID != "" && newReplyCount > 0 && s.OnThreadReply != nil {
		s.OnThreadReply(sess.ID, msg.ParentMessageID, newReplyCount)
	}

	sess.mu.Lock()
	sess.Manifest.MessageCount++
	sess.Manifest.LastMessageID = msg.ID
	sess.mu.Unlock()

	return nil
}

// TailMessages returns the last n messages for a session in chronological order.
// Thread replies (messages with a parent_message_id) are excluded; only root
// messages are returned. Each root message is populated with its ThreadReplyCount.
func (s *SQLiteSessionStore) TailMessages(id string, n int) ([]SessionMessage, error) {
	if n <= 0 {
		return nil, nil
	}

	rdb := s.db.Read()
	if rdb == nil {
		return nil, fmt.Errorf("session sqlite: database is closed")
	}

	rows, err := rdb.Query(`
		SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
		       type, prompt_tokens, completion_tokens, cost_usd, model,
		       COALESCE(thread_reply_count, 0), tool_calls_json
		FROM (
			SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
			       type, prompt_tokens, completion_tokens, cost_usd, model,
			       thread_reply_count, tool_calls_json
			FROM messages
			WHERE container_type = 'session' AND container_id = ?
			  AND (parent_message_id IS NULL OR parent_message_id = '')
			ORDER BY seq DESC
			LIMIT ?
		)
		ORDER BY seq ASC`,
		id, n,
	)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: tail messages %s: %w", id, err)
	}
	defer rows.Close()
	return scanSessionMessagesWithReplyCount(rows)
}

// TailMessagesBefore returns the last n messages for a session where seq < beforeSeq,
// in ascending seq order.
func (s *SQLiteSessionStore) TailMessagesBefore(id string, n int, beforeSeq int64) ([]SessionMessage, error) {
	if n <= 0 {
		return nil, nil
	}

	rows, err := s.db.Read().Query(`
		SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
		       type, prompt_tokens, completion_tokens, cost_usd, model, tool_calls_json
		FROM (
			SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
			       type, prompt_tokens, completion_tokens, cost_usd, model, tool_calls_json
			FROM messages
			WHERE container_type = 'session' AND container_id = ? AND seq < ?
			ORDER BY seq DESC
			LIMIT ?
		)
		ORDER BY seq ASC`,
		id, beforeSeq, n,
	)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: tail messages before %s: %w", id, err)
	}
	defer rows.Close()
	return scanSessionMessages(rows)
}

func scanSessionMessages(rows *sql.Rows) ([]SessionMessage, error) {
	var out []SessionMessage
	for rows.Next() {
		var msg SessionMessage
		var tsStr string
		var toolCallsJSON sql.NullString
		if err := rows.Scan(
			&msg.ID, &tsStr, &msg.Seq, &msg.Role, &msg.Content,
			&msg.Agent, &msg.ToolName, &msg.ToolCallID,
			&msg.Type, &msg.PromptTok, &msg.CompTok, &msg.CostUSD, &msg.ModelName,
			&toolCallsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		// RFC3339Nano accepts both RFC3339 (no fractional seconds) and nano-precision
		// strings, so this is backward-compatible with rows written before the
		// nano upgrade.
		if t, e := time.Parse(time.RFC3339Nano, tsStr); e == nil {
			msg.Ts = t.UTC()
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			_ = json.Unmarshal([]byte(toolCallsJSON.String), &msg.ToolCalls)
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// scanSessionMessagesWithReplyCount scans rows that include a thread_reply_count and tool_calls_json column.
func scanSessionMessagesWithReplyCount(rows *sql.Rows) ([]SessionMessage, error) {
	var out []SessionMessage
	for rows.Next() {
		var msg SessionMessage
		var tsStr string
		var toolCallsJSON sql.NullString
		if err := rows.Scan(
			&msg.ID, &tsStr, &msg.Seq, &msg.Role, &msg.Content,
			&msg.Agent, &msg.ToolName, &msg.ToolCallID,
			&msg.Type, &msg.PromptTok, &msg.CompTok, &msg.CostUSD, &msg.ModelName,
			&msg.ThreadReplyCount, &toolCallsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		if t, e := time.Parse(time.RFC3339Nano, tsStr); e == nil {
			msg.Ts = t.UTC()
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			_ = json.Unmarshal([]byte(toolCallsJSON.String), &msg.ToolCalls)
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func roleOrDefault(r string) string {
	if r == "" {
		return "user"
	}
	return r
}

// --- internal helpers ---

func (s *SQLiteSessionStore) loadManifestRow(id string) (*Manifest, error) {
	row := s.db.Read().QueryRow(`
		SELECT id, title, model, agent, created_at, updated_at, message_count,
		       last_message_id, workspace_root, workspace_name, status, version,
		       source, routine_id, run_id, space_id
		FROM sessions WHERE id = ?`, id)
	m, err := scanManifestRow(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session sqlite: load %s: not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("session sqlite: load %s: %w", id, err)
	}
	return m, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanManifestRow(row rowScanner) (*Manifest, error) {
	var m Manifest
	var createdAtStr, updatedAtStr string
	var spaceID sql.NullString
	err := row.Scan(
		&m.ID, &m.Title, &m.Model, &m.Agent,
		&createdAtStr, &updatedAtStr, &m.MessageCount,
		&m.LastMessageID, &m.WorkspaceRoot, &m.WorkspaceName,
		&m.Status, &m.Version,
		&m.Source, &m.RoutineID, &m.RunID, &spaceID,
	)
	if err != nil {
		return nil, err
	}
	m.SessionID = m.ID
	if spaceID.Valid {
		m.SpaceID = spaceID.String
	}
	if t, e := time.Parse(time.RFC3339, createdAtStr); e == nil {
		m.CreatedAt = t.UTC()
	}
	if t, e := time.Parse(time.RFC3339, updatedAtStr); e == nil {
		m.UpdatedAt = t.UTC()
	}
	if m.Status == "" {
		m.Status = "active"
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return &m, nil
}

// AppendToThread appends a message to a thread. Creates the thread row if it doesn't exist.
func (s *SQLiteSessionStore) AppendToThread(sessionID, threadID string, msg SessionMessage) error {
	if msg.ID == "" {
		msg.ID = NewID()
	}
	if msg.Ts.IsZero() {
		msg.Ts = time.Now().UTC()
	}

	// Ensure thread row exists (INSERT OR IGNORE).
	if _, err := s.db.Write().Exec(`
		INSERT OR IGNORE INTO threads
			(id, parent_type, parent_id, status, created_at,
			 files_modified, key_decisions, artifacts)
		VALUES (?, 'session', ?, 'done', ?, '[]', '[]', '[]')`,
		threadID, sessionID,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("session sqlite: ensure thread %s: %w", threadID, err)
	}

	// Get next seq atomically: fetch max and insert in a transaction.
	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("session sqlite: append thread message %s: begin: %w", msg.ID, err)
	}

	var maxSeq int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE container_type = 'thread' AND container_id = ?`,
		threadID,
	).Scan(&maxSeq); err != nil {
		tx.Rollback()
		return fmt.Errorf("session sqlite: append thread message %s: query max seq: %w", msg.ID, err)
	}
	seq := maxSeq + 1

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content,
			 agent, tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model)
		VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, threadID, seq,
		msg.Ts.UTC().Format(time.RFC3339Nano),
		roleOrDefault(msg.Role), msg.Content,
		msg.Agent, msg.ToolName, msg.ToolCallID,
		msg.Type, msg.PromptTok, msg.CompTok, msg.CostUSD, msg.ModelName,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("session sqlite: append thread message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session sqlite: append thread message %s: commit: %w", msg.ID, err)
	}
	return nil
}

// TailThreadMessages returns the last n messages for a thread in chronological order.
// Returns nil, nil if the thread has no messages.
func (s *SQLiteSessionStore) TailThreadMessages(sessionID, threadID string, n int) ([]SessionMessage, error) {
	if n <= 0 {
		return nil, nil
	}

	var count int
	if err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM messages WHERE container_type = 'thread' AND container_id = ?`,
		threadID,
	).Scan(&count); err != nil {
		return nil, fmt.Errorf("session sqlite: tail thread messages count %s: %w", threadID, err)
	}
	if count == 0 {
		return nil, nil
	}

	rows, err := s.db.Read().Query(`
		SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
		       type, prompt_tokens, completion_tokens, cost_usd, model, tool_calls_json
		FROM (
			SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
			       type, prompt_tokens, completion_tokens, cost_usd, model, tool_calls_json
			FROM messages
			WHERE container_type = 'thread' AND container_id = ?
			ORDER BY seq DESC LIMIT ?
		)
		ORDER BY seq ASC`,
		threadID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: tail thread messages: %w", err)
	}
	defer rows.Close()
	return scanSessionMessages(rows)
}

// ListThreadIDs returns all thread IDs parented to a session, ordered by created_at.
func (s *SQLiteSessionStore) ListThreadIDs(sessionID string) ([]string, error) {
	rows, err := s.db.Read().Query(
		`SELECT id FROM threads WHERE parent_type = 'session' AND parent_id = ? ORDER BY created_at`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session.ListThreadIDs: query: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("session.ListThreadIDs: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session.ListThreadIDs: rows: %w", err)
	}
	return ids, nil
}

// timeNow returns the current UTC time formatted as RFC3339.
// Extracted as a function to allow substitution in tests if needed.
func timeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func statusOrDefault(s string) string {
	if s == "" {
		return "active"
	}
	return s
}

func versionOrDefault(v int) int {
	if v == 0 {
		return 1
	}
	return v
}

// --- Persist layer methods ---

// Create upserts a PersistentSession into the sessions table.
// Note: PersistentSession uses different field names than Manifest:
// MsgCount (not MessageCount), LastMsgID (not LastMessageID),
// Workspace (not WorkspaceRoot), WsName (not WorkspaceName).
func (s *SQLiteSessionStore) Create(ps *PersistentSession) error {
	_, err := s.db.Write().Exec(`
		INSERT INTO sessions
			(id, title, model, agent, created_at, updated_at, message_count,
			 last_message_id, workspace_root, workspace_name, status, version,
			 source, routine_id, run_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, model=excluded.model, agent=excluded.agent,
			updated_at=excluded.updated_at, message_count=excluded.message_count,
			last_message_id=excluded.last_message_id,
			workspace_root=excluded.workspace_root, workspace_name=excluded.workspace_name,
			status=excluded.status, version=excluded.version,
			source=excluded.source, routine_id=excluded.routine_id, run_id=excluded.run_id`,
		ps.ID, ps.Title, ps.Model, ps.Agent,
		ps.CreatedAt.UTC().Format(time.RFC3339),
		ps.UpdatedAt.UTC().Format(time.RFC3339),
		ps.MsgCount, ps.LastMsgID,
		ps.Workspace, ps.WsName,
		statusOrDefault(ps.Status), versionOrDefault(ps.Version),
		ps.Source, ps.RoutineID, ps.RunID,
	)
	if err != nil {
		return fmt.Errorf("session.Create: insert session %s: %w", ps.ID, err)
	}
	return nil
}

// LoadManifest loads a PersistentSession by ID.
func (s *SQLiteSessionStore) LoadManifest(id string) (*PersistentSession, error) {
	var ps PersistentSession
	var createdAtStr, updatedAtStr string
	err := s.db.Read().QueryRow(`
		SELECT id, title, model, agent, created_at, updated_at, message_count,
		       last_message_id, workspace_root, workspace_name, status, version,
		       source, routine_id, run_id
		FROM sessions WHERE id = ?`, id).Scan(
		&ps.ID, &ps.Title, &ps.Model, &ps.Agent,
		&createdAtStr, &updatedAtStr, &ps.MsgCount,
		&ps.LastMsgID, &ps.Workspace, &ps.WsName,
		&ps.Status, &ps.Version,
		&ps.Source, &ps.RoutineID, &ps.RunID,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session sqlite: load manifest %s: not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("session sqlite: load manifest %s: %w", id, err)
	}
	if t, e := time.Parse(time.RFC3339, createdAtStr); e == nil {
		ps.CreatedAt = t.UTC()
	}
	if t, e := time.Parse(time.RFC3339, updatedAtStr); e == nil {
		ps.UpdatedAt = t.UTC()
	}
	if ps.Status == "" {
		ps.Status = "active"
	}
	if ps.Version == 0 {
		ps.Version = 1
	}
	return &ps, nil
}

// AppendMessage inserts a PersistedMessage using INSERT OR IGNORE.
func (s *SQLiteSessionStore) AppendMessage(sessionID string, msg *PersistedMessage) error {
	if msg.ID == "" {
		msg.ID = NewID()
	}
	if msg.Ts == "" {
		msg.Ts = time.Now().UTC().Format(time.RFC3339Nano)
	}

	_, err := s.db.Write().Exec(`
		INSERT OR IGNORE INTO messages
			(id, container_type, container_id, seq, ts, role, content,
			 agent, tool_name, tool_call_id, type,
			 prompt_tokens, completion_tokens, cost_usd, model)
		VALUES (?, 'session', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, sessionID, msg.Seq, msg.Ts,
		roleOrDefault(msg.Role), msg.Content,
		msg.Agent, msg.ToolName, msg.ToolCallID,
		msg.Type, msg.PromptTokens, msg.CompTokens, msg.CostUSD, msg.Model,
	)
	if err != nil {
		return fmt.Errorf("session.AppendMessage: insert message %s: %w", msg.ID, err)
	}
	return nil
}

// ReadMessages returns all messages for a session in chronological order.
func (s *SQLiteSessionStore) ReadMessages(sessionID string) ([]*PersistedMessage, error) {
	return s.queryPersistedMessages(sessionID, 0)
}

// ReadLastN returns the last n messages for a session in chronological order.
func (s *SQLiteSessionStore) ReadLastN(sessionID string, n int) ([]*PersistedMessage, error) {
	return s.queryPersistedMessages(sessionID, n)
}

func (s *SQLiteSessionStore) queryPersistedMessages(sessionID string, limit int) ([]*PersistedMessage, error) {
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.Read().Query(`
			SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
			       type, prompt_tokens, completion_tokens, cost_usd, model
			FROM (
				SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
				       type, prompt_tokens, completion_tokens, cost_usd, model
				FROM messages
				WHERE container_type = 'session' AND container_id = ?
				ORDER BY seq DESC LIMIT ?
			)
			ORDER BY seq ASC`, sessionID, limit)
	} else {
		rows, err = s.db.Read().Query(`
			SELECT id, ts, seq, role, content, agent, tool_name, tool_call_id,
			       type, prompt_tokens, completion_tokens, cost_usd, model
			FROM messages
			WHERE container_type = 'session' AND container_id = ?
			ORDER BY seq ASC`, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("session sqlite: read messages: %w", err)
	}
	defer rows.Close()

	var out []*PersistedMessage
	for rows.Next() {
		var msg PersistedMessage
		if err := rows.Scan(
			&msg.ID, &msg.Ts, &msg.Seq, &msg.Role, &msg.Content,
			&msg.Agent, &msg.ToolName, &msg.ToolCallID,
			&msg.Type, &msg.PromptTokens, &msg.CompTokens, &msg.CostUSD, &msg.Model,
		); err != nil {
			return nil, fmt.Errorf("session sqlite: scan message: %w", err)
		}
		out = append(out, &msg)
	}
	return out, rows.Err()
}

// RepairJSONL is a no-op for the SQLite store. Returns message count and nil.
func (s *SQLiteSessionStore) RepairJSONL(sessionID string) (int, error) {
	var count int
	if err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM messages WHERE container_type = 'session' AND container_id = ?`,
		sessionID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("session sqlite: repair jsonl count %s: %w", sessionID, err)
	}
	return count, nil
}

// UpdateManifest atomically reads and modifies a PersistentSession manifest.
func (s *SQLiteSessionStore) UpdateManifest(sessionID string, fn func(*PersistentSession)) error {
	ps, err := s.LoadManifest(sessionID)
	if err != nil {
		return fmt.Errorf("session.UpdateManifest: load %s: %w", sessionID, err)
	}
	fn(ps)
	ps.UpdatedAt = time.Now().UTC()
	return s.Create(ps)
}

// SearchSessions queries the sessions_fts full-text index using FTS5 MATCH syntax
// and returns the matching session manifests ordered by updated_at DESC.
// The query string is passed directly to the FTS MATCH operator, so callers
// can use FTS5 prefix queries (e.g. "hug*") and phrase queries ("exact phrase").
// Returns an empty slice (never nil) when no sessions match.
func (s *SQLiteSessionStore) SearchSessions(query string) ([]Manifest, error) {
	if query == "" {
		return []Manifest{}, nil
	}
	rows, err := s.db.Read().Query(`
		SELECT s.id, s.title, s.model, s.agent, s.created_at, s.updated_at,
		       s.message_count, s.last_message_id, s.workspace_root, s.workspace_name,
		       s.status, s.version, s.source, s.routine_id, s.run_id, s.space_id
		FROM sessions_fts
		JOIN sessions s ON s.id = sessions_fts.session_id
		WHERE sessions_fts MATCH ?
		ORDER BY s.updated_at DESC
		LIMIT 50`,
		query,
	)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: search sessions: %w", err)
	}
	defer rows.Close()

	var out []Manifest
	for rows.Next() {
		m, err := scanManifestRow(rows)
		if err != nil {
			return nil, fmt.Errorf("session sqlite: search sessions scan: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session sqlite: search sessions rows: %w", err)
	}
	if out == nil {
		out = []Manifest{}
	}
	return out, nil
}

// GetThreadReplyCounts returns a map of message ID → thread reply count for
// the given session. Only messages that have at least one reply are included.
func (s *SQLiteSessionStore) GetThreadReplyCounts(sessionID string) (map[string]int, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, thread_reply_count
		FROM messages
		WHERE container_type = 'session'
		  AND container_id = ?
		  AND thread_reply_count > 0`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("session sqlite: GetThreadReplyCounts %s: %w", sessionID, err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, fmt.Errorf("session sqlite: GetThreadReplyCounts scan: %w", err)
		}
		out[id] = count
	}
	return out, rows.Err()
}

// ReconcileThreadReplyCounts updates thread_reply_count for all messages that
// have threads pointing to them via the threads.parent_msg_id column. This
// fixes existing data where thread_reply_count was never incremented because
// the increment was missing from the original thread creation path.
// Safe to call on every startup — idempotent.
func (s *SQLiteSessionStore) ReconcileThreadReplyCounts() error {
	wdb := s.db.Write()
	if wdb == nil {
		return nil
	}
	_, err := wdb.Exec(`
		UPDATE messages
		SET thread_reply_count = (
			SELECT COUNT(*) FROM threads WHERE parent_msg_id = messages.id
		)
		WHERE id IN (
			SELECT parent_msg_id FROM threads WHERE parent_msg_id != ''
		)
		  AND thread_reply_count != (
			SELECT COUNT(*) FROM threads WHERE parent_msg_id = messages.id
		)`)
	if err != nil {
		return fmt.Errorf("session sqlite: reconcile thread reply counts: %w", err)
	}
	return nil
}

// Compile-time assertion: *SQLiteSessionStore must satisfy StoreInterface.
var _ StoreInterface = (*SQLiteSessionStore)(nil)
