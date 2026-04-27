# Workflows

**Package**: `internal/scheduler` (WorkflowRunner)
**Related**: [scheduler.md](scheduler.md), [multi-agent.md](multi-agent.md)

---

## Overview

A Workflow is an ordered sequence of **inline steps** that share a single cron
trigger. Each step bundles an agent, a prompt, optional connections, optional
inputs from prior steps, and per-step retry/timeout/notify configuration.
Workflows compose multiple agent tasks into a pipeline — each step runs after
the previous one completes, with configurable failure handling.

A typical use case: every weekday morning, run standup-prep, then PR review,
then dependency scan, each as a separate agent prompt, in order, stopping if
any critical step fails.

Workflows build on the Scheduler infrastructure described in
[scheduler.md](scheduler.md).

> **Legacy note.** Earlier versions of Huginn referenced steps by Routine
> slug. That model has been folded into inline steps. Routines on disk are
> migrated to single-step workflows on first boot (see
> [Migration](#migration-from-legacy-routines) below). Slug references at
> step level are rejected by the runner.

---

## Workflow YAML Format

Workflows live at `~/.huginn/workflows/{id}.yaml`.

```yaml
id: "wf-a1b2c3"
slug: "daily-dev-cycle"
name: "Daily Dev Cycle"
description: "Morning standup prep, then PR review, then dependency scan"
enabled: true
schedule: "0 8 * * 1-5"
timeout_minutes: 30
tags: ["morning", "dev"]
steps:
  - name: "Standup prep"
    agent: "Chris"
    prompt: |
      Summarise yesterday's commits and today's open PRs.
    on_failure: stop
    position: 0

  - name: "PR review"
    agent: "Reviewer"
    prompt: |
      Review open PRs targeting {{TARGET_BRANCH}}.
    vars:
      TARGET_BRANCH: "main"
    connections:
      github: "gh-default"
    on_failure: continue
    max_retries: 2
    retry_delay: "30s"
    timeout: "10m"
    position: 1
    inputs:
      - from_step: "Standup prep"
        as: "summary"
    notify:
      on_failure: true
      deliver_to:
        - { type: "inbox" }

  - name: "Dependency scan"
    agent: "Scanner"
    prompt: |
      Scan dependencies. Context from PR review:
      {{prev.output}}
    on_failure: stop
    position: 2

notification:
  on_failure: true
  severity: "warning"
  deliver_to:
    - { type: "inbox" }
```

| Field | Type | Description |
|---|---|---|
| `id` | string | Stable unique identifier |
| `slug` | string | Optional human-friendly identifier (defaults to filename stem) |
| `name` | string | Display name shown in the UI |
| `description` | string | Optional long description |
| `enabled` | bool | When false the workflow is loaded but not scheduled |
| `schedule` | string | Standard 5-field cron expression; empty = manual only |
| `timeout_minutes` | int | Per-run hard cap (default 30, max 1440) |
| `tags` | []string | Free-form tags for filtering in the UI |
| `steps` | array | Ordered list of inline step definitions |
| `notification` | object | Workflow-level notification config |
| `version` | uint64 | Optimistic-locking counter — incremented on every save |

### Step fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Display name; also used as the input alias source key |
| `agent` | string | Name of the agent that executes this step |
| `prompt` | string | Prompt template; supports `{{VAR}}`, `{{inputs.alias}}`, `{{prev.output}}`, `{{run.scratch.K}}` |
| `connections` | map | provider → connection ID (e.g. `github: gh-default`) |
| `vars` | map | Static variables substituted via `{{VAR_NAME}}` |
| `inputs` | array | `{from_step, as}` aliases referencing earlier steps |
| `position` | int | Sort key (ascending). Ties are stable. |
| `on_failure` | enum | `stop` (default) or `continue` |
| `max_retries` | int | 0–10. Retries on transient failure (per step). Inherits from `workflow.retry` when 0. |
| `retry_delay` | duration | e.g. `"5s"`, `"1m"`. Validated at load time. Inherits from `workflow.retry` when empty. |
| `timeout` | duration | 1s–24h. Per-step ceiling, in addition to the workflow timeout. |
| `notify` | object | Per-step notification config (see [Notifications](#notifications)) |
| `model_override` | string | *(Phase 7)* Override the agent's model for this step only. |
| `when` | string | *(Phase 8)* Skip this step when the resolved expression is falsy. |
| `sub_workflow` | string | *(Phase 8)* Invoke another workflow by ID as this step's body. |

---

## Inter-Step Data Flow

Steps **can** pass data forward. Three substitution mechanisms run in order
each step:

1. **Static vars** — `{{VAR_NAME}}` is replaced from `step.vars` (and from
   the workflow-wide vars map if any).
2. **Named inputs** — `{{inputs.alias}}` is replaced with the output of an
   earlier step referenced by `step.inputs[].from_step` and named via
   `step.inputs[].as`.
3. **Implicit prev** — `{{prev.output}}` is replaced with the most recent
   **successful** prior step's output. Failed steps that ran with
   `on_failure: continue` are skipped: `{{prev.output}}` resolves to the
   nearest preceding succeeding step's output (or `""` if none).

### Unresolved-placeholder safety

If after substitution the prompt still contains a `{{...}}` placeholder, the
runner **fails the step** rather than passing a degraded prompt to the
agent. The error captured in the run history names the unresolved
placeholders so the misconfiguration can be fixed quickly. This is a
correctness safety net — it makes broken inputs surface as a hard failure
instead of silently producing useless output.

---

## WorkflowRunner Execution Flow

```
Scheduler fires at workflow cron time (or POST /run)
     │
     ▼
WorkflowRunner(ctx, workflow)
     │
     ▼
sort steps by Position ascending
     │
     ▼
for each step:
     │
     ├── resolve {{VAR}} from step.vars
     ├── resolve {{inputs.alias}} from prior step outputs
     ├── resolve {{prev.output}} from most recent successful step
     ├── reject step if any placeholder remains unresolved
     │
     ├── run agentFn (per-step timeout enforced)
     │
     ├── step succeeded ──► record output, continue
     │
     └── step failed:
           on_failure: stop ──► record failure, halt run
           on_failure: continue ──► record failure, continue to next step
     │
     ▼
emit terminal status: complete | partial | failed | cancelled
emit notifications (workflow + step level)
append run record (JSONL or SQLite)
broadcast WS events
```

Steps run sequentially. There is no parallelism within a Workflow — steps
are intentional pipeline stages. For parallel execution use the Swarm
orchestrator (see [swarm.md](swarm.md)).

---

## Run History

Run history is stored either as newline-delimited JSON at
`~/.huginn/workflow-runs/{workflow_id}.jsonl` or, when SQLite is enabled,
in the `workflow_runs` table. The runtime supports a one-time JSONL →
SQLite migration on first boot.

| Field | Type | Description |
|---|---|---|
| `id` | string | Run ID (ULID) |
| `workflow_id` | string | Workflow ID |
| `status` | enum | `running` \| `complete` \| `partial` \| `failed` \| `cancelled` |
| `started_at` | time.Time | When the runner started |
| `completed_at` | time.Time | When the run finished (terminal state) |
| `error` | string | Workflow-level error if any |
| `steps` | array | Per-step results |
| `steps[].position` | int | Step position |
| `steps[].slug` | string | Step name (legacy field name) |
| `steps[].session_id` | string | Headless session ID for this step |
| `steps[].status` | enum | `success` \| `failed` \| `skipped` |
| `steps[].error` | string | Step error message if failed |
| `steps[].output` | string | Step output (truncated at 64 KiB) |

**Status semantics:**

- `complete` — all steps ran and succeeded.
- `partial` — one or more `on_failure: continue` steps failed; the run still
  finished its remaining work.
- `failed` — a step with `on_failure: stop` failed and aborted the run.
- `cancelled` — the user (or shutdown) cancelled the run.

---

## REST API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/workflows` | List all workflows |
| `GET` | `/api/v1/workflows/{id}` | Get a workflow including its steps |
| `POST` | `/api/v1/workflows` | Create a new workflow |
| `PUT` | `/api/v1/workflows/{id}` | Update (optimistic-locked by `version`) |
| `DELETE` | `/api/v1/workflows/{id}` | Delete a workflow |
| `GET` | `/api/v1/workflows/{id}/runs` | Run history (last N, newest first) |
| `POST` | `/api/v1/workflows/{id}/run` | Trigger workflow manually (ignores cron) |
| `POST` | `/api/v1/workflows/{id}/cancel` | Cancel an in-flight run |
| `GET` | `/api/v1/workflows/templates` | Built-in workflow templates |
| `POST` | `/api/v1/workflows/{id}/runs/{run_id}/replay` | *(Phase 6)* Re-run using the original snapshot and inputs |
| `POST` | `/api/v1/workflows/{id}/runs/{run_id}/fork` | *(Phase 6)* New run from a prior run's inputs, with optional overrides |
| `GET` | `/api/v1/workflows/{id}/runs/{run_id}/diff/{other_run_id}` | *(Phase 6)* Structured per-step diff of two runs |
| `POST` | `/api/v1/agents/{name}/clone` | *(Phase 7)* Clone an agent with a new name and optional model override |

Creating or updating a Workflow re-registers its cron entry immediately
without a restart.

### Optimistic locking

`PUT /api/v1/workflows/{id}` rejects requests whose submitted `version`
does not match the stored value with **HTTP 409 Conflict** and a body of
`{"current_version": N}`. Clients should reload, merge, and retry.
A submitted `version: 0` skips the check (back-compat).

---

## WebSocket Events

The frontend "Live Execution" panel renders these events from
`/ws`:

| Type | When | Payload |
|---|---|---|
| `workflow_started` | run begins | `{workflow_id, run_id, workflow_name}` |
| `workflow_step_started` | sub-workflow step begins | `{workflow_id, run_id, position, slug, sub_workflow}` |
| `workflow_step_token` | streaming token from agent | `{workflow_id, run_id, step_name, step_position, token}` |
| `workflow_step_complete` | each step finishes | `{workflow_id, run_id, position, slug, status, session_id, error?, truncated?}` |
| `workflow_complete` | terminal: all steps OK | `{workflow_id, run_id}` |
| `workflow_partial` | terminal: some `on_failure: continue` failures | `{workflow_id, run_id}` |
| `workflow_failed` | terminal: `on_failure: stop` failure | `{workflow_id, run_id, error?}` |
| `workflow_cancelled` | terminal: cancellation | `{workflow_id, run_id}` |
| `workflow_skipped` | step skipped (`when:` false) or run skipped (concurrency limit) | `{workflow_id, run_id, reason, when_resolved?, position?, slug?}` |

The frontend `useWorkflows` composable wires these via `ws.on(type, fn)` and
keeps a per-workflow buffer (`liveEvents[wfId]`, max 100). On
`workflow_started` the prior buffer for that workflow is dropped so the live
panel shows only the active run.

---

## Notifications

Both workflow-level (`workflow.notification`) and step-level (`step.notify`)
notification configs accept a `deliver_to` array. Each delivery is one of:

| Type | Required fields | Notes |
|---|---|---|
| `inbox` | (none) | Drops a Notification into the user's inbox. |
| `space` | `space_id` | Posts into a Huginn space (channel or DM). |
| `webhook` | `to` | HTTP POST. `connection` may be set for auth. |
| `email` | `to`, SMTP fields *or* `connection` | Inline SMTP creds are deprecated. |

Step-level configs also support `on_success: bool` and `on_failure: bool`
for per-step granularity (e.g. only notify on failure).

---

## Migration from Legacy Routines

Routines are obsolete and have been folded into inline workflow steps. On
first boot the runner runs a one-time migration:

1. Reads every `~/.huginn/routines/*.yaml`.
2. Generates a single-step workflow per routine, **inlining** the
   `agent`, `prompt`, `vars`, and `connections` (NOT a `routine: slug`
   reference — those are hard-rejected at runtime).
3. Renames `~/.huginn/routines` → `~/.huginn/routines.bak` so the migration
   never runs twice.

A separate **repair pass** (`RepairLegacyRoutineSteps`) detects and fixes
any workflows that a buggy prior migrator left with `{routine: slug}` step
references. It loads the original routine YAML from `routines.bak` and
rewrites the step inline. The repair pass is idempotent and safe to run
on every boot.

---

## Vue Management View

**`/workflows`** — Card grid view. Each Workflow is displayed as a card
showing its name, description, schedule, step count, and enabled status. A
search bar filters cards in real time. Clicking a card navigates to the
detail view. The "+ New Workflow" button opens a creation modal.

**`/workflows/{id}`** — Workflow detail/editor view. Live execution panel
shows current run progress via WS events. Edit name/description/schedule,
add/remove/reorder steps, edit per-step agent/prompt/vars/connections/
inputs/notify config.

**`/workflows/{id}/runs/{runId}`** — Deep-link to a specific run within
the detail view. The history panel auto-opens with that run expanded so
the URL can be shared or refreshed.

---

## Phase 8 capabilities

The following capabilities were added after v1. Existing workflow YAMLs
are unaffected unless they add the new fields.

### Conditional execution (`step.when`)

```yaml
- name: deploy
  agent: ops
  when: "{{prev.output.summary}}"   # skips if falsy
  prompt: "Run the deploy."
```

After all `{{…}}` substitutions, the resolved string is evaluated: `""`,
`false`, `0`, `no`, `off` (case-insensitive) → skip; anything else →
run. Skipped steps persist with `status: "skipped"` and emit
`workflow_skipped` (reason `"when_false"`). They do not trigger
`on_failure` handlers and are not counted as failures.

### Sub-workflows (`step.sub_workflow`)

```yaml
- name: gather
  sub_workflow: wf-collect-pr-stats
```

The runner invokes the named workflow synchronously (by ID), seeding its
scratchpad from the parent's current scratchpad. The child's last-step
output becomes this step's output. When set, `agent`/`prompt`/
`model_override` are ignored on the same step.

### Workflow-level retry defaults (`workflow.retry`)

```yaml
retry:
  max_retries: 3
  delay: 30s
```

Inherited by steps that do not set their own `max_retries` /
`retry_delay`. A step that explicitly sets either field keeps its
override.

### Per-step model override (`step.model_override`)

```yaml
- name: classify
  agent: triage
  model_override: claude-haiku-4
  prompt: "Categorise: {{prev.output}}"
```

Runs a single step against a different model than the agent's default.
The override is request-scoped — the agent registry is never mutated.

---

## Design Constraints

**Linear pipeline only (parallelism is opt-in via Swarm).** Steps execute
strictly sequentially. Fan-out (running multiple steps in parallel) is not
yet supported — use the Swarm orchestrator for that pattern.

**`when:` is not a general expression language.** The conditional field
understands a fixed set of falsy literals after placeholder substitution.
There is no operator parsing, arithmetic, or regex. Complex conditional
logic should be expressed inside a step's prompt and emitted as a truthy/
falsy summary that a subsequent step's `when:` can consume.

**Sub-workflow recursion is caller-bounded.** A workflow that calls itself
(directly or indirectly) will not deadlock, but will consume a goroutine
and memory per recursive call. The runner does not enforce a depth limit;
callers are responsible for avoiding unbounded recursion.

---

## Operations

- **Concurrency cap.** A semaphore limits how many workflows can run
  simultaneously (default tuned at startup). Excess triggers receive a
  `workflow_skipped` event with `reason: "concurrency_limit"`.
- **Cancellation.** `POST /cancel` flips a per-run flag. The runner checks
  it between steps and at safe points within long-running step calls.
- **Run pruning.** Histories are retained per-workflow up to a configurable
  limit. Old entries are pruned on a periodic sweep.
- **Notifications dead-letter.** Failed deliveries are recorded in the
  `delivery_dead_letter` table and surfaced in the UI for retry.
