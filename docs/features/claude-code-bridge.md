# Claude Code Bridge

## What it is

The Claude Code Bridge connects Huginn to Claude Code terminal sessions in both directions. Watching, it tails every Claude Code session on the machine — including ones started by hand in a terminal, with no Huginn involvement at the time — and mirrors them live into Huginn's session list as they happen, plus a one-time backfill of history that already existed before the bridge was turned on. Driving, it exposes a `claude_code` tool that any Huginn agent, Routine or Workflow can call to delegate a self-contained coding task to a fresh `claude -p` run and get the result back.

**Enabling this feature means Huginn reads every Claude Code transcript on this machine — not just ones it creates itself.** Claude Code writes each session to an append-only JSONL file under `~/.claude/projects/`, one per session, regardless of whether Huginn is running or has ever heard of that project. Once the bridge is on, Huginn walks and watches that entire directory tree. This is why the bridge defaults to off: it is a broad grant, not a narrow one, and it should be a deliberate choice rather than a surprise.

The mechanism is purely a filesystem watcher (fsnotify) plus a historical backfill — Huginn installs nothing into `~/.claude/settings.json` and registers no Claude Code hooks. A stopped, broken, or entirely absent Huginn process cannot affect a Claude Code session in any way, because Claude Code never talks to Huginn; Huginn only reads files Claude Code was already writing.

---

## How to use it

### Turn it on

Add a `claude_code` block to `~/.huginn/config.json`. Everything shown below is the default except `enabled`, which you must flip to `true` yourself:

```json
{
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
}
```

- `enabled` — the master switch. When `false`, the watcher never starts and the `claude_code` tool is never registered, regardless of the nested settings.
- `binary` — the `claude` executable to invoke for delegation.
- `watch.enabled` — whether the fsnotify watcher runs at all.
- `watch.backfill` — whether Huginn imports pre-existing transcript history on `huginn serve` startup, in addition to watching live. You can also trigger a backfill on demand (see below).
- `watch.max_file_mb` — transcripts larger than this are skipped, not truncated silently. See Limitations.
- `delegate.enabled` — whether the `claude_code` tool is registered for agents to call.
- `delegate.default_model`, `delegate.permission_mode`, `delegate.max_turns`, `delegate.timeout_secs` — defaults applied to a delegated run when the tool call doesn't specify them.

Restart `huginn serve` after changing this block.

### Watch every Claude Code session

Once `claude_code.enabled` and `claude_code.watch.enabled` are both true, Huginn tails `~/.claude/projects/*/*.jsonl` and turns each session into a Huginn session with `external_kind = "claude-code"` and `external_id` set to the Claude session's own UUID. Messages, tool calls, token usage and model appear in the session list exactly as they're written to the transcript, with a `claude code` badge in the sidebar marking them as bridged rather than native.

If `watch.backfill` is true, Huginn also walks existing transcript history on startup so sessions you started before turning the bridge on show up too. Backfill runs with bounded concurrency and is idempotent — running it twice does not duplicate messages, because ingestion tracks a per-session byte offset and resumes from where it left off.

### Trigger a backfill on demand

```
POST /api/v1/claude/backfill
```

Re-scans `~/.claude/projects` and imports anything not already ingested. Returns:

```json
{
  "files": 12,
  "messages": 340,
  "skipped": 1,
  "failed": 0
}
```

`skipped` counts transcripts deliberately not imported because they exceeded `watch.max_file_mb` — a policy decision, not a failure. `failed` counts transcripts that could not be read at all. The two are kept separate so you can tell "this file is just large" apart from "something is actually broken." Returns `409 Conflict` if the bridge is disabled.

### Check bridge status

```
GET /api/v1/claude/status
```

```json
{
  "enabled": true,
  "watching": true,
  "root": "/Users/you/.claude/projects"
}
```

`enabled` reflects the config value; `watching` is true once the watcher has actually started; `root` is the directory being watched.

### Delegate a task with the `claude_code` tool

Any agent with `PermExec` access can call the `claude_code` tool to hand off a self-contained task to a fresh Claude Code run:

| Argument | Type | Required | Description |
|---|---|---|---|
| `prompt` | string | yes | The task for Claude Code. Must be self-contained — the delegated session has no access to the calling conversation. |
| `cwd` | string | no | Working directory for the run. Defaults to the current workspace root. |
| `model` | string | no | Model alias, e.g. `opus` or `sonnet`. Defaults to `delegate.default_model`. |
| `max_turns` | integer | no | Maximum agentic turns before the session stops. Defaults to `delegate.max_turns`. |
| `permission_mode` | string | no | One of `acceptEdits`, `auto`, `manual`, `plan`, `dontAsk`. Defaults to `delegate.permission_mode`. |

Because this is a `PermExec` tool, it goes through Huginn's normal approval gate like any other exec-capable tool — it is not auto-run by default.

Under the hood, Huginn generates a session UUID and runs `claude -p <prompt> --session-id <uuid> --output-format stream-json --verbose` with `cmd.Dir` set to `cwd`. `--dangerously-skip-permissions` is never passed unless explicitly enabled in config; the default permission mode is `acceptEdits`. If the run exceeds `delegate.timeout_secs`, its process group is killed and the tool returns the partial output as an error.

Because Huginn assigns the `--session-id` itself, the same watcher that handles ambient sessions picks up the delegated run's transcript too — so a delegated run appears in the session list as a full session, with its own messages and tool calls, once the watcher has ingested it.

---

## Limitations

**Delegated runs do not stream into the thread panel.** The `claude_code` tool is registered with a nil event callback, so nothing is mirrored live while a delegated run is in progress. You'll see the tool's final result immediately (output, cost, duration, turn count), and the full session will appear separately once the watcher ingests its transcript — but not as a live thread under the delegating message. Making delegated runs stream live is planned future work, not yet built.

**The bridge only watches; it does not write.** There is no path from Huginn back into a live Claude Code session — no sending messages, no steering. That's out of scope by design, not a current gap.

**A planned "phase 2" — Claude Code hooks (`SessionStart`, `Notification`) for signals a filesystem watcher can't infer, such as a session blocked on a permission prompt — is specified in the design document but deliberately not implemented.** Nothing in this build touches `~/.claude/settings.json` or installs any hook.

---

## See also

- [Sessions](sessions.md) — how bridged sessions fit into Huginn's session model
- [Permissions](permissions.md) — the approval gate the `claude_code` tool goes through as a `PermExec` tool
