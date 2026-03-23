# Sessions

## What it is

Every conversation with Huginn is a persistent session with its own message history, stored in `~/.huginn/sessions/`. Sessions survive restarts — close Huginn, reopen it, and every conversation is exactly where you left it. The web UI shows all sessions in the left sidebar, each one automatically named from your first message.

---

## How to use it

### Start a new session

In the web UI (`huginn tray`):
- Click **New Session** in the left sidebar to start a fresh conversation

In the TUI:
- Each `huginn` launch creates a new session automatically

### Resume a past session

In the web UI, click any previous session in the sidebar — the full message history is restored. You can continue from where you left off, including any code changes the agent made.

### Session naming

Sessions are automatically named from the first message you send. Long names are truncated in the sidebar. You can rename a session by clicking its name in the sidebar.

---

## Configuration

| Setting | Description |
|---------|-------------|
| `~/.huginn/sessions/` | Where all sessions are stored; each session is a directory with message history |
| `compact_mode` | When to compress long sessions: `"auto"` (default), `"never"`, `"always"` |
| `compact_trigger` | Context fill ratio (0.0–1.0) that triggers auto-compaction; default `0.8` |

For **long-term cross-session memory** (decisions, preferences, facts that persist across separate sessions), configure MuninnDB in `huginn.workspace.json`:

```json
{
  "memory_vault": "default"
}
```

MuninnDB stores and recalls information across sessions automatically. Within a session, the agent already has full context from earlier in the same conversation.

---

## Tips & common patterns

- **One session per project** — keep a long-running session for each project. The agent retains full context of earlier decisions, what files were changed, and why. Starting fresh sessions loses that context.
- **Let compaction handle long sessions** — for very long sessions, context compaction runs automatically when the context fill ratio reaches `compact_trigger`. The most recent exchanges are preserved; earlier context is summarized.
- **Back up `~/.huginn/sessions/`** — sessions are plain files on disk. Copy the directory to preserve your history before reinstalling or wiping your home directory.
- **Use MuninnDB for cross-session memory** — facts and decisions you want available in future sessions (not just the current one) should be stored via MuninnDB. Within a session, the agent remembers everything automatically.

---

## Per-Agent Memory

Each agent (Chris, Steve, Mark, and any you create) has its own private memory vault. The more you work with an agent, the more it learns about your preferences, coding style, and project context.

### How it works

1. **At session start** — the agent recalls its most relevant memories and injects them into its context under "Your Expertise"
2. **At session end** — Huginn extracts 1–5 key learnings from the conversation and writes them to the agent's vault
3. **Over time** — agents accumulate expertise specific to how you use them

### MuninnDB vs fallback

When [MuninnDB](https://github.com/scrypster/muninndb) is configured, agents use its cognitive memory engine (temporal priority, Hebbian reinforcement, confidence tracking). Without it, Huginn uses a simpler keyword + recency index — functional but not self-improving.

To enable MuninnDB:

```bash
export HUGINN_MUNINN_ENDPOINT=http://localhost:8765
```

Configure credentials in `~/.config/huginn/muninn.json`.

### Per-agent configuration

In your agent definition (`.huginn/agents.json`):

```json
{
  "name": "Steve",
  "plasticity": "default",
  "memory_enabled": true,
  "vault_name": ""
}
```

| Field | Values | Default |
|-------|--------|---------|
| `memory_enabled` | `true` / `false` | `true` |
| `plasticity` | `default` / `knowledge-graph` / `reference` | `default` |
| `vault_name` | fully-qualified vault name | auto-derived |

**Plasticity presets:**
- **`knowledge-graph`** — high write rate; best for code agents (Chris)
- **`reference`** — low write rate; best for research/reference agents (Mark)
- **`default`** — balanced for general-purpose agents (Steve)

### Disabling per-agent memory

Set `"memory_enabled": false` in the agent config to opt out per-agent:

```json
{
  "name": "Steve",
  "memory_enabled": false
}
```

---

## Troubleshooting

**Session history missing after reinstall**

Sessions live in `~/.huginn/sessions/`. If you wiped your home directory, the sessions are gone. Going forward, back up this directory before reinstalling.

**Context getting too long / agent seems confused**

Compaction triggers automatically at the `compact_trigger` threshold (default: 80% full). If it seems too aggressive or not aggressive enough, adjust in config:
```json
{
  "compact_mode": "auto",
  "compact_trigger": 0.85
}
```

Or force a fresh session by clicking **New Session** in the sidebar.

**Agent doesn't remember something from last week**

Within-session memory is automatic. Cross-session memory requires MuninnDB. If you want the agent to remember a decision across sessions, ask it to store it: "Remember that we use snake_case for all database columns."
