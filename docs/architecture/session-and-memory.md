# Session and Memory

How Huginn manages state across the three layers: conversation sessions,
long-term semantic memory, and physical key-value persistence.

- See also: [permissions-and-safety.md](permissions-and-safety.md)

---

## The Problem

A coding agent needs three distinct kinds of state, each with a different
lifetime and invalidation rule:

- **Conversation state** — the messages exchanged in a single run. Dies when
  the run ends unless explicitly saved. Must survive crashes mid-session.
- **Long-term memory** — facts that should persist across sessions and across
  projects (personal preferences, architectural decisions, recurring patterns).
  Must be queryable by semantic context, not just by key.
- **Code intelligence data** — the indexed representation of the codebase
  (symbols, file hashes, import edges). Valid for the current commit; stale as
  soon as any file changes.

Mixing these into one store would couple their invalidation rules. Huginn
separates them into three distinct layers with clearly bounded responsibilities.

---

## Three-Layer State Model

```
+----------------------------------------------+
|            Huginn process                    |
|                                              |
|  +------------------+                        |
|  |    Session       |  in-memory + JSONL     |
|  |  (messages,      |  ~/.huginn/sessions/   |
|  |   manifest,      |  <id>/                 |
|  |   cost)          |  messages.jsonl        |
|  +------------------+  manifest.json         |
|                                              |
|  +------------------+                        |
|  |    Memory        |  MuninnDB SDK          |
|  |  (long-term,     |  over HTTP             |
|  |   cross-session, |  personal + project    |
|  |   semantic)      |  vaults                |
|  +------------------+                        |
|                                              |
|  +------------------+                        |
|  |    Storage       |  Pebble KV             |
|  |  (code intel,    |  ~/.huginn/store/      |
|  |   per-commit)    |  huginn.pebble/        |
|  +------------------+                        |
+----------------------------------------------+
```

Each layer is independently optional: Huginn runs without MuninnDB (memory
disabled) and without the Pebble store (code intelligence disabled). Only
session state is always active.

---

## Session Package

`internal/session/`

### Manifest

The manifest is the lightweight metadata file for a session. It is written to
`manifest.json` alongside the JSONL message log.

```go
type Manifest struct {
    SessionID     string    // canonical session identifier (UUID)
    ID            string    // alias for SessionID (convenience)
    Title         string    // human-readable label (first user message or derived)
    Model         string    // model name used for the session
    Agent         string    // agent type if non-default
    CreatedAt     time.Time
    UpdatedAt     time.Time
    MessageCount  int       // count of appended messages (in-memory authoritative)
    LastMessageID string    // ID of the most recent message
    WorkspaceRoot string    // absolute path to the project directory
    WorkspaceName string    // basename of WorkspaceRoot (display only)
    Status        string    // "active" | "archived"
    Version       int       // schema version (currently 1)
}
```

`MessageCount` and `LastMessageID` are the two fields that change most
frequently. They are updated in-memory after every `Append` call and flushed
to disk by `SaveManifest`. The manifest is small (under 1 KB) and is written
atomically via a tmp-then-rename pattern, so partial writes do not corrupt it.

`Version` is reserved for future schema migrations. Currently all manifests
are version 1. A future reader encountering a higher version number can reject
or upgrade accordingly.

### JSONL Message Store

Each session stores its conversation history as a newline-delimited JSON file:
`~/.huginn/sessions/<id>/messages.jsonl`.

```
{"id":"...","ts":"...","seq":1,"role":"user","content":"..."}
{"id":"...","ts":"...","seq":2,"role":"assistant","content":"..."}
{"id":"...","ts":"...","seq":3,"type":"cost","cost_usd":0.0023,"model":"..."}
```

Every line is a `SessionMessage` object. Cost records share the same format
with `type: "cost"` and populated `prompt_tokens`, `completion_tokens`,
`cost_usd`, and `model` fields.

**Why JSONL instead of SQLite?**

- No CGO dependency. SQLite requires cgo or a pure-Go driver with meaningful
  trade-offs. JSONL is stdlib-only.
- Append-only writes. `Append()` opens the file with `O_APPEND` and writes
  exactly one line. There is no read-modify-write cycle, so the write path
  cannot corrupt existing entries.
- Human-readable for debugging. A developer investigating a session can
  `cat messages.jsonl | jq .` without any tooling.
- Zero migration needed. Adding a new field to `SessionMessage` is backward
  compatible: old lines simply omit it, `json.Unmarshal` leaves the field at
  its zero value.

### Concurrent Append Safety

`Session` contains two synchronization primitives with distinct roles:

```go
type Session struct {
    ID       string
    Manifest Manifest
    seq      int64      // monotonic counter — updated via atomic.AddInt64
    mu       sync.Mutex // guards Manifest field updates (MessageCount, LastMessageID)
}
```

`seq` is incremented with `atomic.AddInt64` before the line is written to
disk. This gives every message a globally monotonic sequence number within the
session without taking a lock on the hot path.

`sess.mu` is taken only after the write succeeds, to update `MessageCount`
and `LastMessageID` in the in-memory manifest. The mutex scope is intentionally
narrow: it does not hold the lock during the file write, so concurrent appends
can proceed in parallel; only the manifest field update is serialized.

The `O_APPEND` flag on the file open ensures that concurrent writes from
multiple goroutines (e.g., parallel swarm agents sharing a session) are
appended atomically at the OS level.

### LoadOrReconstruct

`Store.LoadOrReconstruct(id)` is the recovery path for a corrupt or missing
manifest. It first calls `Load()` (reads `manifest.json`). If that fails for
any reason, it falls back to scanning the JSONL file directly:

```
LoadOrReconstruct(id)
    |
    v
Load(id) -- success --> return session
    |
    error
    |
    v
TailMessages(id, all)  -- reads messages.jsonl
    |
    error --> return error (both files unreadable, session is lost)
    |
    ok
    |
    v
Reconstruct minimal Manifest:
  Title = "(recovered) <id>"
  MessageCount = len(msgs)
  LastMessageID = msgs[last].ID
  Status = "active"
  Version = 1
```

This provides best-effort recovery without losing the conversation history
when only the manifest is corrupt (e.g., interrupted write, disk full during
SaveManifest).

`TailMessages` calls `repairJSONL` before reading. `repairJSONL` scans the
file and truncates at the last valid complete JSON line, discarding any
partial write at the end. This handles the case where a crash occurred mid-line
during an `Append`.

### Cost Tracking

Accumulated session cost is stored as `float64` in cost records appended to
the JSONL file. The precision of IEEE 754 float64 for USD amounts at typical
token costs is adequate: the rounding error accumulates at approximately
6.66e-16 USD per 100 million tokens — negligible for any practical session
length.

---

## Memory Package

`internal/memory/`

### VaultResolver

`VaultResolver` is the mapping layer between the local filesystem and MuninnDB
vault names. It answers two questions:

1. Which vaults should be queried when recalling context? (`ReadVaults()`)
2. Which vaults should new memories be written to? (`WriteVaults()`)

Vault names are derived in priority order:

```
Project vault name priority:
  1. .huginn/muninn.json project_vault field
  2. git remote origin URL, normalized to "huginn:project:<owner/repo>"
  3. basename(projectDir) → "huginn:project:<name>"

Personal vault name:
  "huginn:user:<username>"

Username priority:
  1. global config user_vault field (strips "huginn:user:" prefix)
  2. $HUGINN_USER environment variable
  3. git config user.name (lowercased, spaces→hyphens)
  4. $USER environment variable
  5. os/user.Current().Username
  6. fallback literal "huginn"
```

The project vault derivation caches its result at construction time
(`cachedProjectVault`) to avoid repeated git subprocess calls during a session.

### MemoryClient

`MemoryClient` wraps the MuninnDB Go SDK. It is the sole entry point for all
memory read and write operations within a Huginn session.

Key behaviors:

- **Health check at startup.** `NewMemoryClientWithTokenStore` probes the
  configured endpoint with a 2-second timeout. If the probe fails, `available`
  is set to false and all subsequent calls are no-ops. The agent continues
  operating without memory features.
- **Per-vault SDK clients.** One `muninn.Client` instance is created per
  vault and cached in `perVault`. Vault clients are created lazily on first
  access via `clientFor()`.
- **Token management.** Each vault has its own bearer token stored in
  `TokenFile`. If no token is present for a vault, `clientFor()` returns nil
  and operations on that vault silently degrade.

```
MemoryClient.Available() == false
    --> all Recall/Write calls return immediately without error
    --> Huginn continues operating fully offline

MemoryClient.Available() == true
    --> per-vault clients created with stored tokens
    --> operations routed to MuninnDB over HTTP
```

### Two-Tier Vault Strategy

The default strategy is `StrategyTwoTier`. Under this strategy:

```
Read vaults:  [personal vault, project vault, ...additional]
Write vaults: [personal vault, project vault]
```

This means:

- Memories written in project A are readable from project A's vault.
- Personal preferences (coding style, recurring facts) live in the personal
  vault and are readable from any project.
- Project B does not see project A's vault during recall, preventing context
  bleed between unrelated codebases.

Available strategies:

| Strategy | Read vaults | Write vaults |
|---|---|---|
| `two_tier` (default) | personal + project | personal + project |
| `personal_only` | personal | personal |
| `project_only` | project + additional | project |
| `single` | personal | personal |

Strategy is configured globally in `~/.config/huginn/muninn.json` and can be
overridden per-project in `.huginn/muninn.json`.

### Token File

`TokenFile` manages per-vault auth tokens at `~/.config/huginn/muninn-tokens.json`.

The file is always written with mode `0600` (owner read/write only). The
in-memory representation is protected by a `sync.RWMutex`: reads use `RLock`,
writes use `Lock`.

**Corrupt file recovery.** If `Load()` successfully opens the file but
`json.Unmarshal` fails (truncated write, manual corruption), the token store
is reset to an empty in-memory state and `Load` returns nil. The system
degrades to "not authenticated for any vault" rather than refusing to start.
This matches the principle that memory is an enhancement, not a requirement.

### Session Pipeline

At session close, the conversation summary is submitted to MuninnDB as a new
memory in the project vault. The pipeline:

```
session close
    |
    v
TailMessages() --> last N messages
    |
    v
Summarize (via model call or heuristic)
    |
    v
MemoryClient.Write(projectVault, concept, summary)
    |
    error? --> log warning, continue (non-fatal)
    |
    ok --> memory stored, available for future sessions
```

If MuninnDB is unavailable the pipeline step is skipped silently. The session
JSONL file still exists on disk regardless.

---

## Storage Package (Pebble KV)

`internal/storage/`

### Role

The storage package provides a Pebble-backed key-value store for code
intelligence data. It is not used for session history (that is JSONL) and not
used for long-term memory (that is MuninnDB). Its sole responsibility is
persisting the indexed representation of the codebase so that symbol lookup,
file chunking, and import graph traversal do not require re-parsing on every
agent call.

The Pebble database lives at `~/.huginn/store/huginn.pebble/` (configurable).

### Key Format

All keys are plain byte strings organized by namespace prefix:

```
meta:git_head                       -- current indexed commit SHA
meta:workspace_id                   -- workspace identifier

file:<path>:hash                    -- SHA-256 of file content at index time
file:<path>:parser_version          -- parser version used to index the file
file:<path>:indexed_at              -- RFC3339 timestamp of last index
file:<path>:symbols                 -- JSON-encoded []Symbol
file:<path>:chunks                  -- JSON-encoded []FileChunk

edge:<from-path>:<to-path>          -- JSON-encoded Edge (import/call relationship)

ws:summary                          -- JSON-encoded WorkspaceSummary
```

Prefix scans use Pebble's `IterOptions{LowerBound, UpperBound}` with an
`incrementLastByte` upper bound to efficiently enumerate all keys under a
prefix (e.g., all edges from a given file).

### Invalidation

When the patch layer applies a diff to a file, it calls `store.Invalidate(paths)`.
This deletes the `file:<path>:hash` key for each affected path. On the next
context-build pass, the indexer sees a missing or mismatched hash and
re-indexes the file. Symbols, chunks, and edges for the file are then
overwritten with fresh data.

This invalidation model means the store is always consistent with the hash
key: if `file:<path>:hash` is present, the symbols and chunks are valid for
that hash. If the key is absent, the file must be re-indexed before its
intelligence data can be trusted.

### Not Used for Session History

JSONL files are the session store. Pebble is not involved in recording
conversation messages, managing manifests, or cost tracking. The two stores
are independent and can be cleared independently:

- Deleting `~/.huginn/sessions/` removes all session history but does not
  affect code intelligence.
- Deleting `~/.huginn/store/` removes indexed code data; it will be rebuilt
  on next use. Session history is unaffected.

---

## Why This Design?

### Why separate session / memory / storage?

Each layer has a different invalidation event:

- Session state is per-run. It is created at process start and discarded (or
  archived) at process end.
- Memory is permanent. Facts stored in MuninnDB persist indefinitely and are
  never automatically invalidated.
- Storage is per-commit. A file's indexed data is stale as soon as the file
  changes; the hash key provides the staleness signal.

Coupling these into one store would either under-invalidate (stale code
intelligence leaking into permanent memory) or over-invalidate (wiping
long-term memories on every file change).

### Why JSONL not SQLite?

Four concrete reasons:

1. **No CGO.** SQLite requires either cgo or a pure-Go reimplementation with
   known limitations. JSONL uses only `os` and `encoding/json` from stdlib.
2. **Append-only writes are crash-safe.** A crash during an `O_APPEND` write
   produces at worst one truncated trailing line, which `repairJSONL` removes
   on next open. A crash during a SQLite B-tree write may require WAL recovery.
3. **Human-readable.** JSONL can be inspected with standard Unix tools.
   Debugging a session issue does not require a SQLite client.
4. **Zero migration.** New fields can be added to `SessionMessage` without
   an ALTER TABLE or a migration script. Old sessions remain readable.

The cost is that session listing requires a directory scan and a manifest read
per session (no indexed query). For the expected session counts (hundreds, not
millions) this is acceptable.

### Why VaultResolver?

Without vault scoping, memories from project A would appear in project B's
recall context. A fact like "this repo uses tabs for indentation" is only
relevant in the repo where it was learned. VaultResolver maps the working
directory to a project vault name so that project-scoped memories stay in the
project vault and do not pollute other projects' context windows.

The personal vault carries cross-project facts (user preferences, general
coding patterns) and is included in the read set for all strategies except
`project_only`.

### Why graceful degradation when MuninnDB is unavailable?

Huginn is a local coding tool first. Memory is a quality-of-life enhancement
that requires a separately running MuninnDB server. If that server is not
running — because the user is on a plane, in a restricted network, or simply
hasn't set it up — Huginn must still work. The 2-second health check at
startup determines availability once; all subsequent calls skip the network
entirely if `available == false`.

---

## Hardening Highlights

**TokenFile corrupt file recovery.** `Load()` does not return an error on
`json.Unmarshal` failure. It resets to an empty token map and returns nil.
Startup is never blocked by a bad token file.

**Manifest LoadOrReconstruct.** A corrupt or missing `manifest.json` triggers
a JSONL scan to reconstruct a minimal manifest. The session title is prefixed
with `(recovered)` to make recovery visible in session listings.

**SaveManifest atomic write.** Manifests are written to `<path>.tmp` first,
then renamed over `<path>`. On POSIX systems `os.Rename` is atomic, so a
crash during the write leaves either the old manifest or the new manifest
intact, never a partial file.

**repairJSONL.** Before reading the message log, `TailMessages` calls
`repairJSONL` which scans lines and truncates the file at the last valid JSON
object boundary. A single crash-truncated trailing line does not corrupt the
rest of the history.

---

## Thread Safety

| Shared state | Protected by | Notes |
|---|---|---|
| `Session.Manifest.MessageCount`, `.LastMessageID` | `Session.mu` | Updated after every Append |
| `Session.seq` | `sync/atomic` | Incremented without taking Session.mu |
| `MemoryClient.perVault` | `MemoryClient.perVaultMu` | Lazy vault client creation; Activate fans out goroutines |
| `TokenFile.data` | `TokenFile.mu` (RWMutex) | Reads use RLock, writes use Lock |
| `TokenStore.tokens` | `TokenStore.mu` (RWMutex) | In-memory test double, same pattern |
| Pebble store | Pebble internal | Pebble handles its own concurrency; callers need not lock |

`Store.baseDir` and `MemoryClient.endpoint` are set at construction time and
never mutated, so they require no lock.

---

## Limitations

**JSONL has no indexed queries.** `Store.List()` reads every session directory
and parses every `manifest.json`. For a user with hundreds of sessions this is
fast enough; for thousands it may become noticeable. There is no pagination,
no full-text index on session content, and no query language.

**MuninnDB requires a separately running server.** The memory layer is not
embedded. If the server is not reachable at startup, memory features are
disabled for the entire session with no retry. There is no automatic
reconnection if the server becomes available mid-session.

**Session allow-list is not shared between swarm agents.** Each agent in a
swarm has its own Gate instance. An `AllowAll` decision in agent A does not
propagate to agent B. Each agent must independently prompt for approval.

**JSONL full scan for message replay.** `TailMessages` scans the entire
JSONL file to build the message list. For very long sessions (thousands of
messages) this adds startup latency. There is no binary index or offset file.

---

## See Also

- [permissions-and-safety.md](permissions-and-safety.md) — the Gate,
  permission levels, patch application, and FileLock
