# Notepad

## What it is

Notepads are persistent markdown files that inject standing context into every agent session. Think of them as sticky notes for your agents — project conventions, architectural decisions, recurring reminders, or any standing instruction you want every agent to have in mind without repeating it in every prompt.

Notepads are injected into the agent's system prompt automatically at the start of each session. They are sorted by priority so your most important notes arrive first in the context window.

---

## How to use it

### Create a notepad

Create a `.md` file in `~/.huginn/notepads/` (global, available in all projects) or `.huginn/notepads/` (project-local, inside your repo):

```bash
# Global notepad
mkdir -p ~/.huginn/notepads
touch ~/.huginn/notepads/coding-standards.md

# Project-local notepad
mkdir -p .huginn/notepads
touch .huginn/notepads/project-context.md
```

Notepad files are plain Markdown. No frontmatter is required — write your content directly:

```markdown
# Coding Standards

- All functions must have error returns as the last value
- Use structured logging (slog) — no fmt.Printf in production code
- Wrap all external errors with context: fmt.Errorf("operation: %w", err)
- SQL queries must use prepared statements; no string interpolation
```

### Set priority and scope via frontmatter (optional)

If you want to control injection order or restrict a notepad to a specific scope, add a YAML frontmatter block:

```markdown
---
name: coding-standards
priority: 10
scope: global
---

# Coding Standards

- All functions must have error returns as the last value
...
```

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Identifier (must match `^[a-zA-Z0-9_-]+$`). Defaults to filename stem. |
| `priority` | int | Injection order. Higher numbers are injected **first**. Default: 0. |
| `scope` | string | `"global"` (all projects) or `"project"` (this repo only). Default: `"global"`. |

### Override global notepads per project

A project notepad with the same `name` as a global notepad replaces it for that project. This lets you maintain global defaults while customizing per-repo.

```
~/.huginn/notepads/coding-standards.md     ← global
.huginn/notepads/coding-standards.md       ← overrides global in this project
```

### Manage notepads from the TUI

Use the `/notepad` slash command in the chat input:

```
/notepad         → list all active notepads
/notepad create  → create a new notepad (guided)
/notepad edit    → edit an existing notepad
```

### Manage notepads from the web UI

Navigate to **Settings → Notepads** to create, edit, enable, disable, and delete notepads without touching the filesystem.

---

## Configuration

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `notepads_enabled` | bool | `true` | Enable/disable notepad injection globally |
| `notepads_max_tokens` | int | `0` | Max tokens per notepad injection (0 = no limit) |

In `~/.huginn/config.json`:
```json
{
  "notepads_enabled": true,
  "notepads_max_tokens": 2000
}
```

Setting `notepads_max_tokens` prevents a very long notepad from consuming too much of the context window. Content is truncated at the limit.

### File locations

| Path | Scope | Priority |
|------|-------|----------|
| `~/.huginn/notepads/*.md` | Global — available in all projects | Base |
| `.huginn/notepads/*.md` | Project-local — override global on same name | Override |

Huginn does not scan subdirectories. Files must be directly in the `notepads/` directory.

---

## Tips & common patterns

- **Use notepads for things that never change** — recurring conventions, permanent team decisions, or architectural principles that every agent should know. One-off context belongs in the chat, not a notepad.
- **Keep notepads focused** — one concern per notepad. A "coding-standards" notepad and a separate "architecture-overview" notepad are easier to manage than one giant file.
- **Use priority to control order** — if you have both a "global rules" and a "project context" notepad, give the project context a higher priority (e.g., `priority: 10`) so it appears earlier in the system prompt.
- **Project notepads for repo-specific context** — document the tech stack, key modules, and important constraints in `.huginn/notepads/`. New team members and new sessions start with full project context automatically.
- **Token budget awareness** — each notepad consumes context window tokens. If you have many long notepads, set `notepads_max_tokens` to prevent them from crowding out your conversation history.

---

## Troubleshooting

**Notepad not being injected**

1. Check that `notepads_enabled` is `true` in `~/.huginn/config.json`.
2. Confirm the file is directly in `~/.huginn/notepads/` or `.huginn/notepads/` — subdirectories are not scanned.
3. Check for YAML frontmatter parse errors (unclosed strings, tab indentation). A frontmatter error causes the notepad to load with an empty name and may be silently skipped.
4. Restart Huginn or use the Settings reload if you edited the file while Huginn was running.

**Notepad shows wrong content after edit**

Huginn loads notepads at session start. Content changes take effect on the next new session, not mid-session.

**Project notepad not overriding global**

The `name` field in frontmatter (or filename stem if no frontmatter) must match exactly. Check both files use the same name.

---

## See Also

- [Memory](memory.md) — cross-session memory via MuninnDB (different from notepads)
- [Skills](skills.md) — system prompt injection for agent behaviors (similar concept, different use case)
- [Custom Agents](custom-agents.md) — per-agent system prompts and context notes
