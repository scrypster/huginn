# Local Tools

## What it is

Every agent has a **local tools allowlist** that controls which of Huginn's built-in tools the agent can use. By default, a newly created agent has no local tools — it can't read files, run bash, or grep your code until you explicitly grant those tools.

This is separate from the [Agent Toolbelt](agent-toolbelt.md), which controls access to external connections (GitHub, Slack, AWS). Local tools govern the built-in primitives: file I/O, shell execution, code search, and similar on-machine capabilities.

---

## Why it matters

Not every agent should be able to do everything. Consider:

- A **documentation agent** needs to read files but should never execute bash or commit to git.
- A **reviewer agent** can read and search code but should not write files.
- A **DevOps agent** might need bash and git but not web search.
- An **analyst agent** reading production logs should have no write access whatsoever.

By granting only what's needed, you contain mistakes and prompt injection attacks. If someone tricks a read-only reviewer agent into trying to run `bash`, it simply cannot — that tool isn't in its toolbelt.

---

## Modes

| `local_tools` value | Behavior |
|---------------------|----------|
| Absent / `null` | **No local tools.** The agent cannot use any built-in tools. (Default for new agents.) |
| `["*"]` | **God Mode.** The agent can use all built-in tools. Use for general-purpose assistants. |
| `["read_file", "grep"]` | **Allowlist.** The agent can only use the named tools. |

---

## Configure in the web UI

1. Open the web UI and go to **Agents**
2. Select an agent and click **Edit**
3. Scroll to the **Local Tools** section
4. Choose **None**, **All (God Mode)**, or select individual tools from the list
5. Save — takes effect on the next session

---

## Configure in JSON

Agent definitions live at `~/.huginn/agents/<name>.json`. The `local_tools` field is an array of tool names.

### God Mode — all tools

```json
{
  "name": "Chris",
  "local_tools": ["*"]
}
```

### Read-only agent

```json
{
  "name": "Reviewer",
  "local_tools": ["read_file", "list_dir", "grep", "search_files",
                  "git_status", "git_diff", "git_log", "find_definition"]
}
```

### Coding agent — read, write, and run tests

```json
{
  "name": "Steve",
  "local_tools": ["read_file", "list_dir", "grep", "search_files",
                  "write_file", "edit_file",
                  "git_status", "git_diff", "git_commit",
                  "bash", "run_tests"]
}
```

### No tools — pure reasoning agent

```json
{
  "name": "Architect",
  "local_tools": []
}
```

---

## Available tools by category

### Read (never require approval)

| Tool | What it does |
|------|-------------|
| `read_file` | Read a file's content |
| `list_dir` | List directory contents |
| `grep` | Search file content by regex |
| `search_files` | Semantic + keyword search across the codebase |
| `find_definition` | Look up a symbol's definition |
| `list_symbols` | List symbols in a file or package |
| `git_status` | Current git status |
| `git_diff` | Show unstaged or staged changes |
| `git_log` | Show commit history |
| `git_blame` | Show line-by-line authorship |
| `web_search` | Search the web (requires `brave_api_key` in config) |
| `fetch_url` | Fetch a URL's content |
| `gh_pr_list` | List pull requests |
| `gh_pr_view` | View a pull request |
| `gh_pr_diff` | Show a pull request's diff |
| `gh_issue_list` | List issues |
| `gh_issue_view` | View an issue |

### Write (require user approval unless auto-run is on)

| Tool | What it does |
|------|-------------|
| `write_file` | Create or overwrite a file |
| `edit_file` | Apply a targeted edit to a file |
| `git_commit` | Create a commit |
| `git_branch` | Create or switch branches |
| `git_stash` | Stash and pop changes |
| `git_worktree_create` | Create a git worktree |
| `git_worktree_remove` | Remove a git worktree |
| `gh_pr_create` | Open a pull request |
| `gh_issue_create` | Create a GitHub issue |
| `update_memory` | Update the agent's persistent context notes file |

### Execute (always require approval unless auto-run is on)

| Tool | What it does |
|------|-------------|
| `bash` | Run an arbitrary shell command |
| `run_tests` | Run the project's test suite |

---

## Interaction with the permission gate

Even after granting a tool in the `local_tools` list, the **permission gate** still applies at call time.

- **Read tools** — always execute without prompting
- **Write tools** — prompt unless auto-run is on or you've chosen "Allow all" this session
- **Execute tools** — always prompt unless auto-run is on

The `local_tools` allowlist is an outer gate (does this tool exist in the agent's kit?). The permission gate is the inner gate (is the user okay with this specific call right now?).

Disabling a tool in `local_tools` is therefore stronger than relying on the permission gate — the agent cannot even propose using it.

---

## Interaction with the toolbelt

`local_tools` and the [toolbelt](agent-toolbelt.md) are independent controls:

| | `local_tools` | Toolbelt |
|--|--|--|
| Controls | Huginn's built-in tools | External connection providers (GitHub, Slack, AWS, etc.) |
| Default (new agent) | None | All connections accessible (backward-compat) |
| Restricts | File I/O, shell, code search | OAuth tools, API-key tools, CLI tools |

An agent can have `local_tools: ["read_file"]` but a full toolbelt — or vice versa. Configure each independently based on the agent's job.

---

## Best practices

**Start with the minimum.** Add tools only when a task actually needs them. It's easy to add a tool later; it's harder to contain the fallout from one that was granted unnecessarily.

**Separate read and write agents.** If an agent's only job is to review or analyze, give it only read tools. Create a separate agent for actions that write or commit.

**Use `["*"]` only for your primary general-purpose agent.** God Mode is convenient for a first assistant but is too broad for specialized agents.

**Test your allowlist.** After configuring `local_tools`, ask the agent to do something that requires a tool you didn't grant. It should tell you it can't, not silently fail or produce hallucinated output.

---

## See also

- [Agent Toolbelt](agent-toolbelt.md) — scope access to external connections per agent
- [Security](security.md) — the permission gate, approval levels, and auto-run
- [Multi-Agent](multi-agent.md) — named agents, delegation, and the swarm
