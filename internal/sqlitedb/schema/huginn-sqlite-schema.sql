-- =============================================================================
-- Huginn SQLite Schema — Enterprise-Grade Local AI Assistant
-- =============================================================================
--
-- Version:  1
-- Date:     2026-03-10
-- Author:   Schema design for huginn local AI satellite binary
--
-- Conventions:
--   - All IDs are TEXT (ULIDs) — lexicographically time-ordered
--   - All timestamps are TEXT in RFC 3339 format (e.g. "2026-03-10T14:32:01.234Z")
--   - Sequence numbers are INTEGER (monotonic within a container)
--   - Costs are REAL (USD)
--   - JSON arrays stored as TEXT (JSON-encoded) — e.g. files_modified, key_decisions
--   - tenant_id is present on all data tables for future cloud-hosted multi-tenant
--   - Foreign keys are enforced via PRAGMA
--
-- =============================================================================


-- =============================================================================
-- Section 0: Connection Setup (run on every connection open)
-- =============================================================================
--
-- These PRAGMAs MUST be executed on every new database connection, not just at
-- database creation time. They are not stored in the database file. The Go
-- driver (modernc.org/sqlite or mattn/go-sqlite3) should run these in the
-- ConnInitHook / connection interceptor.
--
-- PRAGMA journal_mode = WAL;
--   WAL (Write-Ahead Logging) allows concurrent reads during writes. Critical
--   for huginn where the relay reader, TUI, and agent goroutines all access
--   the DB simultaneously. WAL persists across connections once set, but we
--   re-assert it defensively.
--
-- PRAGMA synchronous = NORMAL;
--   In WAL mode, NORMAL gives durability guarantees equivalent to full sync
--   in rollback journal mode. FULL is unnecessarily slow for a local binary.
--
-- PRAGMA foreign_keys = ON;
--   SQLite does NOT enforce foreign keys by default. This is per-connection.
--   Without this, CASCADE deletes and constraint checks silently do nothing.
--
-- PRAGMA busy_timeout = 5000;
--   Wait up to 5 seconds for a write lock instead of immediately returning
--   SQLITE_BUSY. Essential when multiple goroutines write concurrently.
--
-- PRAGMA cache_size = -8000;
--   8 MB page cache (negative = KB). Default is 2 MB. Larger cache reduces
--   I/O for repeated queries on messages tables.
--
-- PRAGMA mmap_size = 134217728;
--   128 MB memory-mapped I/O. Significantly speeds up reads on modern OSes.
--   Safe for a local single-user binary. Not recommended for NFS mounts.
--
-- PRAGMA temp_store = MEMORY;
--   Temporary tables and indices in memory. Faster sorts and GROUP BY.
--
-- PRAGMA wal_autocheckpoint = 1000;
--   Checkpoint the WAL every 1000 pages (~4 MB). Default is 1000 but we
--   state it explicitly. Prevents WAL from growing unbounded during long
--   agent sessions with heavy message writes.
--
-- PRAGMA optimize;
--   Run on connection close (not open). Lets SQLite update statistics for
--   the query planner. Cheap, idempotent, and improves query plans over time.
--
-- =============================================================================


-- =============================================================================
-- Section 1: Schema Versioning
-- =============================================================================
-- Single-row table for tracking schema version. Go migration runner reads
-- this to determine which migrations to apply. Version 0 means fresh DB.

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER NOT NULL DEFAULT 0,
    migrated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (version >= 0)
);

INSERT INTO schema_version (version, migrated_at)
SELECT 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE NOT EXISTS (SELECT 1 FROM schema_version);


-- =============================================================================
-- Section 2: Containers — sessions, teams, threads
-- =============================================================================
--
-- DESIGN DECISION: Unified "containers" approach.
--
-- Sessions, teams, and threads are all *conversation containers* — they hold
-- ordered sequences of messages. Rather than three separate message tables,
-- we use one `messages` table with a `container_id` FK. This simplifies:
--   - Message pagination (one query pattern)
--   - Cost aggregation (one SUM query with GROUP BY)
--   - Relay sync (one cursor per container)
--   - Migration from existing JSONL (one target format)
--
-- The container_id is always a ULID that maps to exactly one of:
-- sessions.id, teams.id, or threads.id. We do NOT use a polymorphic FK
-- (SQLite can't enforce those). Instead, each container table has its own
-- messages relationship, and the messages table has a `container_type`
-- discriminator for query convenience. Referential integrity is maintained
-- by application-level checks and the container_type CHECK constraint.
--
-- WHY NOT a single "containers" base table? Because sessions, teams, and
-- threads have very different metadata. A shared base table would be mostly
-- NULL columns. Three typed tables with a shared messages table is cleaner.
-- =============================================================================


-- ---------------------------------------------------------------------------
-- 2a. Sessions
-- ---------------------------------------------------------------------------
-- A session is a 1:1 conversation between the user and a primary agent.
-- This is the most common container type and maps directly to the existing
-- manifest.json + messages.jsonl filesystem layout.

CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT    NOT NULL PRIMARY KEY,  -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',   -- empty for local; future cloud isolation
    title           TEXT    NOT NULL DEFAULT '',
    model           TEXT    NOT NULL DEFAULT '',   -- primary model ID (e.g. "qwen3-coder:30b")
    agent           TEXT    NOT NULL DEFAULT '',   -- primary agent name (e.g. "Alex")
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    message_count   INTEGER NOT NULL DEFAULT 0,
    last_message_id TEXT    NOT NULL DEFAULT '',   -- ULID of most recent message
    workspace_root  TEXT    NOT NULL DEFAULT '',   -- filesystem path, not a DB entity
    workspace_name  TEXT    NOT NULL DEFAULT '',   -- basename of workspace_root
    status          TEXT    NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'archived', 'closed')),
    version         INTEGER NOT NULL DEFAULT 1,    -- schema version for forward compat

    -- Summary for context replay on resume. Computed periodically (every N
    -- messages) by a background summarization pass. NULL means no summary
    -- has been generated yet (new session or very short).
    summary         TEXT,                          -- rolling conversation summary
    summary_through TEXT,                          -- ULID of last message included in summary

    -- Routine linkage. Empty strings mean user-initiated session.
    source          TEXT    NOT NULL DEFAULT ''     -- "routine" | ""
                        CHECK (source IN ('', 'routine')),
    routine_id      TEXT    NOT NULL DEFAULT '',    -- ULID of the Routine definition
    run_id          TEXT    NOT NULL DEFAULT '',    -- ULID for this specific routine execution

    -- Space membership. NULL means the session is not in any space.
    space_id        TEXT    DEFAULT NULL            -- ULID of the Space this session belongs to
);

-- Hot path: list sessions newest first (session picker, relay sync)
CREATE INDEX IF NOT EXISTS idx_sessions_updated
    ON sessions (tenant_id, updated_at DESC);

-- Filter by status (e.g. "show only active sessions")
CREATE INDEX IF NOT EXISTS idx_sessions_status
    ON sessions (tenant_id, status, updated_at DESC);

-- Find all sessions spawned by a routine
CREATE INDEX IF NOT EXISTS idx_sessions_routine
    ON sessions (routine_id)
    WHERE routine_id != '';

-- Find sessions by workspace (common: "show sessions for this project")
CREATE INDEX IF NOT EXISTS idx_sessions_workspace
    ON sessions (tenant_id, workspace_root)
    WHERE workspace_root != '';

-- Find sessions by space
CREATE INDEX IF NOT EXISTS idx_sessions_space
    ON sessions (space_id)
    WHERE space_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2a-fts. Session Full-Text Search (FTS5)
-- ---------------------------------------------------------------------------
-- Contentless FTS5 table for searching session titles by space.
-- The Go layer keeps this in sync by calling INSERT OR REPLACE whenever
-- SaveManifest runs, and DELETE whenever a session is deleted.

CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts
    USING fts5(session_id UNINDEXED, space_id UNINDEXED, title);


-- ---------------------------------------------------------------------------
-- 2b. Teams
-- ---------------------------------------------------------------------------
-- A team is a multi-agent "chat room." Think Slack channel with AI participants.
-- The user can observe, interject, and direct agents. Unlike a session (1 user
-- + 1 primary agent), a team has N agents collaborating on a shared objective.
--
-- DESIGN DECISION: Teams own threads, not sessions.
-- When a team is active, agent delegation creates threads parented to the team,
-- not to a session. This keeps the team as the single container that tracks all
-- collaborative work. A team can optionally be linked to a parent session
-- (the user started a session, then spawned a team from it).
--
-- DESIGN DECISION: Team messages go in the shared `messages` table.
-- The "main room" messages (user interjections, agent announcements, system
-- events) are stored with container_type='team' and container_id=team.id.
-- Thread messages use container_type='thread'. This means you can reconstruct
-- the full team timeline by querying messages WHERE container_id IN (team_id,
-- ...thread_ids...) ORDER BY seq.

CREATE TABLE IF NOT EXISTS teams (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',
    title           TEXT    NOT NULL DEFAULT '',    -- user-facing name
    objective       TEXT    NOT NULL DEFAULT '',    -- the task/goal for this team
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    status          TEXT    NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'completed', 'archived', 'cancelled')),
    message_count   INTEGER NOT NULL DEFAULT 0,
    last_message_id TEXT    NOT NULL DEFAULT '',

    -- Optional link to the session that spawned this team.
    -- NULL means the team was created standalone.
    parent_session_id TEXT  DEFAULT NULL
                        REFERENCES sessions(id) ON DELETE SET NULL,

    -- Workspace context (same as sessions — where the work happens)
    workspace_root  TEXT    NOT NULL DEFAULT '',
    workspace_name  TEXT    NOT NULL DEFAULT '',

    -- Summary for context building when new agents join mid-team
    summary         TEXT,
    summary_through TEXT,

    -- Cost tracking at the team level (sum of all threads + main room)
    total_cost_usd  REAL    NOT NULL DEFAULT 0.0,
    cost_budget_usd REAL    NOT NULL DEFAULT 0.0   -- 0 = unlimited
);

CREATE INDEX IF NOT EXISTS idx_teams_updated
    ON teams (tenant_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_teams_status
    ON teams (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_teams_parent_session
    ON teams (parent_session_id)
    WHERE parent_session_id IS NOT NULL;


-- ---------------------------------------------------------------------------
-- 2c. Team Members
-- ---------------------------------------------------------------------------
-- Junction table: which agents are in which team, and what role they play.
-- An agent can be in multiple teams. A team can have multiple agents.
--
-- DESIGN DECISION: role is a simple enum, not a complex permissions model.
-- "lead" = the primary/orchestrating agent (equivalent to session.agent).
-- "member" = a participating specialist agent.
-- "observer" = can read but not post (future: human observers in cloud mode).

CREATE TABLE IF NOT EXISTS team_members (
    team_id     TEXT    NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    agent_name  TEXT    NOT NULL,                -- matches agents.AgentDef.Name
    role        TEXT    NOT NULL DEFAULT 'member'
                    CHECK (role IN ('lead', 'member', 'observer')),
    joined_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    left_at     TEXT,                            -- NULL = still active

    PRIMARY KEY (team_id, agent_name)
);

-- Find all teams an agent participates in
CREATE INDEX IF NOT EXISTS idx_team_members_agent
    ON team_members (agent_name, team_id);


-- ---------------------------------------------------------------------------
-- 2d. Threads
-- ---------------------------------------------------------------------------
-- A thread is a sub-task spawned by an agent within a session or team.
-- It runs independently with its own agent, LLM context, and message stream.
-- On completion it produces a structured summary (FinishSummary).
--
-- DESIGN DECISION: parent_type + parent_id polymorphic FK.
-- A thread can be parented to either a session or a team. SQLite doesn't
-- support multi-table FK constraints, so we use a discriminator column
-- (parent_type) and enforce integrity at the application level. This is the
-- standard pattern for polymorphic associations in SQLite.
--
-- DESIGN DECISION: Thread IDs are ULIDs, not "t-1" counters.
-- The existing codebase uses "t-1", "t-2" etc. (atomic counter). For the
-- DB, we switch to ULIDs for global uniqueness, time-ordering, and
-- consistency with sessions/teams. The migration maps old IDs to new ULIDs.

CREATE TABLE IF NOT EXISTS threads (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID (was "t-N" in-memory)
    tenant_id       TEXT    NOT NULL DEFAULT '',

    -- Parent container (session or team)
    parent_type     TEXT    NOT NULL
                        CHECK (parent_type IN ('session', 'team')),
    parent_id       TEXT    NOT NULL,              -- ULID of parent session or team

    agent_name      TEXT    NOT NULL DEFAULT '',    -- which agent runs this thread
    task            TEXT    NOT NULL DEFAULT '',    -- the delegated task description
    status          TEXT    NOT NULL DEFAULT 'queued'
                        CHECK (status IN (
                            'queued', 'thinking', 'tooling', 'done',
                            'blocked', 'cancelled', 'error', 'interrupted'
                        )),

    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at      TEXT,                          -- NULL until status leaves 'queued'
    completed_at    TEXT,                          -- NULL until terminal status

    -- Token accounting
    token_budget    INTEGER NOT NULL DEFAULT 0,    -- 0 = unlimited
    tokens_used     INTEGER NOT NULL DEFAULT 0,

    -- Cost tracking
    cost_usd        REAL    NOT NULL DEFAULT 0.0,

    -- Structured completion summary (populated on done/error/interrupted)
    -- These are TEXT columns holding JSON arrays for files_modified,
    -- key_decisions, and artifacts. We use JSON rather than junction tables
    -- because:
    --   1. These are write-once (set on completion, never updated)
    --   2. They are always read as a unit (never queried individually)
    --   3. Junction tables would add 3 more tables for minimal query benefit
    --   4. SQLite's json_each() can query into them if needed
    summary_text    TEXT,                          -- human-readable narrative
    summary_status  TEXT                           -- "completed"|"blocked"|"needs_review"|
                        CHECK (summary_status IS NULL OR summary_status IN (
                            'completed', 'blocked', 'needs_review',
                            'completed-with-timeout', 'error'
                        )),
    files_modified  TEXT    NOT NULL DEFAULT '[]', -- JSON array of file paths
    key_decisions   TEXT    NOT NULL DEFAULT '[]', -- JSON array of decision strings
    artifacts       TEXT    NOT NULL DEFAULT '[]', -- JSON array of artifact references

    -- Per-thread timeout stored as nanoseconds (time.Duration). 0 = no timeout.
    timeout_ns      INTEGER NOT NULL DEFAULT 0,

    -- Chat message that triggered this thread (for sub-thread navigation).
    parent_msg_id   TEXT    NOT NULL DEFAULT '',

    message_count   INTEGER NOT NULL DEFAULT 0,
    last_message_id TEXT    NOT NULL DEFAULT ''
);

-- List threads by parent (common: "show all threads in this session/team")
CREATE INDEX IF NOT EXISTS idx_threads_parent
    ON threads (parent_type, parent_id, created_at);

-- Find threads by status (common: DAG evaluation — find all queued threads)
CREATE INDEX IF NOT EXISTS idx_threads_status
    ON threads (parent_id, status)
    WHERE status NOT IN ('done', 'cancelled', 'error', 'interrupted');

-- Find threads by agent (common: "what has Stacy been working on?")
CREATE INDEX IF NOT EXISTS idx_threads_agent
    ON threads (agent_name, created_at DESC);


-- ---------------------------------------------------------------------------
-- 2e. Thread Dependencies (DAG edges)
-- ---------------------------------------------------------------------------
-- Models the depends_on relationship between threads. This is a DAG
-- (Directed Acyclic Graph): thread B depends on thread A means B cannot
-- start until A reaches status 'done'.
--
-- DESIGN DECISION: Separate junction table, not a JSON array.
-- The existing code stores depends_on as []string on the Thread struct.
-- In SQLite, we normalize this into a proper edge table because:
--   1. The DAG evaluator needs to query "find all threads blocked by X"
--      (reverse lookup) — impossible to do efficiently with JSON arrays
--   2. We need to enforce that dependency targets actually exist
--   3. Cycle detection queries become simple graph traversals
--
-- NOTE: Cycle detection is NOT enforced by the schema — it's enforced by
-- the ThreadManager in Go before inserting edges. SQLite has no built-in
-- DAG constraint. The application must validate acyclicity on INSERT.

CREATE TABLE IF NOT EXISTS thread_deps (
    thread_id   TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depends_on  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,

    PRIMARY KEY (thread_id, depends_on),
    CHECK (thread_id != depends_on)  -- no self-dependencies
);

-- Reverse lookup: "which threads are waiting on thread X to complete?"
CREATE INDEX IF NOT EXISTS idx_thread_deps_upstream
    ON thread_deps (depends_on);


-- =============================================================================
-- Section 3: Messages
-- =============================================================================
--
-- DESIGN DECISION: One messages table for all container types.
--
-- Sessions, teams, and threads all produce ordered message sequences with
-- identical structure: id, timestamp, seq, role, content, agent, tool info,
-- cost fields. Having three separate tables would triplicate every query,
-- every index, and every migration.
--
-- The container_type + container_id pair identifies which container owns
-- the message. The seq counter is monotonic WITHIN a container (not globally).
--
-- ORDERING STRATEGY:
-- Messages are ordered by (container_id, seq ASC) for pagination within a
-- container. The seq is an application-assigned monotonic int64, incremented
-- by the Go code (atomic.AddInt64). ULIDs provide a secondary time-based
-- ordering and are globally unique across all containers.
--
-- For "show me the full team timeline" queries that span a team + its threads,
-- the application queries by container_id IN (...) and orders by the ULID id
-- column (which is lexicographically time-ordered) or by the ts column.
--
-- COST RECORDS:
-- Cost records are stored as messages with type='cost'. This preserves the
-- existing pattern from messages.jsonl. They have role='system' (or 'cost'),
-- no content, and populated token/cost fields. This means cost aggregation
-- is a simple: SELECT SUM(cost_usd) FROM messages WHERE container_id = ?
-- AND type = 'cost'.

CREATE TABLE IF NOT EXISTS messages (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    container_type  TEXT    NOT NULL
                        CHECK (container_type IN ('session', 'team', 'thread')),
    container_id    TEXT    NOT NULL,               -- ULID of parent session/team/thread
    tenant_id       TEXT    NOT NULL DEFAULT '',

    seq             INTEGER NOT NULL,               -- monotonic within container
    ts              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    -- Message content
    role            TEXT    NOT NULL
                        CHECK (role IN ('user', 'assistant', 'tool', 'system', 'cost')),
    content         TEXT    NOT NULL DEFAULT '',
    agent           TEXT    NOT NULL DEFAULT '',     -- which agent wrote this

    -- Tool call fields (populated for role='tool' or assistant messages with tool_calls)
    tool_name       TEXT    NOT NULL DEFAULT '',
    tool_call_id    TEXT    NOT NULL DEFAULT '',
    tool_calls_json TEXT,                           -- JSON array of tool calls (for assistant msgs)

    -- Cost record fields (populated when type='cost')
    type            TEXT    NOT NULL DEFAULT ''      -- 'cost' for cost records, '' for normal
                        CHECK (type IN ('', 'cost')),
    prompt_tokens   INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd        REAL    NOT NULL DEFAULT 0.0,
    model           TEXT    NOT NULL DEFAULT '',     -- model used for this completion

    -- Thread reply columns (NULL for top-level messages)
    parent_message_id     TEXT,                     -- parent message for threaded replies
    triggering_message_id TEXT,                     -- message that triggered a sub-thread
    thread_reply_count    INTEGER NOT NULL DEFAULT 0,
    thread_last_reply_at  TEXT,

    -- Ensure seq is unique within a container (the primary ordering guarantee)
    UNIQUE (container_id, seq)
);

-- THE critical index: paginate messages within a container.
-- This is a covering index for the most common query pattern:
-- SELECT * FROM messages WHERE container_id = ? ORDER BY seq ASC LIMIT ? OFFSET ?
CREATE INDEX IF NOT EXISTS idx_messages_container_seq
    ON messages (container_id, seq);

-- Fetch cost records for aggregation (per-container cost rollup)
CREATE INDEX IF NOT EXISTS idx_messages_cost
    ON messages (container_id, type)
    WHERE type = 'cost';

-- Tenant-scoped queries (future cloud mode: list all messages for a tenant)
CREATE INDEX IF NOT EXISTS idx_messages_tenant
    ON messages (tenant_id, ts DESC)
    WHERE tenant_id != '';

-- Agent attribution queries ("show me everything Alex said")
CREATE INDEX IF NOT EXISTS idx_messages_agent
    ON messages (agent, ts DESC)
    WHERE agent != '';

-- Space timeline queries: resolve session messages ordered by ts for a space.
-- Combined with idx_sessions_space, enables cross-session timeline pagination
-- without a full table scan.
CREATE INDEX IF NOT EXISTS idx_messages_container_ts
    ON messages (container_id, ts DESC, id DESC)
    WHERE container_type = 'session';

-- Thread reply lookups (find replies to a message, or triggering message)
CREATE INDEX IF NOT EXISTS idx_messages_thread_parent
    ON messages (parent_message_id)
    WHERE parent_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_parent_message
    ON messages (parent_message_id)
    WHERE parent_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_triggering_message
    ON messages (triggering_message_id)
    WHERE triggering_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_thread_replies
    ON messages (parent_message_id, ts ASC)
    WHERE parent_message_id IS NOT NULL;


-- =============================================================================
-- Section 4: Routines
-- =============================================================================
--
-- Routines are cron-scheduled automated tasks. They exist as YAML files on
-- disk today. We mirror them into SQLite for:
--   1. Queryable run history (which routine ran when, what session did it create)
--   2. Relay sync (cloud UI needs to show routine status)
--   3. Enable/disable without touching YAML files
--
-- DESIGN DECISION: routines table is a cache/mirror of the YAML files.
-- YAML remains the source of truth for routine definitions. The DB stores
-- a snapshot for query purposes. A sync job (on startup and on YAML change)
-- upserts from YAML into the DB.
--
-- Routine runs are tracked separately in routine_runs, linked to the session
-- they spawned. This replaces the source/routine_id/run_id fields on sessions
-- (which are kept for backward compat but the runs table is authoritative).

CREATE TABLE IF NOT EXISTS routines (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID (stable across YAML reloads)
    tenant_id       TEXT    NOT NULL DEFAULT '',
    slug            TEXT    NOT NULL,               -- filename-derived, human-readable
    name            TEXT    NOT NULL DEFAULT '',
    description     TEXT    NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 0,     -- 0=disabled, 1=enabled
    trigger_mode    TEXT    NOT NULL DEFAULT 'schedule'
                        CHECK (trigger_mode IN ('schedule', 'heartbeat')),
    cron_expr       TEXT    NOT NULL DEFAULT '',     -- cron expression
    agent           TEXT    NOT NULL DEFAULT '',     -- agent to run
    prompt          TEXT    NOT NULL DEFAULT '',     -- task prompt
    workspace       TEXT    NOT NULL DEFAULT '',     -- workspace path
    max_tokens      INTEGER NOT NULL DEFAULT 0,     -- 0 = unlimited
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

    UNIQUE (tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS routine_runs (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID (the run_id)
    routine_id      TEXT    NOT NULL REFERENCES routines(id) ON DELETE CASCADE,
    session_id      TEXT    REFERENCES sessions(id) ON DELETE SET NULL,
    tenant_id       TEXT    NOT NULL DEFAULT '',
    status          TEXT    NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    started_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at    TEXT,
    cost_usd        REAL    NOT NULL DEFAULT 0.0,
    error_message   TEXT    NOT NULL DEFAULT '',

    -- Summary of what the routine produced (extracted from session)
    summary         TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_routine_runs_routine
    ON routine_runs (routine_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_routine_runs_session
    ON routine_runs (session_id)
    WHERE session_id IS NOT NULL;


-- =============================================================================
-- Section 5: Relay State
-- =============================================================================
--
-- DESIGN DECISION: Minimal relay state in DB, not all of it.
--
-- The relay (WebSocket connection between huginn and huginncloud) needs to
-- track "where did I leave off?" for delta sync. The existing Pebble-based
-- SessionStore tracks this as SessionMeta {ID, StartedAt, LastSeq, Status}.
--
-- What belongs in the DB:
--   - last_synced_seq per container: enables relay to resume from where it
--     left off after disconnect, without re-sending all messages
--   - relay_status: whether the container is actively being synced
--
-- What stays in-memory (NOT in DB):
--   - WebSocket connection state (connected/disconnected)
--   - Outbox queue (messages pending send) — these are transient
--   - Heartbeat timing
--
-- The relay_cursors table replaces the Pebble SessionStore entirely. It
-- tracks sync progress per container, not just per session.

CREATE TABLE IF NOT EXISTS relay_cursors (
    container_id    TEXT    NOT NULL PRIMARY KEY,   -- session/team/thread ULID
    container_type  TEXT    NOT NULL
                        CHECK (container_type IN ('session', 'team', 'thread')),
    tenant_id       TEXT    NOT NULL DEFAULT '',
    last_synced_seq INTEGER NOT NULL DEFAULT 0,    -- highest seq successfully sent to cloud
    last_synced_at  TEXT,                          -- when the last sync happened
    relay_status    TEXT    NOT NULL DEFAULT 'idle'
                        CHECK (relay_status IN ('idle', 'syncing', 'paused', 'error'))
);


-- =============================================================================
-- Section 6: Cost Aggregation View
-- =============================================================================
--
-- Materialized-style view for quick cost lookups. SQLite views are not
-- materialized, but this view encapsulates the query pattern so Go code
-- doesn't duplicate it. For hot-path cost display, the application should
-- cache the result rather than re-querying on every render frame.

CREATE VIEW IF NOT EXISTS v_container_costs AS
SELECT
    container_id,
    container_type,
    SUM(prompt_tokens)     AS total_prompt_tokens,
    SUM(completion_tokens) AS total_completion_tokens,
    SUM(cost_usd)          AS total_cost_usd,
    COUNT(*)               AS cost_record_count
FROM messages
WHERE type = 'cost'
GROUP BY container_id, container_type;


-- =============================================================================
-- Section 7: Session Timeline View (for team "full timeline" queries)
-- =============================================================================
-- Reconstructs a team's full timeline by joining team messages with all
-- child thread messages, ordered by timestamp. Used for the team room UI.

CREATE VIEW IF NOT EXISTS v_team_timeline AS
SELECT
    m.id,
    m.container_type,
    m.container_id,
    m.seq,
    m.ts,
    m.role,
    m.content,
    m.agent,
    m.tool_name,
    m.type,
    CASE
        WHEN m.container_type = 'thread' THEN t.task
        ELSE NULL
    END AS thread_task,
    CASE
        WHEN m.container_type = 'thread' THEN t.id
        ELSE NULL
    END AS thread_id
FROM messages m
LEFT JOIN threads t
    ON m.container_type = 'thread'
    AND m.container_id = t.id
ORDER BY m.ts ASC;

-- Usage: SELECT * FROM v_team_timeline
--        WHERE container_id = :team_id
--           OR container_id IN (SELECT id FROM threads WHERE parent_id = :team_id)
--        ORDER BY ts ASC;


-- =============================================================================
-- Section 8: (reserved — migration tracking moved to Section 14 _migrations)
-- =============================================================================


-- =============================================================================
-- Section 9: Connections
-- =============================================================================
-- OAuth and API key connection metadata. Secrets (tokens, keys) are NEVER
-- stored here — they live in the OS Keychain via go-keyring. This table
-- holds only the non-secret metadata needed for listing, filtering, and
-- expiry management.
--
-- DESIGN DECISION: Connections move from connections.json to SQLite because:
--   - Need to query expiring connections (expires_at index)
--   - Need to filter by provider (e.g., "show all GitHub connections")
--   - JSON file requires full-file lock for any read/write (concurrent access
--     bugs waiting to happen during token refresh)
--   - The `metadata` JSON column handles provider-specific fields without
--     requiring schema changes per provider.

CREATE TABLE IF NOT EXISTS connections (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',
    provider        TEXT    NOT NULL,               -- 'google','github','slack','jira','bitbucket','datadog','splunk'
    type            TEXT    NOT NULL DEFAULT 'oauth'
                        CHECK (type IN ('oauth', 'api_key', 'service_account', 'database', 'ssh')),
    account_label   TEXT    NOT NULL DEFAULT '',    -- human-readable name ("mjbonanno@github.com")
    account_id      TEXT    NOT NULL DEFAULT '',    -- provider-assigned ID
    scopes          TEXT    NOT NULL DEFAULT '[]',  -- JSON array of granted OAuth scopes
    metadata        TEXT    NOT NULL DEFAULT '{}',  -- JSON object for provider-specific fields
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at      TEXT                            -- NULL = never expires; RFC3339 otherwise
);

-- List connections by provider ("show all GitHub connections")
CREATE INDEX IF NOT EXISTS idx_connections_provider
    ON connections (tenant_id, provider);

-- Expiry management: proactive refresh, UI warnings
CREATE INDEX IF NOT EXISTS idx_connections_expires
    ON connections (expires_at)
    WHERE expires_at IS NOT NULL;

-- Filter by type
CREATE INDEX IF NOT EXISTS idx_connections_type
    ON connections (tenant_id, type);


-- =============================================================================
-- Section 10: Notifications
-- =============================================================================
-- Replaces the Pebble-based notification store (5 index prefixes → SQL indexes).
-- Notifications are created by routines, workflows, and system events. They
-- have a lifecycle: pending → seen → dismissed/approved/executed/failed.
-- Expired notifications are soft-deleted (expires_at in the past).
--
-- DESIGN DECISION: 5 Pebble prefix-scan indexes replaced by 5 SQL indexes.
-- The Pebble `notifications/id/*`, `notifications/status/*`,
-- `notifications/routine/*`, `notifications/run/*`, `notifications/workflow/*`
-- prefix patterns map exactly to SQL indexed columns. Query performance is
-- equivalent and the code is dramatically simpler.

CREATE TABLE IF NOT EXISTS notifications (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',
    routine_id      TEXT    NOT NULL DEFAULT '',    -- ULID of triggering routine (empty if none)
    run_id          TEXT    NOT NULL DEFAULT '',    -- ULID of routine run (empty if none)
    satellite_id    TEXT    NOT NULL DEFAULT '',    -- future multi-satellite routing
    workflow_id     TEXT    NOT NULL DEFAULT '',    -- ULID of triggering workflow (empty if none)
    workflow_run_id TEXT    NOT NULL DEFAULT '',    -- ULID of workflow run (empty if none)
    summary         TEXT    NOT NULL DEFAULT '',    -- one-line, ≤120 chars (for inbox)
    detail          TEXT    NOT NULL DEFAULT '',    -- full Markdown detail
    severity        TEXT    NOT NULL DEFAULT 'info'
                        CHECK (severity IN ('info', 'warning', 'urgent')),
    status          TEXT    NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'seen', 'dismissed', 'approved', 'executed', 'failed')),
    session_id      TEXT    NOT NULL DEFAULT '',    -- set when user opens "Chat" from notification
    proposed_actions TEXT   NOT NULL DEFAULT '[]', -- JSON array of ProposedAction structs
    step_position   INTEGER,                        -- NULL = workflow-level notification; set for step notifications
    step_name       TEXT    NOT NULL DEFAULT '',    -- workflow step name (empty for workflow-level notifications)
    deliveries      TEXT    NOT NULL DEFAULT '[]', -- JSON array of DeliveryRecord structs
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at      TEXT                            -- NULL = no expiry
);

-- Primary inbox query: pending notifications, newest first
CREATE INDEX IF NOT EXISTS idx_notifications_status
    ON notifications (tenant_id, status, created_at DESC);

-- Filter by severity (urgent badge count)
CREATE INDEX IF NOT EXISTS idx_notifications_severity
    ON notifications (tenant_id, severity, status);

-- Link to routine history
CREATE INDEX IF NOT EXISTS idx_notifications_routine
    ON notifications (tenant_id, routine_id)
    WHERE routine_id != '';

-- Link to specific run
CREATE INDEX IF NOT EXISTS idx_notifications_run
    ON notifications (tenant_id, run_id)
    WHERE run_id != '';

-- Link to workflow history
CREATE INDEX IF NOT EXISTS idx_notifications_workflow
    ON notifications (tenant_id, workflow_id)
    WHERE workflow_id != '';

-- Expiry cleanup: DELETE WHERE expires_at < NOW()
CREATE INDEX IF NOT EXISTS idx_notifications_expires
    ON notifications (expires_at)
    WHERE expires_at IS NOT NULL;


-- =============================================================================
-- Section 10b: Workstreams
-- =============================================================================
-- Workstreams group sessions and artifacts into named projects.

CREATE TABLE IF NOT EXISTS workstreams (
    id          TEXT NOT NULL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- session_id is stored as plain TEXT (no FK to sessions) so that workstreams
-- can be migrated independently of the sessions schema version.
CREATE TABLE IF NOT EXISTS workstream_sessions (
    workstream_id TEXT NOT NULL REFERENCES workstreams(id) ON DELETE CASCADE,
    session_id    TEXT NOT NULL,
    tagged_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (workstream_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_workstream_sessions_ws
    ON workstream_sessions (workstream_id, tagged_at DESC);

CREATE INDEX IF NOT EXISTS idx_workstream_sessions_sess
    ON workstream_sessions (session_id);


-- =============================================================================
-- Section 10b: Spaces
-- =============================================================================
-- Spaces are Slack-like channels or DMs. Each space has a lead agent and
-- optional members. Sessions within a space share a linear timeline.

CREATE TABLE IF NOT EXISTS spaces (
    id            TEXT NOT NULL PRIMARY KEY,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL DEFAULT 'dm'
                      CHECK (kind IN ('dm','channel')),
    lead_agent    TEXT NOT NULL,
    icon          TEXT NOT NULL DEFAULT '',
    color         TEXT NOT NULL DEFAULT '',
    team_id       TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    archived_at   TEXT
);

-- Only one DM space per lead agent
CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_dm_unique_agent
    ON spaces(lead_agent) WHERE kind = 'dm';

CREATE INDEX IF NOT EXISTS idx_spaces_kind    ON spaces(kind);
CREATE INDEX IF NOT EXISTS idx_spaces_updated ON spaces(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_spaces_lead    ON spaces(lead_agent);

-- Junction table: which agents are members of which space
CREATE TABLE IF NOT EXISTS space_members (
    space_id   TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    PRIMARY KEY (space_id, agent_name)
);

CREATE INDEX IF NOT EXISTS idx_space_members_agent ON space_members(agent_name);

-- Per-space read position (for unread badge / mark-read)
CREATE TABLE IF NOT EXISTS space_read_positions (
    space_id     TEXT NOT NULL PRIMARY KEY
                     REFERENCES spaces(id) ON DELETE CASCADE,
    last_read_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Auto-update updated_at on space modifications
CREATE TRIGGER IF NOT EXISTS spaces_updated_at
    AFTER UPDATE ON spaces
    BEGIN UPDATE spaces SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id; END;


-- =============================================================================
-- Section 11: Workflow Runs
-- =============================================================================
-- Replaces the per-workflow JSONL files in ~/.huginn/workflow-runs/.
-- Workflow definitions stay in YAML (user-authored). Run history needs
-- pagination, status filtering, aggregate stats, and eventual cleanup.
--
-- DESIGN DECISION: steps stored as JSON TEXT.
-- WorkflowStepResult is a small array (5-20 items), write-once on completion,
-- always read as a unit. Junction table would add complexity for zero benefit.

CREATE TABLE IF NOT EXISTS workflow_runs (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID (the run_id)
    tenant_id       TEXT    NOT NULL DEFAULT '',
    workflow_id     TEXT    NOT NULL,               -- matches Workflow.ID from YAML
    status          TEXT    NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running', 'complete', 'partial', 'failed', 'cancelled')),
    steps           TEXT    NOT NULL DEFAULT '[]',  -- JSON array of WorkflowStepResult
    error           TEXT    NOT NULL DEFAULT '',    -- error message if failed
    started_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at    TEXT                            -- NULL while running
);

-- List runs for a workflow (dashboard, pagination)
CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow
    ON workflow_runs (tenant_id, workflow_id, started_at DESC);

-- Filter running workflows (health check)
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status
    ON workflow_runs (tenant_id, status)
    WHERE status = 'running';


-- =============================================================================
-- Section 12: Agent Memory
-- =============================================================================
-- Replaces Pebble `agent:summary:*` and `agent:delegation:*` keys.
--
-- agent_summaries: Per-session summaries written by the orchestrator after
--   completing a session. Used when re-engaging an agent on a related task
--   ("here's what we did last time"). Currently unbounded in Pebble.
--   SQLite enables age-based cleanup: DELETE WHERE created_at < ?
--
-- agent_delegations: Records of agent-to-agent task delegation. Currently
--   capped at 10 per (from, to) pair by application code. In SQLite, this
--   cleanup becomes: DELETE FROM agent_delegations WHERE id NOT IN (
--     SELECT id FROM agent_delegations WHERE from_agent=? AND to_agent=?
--     ORDER BY created_at DESC LIMIT 10)

CREATE TABLE IF NOT EXISTS agent_summaries (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',
    machine_id      TEXT    NOT NULL DEFAULT '',    -- satellite machine ID
    agent_name      TEXT    NOT NULL,
    session_id      TEXT    NOT NULL,               -- which session this summarizes
    summary         TEXT    NOT NULL DEFAULT '',    -- prose summary
    files_touched   TEXT    NOT NULL DEFAULT '[]',  -- JSON array of file paths
    decisions       TEXT    NOT NULL DEFAULT '[]',  -- JSON array of decision strings
    open_questions  TEXT    NOT NULL DEFAULT '[]',  -- JSON array of unresolved questions
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- List summaries for an agent, newest first (context for next session)
CREATE INDEX IF NOT EXISTS idx_agent_summaries_agent
    ON agent_summaries (tenant_id, machine_id, agent_name, created_at DESC);

-- Find summary for a specific session
CREATE INDEX IF NOT EXISTS idx_agent_summaries_session
    ON agent_summaries (session_id);


CREATE TABLE IF NOT EXISTS agent_delegations (
    id              TEXT    NOT NULL PRIMARY KEY,   -- ULID
    tenant_id       TEXT    NOT NULL DEFAULT '',
    machine_id      TEXT    NOT NULL DEFAULT '',
    from_agent      TEXT    NOT NULL,
    to_agent        TEXT    NOT NULL,
    question        TEXT    NOT NULL DEFAULT '',    -- what was delegated
    answer          TEXT    NOT NULL DEFAULT '',    -- what was returned
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Trim to N most recent per pair (the core retention query)
CREATE INDEX IF NOT EXISTS idx_agent_delegations_pair
    ON agent_delegations (tenant_id, machine_id, from_agent, to_agent, created_at DESC);


-- =============================================================================
-- Section 12a: Artifacts
-- =============================================================================
-- Agent-produced artifacts: documents, code files, images, etc.
-- Large content (>256 KB) is stored on disk; content_ref holds the relative path.

CREATE TABLE IF NOT EXISTS artifacts (
    id                      TEXT    NOT NULL PRIMARY KEY,   -- ULID
    kind                    TEXT    NOT NULL,               -- 'document', 'code', 'image', etc.
    title                   TEXT    NOT NULL,
    mime_type               TEXT,
    content                 BLOB,                           -- inline content (≤256 KB)
    content_ref             TEXT,                           -- relative path for large content
    metadata_json           TEXT,                           -- JSON object of extra metadata
    agent_name              TEXT    NOT NULL DEFAULT '',
    thread_id               TEXT,
    session_id              TEXT    NOT NULL DEFAULT '',
    triggering_message_id   TEXT,
    status                  TEXT    NOT NULL DEFAULT 'draft'
                                CHECK (status IN ('draft', 'accepted', 'superseded', 'rejected', 'failed')),
    rejection_reason        TEXT,
    created_at              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- List artifacts for a session (newest first)
CREATE INDEX IF NOT EXISTS idx_artifacts_session
    ON artifacts (session_id, created_at DESC);

-- List artifacts by agent (for agent history views)
CREATE INDEX IF NOT EXISTS idx_artifacts_agent
    ON artifacts (agent_name, created_at DESC);

-- Active artifacts (non-terminal states for cleanup queries)
CREATE INDEX IF NOT EXISTS idx_artifacts_status
    ON artifacts (status, created_at DESC)
    WHERE status IN ('draft', 'accepted');


-- =============================================================================
-- Section 12b: Delegations
-- =============================================================================
-- Tracks agent-to-agent task delegation within a thread context.
-- Different from agent_delegations (which is a simple Q&A history store).
-- This table tracks full lifecycle: pending → in_progress → completed/failed.

CREATE TABLE IF NOT EXISTS delegations (
    id                    TEXT NOT NULL PRIMARY KEY,   -- ULID
    thread_id             TEXT NOT NULL,
    from_agent            TEXT NOT NULL,
    to_agent              TEXT NOT NULL,
    task                  TEXT NOT NULL DEFAULT '',
    objective             TEXT NOT NULL DEFAULT '',
    context               TEXT NOT NULL DEFAULT '',
    expected_output_kinds TEXT NOT NULL DEFAULT '[]',
    produced_artifact_ids TEXT NOT NULL DEFAULT '[]',
    status                TEXT NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'in_progress', 'completed', 'failed')),
    result                TEXT NOT NULL DEFAULT '',
    started_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    completed_at          TEXT,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    session_id            TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_delegations_pair
    ON delegations (from_agent, to_agent, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_delegations_thread
    ON delegations (thread_id);
CREATE INDEX IF NOT EXISTS idx_delegations_status
    ON delegations (status)
    WHERE status IN ('pending', 'in_progress');
CREATE UNIQUE INDEX IF NOT EXISTS uq_delegations_thread
    ON delegations (thread_id);
CREATE INDEX IF NOT EXISTS idx_delegations_session
    ON delegations (session_id, created_at DESC);


-- =============================================================================
-- Section 12c: Memory Replication Queue
-- =============================================================================
-- Queues memory operations for cross-space agent memory replication.
-- UNIQUE(target_vault, concept_key, space_id) enables idempotent upserts.

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
);

CREATE INDEX IF NOT EXISTS idx_mrq_drain
    ON memory_replication_queue(status, next_retry_at)
    WHERE status = 'pending';


-- =============================================================================
-- Section 12d: Cloud Vault Queue
-- =============================================================================
-- Queues memory operations for push to HuginnCloud. Separate from
-- memory_replication_queue (channel-member replication).
-- UNIQUE(vault_name, memory_id) enables idempotent upserts.

CREATE TABLE IF NOT EXISTS cloud_vault_queue (
    id             TEXT    NOT NULL PRIMARY KEY,   -- ULID
    session_id     TEXT    NOT NULL,
    agent_id       TEXT    NOT NULL,
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
    next_retry_at  INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at     INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE(vault_name, memory_id)
);

CREATE INDEX IF NOT EXISTS idx_cvq_drain
    ON cloud_vault_queue(status, next_retry_at)
    WHERE status IN ('pending', 'in_progress');


-- =============================================================================
-- Section 13: Convenience Views (extended)
-- =============================================================================

-- Pending notification count (TUI badge, API endpoint)
CREATE VIEW IF NOT EXISTS v_pending_notifications AS
SELECT tenant_id, COUNT(*) AS cnt
FROM notifications
WHERE status = 'pending'
  AND (expires_at IS NULL OR expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
GROUP BY tenant_id;

-- Connections expiring within 24 hours (proactive refresh trigger)
CREATE VIEW IF NOT EXISTS v_expiring_connections AS
SELECT *
FROM connections
WHERE expires_at IS NOT NULL
  AND expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  AND expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+1 day');

-- Workflow success rate (dashboard stats)
CREATE VIEW IF NOT EXISTS v_workflow_stats AS
SELECT
    tenant_id,
    workflow_id,
    COUNT(*)                                                         AS total_runs,
    SUM(CASE WHEN status = 'complete' THEN 1 ELSE 0 END)            AS success_count,
    SUM(CASE WHEN status = 'failed'   THEN 1 ELSE 0 END)            AS failure_count,
    ROUND(
        100.0 * SUM(CASE WHEN status = 'complete' THEN 1 ELSE 0 END)
        / NULLIF(COUNT(*), 0),
    1)                                                               AS success_rate_pct
FROM workflow_runs
GROUP BY tenant_id, workflow_id;


-- =============================================================================
-- Section 14: Migration Tracking
-- =============================================================================
-- Idempotent migration registry. Each migration writes a row here on success.
-- On startup, the Go migration runner checks this table to skip already-run
-- migrations. If a migration fails mid-way, no row is written and it retries.

CREATE TABLE IF NOT EXISTS _migrations (
    name            TEXT    NOT NULL PRIMARY KEY,   -- e.g., 'M1_connections', 'M6_sessions'
    completed_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    record_count    INTEGER NOT NULL DEFAULT 0,     -- rows migrated (for audit)
    source_path     TEXT    NOT NULL DEFAULT ''     -- original file/Pebble prefix
);


-- =============================================================================
-- Section 15: Observability — Stats Persistence, Cost History, Audit Log
-- =============================================================================

-- stats_snapshots: periodic flush of in-memory Registry records/histograms.
-- Pruned to 7-day retention on each flush cycle.
CREATE TABLE IF NOT EXISTS stats_snapshots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,   -- unix epoch seconds
    key         TEXT    NOT NULL,   -- metric name
    kind        TEXT    NOT NULL,   -- 'record' or 'histogram'
    value       REAL    NOT NULL,
    session_id  TEXT                -- optional, if metric is session-scoped
);
-- Composite index covers key-filtered time-range queries:
-- "show me http.request.duration_ms for the last 24h"
CREATE INDEX IF NOT EXISTS idx_stats_snapshots_key_ts
    ON stats_snapshots (key, ts);

-- cost_history: async drain from CostAccumulator.Record() calls.
-- Pruned to 7-day retention on each flush cycle.
CREATE TABLE IF NOT EXISTS cost_history (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ts                INTEGER NOT NULL,   -- unix epoch seconds
    session_id        TEXT    NOT NULL,
    cost_usd          REAL    NOT NULL,
    prompt_tokens     INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cost_history_ts
    ON cost_history (ts);
CREATE INDEX IF NOT EXISTS idx_cost_history_session
    ON cost_history (session_id, ts DESC);

-- audit_log: non-blocking permission gate decisions.
-- Pruned to 30-day retention on each flush cycle; hard cap of 10 000 rows.
-- TODO: add user TEXT column when multi-user support lands
CREATE TABLE IF NOT EXISTS audit_log (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    ts       INTEGER NOT NULL,   -- unix epoch seconds
    action   TEXT    NOT NULL,   -- e.g. 'tool_use', 'bash', 'file_write'
    resource TEXT    NOT NULL,   -- e.g. tool name or file path
    allowed  INTEGER NOT NULL,   -- 1 = permitted, 0 = denied
    reason   TEXT                -- human-readable reason (may be NULL)
);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts
    ON audit_log (ts);
