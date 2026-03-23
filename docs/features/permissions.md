# Permissions

## What it is

Every tool Huginn can use is assigned a permission tier based on its maximum risk. Read-only tools — reading files, searching code, browsing git history — run automatically without interruption. Write tools that modify files, create commits, or post to external services pause and ask for your approval before they execute. Exec tools that run shell commands pause and ask too.

This design keeps you in the loop for actions with side effects while staying out of the way for the safe, high-frequency operations that make up most of an agent's work. The permission system does not prevent agents from being useful. It makes irreversible actions visible before they happen.

Session approvals are in-memory only. If you approve a tool for the session and then restart Huginn, the approval is gone. Each new process starts with a clean slate.

---

## Permission tiers at a glance

| Tier | Example tools | Auto-approved? | Why |
|---|---|---|---|
| **PermRead** | `read_file`, `grep`, `git_log`, `web_search` | Yes | Read-only operations cannot destroy state. |
| **PermWrite** | `write_file`, `edit_file`, `git_commit` | No | Modifies files or creates durable side effects. |
| **PermExec** | `bash`, `run_tests` | No | Arbitrary command execution at OS level. |

---

## How approval works

### TUI approval flow

When an agent attempts a write or exec tool, the TUI pauses and displays an approval prompt. The prompt shows the tool name, its arguments, and a human-readable summary of what is about to happen. You have four seconds to read it before the 30-second timeout begins.

Respond with one of these keys:

| Key | Action |
|---|---|
| `a` or `y` | **Allow once.** This call proceeds. The next call to the same tool will prompt again. |
| `A` | **Allow all.** This call proceeds and all future calls to this tool in the current session proceed without prompting. Does not carry over to new sessions. |
| `d`, `c`, or `n` | **Deny.** The call is blocked. The agent receives a permission error and can try a different approach. |

When a write is about to be applied to a file (the agent wants to overwrite content), you are shown a write-approval prompt specifically for that file:

| Key | Action |
|---|---|
| `a` or `y` | Approve this write. |
| `d` | Deny this write. |

**AllowAll is per-tool, not global.** Pressing `A` for `edit_file` approves all future `edit_file` calls in this session. It does not approve `bash`. Each tool has its own approval state.

### Web UI approval flow

The web UI renders the same approval logic as an inline card in the chat area. The card shows the tool name, arguments, and summary. Click **Approve** (equivalent to Allow once), **Always Approve** (equivalent to AllowAll), or **Deny**. The chat stream resumes immediately after you respond.

### Auto-run mode

Press `Shift+Tab` in the TUI to toggle auto-run mode. When active, all tool calls at any tier are approved automatically — equivalent to pressing `A` for every tool at once. The current auto-run state is shown in the TUI footer.

Auto-run is a convenience for sessions where you trust the agent to work without interruption. It is not persisted. Restarting Huginn restores the default behavior.

---

## Skipping permissions entirely

```bash
huginn --dangerously-skip-permissions
```

This flag bypasses all approval prompts for all tools. Every tool call — read, write, and exec — is allowed without any user interaction.

Use this flag in CI pipelines, Docker containers, and automation environments where there is no user present to respond to prompts and the environment itself provides the safety boundary (e.g., the agent runs in an isolated container with no access to production systems).

The word "dangerously" in the flag name is intentional. An agent running with this flag can write to any file, run any shell command, and create git commits without any checkpoint. Use it only when you have accepted that risk.

Session-level approvals set via `AllowAll` behave the same way as this flag but are limited to specific tools. If you want to skip prompts only for `bash` and not for `write_file`, use `AllowAll` in session rather than the flag.

---

## Complete tool list by tier

### PermRead — auto-approved

These tools never show an approval prompt.

| Tool | What it does |
|---|---|
| `read_file` | Read a file from disk |
| `list_dir` | List directory contents |
| `grep` | Search file contents by pattern |
| `search_files` | Semantic and BM25 code search |
| `git_status` | Show working tree status |
| `git_log` | Browse commit history |
| `git_diff` | Show diffs between refs or files |
| `git_blame` | Show per-line authorship |
| `web_search` | Search the web |
| `fetch_url` | Fetch the content of a URL |
| `memory_recall` | Read from MuninnDB memory vault |
| `find_definition` | Jump to symbol definition |
| `list_symbols` | List symbols in a file or scope |
| `consult_agent` | Ask another agent a question (read-only delegation) |

### PermWrite — requires approval

These tools pause and prompt before executing.

| Tool | What it does |
|---|---|
| `write_file` | Write or overwrite a file on disk |
| `edit_file` | Apply a patch or edit to an existing file |
| `git_branch` | Create or rename a branch |
| `git_commit` | Create a commit |
| `git_stash` | Stash working changes |
| `gh_pr_create` | Open a GitHub pull request |
| `gh_issue_create` | Open a GitHub issue |
| `memory_write` | Write a memory to MuninnDB |
| `memory_decide` | Record a decision in MuninnDB |
| `memory_evolve` | Update an existing memory in MuninnDB |
| `memory_write_batch` | Write multiple memories to MuninnDB at once |
| `git_worktree_create` | Create a git worktree |
| `git_worktree_remove` | Remove a git worktree |

### PermExec — requires approval

| Tool | What it does |
|---|---|
| `bash` | Execute a shell command |
| `run_tests` | Run the project's test suite |

---

## Stale file protection

When an agent reads a file and then later attempts to write to it, Huginn checks whether the file has changed since the agent read it. The mechanism is a SHA-256 hash captured at read time and re-verified at write time.

If you edit the file in your editor between the agent's read and its write attempt, the hashes diverge and the write is rejected with a stale file error. The agent receives the error and must re-read the file before it can proceed.

This prevents silent data loss. Without this check, an agent could overwrite your in-progress edits with a version based on stale state — and the loss might not be noticed until much later. The cost is one extra round-trip when a conflict is detected. The benefit is that your concurrent edits are never silently destroyed.

The agent handles stale file errors automatically by re-reading the file and re-planning the change. If you see an agent pause and re-read a file it already looked at, this is usually why.

---

## Concurrent file safety

When multiple agents run in parallel (for example, in a Swarm), Huginn ensures that two agents cannot write to the same file at the same time. A per-path lock manager (`FileLockManager`) serializes write access to each file path. The second agent's write waits until the first agent's write completes.

This applies to all write operations: `write_file`, `edit_file`, and any other tool that modifies disk state. Read operations are not locked — multiple agents can read the same file simultaneously without issue.

---

## Per-agent tool restrictions

The permission tier system controls the risk level of individual tool calls and whether they require approval. The agent toolbelt is a separate, additional layer that controls which external providers each agent is allowed to use at all.

**`toolbelt`** — a list of connections the agent may access. At session start, only the tool schemas for providers in the toolbelt are sent to the model. The model never learns that other providers exist, so it cannot even attempt to use them. An empty toolbelt (the default) allows access to all configured connections.

**`local_tools`** — a list of builtin tool names the agent is allowed to use. Tools not in this list are excluded from the agent's schema set. An empty `local_tools` list means no builtins are available to the agent.

These are capability restrictions layered on top of the tier system. An agent might have `bash` in its schema (PermExec tier, requires approval) but have `bash` excluded from its `local_tools`, in which case the agent can never call it regardless of approval state.

For the full toolbelt data model and enforcement flow, see [local-tools.md](local-tools.md).

---

## For Routines and headless mode

Routines run in headless sessions. There is no terminal or user present, so there is no approval prompt function registered. The permission gate detects this condition and denies all non-read tool calls by default.

In practice: if a Routine's prompt causes the agent to attempt `write_file`, `edit_file`, or `bash`, the call is denied. The agent receives a permission error. If the Routine does not handle this gracefully, it will stall or fail before `timeout_secs` is reached.

**Design Routines to use only PermRead tools.** A Routine that reads files, searches code, checks git history, and produces a report works perfectly in headless mode. A Routine that writes files or runs commands does not, unless you explicitly opt in.

To run write-capable Routines unattended, start Huginn with `--dangerously-skip-permissions`. Be aware of what that flag enables — see above.

---

## Tips & common patterns

**Start with default behavior.** The defaults are designed for interactive use. Do not reach for `--dangerously-skip-permissions` or auto-run mode until you have a specific reason.

**Use `AllowAll` to smooth a session, not to enable automation.** If you are in the middle of a long interactive session and tired of approving `edit_file` repeatedly, `A` is the right answer. For automation, use `--dangerously-skip-permissions` in an isolated environment.

**Never `AllowAll` bash without isolation.** A session-level `AllowAll` for `bash` means every subsequent shell command in that session runs without a prompt. This is acceptable when you trust the agent and the work. It is not acceptable when you are working in a sensitive environment or the agent is unfamiliar with the codebase.

**CI and Docker: use `--dangerously-skip-permissions` inside a container.** The container is your isolation boundary. The flag is appropriate there. Do not use it on your development machine where you have access to production credentials.

**Keep Routines read-only.** A Routine that only uses PermRead tools works correctly in any mode — interactive, headless, with or without `--dangerously-skip-permissions`. This is the path of least resistance and the safest design.

---

## Troubleshooting

**Agent pauses and appears to be doing nothing**

The agent is waiting at a permission prompt. Look for the approval card in the TUI or web UI. The prompt will show the tool name and what is about to happen. Press `a` to allow or `d` to deny.

If you do not see a prompt and the agent still appears stalled, check whether `timeout_secs` has elapsed (for Routines) or whether there is a background process preventing the TUI from rendering the prompt. Try scrolling up in the chat view.

**Routine never completes**

The most common cause is a Routine prompt that leads the agent to attempt a write or exec tool. Because there is no prompt function in headless mode, the call is denied immediately. The agent may retry several times before giving up, running down the clock until `timeout_secs` is reached.

Review the Routine's prompt and remove or reword anything that would cause the agent to write files, commit code, or run shell commands. If you need write access in a Routine, run Huginn with `--dangerously-skip-permissions`.

**Stale file error**

The agent attempted to write a file it had already read, but the file was modified externally between the read and the write. The agent will automatically re-read the file and retry. If you see repeated stale file errors, check whether another process or agent is also writing to the same file concurrently.

**Permission prompt timeout**

The approval prompt has a 30-second timeout. If no response is received within 30 seconds, the call is denied. The agent receives a timeout error and can try a different approach. If you need more time to review the prompt, you can deny it and ask the agent to explain what it is about to do before proceeding.
