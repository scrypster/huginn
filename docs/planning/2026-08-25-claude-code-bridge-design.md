# Claude Code Bridge — Design

**Date:** 2026-08-25
**Status:** Implemented on branch `feat/claude-code-bridge-spec` (phase 1). Phase 2 (hooks) specified but not built.
**Scope:** Phase 1 (watcher + delegation). Phase 2 (hooks) specified but deferred.

## Goal

Make Claude Code terminal sessions first-class citizens inside Huginn, in both
directions:

- **Watch** — every Claude Code session on the machine, including ones started
  by hand in a terminal, appears live in Huginn's session sidebar with its
  messages, tool calls, token usage and cost.
- **Drive** — a Huginn agent, routine or workflow can delegate a one-shot task
  to Claude Code and receive the result.

Explicitly *not* a goal: typing into a Claude Code session from Huginn.

## Background

Claude Code writes each session to an append-only JSONL transcript at
`~/.claude/projects/<slugified-cwd>/<session-uuid>.jsonl`, flushed as the
session proceeds. The CLI accepts `--session-id <uuid>` to assign the session
ID in advance, and `--output-format stream-json` for machine-readable output.

Huginn already has the pieces this needs: a session store whose `Manifest` has
a `Source` discriminator, a `SessionMessage` type carrying role, content, tool
calls, tokens and cost, a WebSocket broadcast path to the web UI, and a thread
panel for sub-agent output.

## Approach and rejected alternatives

Three approaches were considered for the watch half.

**A — Hooks as transport.** Register Claude Code hooks that POST each event to
Huginn. Rejected: hook payloads do not carry assistant text, token usage or
cost, so sessions would be skeletons.

**B — Filesystem watcher.** Tail the transcripts directly with fsnotify.
Chosen. Fully live, requires no modification to `~/.claude/settings.json`,
cannot slow down or wedge a Claude Code session, and backfills existing history
for free. Its weakness is the absence of semantic events: it cannot distinguish
a session that is thinking from one blocked on a permission prompt.

**C — Hybrid.** B plus two cheap hooks (`SessionStart`, `Notification`) for the
signals B cannot infer. Deferred to phase 2 so the watcher can be judged on its
own first.

The chosen path is **B now, C later**. The decisive property is that phase 1
installs nothing into Claude Code's configuration, so a broken, stopped or
absent Huginn cannot affect a Claude Code session in any way.

The drive half has no meaningful alternatives: a tool in `internal/tools/` that
shells out to `claude -p`.

## Architecture

One new package plus small touch-points in existing code. Nothing outside this
repository changes in phase 1.

```
internal/claudecode/
  transcript.go   Claude Code JSONL line types + tolerant parser
  mapper.go       transcript lines -> session.SessionMessage (stateful)
  watcher.go      fsnotify over ~/.claude/projects, tail-from-offset
  backfill.go     one-shot historical import
  delegate.go     run `claude -p`, parse stream-json
  config.go       config struct + defaults

internal/tools/claude_code.go        the `claude_code` tool (PermExec)
internal/server/handlers_claude.go   GET /api/v1/claude/status
                                     POST /api/v1/claude/backfill
init_tools.go                        register the tool
web/src/views/SessionsView.vue       sidebar badge for external_kind="claude-code"
web/src/composables/useSessions.ts   Session type gains external_kind
```

**Amended:** `internal/server/dist` is gitignored build output, not a source
location; the sidebar badge is implemented in the two frontend files above.

The watcher and the delegate tool share `mapper.go`. A delegated run is launched
with an assigned `--session-id`, so the watcher ingests its transcript exactly
like any other session, and it appears in the sidebar as a full session — this
is the delegate path's only integration with persistence; it does not
duplicate it. **Amended:** live thread-panel streaming of that run's events,
described below in the original Delegation section, was not built — see that
section.

## Session identity

`session.Manifest` gains two fields:

```go
ExternalKind string `json:"external_kind,omitempty"`
ExternalID   string `json:"external_id,omitempty"`
```

**Amended:** the spec originally called for `Source: "claude-code"`. That is
not the mechanism that was built. `sessions` declares
`CHECK (source IN ('', 'routine'))`, and SQLite cannot drop a CHECK constraint
without rebuilding the table — which here would mean rebuilding under two
inbound foreign keys with `PRAGMA foreign_keys = ON`, inside the transaction
that wraps every migration. Instead, Claude Code sessions are stored with
`ExternalKind = "claude-code"` and `ExternalID` set to the Claude session
UUID; `source` is left untouched. Lookup is by `(ExternalKind, ExternalID)`;
Huginn session IDs remain ULIDs.

| Huginn field    | Source                                              |
|-----------------|-----------------------------------------------------|
| `ExternalKind`  | literal `"claude-code"`                             |
| `ExternalID`    | transcript `sessionId`                              |
| `WorkspaceRoot` | transcript `cwd`                                    |
| `WorkspaceName` | basename of `cwd`                                   |
| `Model`         | `message.model` of the most recent assistant line   |
| `Title`         | `customTitle` line if present, else first user text |
| `CreatedAt`     | `timestamp` of the first ingested line              |
| `UpdatedAt`     | `timestamp` of the most recent ingested line        |

Deriving `WorkspaceRoot` from `cwd` means Claude Code sessions group correctly
in Huginn's existing workspace UI with no additional work.

**Amended:** the unique index `uq_sessions_external` on
`(external_kind, external_id)` is NOT part of the base schema DDL — this is
not stated anywhere in the original spec and is worth calling out explicitly.
It is created by the `sessions_external_columns_v1` migration
(`migrateSessionsExternalColumnsV1`) instead, because `ApplySchema()` runs
BEFORE `Migrate()`: on an existing database the `CREATE TABLE IF NOT EXISTS`
for `sessions` is a no-op, so an index in the base schema would reference
`external_kind`/`external_id` before the migration's `ALTER TABLE` had added
those columns, and every existing install would fail to start with
`no such column: external_kind`. This actually happened during
implementation; see the warning comment in the schema file itself.

## Ingestion

### Line filtering

Only `type: "user"` and `type: "assistant"` lines produce messages. The
following observed types are metadata and MUST be dropped: `last-prompt`,
`custom-title` (consumed for the title only), `agent-name`, `mode`,
`permission-mode`, `atis-latch`, `attachment`, `file-history-snapshot`.
`type: "system"` lines are dropped in phase 1.

Unknown types are dropped and counted, never ingested. This list is a filter on
known-good types, not a blocklist, so new Claude Code metadata types cannot leak
into the timeline.

### Tool call assembly

The mapper is a state machine over lines, not a pure per-line function.

An assistant line's `message.content[]` array yields:
- `text` blocks, concatenated into `SessionMessage.Content`
- `tool_use` blocks, each becoming `PersistedToolCall{ID, Name, Args}`

The *following* `type: "user"` line carries `toolUseResult` and a
`tool_result` content block. Its `tool_use_id` identifies which open
`PersistedToolCall` to fill in with `Result`. A user line whose content is
exclusively `tool_result` blocks does NOT produce a user message; it only
completes tool calls on the preceding assistant message.

Open tool calls are tracked in a map keyed by `tool_use_id`. Calls still open
when a session goes idle are persisted with an empty `Result`.

### Sidechains

Lines with `isSidechain: true` are subagent turns. They are routed to a Huginn
thread hung off the parent message identified by `parentUuid`, rather than onto
the main timeline. This maps onto the existing thread panel with no new UI.

### Offsets and idempotency

Per Claude session UUID, SQLite (`internal/sqlitedb`) stores:

```
claude_ingest(external_id TEXT PRIMARY KEY,
              huginn_session_id TEXT,
              path TEXT,
              size INTEGER,
              byte_offset INTEGER,
              last_uuid TEXT,
              updated_at INTEGER)
```

On each append event:

1. `stat` the file. If `size < byte_offset`, the file was truncated or
   rotated: reset `byte_offset` to 0 and replay from the beginning.
2. Seek to `byte_offset` and read to EOF.
3. **Truncate the read buffer at the last `\n`.** A partial trailing line is the
   normal case when Claude Code is mid-write; consuming it corrupts the stream
   and advancing past it loses a message. Only whole lines are parsed, and
   `offset` advances only by the bytes of whole lines consumed.
4. Only when replaying from 0 after a reset: scan forward discarding lines until
   the line whose `uuid` equals `last_uuid` has been passed, then resume
   ingesting from the next line. UUIDs are unordered, so this is a positional
   re-sync, not a comparison. If `last_uuid` is never found in the replayed
   file, the file is a different session's content at the same path: clear the
   row and ingest from the beginning. This is the second guard that makes
   step 1's reset safe. During a normal forward read from `byte_offset` there is
   nothing to skip.
5. Append the resulting messages and update `byte_offset`, `size` and
   `last_uuid` in one transaction.

`byte_offset` rather than `offset` because `OFFSET` is a reserved word in
SQLite and would need quoting at every use site.

### Cost

Cost is computed from the model and `message.usage` via the existing
`internal/pricing` package, not read from any transcript field.

### Live push

After appending, the watcher calls the existing
`Server.BroadcastToSession(huginnID, "message_new", payload)`. No new WebSocket
message types and no frontend protocol work are required. The only frontend
change is a badge in the session sidebar for `external_kind == "claude-code"`
(**amended** — see Session identity above for why this is `external_kind`, not
`source`).

### Backfill

On `huginn serve` startup (when `watch.backfill` is true) and on demand via
`POST /api/v1/claude/backfill`, walk `~/.claude/projects/*/*.jsonl` and ingest
any file not already consumed to its full length. Bounded concurrency of 4.

Files larger than `watch.max_file_mb` are skipped **with a log line naming the
file and its size**. Truncation is never silent.

Backfill is idempotent by construction: it uses the same offset and `uuid`
dedupe path as live ingestion.

**Amended:** the result type is `BackfillResult{Files, Messages, Skipped,
Failed}` — a fourth field, `Failed`, beyond what this document originally
specified. `Skipped` counts transcripts deliberately not imported for
exceeding `max_file_mb`; `Failed` counts transcripts that could not be read at
all. The two are kept separate on purpose, so a caller can tell "too large, by
policy" apart from "broken or unreadable" — collapsing them would hide a real
failure behind an expected one. `POST /api/v1/claude/backfill` returns all
four fields as JSON (`{"files": ..., "messages": ..., "skipped": ...,
"failed": ...}`) and responds `409 Conflict` when the bridge is disabled.

## Delegation

A new tool `claude_code` registered at `tools.PermExec`, so toolbelt
`approval_gate` and Huginn's normal approval flow apply.

Schema:

```
prompt           string   required
cwd              string   optional, defaults to the session workspace root
model            string   optional, defaults to delegate.default_model
max_turns        integer  optional, defaults to delegate.max_turns
permission_mode  string   optional, defaults to delegate.permission_mode
```

**Amended:** the spec originally listed a sixth argument, `allowed_tools
[]string optional`. `DelegateRequest.AllowedTools` and `BuildArgs`'
`--allowedTools` handling exist and work, but `ClaudeCodeTool.Schema()` does
not expose an `allowed_tools` argument and `Execute` never populates the
field, so a delegated run currently inherits the CLI's default tool set. The
plumbing is real and reachable from Go, just not from a model's tool call.
Exposing it is a one-line schema addition plus a line in `Execute`; it was
left out of phase 1 deliberately, not by oversight.

Execution:

1. Generate a UUID for the run.
2. `exec.CommandContext(ctx, binary, "-p", prompt, "--session-id", uuid,
   "--output-format", "stream-json", "--verbose", ...)` with `cmd.Dir = cwd`.
3. Read stdout line by line and parse each event. **Amended:** the spec
   originally called for each `assistant` and `tool_use` event to be emitted
   as a thread reply on the delegating message, in-process, so the thread
   panel streamed live. That is not what was built: the `claude_code` tool is
   registered with a nil `onEvent` callback (see `init_tools.go`), so no
   events are mirrored anywhere while the run is in progress. What IS true:
   because Huginn assigns the `--session-id` up front, the transcript is
   picked up and ingested by the fsnotify watcher independently of the tool
   call, so the delegated run appears in the sidebar as a full session once
   ingested — just not live, and not in the thread panel. Live thread-panel
   streaming of delegated runs is deliberate future work, not a bug.
4. The final `type: "result"` line supplies `ToolResult.Output` from its
   `result` field and `ToolResult.Metadata` from
   `{cost_usd, duration_ms, num_turns, claude_session_id}`.

The transcript for this run is picked up by the watcher independently, so the
delegated session also appears in the sidebar as a full session.

`--dangerously-skip-permissions` is never passed unless explicitly set in
config. The default `permission_mode` is `acceptEdits`.

On context expiry the process group is killed and the tool returns the partial
output with `IsError: true` and an explanatory `Error`.

## Configuration

Added to `~/.huginn/config.json`:

```json
"claude_code": {
  "enabled": false,
  "binary": "claude",
  "watch": {
    "enabled": true,
    "backfill": true,
    "max_file_mb": 50
  },
  "delegate": {
    "enabled": true,
    "default_model": "sonnet",
    "permission_mode": "acceptEdits",
    "max_turns": 30,
    "timeout_secs": 900
  }
}
```

`enabled` defaults to **false**. Enabling it means Huginn reads every Claude
Code transcript on the machine, which is a deliberate opt-in.

When `enabled` is false the watcher does not start and the `claude_code` tool is
not registered.

## Failure modes

| Condition                         | Behaviour                                                        |
|-----------------------------------|------------------------------------------------------------------|
| fsnotify error                    | Log, exponential backoff, retry. Never crashes `serve`.           |
| Malformed JSONL line              | Skip and count. The rest of the file still ingests.               |
| Transcript truncated or rotated   | Reset `byte_offset` to 0, replay, re-sync on `last_uuid`.         |
| Partial trailing line             | Not consumed; `byte_offset` does not advance past the last `\n`.  |
| File exceeds `max_file_mb`        | Skipped with a log line naming file and size.                     |
| `claude` binary missing           | `claude_code` tool returns `IsError` with a clear message. The watcher is unaffected. |
| Delegation timeout                | Process group killed, partial output returned as an error.        |
| Huginn not running                | Nothing happens. Phase 1 installs no hooks, so Claude Code is never in Huginn's failure path. |

That last row is the reason approach B was chosen over A.

## Testing

- **Golden fixtures.** Two or three real transcripts copied from
  `~/.claude/projects`, redacted, in `testdata/`. Assert the exact
  `[]session.SessionMessage` the mapper produces.
- **Byte-at-a-time tailer test.** Feed a transcript to the tailer one byte at a
  time. Assert the final message list is identical to a single-shot read and
  that no message is emitted twice. This is the test that pins the partial-line
  rule.
- **Truncation reset.** Ingest, truncate the file, append different content,
  ingest again. Assert no duplicates and no lost messages.
- **Sidechain routing.** A transcript containing `isSidechain: true` lines must
  produce thread replies, not main-timeline messages.
- **Backfill idempotency.** Run backfill twice over the same directory; assert
  the message count is unchanged.
- **Delegate tool.** A fake `claude` shell script in `testdata/` emitting canned
  stream-json. No test touches the network or a real model.
- **Filtering.** A transcript containing every known metadata type must produce
  zero messages from those lines.

## Out of scope

Deliberately excluded from this design:

- Sending messages into a Claude Code session from Huginn
- The `--input-format stream-json` live-steering path
- Managing a fleet of `claude --bg` background agents
- Muninn write-through of Claude Code sessions
- Exposing Huginn to Claude Code as an MCP server

### What was not built (phase 1)

Two things this document describes are not part of the shipped phase 1 build,
and should not be assumed to exist by anyone reading only this spec:

- **Live thread-panel streaming of delegated runs.** See the amended
  Delegation section above — the `claude_code` tool is registered with a nil
  `onEvent` callback. A delegated run still appears as a full session via the
  watcher, just not live in the thread panel while it runs.
- **The phase-2 hooks** (`SessionStart`, `Notification`) described below.
  They remain specified here so the boundary is clear, but are deliberately
  unimplemented.

## Phase 2 — hooks (deferred)

Specified here so the boundary is clear; not part of the phase 1 build.

Add `huginn claude install-hooks` and `huginn claude uninstall-hooks`, which
register two Claude Code hooks in `~/.claude/settings.json`:

- `SessionStart` — tells Huginn a session has begun and in which directory,
  before the transcript file necessarily exists.
- `Notification` — tells Huginn a session is blocked waiting on the user, which
  drives an Inbox badge.

Requirements on the installer:

- Merge into the existing `hooks` object; never overwrite it. The target machine
  already has `enabledPlugins` that register their own hooks.
- Write a timestamped backup of `settings.json` before modifying it.
- The hook command must exit 0 unconditionally and use a sub-second timeout, so
  an unreachable Huginn can never block or fail a Claude Code turn.
