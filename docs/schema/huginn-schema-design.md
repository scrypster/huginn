# Huginn SQLite Schema — Design Rationale

**Date:** 2026-03-10
**Schema Version:** 1

---

## 1. Full DDL

The complete DDL is in `huginn-sqlite-schema.sql` in this directory. Key design choices:

- **TEXT for ULIDs/timestamps.** SQLite has no native UUID or timestamp type. TEXT with RFC 3339 format sorts correctly lexicographically and is human-readable in the sqlite3 CLI.
- **INTEGER for sequences/counts.** SQLite's INTEGER affinity is 64-bit, matching Go's int64.
- **REAL for costs.** IEEE 754 double — sufficient precision for USD at the sub-cent level.
- **CHECK constraints on all enum columns.** Status, role, type, and container_type columns all have explicit CHECK constraints. This catches bugs at the DB level rather than relying on application validation alone.
- **DEFAULT values on every NOT NULL column.** Ensures INSERT statements don't need to specify every field. New fields can be added with defaults without breaking existing Go code.

---

## 2. Teams Model

### Table: `teams`

A team is a multi-agent collaboration room. It has its own title, objective, message stream, and lifecycle. Key fields:

- **`objective`** — the high-level task the team is working on. Injected into each agent's system prompt context.
- **`parent_session_id`** — optional FK to the session that spawned the team. The user might say "build this as a team" during a session; the team remembers where it came from. SET NULL on session delete (the team survives).
- **`total_cost_usd` / `cost_budget_usd`** — aggregate cost tracking at the team level. Updated by the CostAccumulator when any thread or main-room message incurs cost.
- **`summary` / `summary_through`** — rolling summary, same pattern as sessions. Used when a new agent joins a team mid-conversation.

### Table: `team_members`

Junction table with composite PK `(team_id, agent_name)`. Each row is "agent X is in team Y with role Z."

- **Roles:** `lead` (orchestrator, like session.agent), `member` (specialist), `observer` (read-only, future cloud mode for human spectators).
- **`left_at`** — nullable. When an agent is removed from a team, we set left_at rather than deleting the row. This preserves the audit trail of who participated.

### How team conversations work vs sessions

Team "main room" messages go in the `messages` table with `container_type='team'` and `container_id=teams.id`. These are the announcements, user interjections, and agent status updates — the Slack channel.

Work happens in threads. When the team lead delegates to a specialist, a thread is created with `parent_type='team'` and `parent_id=teams.id`. Thread messages use `container_type='thread'`.

To render the full team timeline (main room + thread activity interleaved), query with `container_id IN (team_id, ...thread_ids...)` and order by `ts`.

---

## 3. Threads Model

### Table: `threads`

Maps directly from the existing `threadmgr.Thread` struct with these key changes:

- **`id` is a ULID** (replaces "t-1" counter). Global uniqueness, time-ordering, and consistency with all other IDs.
- **`parent_type` + `parent_id`** — polymorphic parent. A thread can belong to a session OR a team. The application enforces that `parent_id` exists in the corresponding table.
- **`summary_text`, `summary_status`, `files_modified`, `key_decisions`, `artifacts`** — the FinishSummary fields. Summary text and status are plain TEXT columns. The array fields (files, decisions, artifacts) are JSON-encoded TEXT.

### Why JSON arrays instead of junction tables for summary fields

These arrays are:
1. **Write-once** — set on thread completion, never updated
2. **Read-as-a-unit** — always fetched together, never queried individually ("find all threads that modified file X" is not a hot query)
3. **Small** — typically 1-20 items
4. **Not FK targets** — no other table references them

Junction tables would add 3 tables, 6 indexes, and N INSERT statements per thread completion for zero query benefit. If "find threads by file" becomes a hot query later, SQLite's `json_each()` can handle it, or we add a materialized index table.

### DAG: `thread_deps` table

The `depends_on []string` from the Thread struct becomes a proper edge table:

```sql
CREATE TABLE thread_deps (
    thread_id   TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    depends_on  TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    PRIMARY KEY (thread_id, depends_on),
    CHECK (thread_id != depends_on)
);
```

**Why a junction table, not a JSON array:**

The DAG evaluator needs two query patterns:
1. **Forward:** "What does thread X depend on?" — `SELECT depends_on FROM thread_deps WHERE thread_id = ?`
2. **Reverse:** "What threads are waiting on thread X?" — `SELECT thread_id FROM thread_deps WHERE depends_on = ?`

The reverse lookup is impossible to index with a JSON array. The junction table gets both queries via simple index scans.

**Cycle detection** is NOT enforced by the schema (SQLite has no DAG constraint). The `ThreadManager.Create()` in Go validates acyclicity before inserting edges, same as today.

---

## 4. Messages Model

### One table for all containers

```sql
CREATE TABLE messages (
    id              TEXT PRIMARY KEY,     -- ULID
    container_type  TEXT NOT NULL,        -- 'session' | 'team' | 'thread'
    container_id    TEXT NOT NULL,        -- FK to parent
    seq             INTEGER NOT NULL,     -- monotonic within container
    ts              TEXT NOT NULL,
    role            TEXT NOT NULL,        -- 'user' | 'assistant' | 'tool' | 'system' | 'cost'
    content         TEXT NOT NULL DEFAULT '',
    agent           TEXT NOT NULL DEFAULT '',
    ...
    UNIQUE (container_id, seq)
);
```

**Why one table, not three:**

- The message record format is identical across sessions, teams, and threads (same as today — SessionMessage is reused for thread JSONL).
- One table = one set of indexes, one pagination query, one relay sync cursor format.
- Cost aggregation is `SELECT SUM(cost_usd) FROM messages WHERE container_id = ? AND type = 'cost'` — works for any container type.
- The `container_type` discriminator exists for query filtering, not for FK enforcement (SQLite can't do polymorphic FKs anyway).

**Why not separate tables:** Three tables would require UNION queries for cross-container operations (e.g., "total cost across a team and its threads"), duplicate every index, and require three migration paths. The storage overhead of the `container_type` column is trivial.

### Role values

- `user` — human input
- `assistant` — LLM response (with optional tool_calls_json)
- `tool` — tool execution result (has tool_name, tool_call_id)
- `system` — system events (thread started, thread done, etc.)
- `cost` — cost accounting record (has prompt_tokens, completion_tokens, cost_usd, model)

The `role='cost'` value is new — the existing code uses `type='cost'` with `role='system'`. The schema supports both patterns: the CHECK allows `role` to be `'cost'` directly, and `type='cost'` remains the canonical flag for cost queries.

### tool_calls_json

Assistant messages can include tool calls. The existing code stores these as `[]backend.ToolCall` in memory and as a JSON array in JSONL. We preserve this as a TEXT column with the JSON array. Alternatives considered:

- **Separate tool_calls table:** Would enable queries like "find all messages that called read_file." This is useful for code intelligence but not for message rendering. We can add it later without schema changes (just an additional table that references messages).
- **Inline in content:** Would lose structure. Rejected.

---

## 5. Ordering Strategy

### Within a container: `seq` (monotonic integer)

Every container (session, team, thread) has an independent seq counter starting at 1. Messages within a container are ordered by `(container_id, seq ASC)`. This is:

- **Gapless** — no missing sequence numbers (unlike timestamps which can have collisions)
- **Efficient** — INTEGER comparison is faster than TEXT timestamp comparison
- **Pageable** — `WHERE container_id = ? AND seq > ? ORDER BY seq ASC LIMIT 50` is a simple range scan on the covering index

The Go code assigns seq via `atomic.AddInt64`, same as today.

### Across containers: `ts` (RFC 3339 timestamp) or `id` (ULID)

For cross-container queries (team timeline), order by `ts ASC` or by `id ASC` (ULIDs encode creation time in their first 48 bits, so lexicographic sort = chronological sort).

### The UNIQUE constraint

```sql
UNIQUE (container_id, seq)
```

This guarantees no two messages in the same container share a sequence number. It also creates an implicit index that the pagination query uses. Combined with the explicit `idx_messages_container_seq` index, this ensures the query planner always uses an index scan, never a table scan.

---

## 6. Routines Link

### Two tables: `routines` + `routine_runs`

**`routines`** — mirrors YAML definitions. YAML remains source of truth; the DB is a queryable cache. Sync on startup and filesystem watch.

**`routine_runs`** — one row per execution. Links to the session it spawned via `session_id` FK. This replaces the loose `source`, `routine_id`, `run_id` fields on the session manifest.

The session still carries `source` and `routine_id` for backward compatibility and quick filtering, but the `routine_runs` table is the authoritative history.

**Query patterns served:**
- "Show all runs of the morning standup routine" — `WHERE routine_id = ? ORDER BY started_at DESC`
- "Did this session come from a routine?" — `SELECT * FROM routine_runs WHERE session_id = ?`
- "Total cost of all routine runs this week" — `SELECT SUM(cost_usd) FROM routine_runs WHERE started_at > ?`

---

## 7. Relay State

### What's in the DB: `relay_cursors`

One row per container being synced. Tracks `last_synced_seq` — the highest message sequence number that was successfully delivered to huginncloud. On reconnect, the relay queries:

```sql
SELECT * FROM messages
WHERE container_id = ? AND seq > ?
ORDER BY seq ASC
```

This gives an exact delta of unsent messages. No full re-sync needed.

### What stays in-memory (not in DB)

- **WebSocket connection state** — transient, changes every second
- **Outbox queue** — messages waiting to be sent. If the process crashes, they're reconstructed from the cursor delta on restart
- **Heartbeat timer** — pure runtime state
- **Registration token** — stored in the system keychain, not the DB

### Why this replaces Pebble SessionStore

The existing Pebble-based SessionStore stores `{ID, StartedAt, LastSeq, Status}` per session. The relay_cursors table provides the same data with:
- SQL queryability (vs Pebble's key-value iteration)
- Consistency with the rest of the schema (vs a separate Pebble database)
- Support for teams and threads (vs session-only)
- Transactional writes (vs Pebble's separate write path)

---

## 8. Migration from Filesystem

### Migration strategy

The migration reads existing filesystem data and writes to SQLite in a single transaction per session:

```
For each session directory in ~/.huginn/sessions/:
  1. Read manifest.json → INSERT INTO sessions
  2. Read messages.jsonl line by line → INSERT INTO messages (container_type='session')
  3. Read thread-*.jsonl files →
     a. INSERT INTO threads (remap "t-N" ID to ULID)
     b. INSERT INTO messages (container_type='thread')
  4. Rebuild thread_deps from in-memory ThreadManager state (if available)
     or from thread JSONL metadata
  5. Log progress in migration_log
```

### Schema choices that simplify migration

1. **DEFAULT values everywhere** — old sessions missing newer fields (like summary) just get NULL/empty defaults. No special handling needed.
2. **`status` CHECK allows 'closed'** — old sessions used 'closed' alongside 'archived'. Both are valid.
3. **seq assignment** — JSONL files already have seq fields. Direct copy. For thread messages that lack seq (some older formats), assign seq by line number.
4. **Thread ID remapping** — The `migration_log` table tracks old "t-1" → new ULID mappings. Used to rewrite `depends_on` references in `thread_deps`.
5. **Idempotent** — Each entity is logged in migration_log. If migration is interrupted, it resumes by skipping already-migrated entities.

### Post-migration

After migration, the filesystem data is NOT deleted — it becomes a backup. A flag in `config.json` (`"storage_backend": "sqlite"`) controls whether the Go code reads from filesystem or SQLite.

---

## 9. Future-Proofing for Cloud-Hosted Satellite

### `tenant_id` column on all data tables

Every table that holds user data has a `tenant_id TEXT NOT NULL DEFAULT ''` column. For local use, this is always empty string. When huginn runs in a container serving multiple users:

1. `tenant_id` is set to the user's account ID on every write
2. All queries include `WHERE tenant_id = ?` (or the indexes filter by it)
3. Row-level isolation is achieved without schema changes

### Why DEFAULT '' instead of NULL

- Empty string is a valid index key (NULL requires IS NULL, complicates queries)
- Go zero value for string is "" (no pointer gymnastics)
- Index on `(tenant_id, ...)` works correctly with empty strings

### Indexes are tenant-aware

All multi-column indexes that serve filtered queries lead with `tenant_id`:
```sql
CREATE INDEX idx_sessions_updated ON sessions (tenant_id, updated_at DESC);
```

For local use (tenant_id=''), this adds zero overhead — SQLite skips the leading column efficiently when it's a constant.

### What else would need to change for cloud

1. **Authentication middleware** — set tenant_id from JWT claims
2. **Connection pooling** — single WAL database handles ~100 concurrent readers
3. **Backup** — periodic `.backup` command or Litestream replication
4. **Encryption at rest** — SQLCipher drop-in replacement, no schema changes
5. **Per-tenant quotas** — add a `tenants` table with limits; check on INSERT

None of these require schema migration.

---

## 10. Pragmas and Connection Setup

### Required on every connection open

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -8000;        -- 8 MB
PRAGMA mmap_size = 134217728;     -- 128 MB
PRAGMA temp_store = MEMORY;
PRAGMA wal_autocheckpoint = 1000;
```

### Required on connection close

```sql
PRAGMA optimize;
```

### Go implementation pattern

```go
func openDB(path string) (*sql.DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }

    // Connection-level pragmas (run on every new connection)
    db.SetConnMaxLifetime(0)  // don't close idle connections
    db.SetMaxOpenConns(1)     // WRITE serialization (WAL allows concurrent reads via separate conns)

    // For reads, open a second *sql.DB with higher MaxOpenConns
    // This is the standard WAL pattern for Go + SQLite.

    pragmas := []string{
        "PRAGMA journal_mode = WAL",
        "PRAGMA synchronous = NORMAL",
        "PRAGMA foreign_keys = ON",
        "PRAGMA busy_timeout = 5000",
        "PRAGMA cache_size = -8000",
        "PRAGMA mmap_size = 134217728",
        "PRAGMA temp_store = MEMORY",
        "PRAGMA wal_autocheckpoint = 1000",
    }
    for _, p := range pragmas {
        if _, err := db.Exec(p); err != nil {
            db.Close()
            return nil, fmt.Errorf("pragma %s: %w", p, err)
        }
    }

    return db, nil
}
```

### Why these specific values

| Pragma | Value | Rationale |
|--------|-------|-----------|
| journal_mode | WAL | Concurrent reads during writes. Critical for multi-goroutine access. |
| synchronous | NORMAL | In WAL mode, NORMAL provides crash-safe durability without the performance cost of FULL. |
| foreign_keys | ON | SQLite ignores FK constraints by default. Must be set per-connection. |
| busy_timeout | 5000ms | Prevents SQLITE_BUSY errors when goroutines contend on writes. 5s is generous — most writes take <1ms. |
| cache_size | 8 MB | 4x the default. Messages table can be large; caching avoids repeated I/O during pagination. |
| mmap_size | 128 MB | Memory-mapped I/O for reads. Significant speedup on macOS/Linux. Safe for local single-user. |
| temp_store | MEMORY | Temp tables in RAM. Faster GROUP BY and ORDER BY on large result sets. |
| wal_autocheckpoint | 1000 pages | Default value, stated explicitly. Prevents WAL growth during long agent sessions. |

### Write serialization strategy

SQLite allows only one writer at a time (even in WAL mode). The recommended Go pattern:

1. **Write connection:** single `*sql.DB` with `MaxOpenConns(1)`. All INSERTs, UPDATEs go through this.
2. **Read connection:** separate `*sql.DB` with `MaxOpenConns(4)`. All SELECTs go through this.
3. Both open the same database file. WAL mode allows them to operate concurrently.

This eliminates "database is locked" errors that plague naive single-connection SQLite usage in Go.
