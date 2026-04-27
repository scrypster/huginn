---
name: workflow-designer
version: 1.0.0
author: huginn
source: builtin
description: Schema reference for writing huginn workflow YAML files
huginn:
  priority: 10
---

You can create, inspect, and modify huginn workflows as plain YAML files.

## Discovery

Before writing a workflow, enumerate available agents:

```bash
ls ~/.huginn/agents/
```

Each file is a JSON or YAML agent definition. Read it to learn the agent's name,
model, toolbelt (external service connections), and skills.

## Placement

Write completed YAML to `~/.huginn/workflows/{id}.yaml`.
The scheduler picks it up automatically within 2 seconds.
The workflow also appears in the UI immediately.

## Validation (optional dry-run)

```
POST /api/v1/workflows/validate
Content-Type: application/json
Authorization: Bearer <token>

{ <workflow JSON body> }
```

Returns `{"valid": true}` on success or `{"error": "..."}` on failure.
Use this before writing to disk to catch schema errors early.

## Workflow YAML Reference

```yaml
id: daily-report                      # required; used as the filename
name: Daily Report                    # required; human display name
description: Summarise overnight data # optional
enabled: true                         # true = scheduled; false = paused
schedule: "0 8 * * 1-5"              # cron (5-field) or @daily/@hourly
timeout_minutes: 30                   # optional; 0 = default 30 min; max 1440

tags:                                 # optional list of labels
  - reporting
  - daily

retry:                                # optional workflow-level retry defaults
  max_retries: 2                      # steps inherit this unless they override
  delay: 30s

steps:
  - name: Gather Data                 # optional but recommended; used by from_step
    position: 0                       # required; 0-indexed execution order
    agent: Researcher                 # agent name exactly as stored on disk
    prompt: |
      Search for overnight news about {{inputs.topic}}.
      Summarise in bullet points.
    vars:                             # static vars available as {{inputs.KEY}}
      topic: AI industry
    connections:                      # override agent toolbelt for this step
      search: brave-search
    on_failure: stop                  # stop (default) | continue
    max_retries: 1                    # 0-10; overrides workflow retry.max_retries
    retry_delay: 10s                  # e.g. "30s", "2m"
    timeout: 5m                       # step-level timeout; "1s"-"24h"
    model_override: claude-haiku-4    # optional; overrides agent's default model
    when: "{{run.scratch.ready}}"     # skip step when falsy: "", "false", "0", "no", "off"
    notify:
      on_success: false
      on_failure: true
      deliver_to:
        - type: inbox                 # inbox | space | agent_dm | webhook | email
        - type: space
          space_id: general
        - type: agent_dm
          user: mjbonanno
          from: Reporter
        - type: webhook
          to: https://hooks.example.com/notify
          connection: my-webhook

  - name: Send Summary
    position: 1
    agent: Writer
    prompt: |
      Write a concise email body from these bullets:
      {{inputs.research}}
    inputs:
      - from_step: Gather Data        # reference the previous step by name
        as: research                  # available as {{inputs.research}}
    sub_workflow: child-workflow-id   # set to run a child workflow instead of
                                      # agent+prompt (agent/prompt ignored when set)

chain:                                # optional: trigger another workflow on completion
  next: send-weekly-digest
  on_success: true
  on_failure: false

notification:                         # optional workflow-level notification
  on_success: true
  on_failure: true
  severity: info
  deliver_to:
    - type: inbox
```

## Template Variables

| Variable | Description |
|----------|-------------|
| `{{prev.output}}` | Output of the immediately preceding step |
| `{{run.scratch.KEY}}` | Value written to the run scratchpad under KEY |
| `{{inputs.ALIAS}}` | Value injected via a step's `vars` map or `inputs[].as` alias |

## Falsy Values for `when:`

A `when:` expression is falsy (step skipped) when the resolved string is:
`""`, `"false"`, `"0"`, `"no"`, `"off"` (case-insensitive).
Everything else runs the step.

## Sub-workflow Semantics

When `sub_workflow` is set on a step:
- The named workflow runs synchronously as the step body.
- The child workflow inherits the parent run's scratchpad.
- The child's last-step output becomes this step's output.
- `agent` and `prompt` on the step are ignored.

## Minimal Working Example

```yaml
id: hello-world
name: Hello World
enabled: true
schedule: "@daily"
steps:
  - name: Greet
    position: 0
    agent: Assistant
    prompt: "Say hello and share an interesting fact about today's date."

  - name: Summarize
    position: 1
    agent: Assistant
    prompt: "Summarize this greeting in one sentence: {{prev.output}}"
```
