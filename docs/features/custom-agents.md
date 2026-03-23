# Custom Agents

## What it is

Huginn ships with three named agents by default — Chris, Steve, and Mark — but these are just starting points with pre-written personas. You can create agents with any name, model, persona, memory vault, and tool access. A custom agent is a full peer of the defaults: it appears in the web UI agent list, participates in delegation, and accumulates its own long-term memory. Common uses are specialized agents scoped to one codebase, agents backed by cloud models while the rest use local Ollama, and read-only agents that can never write files.

---

## How to create an agent

### From the web UI

1. Open the web UI (`huginn tray`) and click **Agents** in the left navigation
2. Click **+ New Agent** in the top-right of the agents screen
3. Fill in the fields. The only required field is **Name**; everything else has a sensible default
4. Click **Save**

The UI writes the agent to `~/.huginn/agents/<name>.json`. The file is detected immediately — no restart required.

### From the TUI

**Wizard:**

Press `Ctrl+A` anywhere in the TUI to open the agent creation wizard. It walks through name, model, and persona in sequence. Press `Enter` to confirm each field.

Alternatively, run:
```
/agents new
```

**Quick creation:**

If you already know the name and model, skip the wizard:
```
/agents create Elena claude-sonnet-4-6
/agents create Lola qwen2.5-coder:14b
/agents create Auditor deepseek-r1:14b
```

The agent is written to disk immediately and is available in the same session.

### From a JSON file

Drop a `.json` file in `~/.huginn/agents/`. The web UI picks it up without a restart. The TUI reads it on next launch.

Below is a complete example for a security-reviewer agent:

```json
{
  "name": "Elena",
  "model": "claude-sonnet-4-6",
  "system_prompt": "You are Elena, a security-focused code reviewer. You look for injection vulnerabilities, authentication flaws, insecure defaults, and data exposure risks. You always cite the line number and explain why the pattern is dangerous before suggesting a fix. You do not write new code — only review and advise.",
  "color": "#E06C75",
  "icon": "E",
  "provider": "anthropic",
  "endpoint": "https://api.anthropic.com",
  "api_key": "$ANTHROPIC_API_KEY",
  "vault_name": "",
  "vault_description": "Security review knowledge: vulnerability patterns, CVE history, project-specific threat model.",
  "plasticity": "reference",
  "memory_enabled": true,
  "context_notes_enabled": false,
  "memory_mode": "conversational",
  "skills": ["security-review", "owasp"],
  "local_tools": ["read_file", "list_dir", "grep", "search_files", "git_diff", "git_log"],
  "toolbelt": [],
  "description": "Security reviewer. Finds vulnerabilities, authentication flaws, and data exposure risks. Read-only — does not write or execute.",
  "version": 0
}
```

Field-by-field explanation:

| Field | Value in example | What it does |
|-------|-----------------|--------------|
| `name` | `"Elena"` | Display name; used in delegation and slash commands |
| `model` | `"claude-sonnet-4-6"` | The model ID sent to the backend |
| `system_prompt` | long string | Persona injected as the system message for every session |
| `color` | `"#E06C75"` | Hex color shown next to the agent name in the UI |
| `icon` | `"E"` | Single character shown in the TUI status bar |
| `provider` | `"anthropic"` | Overrides the global backend provider for this agent |
| `endpoint` | `"https://api.anthropic.com"` | API endpoint; inherits global if omitted |
| `api_key` | `"$ANTHROPIC_API_KEY"` | API key or `$ENV_VAR` reference; inherits global if omitted |
| `vault_name` | `""` | Empty = auto-derive as `huginn:agent:<user>:elena` |
| `vault_description` | string | Injected into system prompt to ground the agent's memory use |
| `plasticity` | `"reference"` | Low write rate; appropriate for review agents |
| `memory_enabled` | `true` | Use MuninnDB for cross-session memory |
| `context_notes_enabled` | `false` | No file-based context notes (using MuninnDB instead) |
| `memory_mode` | `"conversational"` | Proactive recall at start, write learnings at end |
| `skills` | `["security-review", "owasp"]` | Only these skills are injected; empty falls back to global |
| `local_tools` | read-only list | Allowlist of built-in tools; no write or execute tools |
| `toolbelt` | `[]` | No external connection providers |
| `description` | string | Shown to other agents during delegation decisions; max 500 bytes |
| `version` | `0` | Optimistic-lock counter; auto-incremented on every save |

---

## Agent configuration reference

Complete field listing for `AgentDef` (the on-disk JSON format):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | required | Display name. Max 128 characters. |
| `model` | string | `""` | Model ID, e.g. `"qwen2.5-coder:14b"` or `"claude-sonnet-4-6"`. Inherits global if empty. |
| `system_prompt` | string | `""` | Persona text injected as the system message. Empty = no persona. |
| `color` | string | `""` | Hex color `#RRGGBB` shown in the UI. Empty = default theme color. |
| `icon` | string | `""` | Single character shown in TUI status bar. |
| `provider` | string | `""` | Backend provider: `"anthropic"`, `"openai"`, `"openrouter"`, `"ollama"`, or `""` to inherit global. |
| `endpoint` | string | `""` | API endpoint override. Empty = inherits global. |
| `api_key` | string | `""` | API key. Supports `"$ENV_VAR"` syntax. Empty = inherits global. |
| `vault_name` | string | auto-derived | MuninnDB vault name. Empty = `huginn:agent:<username>:<agentname>`. |
| `vault_description` | string | `""` | Description of this agent's vault; injected into the system prompt. |
| `plasticity` | string | `"default"` | MuninnDB learning-rate preset: `"knowledge-graph"`, `"default"`, or `"reference"`. |
| `memory_enabled` | *bool | nil (inherit) | `true` = MuninnDB active; `false` = no memory; `nil` = inherits global default (true). |
| `context_notes_enabled` | bool | `false` | File-based memory at `~/.huginn/agents/<name>.memory.md`. Gives agent `update_memory` tool. |
| `memory_mode` | string | `"conversational"` | `"passive"`, `"conversational"`, or `"immersive"`. Controls how proactively memory tools are used. |
| `skills` | []string | `[]` | Skill names assigned to this agent. Empty = falls back to globally-enabled skills. |
| `local_tools` | []string | `[]` | Built-in tool allowlist. `[]`/absent = no tools; `["*"]` = all tools; named list = only those. |
| `toolbelt` | []ToolbeltEntry | `[]` | External connection providers this agent can access. Empty = no external tools. |
| `description` | string | `""` | Short description visible to other agents during delegation. Max 500 bytes. |
| `version` | int | 0 | Optimistic-lock counter. Auto-incremented on every save. |

---

## Per-agent model and backend

Each agent can use a different provider, endpoint, and API key. This lets you run cost-sensitive agents on Ollama while routing a high-stakes reviewer to Anthropic.

**Example: mixed backend setup**

`~/.huginn/agents/steve.json` — local Ollama (uses global backend, no overrides):
```json
{
  "name": "Steve",
  "model": "qwen2.5-coder:14b"
}
```

`~/.huginn/agents/elena.json` — Anthropic Claude:
```json
{
  "name": "Elena",
  "model": "claude-sonnet-4-6",
  "provider": "anthropic",
  "endpoint": "https://api.anthropic.com",
  "api_key": "$ANTHROPIC_API_KEY"
}
```

`~/.huginn/agents/lola.json` — OpenRouter with an explicit key:
```json
{
  "name": "Lola",
  "model": "anthropic/claude-3.5-sonnet",
  "provider": "openrouter",
  "endpoint": "https://openrouter.ai/api/v1",
  "api_key": "$OPENROUTER_API_KEY"
}
```

**Provider auto-inference**

If you omit `provider`, Huginn infers it from the model name prefix:

| Model prefix | Inferred provider |
|-------------|------------------|
| `claude*` | `anthropic` |
| `gpt-*`, `o1*`, `o3*` | `openai` |
| `gemini*` | `google` |
| `llama*`, `mistral*` | `ollama` |
| anything else | `""` (uses global) |

Set `provider` explicitly if you need to override the inference (e.g. running a Claude model through a proxy).

---

## Per-agent tool access

Tool access is controlled by two independent fields: `local_tools` (built-in tools) and `toolbelt` (external connections). Both default to deny — a new agent has no tools until you grant them.

### local_tools

Controls which of Huginn's built-in tools the agent can use.

| Value | Behavior |
|-------|----------|
| Absent or `[]` | No built-in tools. Agent cannot read files, run bash, or grep code. |
| `["*"]` | All built-in tools. Use for general-purpose agents only. |
| `["read_file", "grep"]` | Only the named tools. |

**Read-only reviewer** (can inspect code, cannot write or execute):
```json
{
  "local_tools": [
    "read_file", "list_dir", "grep", "search_files",
    "git_status", "git_diff", "git_log", "git_blame",
    "find_definition", "list_symbols"
  ]
}
```

**Coding agent** (read, write, run tests):
```json
{
  "local_tools": [
    "read_file", "list_dir", "grep", "search_files",
    "write_file", "edit_file",
    "git_status", "git_diff", "git_commit",
    "bash", "run_tests"
  ]
}
```

**Pure reasoning** (no file access at all):
```json
{
  "local_tools": []
}
```

### toolbelt

Controls which external connection providers the agent can call (GitHub, Slack, AWS, Jira, etc.). An empty toolbelt means no external tools.

```json
{
  "toolbelt": [
    { "provider": "github" },
    { "provider": "jira" }
  ]
}
```

Use `{ "provider": "*" }` as a wildcard to grant all configured connections.

**Example: read-only GitHub reviewer**

```json
{
  "name": "Elena",
  "local_tools": ["read_file", "grep", "git_diff", "git_log"],
  "toolbelt": [
    { "provider": "github" }
  ]
}
```

Elena can read files and inspect git history, and she can call GitHub APIs (PR lists, issue detail), but she cannot write files, run bash, or access any other external service.

For the full list of available built-in tools, see [Local Tools](local-tools.md). For external connections, see [Agent Toolbelt](agent-toolbelt.md).

---

## Managing agents

### Slash commands

| Command | What it does |
|---------|-------------|
| `/agents` | List all agents and open the agents screen |
| `/agents new` | Open the agent creation wizard |
| `/agents create <name> <model>` | Create an agent inline without the wizard |
| `/agents swap <name> <model>` | Change an agent's model; persists to disk immediately |
| `/agents rename <name> <new>` | Rename an agent; persists to disk immediately |
| `/agents persona <name>` | Display the agent's current system prompt |
| `/agents delete <name>` | Delete an agent permanently |

### Keyboard shortcuts (TUI)

| Shortcut | What it does |
|----------|-------------|
| `Ctrl+A` | Open the agent creation wizard |
| `Ctrl+P` | Cycle the primary agent (rotates through available agents) |

### Web UI

All CRUD operations (create, edit, delete) are available in the **Agents** view. Changes are reflected immediately without restarting the server.

---

## Tips & common patterns

**One agent per concern.** Rather than giving a single agent all tools, create separate agents for planning, coding, and reviewing. Delegation is cheap; context pollution is not.

**Use `$ENV_VAR` for API keys.** Never put raw API keys in agent JSON files. Reference environment variables with the `$VARNAME` syntax — Huginn resolves them at runtime. This keeps secrets out of `~/.huginn/` and lets you rotate keys without editing files.

**Match plasticity to the agent's job.** Code-writing agents benefit from `"knowledge-graph"` (high write rate, accumulates patterns quickly). Review agents should use `"reference"` (low write rate, knowledge is authoritative not rapidly evolving). Steve-style general agents use `"default"`.

**Write a description even if it seems obvious.** The `description` field is what other agents read when deciding who to delegate a task to. A sentence like "Security reviewer. Finds vulnerabilities and authentication flaws. Does not write code." prevents the planner from delegating a coding task to Elena.

**Start with the minimum local_tools and add.** It is easier to add a tool after noticing an agent can't do something than to contain the damage after a write tool was granted unnecessarily. Start with the read-only set and promote as needed.

---

## Troubleshooting

**Agent not appearing after dropping a JSON file in `~/.huginn/agents/`**

The web UI auto-detects new files; if the agent still does not appear, refresh the browser. The TUI reads agent files at launch — restart the TUI to pick up new agents.

If the agent appears with wrong values, check the JSON for syntax errors. Huginn skips files that fail to parse; it does not crash or report an error in the UI. Run `cat ~/.huginn/agents/<name>.json | python3 -m json.tool` to validate the file.

**`/agents create` succeeds but the model seems wrong**

`/agents create` sets only the name and model; all other fields use defaults (no tools, no system prompt, global backend). Edit the file at `~/.huginn/agents/<name>.json` to add the remaining configuration, or use the web UI to fill in the rest.

**Agent is using the global backend instead of its own provider**

Check that both `provider` and `api_key` are set. Setting `endpoint` alone is not sufficient — the provider field controls which authentication scheme Huginn uses. If `api_key` is an `$ENV_VAR` reference, verify the variable is exported in the environment where Huginn runs.

**409 Conflict when saving an agent from the web UI**

This means the `version` counter in the UI is behind the on-disk counter (another client wrote a newer version). Reload the agent in the UI and re-apply your changes. The `version` field is an optimistic lock; the last-writer-wins only when `version` is `0` in the request.

---

## See also

- [Local Tools](local-tools.md) — complete list of built-in tools and the permission gate
- [Agent Toolbelt](agent-toolbelt.md) — scoping external connections per agent
- [Memory](memory.md) — per-agent MuninnDB vaults and context notes
- [Multi-Agent](multi-agent.md) — delegation, the swarm, and the thread panel
