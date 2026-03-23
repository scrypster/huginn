# Scheduler / Automations

**Package**: `internal/scheduler`
**Related**: [workflows.md](workflows.md), [multi-agent.md](multi-agent.md), [session-and-memory.md](session-and-memory.md)

---

## Overview

A Routine is a named, YAML-defined agent task that runs on a cron schedule without
human intervention. The Scheduler solves the problem of recurring code intelligence
work — nightly dependency scans, morning PR reviews, end-of-sprint summaries — that
would otherwise require a developer to remember to run them.

When a Routine fires, Huginn creates a headless agent session, runs the configured
prompt against the configured agent, and stores the output as a structured
Notification in the local Pebble KV store. The Inbox in the web UI surfaces those
notifications with a live badge counter updated over WebSocket.

---

## Routine YAML Format

Routines live at `~/.huginn/routines/{slug}.yaml`. The slug is the filename stem
and is used to reference the Routine from Workflows.

```yaml
id: "a3f8bc12"
name: "Morning PR Review"
description: "Scan open PRs and summarize findings in inbox"
enabled: true
trigger:
  mode: schedule
  cron: "0 9 * * 1-5"   # weekdays at 9am
agent: Mark
prompt: |
  Review all open PRs in this repository. For each one, summarize:
  - What it changes
  - Whether tests cover the change
  - Any obvious issues
  Report findings as a structured list.
timeout_secs: 300
```

| Field | Type | Description |
|---|---|---|
| `id` | string | Stable unique identifier (short UUID or human-chosen string) |
| `name` | string | Display name shown in the UI and Inbox |
| `description` | string | Optional long description |
| `enabled` | bool | When false the Routine is loaded but not scheduled |
| `trigger.mode` | enum | `schedule` \| `manual` (future: `event`) |
| `trigger.cron` | string | Standard 5-field cron expression (required when mode is `schedule`) |
| `agent` | string | Agent name to run (`Chris`, `Steve`, `Mark`, or a custom agent) |
| `prompt` | string | Prompt sent to the agent each time the Routine fires |
| `timeout_secs` | int | Maximum seconds before the headless session is cancelled (default: 300) |

A Routine with no `trigger.cron` (or `trigger.mode: manual`) is a **workflow-only
routine** — it can be referenced by a Workflow but does not fire on its own schedule.
See [workflows.md](workflows.md).

---

## Trigger Modes

| Mode | Fires when | Status |
|---|---|---|
| `schedule` | cron expression matches | Implemented |
| `manual` | user calls `POST /api/v1/routines/{id}/run` | Planned |
| `event` | incoming webhook matches a registered event | Future |

---

## Cron Syntax

Huginn uses standard 5-field cron syntax via [`robfig/cron/v3`](https://github.com/robfig/cron):

```
┌───── minute (0–59)
│ ┌───── hour (0–23)
│ │ ┌───── day of month (1–31)
│ │ │ ┌───── month (1–12)
│ │ │ │ ┌───── day of week (0–6, Sunday = 0)
│ │ │ │ │
* * * * *
```

A seconds field is **not** supported by default. Routines are not designed for
sub-minute scheduling — use `timeout_secs` to bound execution instead.

Common examples:

| Expression | Meaning |
|---|---|
| `0 9 * * 1-5` | Every weekday at 9:00am |
| `0 8 * * 1` | Every Monday at 8:00am |
| `0 23 * * *` | Every night at 11:00pm |
| `*/30 * * * *` | Every 30 minutes |

---

## Cross-Platform Compatibility

Huginn's scheduler runs entirely **inside the Huginn process** using the
[`robfig/cron/v3`](https://github.com/robfig/cron) Go library. It has no
dependency on any operating-system scheduling mechanism:

| Platform | What is NOT used | What IS used |
|---|---|---|
| macOS | `crontab`, `launchd`, Automator, Shortcuts | `robfig/cron` in-process |
| Linux | system `cron` / `crond` | `robfig/cron` in-process |
| Windows | Task Scheduler | `robfig/cron` in-process |

When Huginn is compiled for a target platform (`GOOS=darwin/linux/windows`), the
scheduler is baked into the binary. No system-level setup, no root access, and no
platform-specific configuration files are needed. Routines fire as long as the
Huginn process is running — stopping Huginn pauses all automations; restarting it
re-registers all enabled schedules automatically.

**Timezone** is resolved from the host OS via Go's `time` package, which reads
system timezone data correctly on all three platforms.

---

## Execution Lifecycle

```
Huginn starts
     │
     ▼
Scheduler.Load()
  reads ~/.huginn/routines/*.yaml
  skips disabled routines
     │
     ▼
for each enabled Routine with trigger.mode == "schedule":
  robfig/cron registers entry with the cron expression
     │
     ▼
[cron fires at scheduled time]
     │
     ▼
headless agent session created via agent.Orchestrator
  agent: as configured in routine.yaml
  prompt: routine.prompt
  timeout: routine.timeout_secs (context deadline)
     │
     ├── agent runs (LLM calls, tool execution)
     │
     ▼
Notification built and written to Pebble KV
  path: ~/.huginn/store/notifications/
  indexed by: notification ID, routine_id, created_at, read status
     │
     ▼
WS badge event broadcast to all connected clients
  Inbox icon badge count increments in real time
```

The Scheduler registers all Routines in a single `robfig/cron` instance. Workflow
cron entries (see [workflows.md](workflows.md)) are registered in the same instance
via `RegisterWorkflow` / `UnregisterWorkflow`.

---

## Notification Structure

Each Notification stored in Pebble KV has the following shape:

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique notification ID |
| `routine_id` | string | ID of the Routine that produced this notification |
| `routine_name` | string | Display name (snapshot at creation time) |
| `created_at` | time.Time | Wall clock time when the headless session finished |
| `title` | string | Short title extracted from agent output or routine name |
| `body` | string | Full agent output text |
| `read` | bool | Whether the user has marked this notification read |
| `satellite_id` | string | Reserved for future cloud sync (Satellite relay) |

Multi-index prefix keys in Pebble allow efficient queries: list all notifications,
list unread only, list by routine, and order by recency without a full scan.

---

## Permissions

Routines run with the same three-tier permission gate as interactive agents
(`PermRead`, `PermWrite`, `PermExec`). See [permissions-and-safety.md](permissions-and-safety.md).

Key considerations for unattended operation:

- Write and Exec tools still require approval by default, even in headless sessions.
- A Routine that calls a write tool will stall waiting for approval that never comes
  unless `--dangerously-skip-permissions` is active or the Routine is explicitly
  configured to bypass the gate.
- For safe unattended operation, design Routines to use only `PermRead` tools
  (file reads, grep, code search, git log). Reserve write-capable Routines for
  environments where `--dangerously-skip-permissions` is acceptable.

---

## Configuration

The global scheduler switch lives in `~/.huginn/config.json`:

```json
{
  "scheduler_enabled": true
}
```

Setting `scheduler_enabled: false` pauses all automations — no Routines or Workflows
will fire — without deleting any YAML files. Individual Routines can be disabled
via the `enabled: false` field in their YAML without affecting others.

---

## REST API

### Routines

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/routines` | List all routines |
| `GET` | `/api/v1/routines/{id}` | Get a single routine |
| `POST` | `/api/v1/routines` | Create a new routine |
| `PUT` | `/api/v1/routines/{id}` | Update a routine (replaces YAML on disk) |
| `DELETE` | `/api/v1/routines/{id}` | Delete a routine |

Creating or updating a Routine via the API re-registers it with the cron instance
immediately — no restart required.

### Notifications

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/notifications` | List notifications (supports `?unread_only=true`) |
| `POST` | `/api/v1/notifications/{id}/read` | Mark a notification as read |
| `DELETE` | `/api/v1/notifications/{id}` | Delete a notification |

---

## Web UI

**Automation panel** (bolt icon in sidebar) — Shows the 3 most recent Routines and
Workflows as quick-access links. Clicking "Routines" or "Workflows" navigates to
their full management pages.

**`/routines/{id}`** — Routine detail and editor:
- All fields are directly editable inline — no separate "edit mode" required
- **Agent** field: `@`-mention autocomplete dropdown listing configured agents
- **Schedule** field: cron expression input with a human-readable description
  (e.g. `0 8 * * 1-5` displays as "Every weekday at 8:00 AM") and preset quick-select
  chips for common schedules
- **Prompt** field: resizable textarea
- A "Save Changes" / "Discard" bar appears when unsaved changes are present
- Run Now, Enable/Disable, and Delete actions in the header
- Built-in template picker ("add from template →") for common Routine patterns

**`/inbox`** — Notification inbox:
- Paginated list of notifications ordered by `created_at` descending
- Each notification shows: routine name, timestamp, title, and expandable body
- Mark-read action (single or bulk); unread notifications are highlighted
- Badge overlay on the Inbox nav icon, updated in real time over WebSocket

---

## Relationship to Workflows

A Workflow is an ordered sequence of Routines that shares a single cron trigger.
The Workflow fires at its configured time, then executes each referenced Routine in
sequence via `WorkflowRunner`.

From the Scheduler's perspective, a Workflow is just another cron entry registered
in the same `robfig/cron` instance. Routines referenced by Workflows do not need
their own `trigger.cron` — they can be workflow-only (no top-level schedule).

See [workflows.md](workflows.md) for full details.

---

## See Also

- [workflows.md](workflows.md) — Ordered Routine sequences with per-step failure handling
- [multi-agent.md](multi-agent.md) — Live sub-agent delegation in interactive sessions
- [session-and-memory.md](session-and-memory.md) — Session store and MuninnDB memory
- [permissions-and-safety.md](permissions-and-safety.md) — Three-tier permission gate
