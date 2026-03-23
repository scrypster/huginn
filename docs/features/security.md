# Security and Permissions

## The short version

Huginn never does anything destructive without asking first. Read operations run freely. Write and execute operations pause and show you exactly what the agent wants to do. You decide — once, for the session, or never.

---

## Three permission levels

Every tool in Huginn has a permission level that determines how it's handled:

### Read — always allowed

These tools cannot destroy state. No prompt, no delay.

`read_file`, `list_dir`, `grep`, `search_files`, `git_status`, `git_diff`, `git_log`, `git_blame`, `web_search`, `fetch_url`, `find_definition`, `list_symbols`, `gh_pr_list`, `gh_pr_view`, `gh_pr_diff`, `gh_issue_list`, `gh_issue_view`

### Write — approval required

These tools modify durable state: files, git history, GitHub, or external services.

`write_file`, `edit_file`, `git_commit`, `git_branch`, `git_stash`, `git_worktree_create`, `git_worktree_remove`, `gh_pr_create`, `gh_issue_create`, `update_memory`

### Execute — approval required

These tools run arbitrary code. Shell access is the highest-risk tier.

`bash`, `run_tests`

---

## The approval prompt

When an agent wants to run a write or execute tool, Huginn pauses and shows you a prompt before anything happens:

```
Agent wants to: write_file
Path: internal/db/query.go
─────────────────────────────
[a] Allow once    [A] Allow all for this session    [d] Deny
```

You have three choices:

| Key | Action |
|-----|--------|
| `a` or `y` | Allow this one call. You'll be asked again next time. |
| `A` | Allow all calls to this tool for the rest of the session. You won't be prompted again. |
| `d`, `c`, or `n` | Deny. The agent receives a rejection and must continue without taking that action. |

The "Allow all for this session" decision is per-tool. Approving `write_file` for the session does not approve `bash`.

---

## Auto-run mode

If you trust the agent for a session and don't want to approve every write, toggle auto-run:

- **In the TUI:** press `Shift+Tab` to toggle. The status bar shows the current state.
- **At startup:** `huginn --dangerously-skip-permissions` enables auto-run from the first turn.

With auto-run on, write and execute tools run without prompting. You can toggle it back off mid-session.

---

## The approval gate (cannot be bypassed)

The **approval gate** is a per-connection safety checkpoint configured on each agent's toolbelt. When `approval_gate: true` is set for a connection, writes to that provider always require approval — even with auto-run on, even with `--dangerously-skip-permissions`.

```json
{
  "name": "DevOps",
  "toolbelt": [
    {
      "connection_id": "aws-prod",
      "provider": "aws",
      "approval_gate": true
    }
  ]
}
```

With this configuration, the DevOps agent can read from AWS freely, but any action that modifies production infrastructure pauses and asks you first — regardless of what mode Huginn is running in.

This is intentional: the gate is a policy you set when configuring the agent, not something a CI flag or automation script can override. A scheduled routine cannot silently write to production.

---

## Diff review

Before any file is written, Huginn shows you a unified diff of the proposed change. You can:

- **Approve** the change (`a` or `s`)
- **Reject** it (`r`)
- **Approve all** pending changes at once (`A`)
- **Reject all** (`R`)

This applies even in auto-run mode when `diff_review_mode: "always"` is set in config.

---

## What agents can't do (by default)

A freshly configured agent with default settings:

- **Cannot write or execute** without your approval
- **Cannot reach external connections** whose providers aren't in its toolbelt
- **Cannot use built-in tools** not in its `local_tools` allowlist

The combination of `local_tools` (built-in access) and `toolbelt` (external access) plus the permission gate gives you three independent layers of control.

---

## Configuration reference

**In `~/.huginn/config.json`:**

| Field | Default | Description |
|-------|---------|-------------|
| `diff_review_mode` | `"auto"` | `"always"` shows diffs before every write; `"never"` skips; `"auto"` shows only when the agent is not in auto-run |

**At launch:**

| Flag | Description |
|------|-------------|
| `--dangerously-skip-permissions` | Enable auto-run from startup. All writes and executes proceed without prompting (except approval-gated connections). |
| `--no-tools` | Disable all tool use. Pure chat mode. |

**Per-agent (in `~/.huginn/agents/<name>.json`):**

| Field | Description |
|-------|-------------|
| `local_tools` | Allowlist of built-in tools. See [Local Tools](local-tools.md). |
| `toolbelt[].approval_gate` | Force approval for writes to this connection. See [Agent Toolbelt](agent-toolbelt.md). |

---

## Crash safety

Sessions are stored as append-only JSONL files. If Huginn crashes mid-write:

- At most one partial line is truncated at next open — `repairJSONL` removes it
- Manifests are written to a temp file first, then renamed atomically — a crash never produces a half-written manifest
- The last known state is always recoverable

No destructive action can silently slip past a crash. Either the action completed and was logged, or it did not complete.

---

## For a deeper technical look

→ [Architecture: Permissions & Safety](../architecture/permissions-and-safety.md) — Gate internals, patch verification, file locking, thread safety, and the full threat model
