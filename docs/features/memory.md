# Memory

## What it is

Huginn has two distinct memory systems with different scopes and lifetimes. Within a session, every message you exchange is retained in full — the agent has complete context of everything said in the current conversation, with no action required on your part. This is automatic and always active. Across sessions, context does not carry over by default; you need to configure one of two cross-session memory systems to give agents persistent knowledge. The first option is **context notes**: a plain Markdown file on disk that the agent can read and update, with no external dependencies. The second option is **MuninnDB**: a cognitive memory engine that the agent queries at session start and writes to at session end, providing semantic recall across every project and every conversation you have ever had with that agent.

---

## Within-session memory

No configuration needed. Every message in the current session is part of the agent's context window. The agent can refer back to something you said twenty messages ago, to a file it read three tool calls ago, or to an error it encountered and fixed — all without you repeating yourself.

When a session grows large enough to approach the model's context limit, Huginn triggers **context compaction** automatically. The threshold is controlled by the `compact_trigger` config value (default: `0.8`, meaning 80% full). Compaction summarizes older parts of the conversation and retains the most recent exchanges in full. You can adjust the threshold:

```json
{
  "compact_mode": "auto",
  "compact_trigger": 0.85
}
```

Set `compact_mode` to `"never"` to disable compaction (not recommended for long sessions) or `"always"` to force it on every session start.

---

## Cross-session memory

### Option 1: Context Notes (file-based, no external service)

Context notes are a single Markdown file at `~/.huginn/agents/<name>.memory.md`. When enabled for an agent, the file is read at the start of every session and injected into the agent's context. The agent also gains access to the `update_memory` tool, which lets it rewrite the file during the conversation.

This is the simplest cross-session memory option — no server, no credentials, no network. The file is human-readable and you can edit it directly.

**When to use it:**

- You want quick, durable context for an agent without setting up MuninnDB
- The agent's knowledge is stable (coding style rules, project conventions, recurring preferences)
- You want full control over what the agent remembers and want to edit the file manually

**Enable in the agent JSON:**

```json
{
  "name": "Steve",
  "context_notes_enabled": true,
  "memory_enabled": false
}
```

After enabling, you can seed the file by creating it manually:

```bash
cat > ~/.huginn/agents/steve.memory.md << 'EOF'
# Steve's Context Notes

## Project conventions
- All database columns use snake_case
- Error returns before happy path (early-return style)
- Prefer table-driven tests

## Recurring preferences
- Do not add logging unless explicitly asked
- Commit messages: imperative mood, 72-char limit
EOF
```

The agent reads this at session start and can update it with `update_memory` when it learns something new.

### Option 2: MuninnDB (cognitive memory engine)

MuninnDB is a separately running memory service. When configured, each agent has a private vault. At the start of every session, the agent queries its vault for memories relevant to the current context and injects them under a "Your Expertise" block in its system prompt. At session end, Huginn extracts 1–5 key learnings from the conversation and writes them to the vault. Over time, agents accumulate expertise specific to how you use them.

MuninnDB stores memories with temporal priority (recent memories are weighted higher), Hebbian reinforcement (memories recalled more often grow stronger), and confidence tracking. This means the agent's recall improves the more you use it — frequently relevant knowledge surfaces reliably; stale knowledge fades.

**When to use it:**

- You want agents that genuinely improve over time
- You work across multiple projects and want agents to carry general knowledge (coding patterns, preferences) while keeping project-specific knowledge scoped to each project
- You want semantic recall, not just keyword matching

**Setup:**

1. Start MuninnDB (see [MuninnDB documentation](https://github.com/scrypster/muninndb))
2. Set the endpoint:
   ```bash
   export HUGINN_MUNINN_ENDPOINT=http://localhost:8765
   ```
3. Ensure `memory_enabled` is `true` (or nil, which inherits the global default of `true`) in the agent's config

Huginn probes the endpoint at startup with a 2-second timeout. If MuninnDB is not reachable, memory is silently disabled for that session — the agent continues to work normally.

---

## Memory configuration

### Per-agent JSON fields

```json
{
  "name": "Elena",
  "model": "claude-sonnet-4-6",

  "memory_enabled": true,
  "context_notes_enabled": false,
  "memory_mode": "conversational",
  "plasticity": "reference",

  "vault_name": "",
  "vault_description": "Security review knowledge: vulnerability patterns, CVE history, project-specific threat model."
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `memory_enabled` | *bool | nil (inherit) | `true` = MuninnDB active; `false` = disabled; `nil` = inherits global default (true) |
| `context_notes_enabled` | bool | `false` | Enables `~/.huginn/agents/<name>.memory.md` and the `update_memory` tool |
| `memory_mode` | string | `"conversational"` | How proactively memory tools are used. See [Memory modes](#memory-modes). |
| `plasticity` | string | `"default"` | MuninnDB learning-rate preset. See [Plasticity presets](#plasticity-presets). |
| `vault_name` | string | auto-derived | Fully-qualified MuninnDB vault name. Empty = auto-derive from username + agent name. |
| `vault_description` | string | `""` | Description of the vault; injected into system prompt to ground memory use. |

**`memory_type` (API convenience field — not persisted)**

The web UI and API accept a `memory_type` shorthand on PUT/PATCH requests. It is translated to the canonical fields on write and is never stored in the JSON file.

| `memory_type` | Translates to |
|--------------|---------------|
| `"none"` | `memory_enabled: false`, `context_notes_enabled: false` |
| `"context"` | `memory_enabled: false`, `context_notes_enabled: true` |
| `"muninndb"` | `memory_enabled: true`, `context_notes_enabled: false` |

When reading an agent via GET, the API derives and returns `memory_type` from the canonical fields for convenience. Do not rely on `memory_type` in files you manage manually — use the canonical fields directly.

### Workspace-level vault mapping

You can pin a project's memory to a specific vault in `huginn.workspace.json` at the project root:

```json
{
  "memory_vault": "huginn:project:my-org/my-repo"
}
```

When set, all agents write their session learnings to this project vault in addition to their personal agent vault. Recall at session start pulls from both.

### Environment variables

| Variable | Description |
|----------|-------------|
| `HUGINN_MUNINN_ENDPOINT` | MuninnDB server URL, e.g. `http://localhost:8765`. Required for MuninnDB features. |

---

## Plasticity presets

Plasticity controls how aggressively MuninnDB updates an agent's vault during a session. Higher plasticity means more memories are written; lower plasticity means existing knowledge is treated as more authoritative and new writes are more selective.

| Preset | Write rate | Best for |
|--------|------------|---------|
| `"knowledge-graph"` | High | Code agents that accumulate patterns rapidly (Chris). Use when the agent is actively learning your codebase. |
| `"default"` | Balanced | General-purpose agents (Steve). Writes new knowledge without overwriting stable facts too aggressively. |
| `"reference"` | Low | Review and research agents (Mark). Treats vault knowledge as authoritative; only writes when highly confident. |

Set `plasticity` in the agent's JSON:
```json
{
  "plasticity": "knowledge-graph"
}
```

---

## Memory modes

Memory mode controls when and how proactively an agent uses its memory tools during a session.

| Mode | Behavior | When to use |
|------|----------|------------|
| `"passive"` | Memory tools are only invoked when you explicitly ask ("remember this", "what do you know about X") | Agents where you want full manual control over memory operations |
| `"conversational"` | Proactive recall at session start; writes key learnings at session end. No mid-conversation memory prompts. | Default for most agents. Balances autonomy with predictability. |
| `"immersive"` | Maximum engagement: orientation recall at start, mid-conversation memory hygiene, confidence feedback loops, active write at end | Long-running specialist agents where memory depth matters more than predictability |

Set `memory_mode` in the agent's JSON:
```json
{
  "memory_mode": "conversational"
}
```

---

## CLI memory commands

```bash
# Show memory status for all agents
huginn memory status

# Store a memory directly (bypasses session pipeline)
huginn memory store "We use snake_case for all database columns in this project"

# List recent memories from the default vault
huginn memory list
```

`huginn memory store` is useful for bootstrapping a new agent's vault before it has had any sessions, or for injecting authoritative facts you don't want the agent to have to learn through conversation.

---

## Vault names and scoping

**Default pattern**

When `vault_name` is empty in the agent's config, Huginn derives the vault name as:

```
huginn:agent:<username>:<agentname>
```

The `<agentname>` segment is normalized: lowercased, spaces replaced with hyphens, all other non-alphanumeric characters dropped.

Examples:

| Agent name | Username | Resolved vault name |
|-----------|---------|---------------------|
| `Steve` | `alice` | `huginn:agent:alice:steve` |
| `Code Reviewer` | `alice` | `huginn:agent:alice:code-reviewer` |
| `Elena` | `bob` | `huginn:agent:bob:elena` |

**Overriding the vault name**

Set `vault_name` explicitly to use a shared or pre-existing vault:

```json
{
  "vault_name": "huginn:team:platform-eng:security"
}
```

This is useful when multiple Huginn instances (e.g. different machines or team members) should share a single agent vault.

**Vault name collisions**

Two agents whose names normalize to the same string — for example `"Code Review"` and `"Code-Review"` — would resolve to the same vault name. Huginn detects this at save time and rejects the second agent with an error. Rename one of them or set an explicit `vault_name` on one to resolve the conflict.

**Scoping across projects**

By default, an agent's vault is personal — it is not project-scoped. If you work on multiple codebases with the same agent, all project knowledge accumulates in the same vault. This is usually desirable (the agent learns general patterns). To keep a project's knowledge isolated, set `memory_vault` in `huginn.workspace.json` as described above.

---

## Tips & common patterns

**Start with context notes, graduate to MuninnDB.** Context notes have zero setup cost. Use them first to understand what knowledge is actually useful for an agent. Once you know what should persist, MuninnDB's semantic recall makes retrieval more accurate — especially as the knowledge base grows.

**Seed the vault before the first session.** Use `huginn memory store "..."` or create the context notes file manually before the agent has had any sessions. Don't wait for the agent to learn everything through conversation — authoritative facts (project conventions, team preferences) should be injected directly.

**Use `vault_description` to focus memory.** Agents with a `vault_description` are grounded in what their memory is for. Without it, an agent may store and recall a broad range of things with no prioritization. Write one or two sentences describing the domain of knowledge the vault should contain.

**Match plasticity to the agent's learning curve.** A new code-writing agent benefits from `"knowledge-graph"` so it builds up project knowledge quickly. Once it has learned your codebase, dropping to `"default"` prevents it from overwriting stable knowledge with noise.

**Context notes and MuninnDB are mutually exclusive per agent.** You can set either `context_notes_enabled: true` or `memory_enabled: true`, but enabling both simultaneously is not recommended — the agent would receive duplicate memory injections and the `update_memory` tool might conflict with MuninnDB writes. Pick one per agent.

---

## Troubleshooting

**Agent doesn't recall anything from past sessions**

If MuninnDB is configured, verify it is running and the endpoint is reachable:
```bash
curl http://localhost:8765/health
```

If the health check fails, Huginn disables memory for the session silently. Check that `HUGINN_MUNINN_ENDPOINT` is set in the environment where Huginn runs (not just your shell profile — confirm the value is present in the process environment).

If using context notes, check that `context_notes_enabled: true` is set in the agent's JSON and that `~/.huginn/agents/<name>.memory.md` exists. If the file is missing, the agent has nothing to read at session start.

**Memory is being written but recall seems irrelevant**

The agent's `plasticity` may be too high, causing low-signal memories to be written with the same weight as important ones. Try lowering plasticity to `"default"` or `"reference"`. Also check `vault_description` — a missing or vague description means the agent has no guidance on what is worth retaining.

**`memory_type` field in the JSON file is not being read**

`memory_type` is a transient API field. It is not persisted to disk and is not read from disk. Set the canonical fields directly in your JSON file: `memory_enabled` and `context_notes_enabled`. The `memory_type` shorthand only works when sending requests to the HTTP API.

**Two agents sharing a vault are interfering with each other**

This usually means their names normalized to the same vault name (see [Vault name collisions](#vault-names-and-scoping)), or you intentionally set the same `vault_name` on both. If unintentional, give one agent an explicit `vault_name` that differs from the other. If intentional (shared team vault), this is expected behavior — all writes from both agents go to the same vault.

**Context notes file grows unbounded**

The `update_memory` tool rewrites the entire file on each call. If the agent is in `"immersive"` mode it may rewrite the file frequently. Switch to `"conversational"` mode to limit writes to session end only. You can also manually trim the file — the agent reads whatever is currently in the file at session start, so editing it directly is safe.

---

## See also

- [Custom Agents](custom-agents.md) — per-agent memory fields and configuration reference
- [Sessions](sessions.md) — within-session context, compaction, and session history
- [Local Tools](local-tools.md) — the `update_memory` tool and tool access controls
- [Architecture: Session and Memory](../architecture/session-and-memory.md) — internals of the three-layer state model
