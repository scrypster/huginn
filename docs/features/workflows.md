# Workflows

## What it is

A Workflow chains multiple Routines into an ordered pipeline that runs on a single cron schedule. Where a Routine is one agent task (one prompt, one agent, one output), a Workflow is a sequence of Routines that run one after the other — with configurable failure handling between each step. Use a Workflow when you have a series of tasks that should happen in a fixed order and you want a single schedule to drive all of them.

This guide assumes you have already read [routines.md](routines.md). Workflows build directly on the Routine system — every step in a Workflow is a Routine.

---

## Routine vs Workflow

| | Routine | Workflow |
|---|---|---|
| **What runs** | One agent prompt | Ordered sequence of Routines |
| **Schedule** | Its own `trigger.cron` | One shared `trigger.cron` for all steps |
| **Failure handling** | Stop on error | Per-step `on_failure: stop` or `continue` |
| **Output** | One Inbox notification | One notification per step that runs |
| **File** | `~/.huginn/routines/{slug}.yaml` | `~/.huginn/workflows/{id}.yaml` |
| **When to use** | Single task | Multi-step pipeline |

If you find yourself creating several Routines that all fire at the same time and logically depend on each other running in sequence, convert them into a Workflow.

---

## How to use it

### Create your first Workflow

Workflows reference Routines by **slug** — the filename stem of the Routine YAML file, not the `id` field inside it. If your Routine file is `~/.huginn/routines/standup-prep.yaml`, its slug is `standup-prep`.

Start by creating the Routines you want to chain. Both of these use `trigger.mode: manual` so they do not run on their own schedule — only the Workflow will trigger them.

```yaml
# ~/.huginn/routines/standup-prep.yaml
id: "rtn-standup-001"
name: "Standup Prep"
description: "Summarize yesterday's commits and open PRs for standup"
enabled: true
trigger:
  mode: manual
agent: Chris
prompt: |
  Review the git log for commits since yesterday at 5pm.
  List each commit with its author and a one-line summary.
  Then list any open PRs and their current review status.
  Format the output as a standup-friendly bullet list.
timeout_secs: 120
```

```yaml
# ~/.huginn/routines/pr-review.yaml
id: "rtn-pr-review-001"
name: "PR Review"
description: "Scan all open PRs and flag issues"
enabled: true
trigger:
  mode: manual
agent: Mark
prompt: |
  Review all open PRs in this repository. For each one, summarize:
  - What the change does
  - Whether tests cover the change
  - Any obvious issues or missing documentation
  Report findings as a structured list, flagging any PRs that need attention.
timeout_secs: 300
```

Now create the Workflow that chains them:

```yaml
# ~/.huginn/workflows/morning-cycle.yaml
id: "wf-morning-cycle"
name: "Morning Dev Cycle"
description: "Standup prep followed by a full PR review, every weekday morning"
enabled: true
trigger:
  mode: schedule
  cron: "0 8 * * 1-5"
steps:
  - routine: standup-prep
    on_failure: stop
  - routine: pr-review
    on_failure: continue
```

When this Workflow fires at 8am on weekdays, it runs `standup-prep` first. If that step fails, execution stops (because `on_failure: stop`). If it succeeds, `pr-review` runs next. If `pr-review` fails, execution continues to the next step anyway (because `on_failure: continue`) — though in this example there are no further steps.

**Important:** the slug `standup-prep` must exactly match the filename stem `standup-prep.yaml`. The `id` field inside the YAML (`rtn-standup-001`) is not used for slug resolution.

### Using variables to parameterize Routines

Routines can declare variables that callers (Workflows) can override per-step. This lets you write one Routine and reuse it in multiple Workflows with different parameters.

Declare variables in the Routine's `vars` block:

```yaml
# ~/.huginn/routines/pr-review.yaml
id: "rtn-pr-review-001"
name: "PR Review"
enabled: true
trigger:
  mode: manual
agent: Mark
vars:
  TARGET_BRANCH:
    description: "Filter PRs that target this branch"
    default: "main"
    required: false
  REVIEWER_TEAM:
    description: "GitHub team slug to check for review assignments"
    default: ""
    required: true
prompt: |
  Review open PRs targeting {{TARGET_BRANCH}}.
  Check whether members of {{REVIEWER_TEAM}} have been assigned as reviewers.
  Flag any PRs that are unreviewed or stale.
timeout_secs: 300
```

Override the variables in your Workflow step:

```yaml
steps:
  - routine: pr-review
    on_failure: stop
    vars:
      TARGET_BRANCH: "release/v2"
      REVIEWER_TEAM: "platform-eng"
```

**Variable resolution order:**

1. Start with the Routine's declared `vars` defaults.
2. Merge step-level `vars` overrides on top.
3. Validate that all `required: true` variables have a non-empty value.
4. Substitute `{{VAR_NAME}}` placeholders in the `prompt` string.
5. Run the agent with the resolved prompt.

If a required variable has no value after merging (no default and no step override), the step fails immediately with an error describing which variable is missing. The agent is never invoked with an incomplete prompt.

Variable substitution is a simple string replace — not a template engine. There are no loops, conditionals, or filters. Variables that appear in `vars` but not in the `prompt` string are silently ignored.

### Manage via the web UI

Open the web UI (`huginn tray`) and navigate to `/workflows`. You will see a card grid with each Workflow showing its name, description, schedule, step count, and enabled status. A search bar at the top filters the grid in real time.

Click any card to open the Workflow detail view. From there you can:

- **Enable or disable** the Workflow without editing YAML.
- **Run Now** — triggers the Workflow immediately, ignoring the cron schedule. Useful for testing.
- **Manage steps** — add a step (specify slug, position, `on_failure`), reorder, or remove individual steps.
- **View run history** — an append-only list of past runs showing `started_at`, overall status (`completed` / `failed` / `partial`), and per-step status breakdown.

Creating or updating a Workflow in the UI takes effect immediately. No restart required.

### Trigger manually via the REST API

To fire a Workflow outside of its schedule, send a POST request to the run endpoint:

```bash
curl -X POST http://localhost:8421/api/v1/workflows/wf-morning-cycle/run
```

The Workflow runs immediately in the background. Poll `/api/v1/workflows/wf-morning-cycle/runs` to check the result:

```bash
curl http://localhost:8421/api/v1/workflows/wf-morning-cycle/runs
```

The response is an array of run entries, newest first.

---

## Configuration

### Workflow YAML reference

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Stable unique identifier. Used in file paths and API routes. |
| `name` | string | Yes | Display name shown in the web UI. |
| `description` | string | No | Optional long description. |
| `enabled` | bool | Yes | When `false`, the Workflow is loaded but not scheduled. |
| `trigger.mode` | enum | Yes | `schedule` or `manual`. Use `manual` for Workflows you only trigger via API or the Run Now button. |
| `trigger.cron` | string | When `mode` is `schedule` | Standard 5-field cron expression. |
| `steps` | array | Yes | Ordered list of step definitions. Steps run sequentially. |
| `steps[].routine` | string | Yes | Slug of the Routine to run. Must match the filename stem of a file in `~/.huginn/routines/`. |
| `steps[].on_failure` | enum | No | `stop` (default) or `continue`. |
| `steps[].vars` | map | No | Variable overrides for this step. Keys are variable names; values are strings. |

### Variable definition reference

Declared in the Routine YAML's `vars` block:

| Field | Type | Required | Description |
|---|---|---|---|
| `vars` | map | No | Map of variable name to `RoutineVar` definition. Key is the variable name used in `{{VAR_NAME}}` substitution. |
| `vars[].description` | string | No | Human-readable description. Shown in the web UI's step editor. |
| `vars[].default` | string | No | Value used when the Workflow step does not override this variable. |
| `vars[].required` | bool | No | When `true`, the step must supply a value, or `default` must be non-empty. A missing required variable causes the step to fail immediately. |

---

## Failure handling and run history

### on_failure behavior

Each step declares what happens if that step fails:

| Value | Behavior |
|---|---|
| `stop` (default) | The Workflow halts. Subsequent steps do not run. The overall run status is `failed`. |
| `continue` | The failure is logged and recorded, but the next step runs anyway. If any step fails with `continue`, the overall run status is `partial`. |

Use `stop` for steps that later steps depend on. Use `continue` for optional or non-critical steps where you want the pipeline to finish even if that step has an error.

### Run status values

| Status | Meaning |
|---|---|
| `completed` | All steps ran and succeeded. |
| `failed` | A step with `on_failure: stop` failed. Subsequent steps were not run. |
| `partial` | One or more steps failed with `on_failure: continue`. The pipeline ran to completion. |

### Run history storage

Each Workflow maintains an append-only run history at:

```
~/.huginn/workflows/{id}.runs.jsonl
```

Each line is a JSON object with: `workflow_id`, `started_at`, `completed_at`, `status`, and a `steps` array with per-step `routine` slug, `status` (`completed` / `failed` / `skipped`), and the notification ID of any Inbox result produced.

The history is viewable in the web UI on the Workflow detail page. Entries are never pruned automatically.

---

## Design constraints

These are intentional v1 design decisions.

**No inter-step data passing.** Each step's prompt is defined statically in its Routine YAML. There is no mechanism to pipe one step's output as the next step's input. Steps communicate only through shared side effects — files written, notifications posted, memory stored. This keeps each Routine independently runnable and testable.

**No nested Workflows.** A Workflow step must be a Routine. You cannot reference another Workflow as a step. This prevents recursive scheduling and keeps the execution model flat. If you need a more complex dependency graph, use the Swarm orchestrator (see [multi-agent.md](multi-agent.md)).

**No conditional branching.** The only control flow is `on_failure: stop | continue`. There is no `if`, `switch`, or dynamic step selection. If your pipeline needs conditional logic, express it inside a single Routine's prompt rather than in Workflow structure.

**Variable substitution is string replace, not a template engine.** `{{VAR_NAME}}` placeholders are replaced with their resolved string values. There are no loops, conditionals, filters, or nested substitution.

---

## Tips & common patterns

**Test each Routine individually before composing it into a Workflow.** Run each Routine via the web UI's Run Now button or the REST API. Confirm the output format and timing before wiring them together. A Routine that stalls in isolation will stall in a Workflow too.

**Use `on_failure: continue` for non-critical steps.** If your morning pipeline includes an optional dependency scan that occasionally times out, mark it `continue` so a slow scan does not block the PR review step that runs after it.

**Use `trigger.mode: manual` for Workflow-only Routines.** If a Routine is only meant to be used as a step inside a Workflow, set its trigger mode to `manual`. It will never run on its own schedule, reducing noise in the Inbox.

**Keep Routines read-only for unattended safety.** Routines run in headless mode where write and exec tools require approval by default. A Routine that calls `write_file` or `bash` will stall waiting for approval that never arrives. Design Routine prompts to read and report, not to modify state. See [permissions.md](permissions.md) for the full picture.

**Changes take effect immediately.** Creating or updating a Workflow via the web UI or REST API re-registers its cron entry right away. You do not need to restart Huginn.

---

## Troubleshooting

**Slug not found**

The Workflow step references a slug that does not resolve to any file in `~/.huginn/routines/`. Check that the filename stem of the Routine YAML matches the slug exactly, including case. For example, if the step says `routine: pr-review`, the file must be `pr-review.yaml` — not `PR-Review.yaml` or `pr_review.yaml`. Renaming a Routine file changes its slug and breaks any Workflow step that referenced the old name.

**Required variable missing**

A step fails immediately with an error about a missing variable. Either add the variable to the step's `vars` block in the Workflow YAML, or set a non-empty `default` in the Routine's `vars` declaration. Check that the variable name in the step's `vars` map exactly matches the name in the Routine's `vars` block.

**Workflow not firing on schedule**

First, verify that Huginn is running when the scheduled time arrives — Routines and Workflows only execute while the Huginn process is active. The Scheduler does not fire missed jobs. Second, check the cron expression with a validator such as [crontab.guru](https://crontab.guru). Third, confirm that `enabled: true` is set in the Workflow YAML and that `scheduler_enabled` is `true` in `~/.huginn/config.json`. Finally, check that `trigger.mode: schedule` is set — a Workflow with `trigger.mode: manual` never fires on a cron schedule.

**Run shows `partial` but steps look correct**

At least one step failed with `on_failure: continue`. Open the Workflow detail view in the web UI and expand the run entry to see the per-step status breakdown. The failing step will show `failed` with an error message. Check that step's Routine YAML and test it independently.

---

## See Also

- [routines.md](routines.md) — Routine YAML format, cron syntax, Inbox integration, and scheduler configuration
- [permissions.md](permissions.md) — permission tiers and headless mode behavior that affects Routines running inside Workflows
- [headless.md](headless.md) — how headless sessions work and the `--dangerously-skip-permissions` flag
- [multi-agent.md](multi-agent.md) — parallel agent execution for cases where sequential Workflows are not sufficient
