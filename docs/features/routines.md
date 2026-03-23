# Routines

## What it is

YAML-defined agent tasks that run automatically on a cron schedule. Define a Routine once, and Huginn runs it at the configured time — without any manual trigger. Results from each Routine run land in the **Inbox** in the web UI, separate from your active chat sessions.

The Scheduler runs entirely inside the Huginn process using [`robfig/cron/v3`](https://github.com/robfig/cron). There is no dependency on any OS scheduling mechanism — no `crontab`, `launchd`, or Windows Task Scheduler required. Routines fire as long as the Huginn process is running.

---

## How to use it

### Create a Routine

Create a YAML file in `~/.huginn/routines/`. The filename stem (the slug) is used to reference the Routine from Workflows.

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

This Routine runs every weekday at 9am and has Mark review all open PRs, summarizing changes and flagging issues.

### Check Routine results

Open the web UI (`huginn tray`) and click **Inbox** in the left sidebar. Each Routine run appears as a separate notification with the Routine name, timestamp, and the full agent output. Unread notifications are highlighted and a live badge counter updates in real time over WebSocket.

You can also manage Routines directly from the web UI. The **Automation panel** (bolt icon in sidebar) shows recent Routines as quick-access links. Clicking through to `/routines/{id}` opens a detail and editor page where all fields are editable inline — no separate edit mode required.

### Cron expression syntax

Huginn uses standard 5-field cron syntax. A seconds field is **not** supported — Routines are not designed for sub-minute scheduling.

```
┌───── minute (0–59)
│ ┌───── hour (0–23)
│ │ ┌───── day of month (1–31)
│ │ │ ┌───── month (1–12)
│ │ │ │ ┌───── day of week (0–6, Sunday = 0)
│ │ │ │ │
* * * * *
```

Common examples:

| Expression | Meaning |
|-----------|---------|
| `0 9 * * 1-5` | Every weekday at 9:00am |
| `0 8 * * 1` | Every Monday at 8:00am |
| `0 23 * * *` | Every night at 11:00pm |
| `*/30 * * * *` | Every 30 minutes |

The schedule field in the UI shows a human-readable description alongside the cron expression (e.g. `0 9 * * 1-5` displays as "Every weekday at 9:00 AM") and offers preset quick-select chips for common schedules.

---

## Configuration

### Routine YAML fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Stable unique identifier (short UUID or human-chosen string) |
| `name` | string | Display name shown in the UI and Inbox |
| `description` | string | Optional long description |
| `enabled` | bool | When `false`, the Routine is loaded but not scheduled |
| `trigger.mode` | enum | `schedule` or `manual` (future: `event`) |
| `trigger.cron` | string | Standard 5-field cron expression (required when `trigger.mode` is `schedule`) |
| `agent` | string | Agent name to run (`Chris`, `Steve`, `Mark`, or a custom agent) |
| `prompt` | string | Prompt sent to the agent each time the Routine fires |
| `timeout_secs` | int | Maximum seconds before the headless session is cancelled (default: `300`) |

### Global scheduler switch

The scheduler can be paused entirely in `~/.huginn/config.json`:

```json
{
  "scheduler_enabled": true
}
```

Setting `scheduler_enabled: false` pauses all automations without deleting any YAML files. Individual Routines can also be disabled via `enabled: false` in their YAML without affecting others.

### Workflow-only Routines

A Routine with `trigger.mode: manual` (or no `trigger.cron`) does not fire on its own schedule. It can still be referenced and executed as a step inside a Workflow. See [Workflows](workflows.md) for user documentation and [Workflow Architecture](../architecture/workflows.md) for internals.

### Permissions

Routines run with the same three-tier permission gate as interactive agents (`PermRead`, `PermWrite`, `PermExec`). Write and Exec tools still require approval by default, even in headless sessions. A Routine that calls a write tool will stall waiting for approval that never arrives.

For safe unattended operation, design Routines to use only `PermRead` tools — file reads, grep, code search, git log. Reserve write-capable Routines for environments where `--dangerously-skip-permissions` is acceptable.

### Failure handling

If the agent session returns an error or the `timeout_secs` limit is reached, the routine run is marked as failed and the Inbox notification is flagged accordingly. No retry is attempted automatically. Design routines defensively — prefer read-only prompts where possible to minimize the chance of a stall.

### Variable substitution

Variable substitution in `prompt` fields is not supported in the current release. Prompts are sent verbatim to the agent.

---

## Tips & common patterns

- **Use the Inbox to review without interrupting active sessions** — Routine results never appear in your active chat. Check the Inbox at the start of your day for overnight or early-morning output.
- **Keep Routines read-only for unattended safety** — write and exec tools require approval by default. A Routine blocked on an approval prompt will stall until `timeout_secs` is reached.
- **Set `enabled: false` to pause without deleting** — you can turn a Routine off temporarily in YAML or via the UI without losing its configuration.
- **Start with a simple single-step Routine** — confirm the output format and timing before building multi-step Workflows on top of it. Cron mistakes are easy to make; test with a short interval first.
- **Use the template picker in the UI** — the Routine editor has a built-in template picker ("add from template") for common patterns like PR review, dependency scanning, and end-of-day summaries.
- **Changes take effect immediately** — creating or updating a Routine via the UI or REST API re-registers it with the cron instance right away. No restart required.

---

## Troubleshooting

**Routine not running**

Check the cron expression with a validator (e.g., [crontab.guru](https://crontab.guru)). A common mistake is using 24-hour time where a 12-hour format was intended. Also confirm that Huginn is running when the scheduled time arrives — Routines only execute while the Huginn process is active. The Scheduler does not fire missed jobs from while Huginn was stopped.

**Results not appearing in Inbox**

The Inbox is only visible in the web UI (`huginn tray`). Verify `scheduler_enabled` is `true` in `~/.huginn/config.json` and that the Routine's `enabled` field is `true`. If the Routine ran but produced no output, check `timeout_secs` — the session may have been cancelled before the agent finished.

**Routine stalls and never completes**

The agent likely attempted a write or exec tool that is waiting for permission approval. Design unattended Routines to use only read-oriented tools, or run Huginn with `--dangerously-skip-permissions` if your environment permits it.

**Unrecognized agent name**

Check that the value in `agent:` exactly matches a configured agent name (`Chris`, `Steve`, or `Mark` by default, or a custom agent name from your configuration). An unrecognized agent name causes the headless session to fail at startup.

---

## See Also

- [Workflows](workflows.md) — chain Routines into ordered pipelines with variables and failure handling
- [Permissions](permissions.md) — why write tools stall in unattended Routines and how to handle it
- [Headless Mode](headless.md) — running Routines in Docker and CI environments
