// internal/session/migrations.go
package session

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/scrypster/huginn/internal/sqlitedb"
	sqlite "modernc.org/sqlite"
)

// Migrations returns the list of incremental schema migrations for existing
// Huginn installations. Fresh installs use the base schema DDL (ApplySchema)
// and do not run these migrations. Existing installs run them in order on first startup.
// All migrations are idempotent (IF NOT EXISTS / column-existence guards).
func Migrations() []sqlitedb.Migration {
	return []sqlitedb.Migration{
		{Name: "thread_columns_and_artifacts_v1", Up: migrateThreadColumnsAndArtifacts},
		{Name: "messages_type_thread_event_v1", Up: migrateMessagesTypeThreadEventV1},
		{Name: "delegations_session_id_v1", Up: migrateDelegationsSessionIDV1},
		{Name: "sessions_space_id_index_v1", Up: migrateAddSpaceIDIndex},
		{Name: "sessions_fts_v2", Up: migrateSessionsFTSv2},
		{Name: "memory_replication_queue_v1", Up: migrateMemoryReplicationQueueV1},
		{Name: "memory_replication_queue_v2", Up: migrateMemoryReplicationQueueV2},
		{Name: "cloud_vault_queue_v1", Up: migrateCloudVaultQueueV1},
		{Name: "connections_refresh_error_columns_v1", Up: migrateConnectionsRefreshErrorColumnsV1},
		{Name: "sessions_message_count_backfill_v1", Up: migrateSessionsMessageCountBackfillV1},
	}
}

// migrateMessagesTypeThreadEventV1 rebuilds the messages table so its type CHECK
// constraint accepts lifecycle entries (`thread_event`) in addition to normal and
// cost records. Existing databases shipped with CHECK(type IN (”, 'cost')), which
// silently dropped thread lifecycle rows due to INSERT OR IGNORE write paths.
func migrateMessagesTypeThreadEventV1(tx *sql.Tx) error {
	var ddl sql.NullString
	err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(ddl.String), "thread_event") {
		return nil
	}

	if _, err := tx.Exec(`ALTER TABLE messages RENAME TO messages_old`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE messages (
		    id              TEXT    NOT NULL PRIMARY KEY,
		    container_type  TEXT    NOT NULL
		                        CHECK (container_type IN ('session', 'team', 'thread')),
		    container_id    TEXT    NOT NULL,
		    tenant_id       TEXT    NOT NULL DEFAULT '',
		    seq             INTEGER NOT NULL,
		    ts              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		    role            TEXT    NOT NULL
		                        CHECK (role IN ('user', 'assistant', 'tool', 'system', 'cost')),
		    content         TEXT    NOT NULL DEFAULT '',
		    agent           TEXT    NOT NULL DEFAULT '',
		    tool_name       TEXT    NOT NULL DEFAULT '',
		    tool_call_id    TEXT    NOT NULL DEFAULT '',
		    tool_calls_json TEXT,
		    type            TEXT    NOT NULL DEFAULT ''
		                        CHECK (type IN ('', 'cost', 'thread_event')),
		    prompt_tokens   INTEGER NOT NULL DEFAULT 0,
		    completion_tokens INTEGER NOT NULL DEFAULT 0,
		    cost_usd        REAL    NOT NULL DEFAULT 0.0,
		    model           TEXT    NOT NULL DEFAULT '',
		    parent_message_id     TEXT,
		    triggering_message_id TEXT,
		    thread_reply_count    INTEGER NOT NULL DEFAULT 0,
		    thread_last_reply_at  TEXT,
		    UNIQUE (container_id, seq)
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO messages (
			id, container_type, container_id, tenant_id, seq, ts, role, content,
			agent, tool_name, tool_call_id, tool_calls_json, type,
			prompt_tokens, completion_tokens, cost_usd, model,
			parent_message_id, triggering_message_id, thread_reply_count, thread_last_reply_at
		)
		SELECT
			id, container_type, container_id, tenant_id, seq, ts, role, content,
			agent, tool_name, tool_call_id, tool_calls_json, type,
			prompt_tokens, completion_tokens, cost_usd, model,
			parent_message_id, triggering_message_id, thread_reply_count, thread_last_reply_at
		FROM messages_old`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE messages_old`); err != nil {
		return err
	}
	for _, ddl := range []string{
		`CREATE INDEX IF NOT EXISTS idx_messages_container_seq
		    ON messages (container_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_cost
		    ON messages (container_id, type)
		    WHERE type = 'cost'`,
		`CREATE INDEX IF NOT EXISTS idx_messages_tenant
		    ON messages (tenant_id, ts DESC)
		    WHERE tenant_id != ''`,
		`CREATE INDEX IF NOT EXISTS idx_messages_agent
		    ON messages (agent, ts DESC)
		    WHERE agent != ''`,
		`CREATE INDEX IF NOT EXISTS idx_messages_container_ts
		    ON messages (container_id, ts DESC, id DESC)
		    WHERE container_type = 'session'`,
		`CREATE INDEX IF NOT EXISTS idx_messages_thread_parent
		    ON messages (parent_message_id)
		    WHERE parent_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_parent_message
		    ON messages (parent_message_id)
		    WHERE parent_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_triggering_message
		    ON messages (triggering_message_id)
		    WHERE triggering_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_thread_replies
		    ON messages (parent_message_id, ts ASC)
		    WHERE parent_message_id IS NOT NULL`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateDelegationsSessionIDV1 adds a session_id column to the delegations
// table (for efficient per-session list queries) and creates two indexes:
//   - uq_delegations_thread: ensures at most one delegation record per thread_id
//   - idx_delegations_session: accelerates LIST queries ordered by creation time
//
// The delegations table already exists with started_at TEXT NOT NULL DEFAULT (...),
// so we use ALTER TABLE ADD COLUMN for the new column only.
func migrateDelegationsSessionIDV1(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE delegations ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isColumnAlreadyExistsError(err) {
			return err
		}
	}
	for _, ddl := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_delegations_thread ON delegations (thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delegations_session ON delegations (session_id, created_at DESC)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// migrateAddSpaceIDIndex creates an index on sessions.space_id to accelerate
// TailSpaceMessages queries that JOIN messages on sessions WHERE space_id = ?.
func migrateAddSpaceIDIndex(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_space_id ON sessions (space_id) WHERE space_id IS NOT NULL`)
	return err
}

// migrateConnectionsRefreshErrorColumnsV1 adds persistent refresh-failure fields
// to connections so proactive OAuth refresh failures can be surfaced and queried.
func migrateConnectionsRefreshErrorColumnsV1(tx *sql.Tx) error {
	var marker int
	err := tx.QueryRow(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='connections' LIMIT 1`).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, ddl := range []string{
		`ALTER TABLE connections ADD COLUMN refresh_failed_at TEXT`,
		`ALTER TABLE connections ADD COLUMN last_refresh_error TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			if !isColumnAlreadyExistsError(err) {
				return err
			}
		}
	}
	return nil
}

// migrateSessionsFTSv2 drops the contentless sessions_fts virtual table
// (content=”) and recreates it as a standard FTS5 table that stores column
// values. The contentless variant was written but could never return session_id
// or title values, causing SearchSessions to always return 0 results because
// the JOIN ON s.id = sessions_fts.session_id always failed.
//
// After recreating the table, all existing sessions are re-indexed so that
// searches work immediately without requiring a server restart.
func migrateSessionsFTSv2(tx *sql.Tx) error {
	// Drop the old contentless table (ignore "no such table" if never existed).
	if _, err := tx.Exec(`DROP TABLE IF EXISTS sessions_fts`); err != nil {
		return err
	}

	// Recreate as a standard FTS5 table (no content='', columns are stored).
	if _, err := tx.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
			session_id UNINDEXED,
			space_id   UNINDEXED,
			title
		)`); err != nil {
		// If it already exists as the new form, that's fine.
		if !isFTSAlreadyExistsError(err) {
			return err
		}
	}

	// Re-index all existing sessions so searches work immediately.
	rows, err := tx.Query(`SELECT id, space_id, title FROM sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id, title string
		var spaceID sql.NullString
		if err := rows.Scan(&id, &spaceID, &title); err != nil {
			return err
		}
		// DELETE any stale row first (safe no-op if not present).
		if _, err := tx.Exec(`DELETE FROM sessions_fts WHERE session_id = ?`, id); err != nil {
			return err
		}
		var spaceIDVal *string
		if spaceID.Valid {
			spaceIDVal = &spaceID.String
		}
		if _, err := tx.Exec(
			`INSERT INTO sessions_fts(session_id, space_id, title) VALUES (?, ?, ?)`,
			id, spaceIDVal, title,
		); err != nil {
			return err
		}
	}
	return rows.Err()
}

// migrateThreadColumnsAndArtifacts adds threading columns to the messages
// table and creates the delegations table for existing databases.
// All DDL uses IF NOT EXISTS / column-existence checks so the migration is
// idempotent — safe to re-run on databases that already have the schema.
//
// Note: The artifacts table is created by ApplySchema (Section 12a of the DDL).
// This migration only handles columns and tables that were added after the
// initial schema was released.
func migrateThreadColumnsAndArtifacts(tx *sql.Tx) error {
	// Add thread columns to messages (idempotent: ALTER TABLE ADD COLUMN is
	// a no-op error when the column already exists — we catch and ignore it).
	threadCols := []string{
		`ALTER TABLE messages ADD COLUMN parent_message_id TEXT`,
		`ALTER TABLE messages ADD COLUMN triggering_message_id TEXT`,
		`ALTER TABLE messages ADD COLUMN thread_reply_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN thread_last_reply_at TEXT`,
	}
	for _, ddl := range threadCols {
		if _, err := tx.Exec(ddl); err != nil {
			if !isColumnAlreadyExistsError(err) {
				return err
			}
		}
	}

	// Indexes for thread reply lookups (also removed from schema.sql to avoid
	// ApplySchema failures on upgraded databases that don't yet have the columns).
	for _, idxDDL := range []string{
		`CREATE INDEX IF NOT EXISTS idx_messages_thread_parent
		    ON messages (parent_message_id)
		    WHERE parent_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_parent_message
		    ON messages (parent_message_id)
		    WHERE parent_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_triggering_message
		    ON messages (triggering_message_id)
		    WHERE triggering_message_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_messages_thread_replies
		    ON messages (parent_message_id, ts ASC)
		    WHERE parent_message_id IS NOT NULL`,
	} {
		if _, err := tx.Exec(idxDDL); err != nil {
			return err
		}
	}

	// Create the delegations table (tracks who delegated what to whom).
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS delegations (
		    id                  TEXT    NOT NULL PRIMARY KEY,
		    thread_id           TEXT    NOT NULL,
		    from_agent          TEXT    NOT NULL,
		    to_agent            TEXT    NOT NULL,
		    task                TEXT    NOT NULL DEFAULT '',
		    objective           TEXT    NOT NULL DEFAULT '',
		    context             TEXT    NOT NULL DEFAULT '',
		    expected_output_kinds TEXT NOT NULL DEFAULT '[]',
		    produced_artifact_ids TEXT NOT NULL DEFAULT '[]',
		    status              TEXT    NOT NULL DEFAULT 'pending'
		                            CHECK (status IN ('pending', 'in_progress', 'completed', 'failed')),
		    result              TEXT    NOT NULL DEFAULT '',
		    started_at          TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		    completed_at        TEXT,
		    created_at          TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
		)`); err != nil {
		return err
	}

	delegationIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_delegations_pair ON delegations (from_agent, to_agent, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_delegations_thread ON delegations (thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delegations_status ON delegations (status) WHERE status IN ('pending', 'in_progress')`,
	}
	for _, ddl := range delegationIndexes {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}

	return nil
}

// migrateSessionsMessageCountBackfillV1 recomputes sessions.message_count /
// last_message_id / updated_at from the messages table. Append used to bump
// those fields only in memory, so existing rows froze at create-time zeros
// while transcripts accumulated.
func migrateSessionsMessageCountBackfillV1(tx *sql.Tx) error {
	var marker int
	err := tx.QueryRow(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='sessions' LIMIT 1`).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		UPDATE sessions
		SET message_count = (
		        SELECT COUNT(*) FROM messages
		        WHERE messages.container_type = 'session'
		          AND messages.container_id = sessions.id
		    ),
		    last_message_id = COALESCE((
		        SELECT id FROM messages
		        WHERE messages.container_type = 'session'
		          AND messages.container_id = sessions.id
		        ORDER BY seq DESC, ts DESC
		        LIMIT 1
		    ), last_message_id),
		    updated_at = COALESCE((
		        SELECT ts FROM messages
		        WHERE messages.container_type = 'session'
		          AND messages.container_id = sessions.id
		        ORDER BY seq DESC, ts DESC
		        LIMIT 1
		    ), updated_at)`)
	return err
}

// isColumnAlreadyExistsError returns true when an ALTER TABLE ADD COLUMN fails
// because the column already exists (duplicate column name).
func isColumnAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "already exists")
}

// migrateMemoryReplicationQueueV1 creates an initial memory_replication_queue
// table (preliminary schema — superseded by v2 which replaces it with the
// production design). Kept for idempotency on databases that already ran v1.
func migrateMemoryReplicationQueueV1(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS memory_replication_queue (
		    id              TEXT    NOT NULL PRIMARY KEY,
		    session_id      TEXT    NOT NULL,
		    agent_id        TEXT    NOT NULL,
		    vault_name      TEXT    NOT NULL,
		    operation       TEXT    NOT NULL
		                        CHECK (operation IN ('insert', 'update', 'delete')),
		    memory_id       TEXT    NOT NULL,
		    memory_content  TEXT    NOT NULL DEFAULT '',
		    status          TEXT    NOT NULL DEFAULT 'pending'
		                        CHECK (status IN ('pending', 'in_progress', 'completed', 'failed')),
		    error_message   TEXT    NOT NULL DEFAULT '',
		    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`); err != nil {
		return err
	}
	return nil
}

// migrateMemoryReplicationQueueV2 replaces the preliminary v1 schema with the
// production design: integer timestamps, dedup UNIQUE constraint, status enum,
// backoff columns, and a partial drain index. Drops the old table first.
func migrateMemoryReplicationQueueV2(tx *sql.Tx) error {
	// Drop old preliminary table (may not exist on fresh installs — IF EXISTS is safe).
	if _, err := tx.Exec(`DROP TABLE IF EXISTS memory_replication_queue`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS memory_replication_queue (
		    id            INTEGER PRIMARY KEY,
		    target_vault  TEXT    NOT NULL,
		    source_agent  TEXT    NOT NULL,
		    space_id      TEXT    NOT NULL,
		    concept_key   TEXT    NOT NULL,
		    payload       TEXT    NOT NULL,
		    operation     TEXT    NOT NULL DEFAULT 'remember',
		    status        TEXT    NOT NULL DEFAULT 'pending',
		    attempts      INTEGER NOT NULL DEFAULT 0,
		    max_attempts  INTEGER NOT NULL DEFAULT 5,
		    next_retry_at INTEGER NOT NULL,
		    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
		    UNIQUE(target_vault, concept_key, space_id)
		)`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_mrq_drain
		    ON memory_replication_queue(status, next_retry_at)
		    WHERE status = 'pending'`); err != nil {
		return err
	}

	return nil
}

// migrateCloudVaultQueueV1 creates the cloud_vault_queue table used by the
// agents.CloudVaultReplicator to push memory operations to HuginnCloud.
// Separate from memory_replication_queue (channel-member replication) to avoid
// schema conflicts. UNIQUE(vault_name, memory_id) enables idempotent upserts.
func migrateCloudVaultQueueV1(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS cloud_vault_queue (
		    id             TEXT    NOT NULL PRIMARY KEY,          -- ULID
		    session_id     TEXT    NOT NULL,
		    agent_id       TEXT    NOT NULL,                      -- agent name
		    vault_name     TEXT    NOT NULL,
		    operation      TEXT    NOT NULL
		                       CHECK (operation IN ('insert', 'update', 'delete')),
		    memory_id      TEXT    NOT NULL,
		    concept        TEXT    NOT NULL DEFAULT '',
		    memory_content TEXT    NOT NULL DEFAULT '',
		    status         TEXT    NOT NULL DEFAULT 'pending'
		                       CHECK (status IN ('pending', 'in_progress', 'completed', 'failed', 'dead')),
		    error_message  TEXT    NOT NULL DEFAULT '',
		    attempts       INTEGER NOT NULL DEFAULT 0,
		    max_attempts   INTEGER NOT NULL DEFAULT 5,
		    next_retry_at  INTEGER NOT NULL DEFAULT 0,            -- unix epoch seconds
		    created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
		    updated_at     INTEGER NOT NULL DEFAULT (unixepoch()),
		    UNIQUE(vault_name, memory_id)
		)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_cvq_drain
		    ON cloud_vault_queue(status, next_retry_at)
		    WHERE status IN ('pending', 'in_progress')`); err != nil {
		return err
	}
	return nil
}

// isFTSAlreadyExistsError checks if an error indicates that an FTS table
// already exists. modernc.org/sqlite returns *sqlite.Error with Code() == 1
// (SQLITE_ERROR) for "table already exists"; we also check message patterns
// as a fallback.
func isFTSAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}

	// Check for modernc.org/sqlite error code 1 (SQLITE_ERROR)
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		if sqliteErr.Code() == 1 { // SQLITE_ERROR
			return true
		}
	}

	// Fallback: check message patterns (case-insensitive)
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "table already exists") ||
		strings.Contains(msg, "duplicate")
}
