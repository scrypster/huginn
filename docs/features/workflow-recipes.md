# Workflow Recipes

Drop-in examples that exercise the post-v1 workflow features documented in [workflows.md](workflows.md). Each recipe is a complete `~/.huginn/workflows/<id>.yaml` you can copy and edit.

These recipes are deliberately small and read-only so you can run them end-to-end in a development environment without side effects. Once you have one working, swap in your own agent names, prompts, and connections.

---

## Triage and report (model_override)

Use a cheap model to classify, an expensive model to write the human-facing summary.

```yaml
id: wf-pr-triage
name: PR Triage and Report
description: Classify open PRs with Haiku, then have Sonnet write a daily digest.
enabled: true
trigger:
  mode: schedule
  cron: "0 9 * * 1-5"
steps:
  - position: 1
    name: classifier
    agent: reviewer
    model_override: claude-haiku-4
    prompt: |
      List every open PR in the current repo. For each, output one line:
      "<repo>:<pr_number> | <bug|feature|chore> | <one-sentence reason>"
      Respond with the JSON summary first, then the table.
  - position: 2
    name: digest
    agent: reviewer
    model_override: claude-sonnet-4
    prompt: |
      Turn this PR classification into a Slack-friendly digest with three
      sections (bugs, features, chores). One bullet per item, no preamble.
      Source: {{prev.output}}
notification:
  on_success: true
  deliver_to:
    - type: inbox
```

**Why this is interesting.** Step 1 keeps the bulk-classification cost low; step 2 spends the budget where it actually matters. Neither agent is cloned: the `model_override` is request-scoped.

---

## Conditional follow-up (when)

Probe first, only act when the answer is non-empty.

```yaml
id: wf-deploy-gate
name: Deploy Gate
description: Only deploy when the morning safety probe says "yes".
enabled: true
trigger:
  mode: schedule
  cron: "0 7 * * 1-5"
steps:
  - position: 1
    name: probe
    agent: ops
    prompt: |
      Check CI green, on-call rotation, and freeze calendar. Reply with one
      JSON line: {"summary": "yes" | "no"} and a short reason after.
  - position: 2
    name: deploy
    agent: ops
    when: "{{prev.output.summary}}"   # "no" / "off" / "" → skip; anything else runs
    prompt: |
      Run the deploy script. Source decision: {{prev.output}}
notification:
  on_success: true
  on_failure: true
  deliver_to:
    - type: inbox
```

**Why this is interesting.** The `when:` field consumes the JSON-typed output from step 1 via `{{prev.output.summary}}`. Skipped steps emit `workflow_skipped` WS events and persist with `status: "skipped"`, so the run history makes the decision visible without surfacing it as a failure.

---

## Reusable child workflow (sub_workflow)

Author a generic "fetch and validate" pipeline once; call it from multiple parents.

```yaml
# ~/.huginn/workflows/wf-fetch-and-validate.yaml
id: wf-fetch-and-validate
name: Fetch + Validate
description: Internal helper invoked by parent workflows.
enabled: false           # never fires on its own; parents trigger it
trigger:
  mode: manual
steps:
  - position: 1
    name: fetch
    agent: vendor-bot
    prompt: |
      Pull the manifest URL: {{run.scratch.manifest_url}}
  - position: 2
    name: validate
    agent: vendor-bot
    prompt: |
      Validate the manifest body. Source: {{prev.output}}
```

```yaml
# ~/.huginn/workflows/wf-daily-vendor-sync.yaml
id: wf-daily-vendor-sync
name: Daily Vendor Sync
enabled: true
trigger:
  mode: schedule
  cron: "0 6 * * *"
steps:
  - position: 1
    name: gather
    sub_workflow: wf-fetch-and-validate
  - position: 2
    name: report
    agent: ops
    prompt: |
      Summarise the validated manifest for the standup.
      Source: {{prev.output}}
```

**Why this is interesting.** The parent's scratchpad (`manifest_url`, etc) flows in as the child's initial inputs, so the child can reference `{{run.scratch.manifest_url}}` on its first step. The child's last-step output becomes the parent step's output, so step 2's `{{prev.output}}` reads the validated manifest summary. `enabled: false` on the child stops it from firing on its own cron while still allowing parents to invoke it.

---

## Retries everywhere (workflow-level retry)

A flaky upstream API; retry every step three times by default.

```yaml
id: wf-vendor-pull
name: Flaky Vendor Pull
description: Network is unreliable; retry every step.
enabled: true
trigger:
  mode: schedule
  cron: "*/30 * * * *"
retry:
  max_retries: 3
  delay: 30s
steps:
  - position: 1
    name: fetch
    agent: vendor-bot
    prompt: "Pull the latest manifest from the vendor API."
  - position: 2
    name: process
    agent: vendor-bot
    prompt: "Validate: {{prev.output}}"
    max_retries: 1     # critical step: don't loop more than once
notification:
  on_failure: true
  deliver_to:
    - type: agent_dm
```

**Why this is interesting.** Steps inherit `max_retries: 3` and `delay: 30s` from the workflow level. Step 2 explicitly overrides `max_retries: 1` because we don't want the validation logic to mask a real bug behind retries.

---

## Chain alerts on failure (chain)

When the morning sync fails, run an alert workflow that pages the on-call.

```yaml
id: wf-morning-sync
name: Morning Sync
enabled: true
trigger:
  mode: schedule
  cron: "0 8 * * 1-5"
steps:
  - position: 1
    name: sync
    agent: ops
    prompt: "Run the morning sync. Output a JSON summary."
chain:
  next: wf-page-oncall
  on_failure: true
  on_success: false      # only chain when something is wrong
```

```yaml
id: wf-page-oncall
name: Page On-call
enabled: true
trigger:
  mode: manual
steps:
  - position: 1
    name: page
    agent: oncall-bot
    prompt: |
      Send a page. Upstream context:
        run: {{run.scratch.upstream_run_id}}
        status: {{run.scratch.upstream_status}}
        output: {{run.scratch.upstream_output}}
notification:
  on_success: true
  deliver_to:
    - type: webhook
      to: https://hooks.example.com/page
```

**Why this is interesting.** The chain config makes failure handling declarative: no orchestrator code, just two workflows that link through `chain.next`. The downstream workflow reads upstream context from `run.scratch.upstream_*` keys that the chain trigger seeds automatically.

---

## See also

- [workflows.md](workflows.md) — full feature reference, configuration, and design constraints
- [routines.md](routines.md) — single-agent task primitive that workflows compose
- [permissions.md](permissions.md) — headless mode behaviour relevant to scheduled runs
