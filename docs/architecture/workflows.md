# Workflows

**Package**: `internal/scheduler` (WorkflowRunner)
**Related**: [scheduler.md](scheduler.md), [multi-agent.md](multi-agent.md)

---

## Overview

A Workflow is an ordered sequence of Routines that share a single cron trigger.
Where a Routine is a single agent task, a Workflow composes multiple Routines into
a pipeline — each step runs after the previous one completes, with configurable
failure handling at each step.

A typical use case: every weekday morning, run standup-prep, then PR review, then
dependency scan, each as a separate agent prompt, in order, stopping if any critical
step fails.

Workflows build on the Routine and Scheduler infrastructure described in
[scheduler.md](scheduler.md).

---

## Workflow YAML Format

Workflows live at `~/.huginn/workflows/{id}.yaml`.

```yaml
id: "wf-a1b2c3"
name: "Daily Dev Cycle"
description: "Morning standup prep, then PR review, then dependency scan"
enabled: true
trigger:
  mode: schedule
  cron: "0 8 * * 1-5"
steps:
  - routine: standup-prep       # slug = filename stem of ~/.huginn/routines/standup-prep.yaml
    on_failure: stop            # default
  - routine: pr-review
    on_failure: continue        # run next step even if this fails
  - routine: dep-scan
    on_failure: stop
```

| Field | Type | Description |
|---|---|---|
| `id` | string | Stable unique identifier |
| `name` | string | Display name shown in the UI |
| `description` | string | Optional long description |
| `enabled` | bool | When false the Workflow is loaded but not scheduled |
| `trigger.mode` | enum | `schedule` \| `manual` (same modes as Routines) |
| `trigger.cron` | string | Standard 5-field cron expression |
| `steps` | array | Ordered list of step definitions |
| `steps[].routine` | string | Slug of the Routine to run (filename stem) |
| `steps[].on_failure` | enum | `stop` (default) or `continue` |

---

## Slug Resolution

Steps reference Routines by **slug** — the filename stem of the Routine YAML. For
example, the file `~/.huginn/routines/pr-review.yaml` has slug `pr-review`.

`WorkflowRunner` resolves slugs at run time by scanning `~/.huginn/routines/` and
matching the stem against `steps[].routine`. This means:

- Renaming a Routine file changes its slug and breaks references in Workflows.
- The `id` field inside the Routine YAML is not used for slug resolution — only the
  filename stem matters.
- A Routine used in a Workflow does not need its own `trigger.cron`; it can be
  a workflow-only Routine (no top-level schedule).

---

## WorkflowRunner Execution Flow

```
Scheduler fires at workflow cron time
     │
     ▼
WorkflowRunner.Run(ctx, workflow)
     │
     ▼
for each step in workflow.steps (in order):
     │
     ├── resolve slug → Routine YAML
     │   (error if slug not found → treat as step failure)
     │
     ├── run via scheduler.Runner (same path as a standalone Routine)
     │   creates headless agent session, waits for completion
     │
     ├── step succeeded ──► record result, continue to next step
     │
     └── step failed:
           on_failure: stop ──► log error, mark workflow failed, halt
           on_failure: continue ──► log error, record partial result,
                                    continue to next step
     │
     ▼
Run history entry appended to ~/.huginn/workflows/{id}.runs.jsonl
```

Each step runs sequentially. There is no parallelism within a Workflow — steps are
intentional pipeline stages, not parallel branches. For parallel execution, use the
Swarm orchestrator (see [swarm.md](swarm.md)).

---

## Run History

Run history is stored as newline-delimited JSON at
`~/.huginn/workflows/{id}.runs.jsonl`. Each line is one run entry:

| Field | Type | Description |
|---|---|---|
| `workflow_id` | string | Workflow ID |
| `started_at` | time.Time | When the WorkflowRunner began |
| `completed_at` | time.Time | When the run finished (all steps done or halted) |
| `status` | enum | `completed` \| `failed` \| `partial` |
| `steps` | array | Per-step result array |
| `steps[].routine` | string | Slug of the step |
| `steps[].status` | enum | `completed` \| `failed` \| `skipped` |
| `steps[].notification_id` | string | ID of the Notification produced (if any) |

**Status semantics:**
- `completed` — all steps ran and succeeded.
- `failed` — a step with `on_failure: stop` failed; subsequent steps were not run.
- `partial` — one or more steps failed with `on_failure: continue`; execution finished.

Run history is append-only. The API exposes the last N runs; older entries are not
pruned automatically in v1.

---

## Step Variables

Routines can declare **variables** in their YAML, and Workflow steps can override
them per-step. This allows a single Routine to be reused with different parameters
across multiple Workflows without duplicating YAML files.

### Declaring variables in a Routine

```yaml
# ~/.huginn/routines/pr-review.yaml
name: "PR Review"
vars:
  TARGET_BRANCH:
    description: "Branch to filter PRs against"
    default: "main"
    required: false
  REVIEWER_TEAM:
    description: "GitHub team slug to check for review assignments"
    default: ""
    required: true
prompt: |
  Review open PRs targeting {{TARGET_BRANCH}}.
  Check whether {{REVIEWER_TEAM}} has been assigned as reviewer.
```

| Field | Type | Description |
|---|---|---|
| `vars` | map | Map of variable name → `RoutineVar` definition |
| `vars[].description` | string | Human-readable description shown in the UI |
| `vars[].default` | string | Default value used if the step does not override it |
| `vars[].required` | bool | If true, the step must supply a value (or default must be non-empty) |

Variable names are substituted using `{{VAR_NAME}}` in the `prompt` field. Substitution
is a simple string replace — not a template engine. Variables that are declared but not
present in the prompt are silently ignored.

### Overriding variables in a Workflow step

```yaml
steps:
  - routine: pr-review
    vars:
      TARGET_BRANCH: "release/v2"
      REVIEWER_TEAM: "platform-eng"
    on_failure: stop
```

`WorkflowRunner` calls `ResolvePrompt` before executing each step:

1. Start with the Routine's declared `vars` defaults.
2. Merge step-level `vars` overrides on top.
3. Validate that all `required` variables have a non-empty value.
4. Substitute `{{VAR_NAME}}` placeholders in the prompt string.
5. Run the headless session with the resolved prompt.

If a required variable is missing a value, the step fails immediately with a
descriptive error rather than running the agent with an incomplete prompt.

---

## Design Constraints

These constraints are intentional v1 decisions, not gaps:

**No inter-step data passing.** Each Routine's prompt is defined statically in its
YAML. There is no mechanism to pipe one step's output as the next step's input.
Steps communicate only through shared side effects (files written, notifications
posted). This keeps Routines independently runnable and testable.

**No nested Workflows.** Steps must be Routines. A Workflow cannot reference another
Workflow as a step. This prevents hard-to-debug recursive scheduling and keeps the
execution model flat.

**No conditional branching.** The only control flow is `on_failure: stop | continue`.
There is no `if`, `switch`, or dynamic step selection. Complex conditional logic
should be expressed inside a single Routine's prompt rather than in Workflow structure.

---

## REST API

Seven new routes are added alongside the existing Routine and Notification endpoints:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/workflows` | List all workflows |
| `GET` | `/api/v1/workflows/{id}` | Get a workflow including its steps |
| `POST` | `/api/v1/workflows` | Create a new workflow |
| `PUT` | `/api/v1/workflows/{id}` | Update a workflow (replaces YAML on disk) |
| `DELETE` | `/api/v1/workflows/{id}` | Delete a workflow |
| `GET` | `/api/v1/workflows/{id}/runs` | Run history (last N runs, newest first) |
| `POST` | `/api/v1/workflows/{id}/run` | Trigger workflow manually (ignores cron) |

Creating or updating a Workflow via the API re-registers its cron entry immediately
without a restart.

---

## Vue Management View

**`/workflows`** — Card grid view. Each Workflow is displayed as a card showing its
name, description, schedule, step count, and enabled status. A search bar filters
cards in real time. Clicking a card navigates to the detail view. The "+ New Workflow"
button opens a creation modal (name, description, optional cron schedule).

**`/workflows/{id}`** — Workflow detail view. Breadcrumb back to the grid. Header
shows the workflow name with Enable/Disable, Run Now, and Delete actions.

**Metadata panel**: Status, schedule (cron expression), and description.

**Steps list**: Ordered list of steps showing the routine slug and `on_failure`
setting. Add Step button opens a modal to specify slug, position, and failure mode.
Steps can be removed individually.

**Run history**: Append-only list of past runs for this Workflow — started_at,
overall status (completed / failed / partial), and per-step status breakdown
(step position, slug, success/failure).

---

## Relationship to Scheduler

Workflow cron entries are registered in the same `robfig/cron/v3` instance that
handles standalone Routine schedules. The `Scheduler` type exposes:

```go
// Routine registration (existing)
scheduler.Register(routine)
scheduler.Unregister(routineID)

// Workflow registration (Phase 2)
scheduler.RegisterWorkflow(workflow)
scheduler.UnregisterWorkflow(workflowID)
```

At startup, `Scheduler.Load()` reads both `~/.huginn/routines/*.yaml` and
`~/.huginn/workflows/*.yaml` and registers enabled entries. The cron instance
knows nothing about Workflows specifically — from its perspective, every registered
entry is just a function to call at a given cron expression.

---

## See Also

- [scheduler.md](scheduler.md) — Routines, cron triggers, headless sessions, Inbox notifications
- [multi-agent.md](multi-agent.md) — Live sub-agent delegation in interactive sessions
- [swarm.md](swarm.md) — Parallel agent execution (distinct from sequential Workflows)
- `internal/scheduler/workflow_runner.go` — WorkflowRunner implementation
- `internal/scheduler/scheduler.go` — Scheduler.RegisterWorkflow / UnregisterWorkflow
