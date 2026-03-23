# Skills

## What it is

Markdown files that inject instructions into agent system prompts. Drop a `.md` file into `~/.huginn/skills/` and enable it via the web UI Skills screen — the skill is then injected into every agent's system prompt at startup.

Skills extend agent behavior without modifying Huginn itself. You can add coding conventions, domain knowledge, review checklists, or any standing instruction you want agents to follow.

---

## How to create a skill

### Create a SKILL.md file

Each skill is a single Markdown file with a YAML frontmatter block. The filename does not matter; the `name` field in frontmatter is the skill's identifier.

1. Create the skills directory if it does not already exist:
   ```bash
   mkdir -p ~/.huginn/skills/
   ```

2. Create a `.md` file for your skill:
   ```bash
   touch ~/.huginn/skills/nil-guard.md
   ```

3. Write the skill file. The file must start with `---` YAML frontmatter and include at least a `name` field:

   ```markdown
   ---
   name: nil-guard
   author: yourname
   description: Always add nil guards before dereferencing pointers
   huginn:
     priority: 10
   ---

   Before writing any code that dereferences a pointer, always add a nil
   check and return a descriptive error rather than panicking.

   ## Rules

   - Never dereference a pointer without checking for nil first
   - Return descriptive errors, not panics
   - Use `if x == nil { return fmt.Errorf("...") }` pattern
   ```

The file body is split at the `## Rules` heading:

- Everything **before** `## Rules` is the **system prompt fragment** — injected verbatim into the agent's system prompt.
- Everything **after** `## Rules` is the **rule content** — treated as structured rules for the agent to follow.

Both sections are optional. A skill with no `## Rules` heading injects the entire body as a system prompt fragment.

### Enable the skill

Adding the file to `~/.huginn/skills/` is not sufficient on its own. The skill must also be enabled in the manifest. The easiest way is through the web UI:

1. Open the web UI (default: `http://localhost:8421`).
2. Go to **Settings → Skills**.
3. Find your skill in the list and toggle it on.

Alternatively, edit `~/.huginn/skills/installed.json` directly:

```json
{
  "skills": [
    { "name": "nil-guard", "enabled": true }
  ]
}
```

After enabling, restart Huginn or use the Skills screen reload button for the change to take effect.

---

## SKILL.md format reference

### Frontmatter fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the skill. Used in the manifest and UI. |
| `author` | string | No | Author name, shown in the Skills screen. |
| `source` | string | No | Source URL (e.g., a GitHub link) for community skills. |
| `description` | string | No | Short description shown in the UI skill picker. |
| `huginn.priority` | int | No | Injection priority. Higher numbers are injected first. Default is 0. |

### Body sections

| Section | Description |
|---------|-------------|
| Content before `## Rules` | System prompt fragment — appended to the agent system prompt on every request. |
| Content after `## Rules` | Rule content — structured rules surfaced to the agent as rule blocks. |

Both sections can contain any Markdown. Code blocks and bullet lists work well for precision.

---

## Skill tools (advanced)

Skills can expose custom tools to agents by creating a `tools/` subdirectory alongside the skill file. Each `.md` file in that directory defines one tool.

### Example tool file

`~/.huginn/skills/tools/run-lint.md`:

```markdown
---
tool: run-lint
description: Run golangci-lint on the current workspace and return output
mode: shell
shell: golangci-lint
args:
  - run
  - --out-format
  - line-number
timeout: 30
max_output_kb: 32
schema:
  type: object
  properties:
    path:
      type: string
      description: Directory to lint (default ".")
  required: []
---
```

### Tool frontmatter fields

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name exposed to the agent. Required. |
| `description` | string | Description shown to the agent when selecting tools. |
| `schema` | object | JSON Schema for the tool's input parameters. |
| `mode` | string | Execution mode: `template`, `shell`, or `agent`. |
| `shell` | string | Binary name or path (shell mode only). |
| `args` | []string | Static arguments passed to the binary before dynamic ones. |
| `timeout` | int | Maximum execution time in seconds. |
| `max_output_kb` | int | Cap on output size in KB. Default is 64 KB (0 = use default). |
| `agent_model` | string | Model to use for agent mode sub-calls. |
| `budget_tokens` | int | Token budget for agent mode sub-calls. |

### Tool modes

| Mode | Description |
|------|-------------|
| `template` | Renders the tool body as a Go template and returns the result as the tool output. |
| `shell` | Executes the specified binary with the given args and returns stdout. |
| `agent` | Spawns a sub-agent to handle the tool call. The agent body is the sub-agent's system prompt. |

---

## Auto-discovered rule files

The following files are loaded automatically from the project root without any manifest entry. They do not need to be in `~/.huginn/skills/` and do not need to be enabled anywhere.

| File | Notes |
|------|-------|
| `.huginn/rules.md` | Huginn-specific rules for this project. |
| `CLAUDE.md` | Claude Code rules file — Huginn also reads this. |
| `.claude/CLAUDE.md` | Alternate Claude Code rules location. |
| `.cursorrules` | Cursor editor rules — also respected by Huginn. |
| `.cursor/rules` | Alternate Cursor rules location. |
| `.github/copilot-instructions.md` | GitHub Copilot instructions — also respected. |

Auto-discovered files must be in the workspace root (the directory where you run `huginn`). Huginn does not scan subdirectories for them.

---

## Configuration

| Path | Description |
|------|-------------|
| `~/.huginn/skills/*.md` | Global skills. Available in all projects. |
| `.huginn/skills/*.md` | Project-local skills. Override global skills of the same name. |
| `~/.huginn/skills/installed.json` | Manifest tracking enabled/disabled state for each skill. |

A skill file in `~/.huginn/skills/` that is not listed in `installed.json` (or is listed as disabled) is not loaded.

Project-local skills (`.huginn/skills/*.md`) follow the same format. A project skill with the same `name` as a global skill takes precedence for that project.

---

## Tips and common patterns

- **Skills compose.** Multiple skills can be active at the same time. Each enabled skill's fragment is appended to the system prompt in priority order. Keep individual skills focused on one concern to avoid conflicting instructions.
- **Use `huginn.priority` to control ordering.** When multiple skills need to appear in a specific order in the system prompt, set `huginn.priority` on each. Higher values are injected first.
- **Project skills override globals.** Place a `.huginn/skills/my-skill.md` with the same `name` as a global skill to customize it per repo without modifying the global file.
- **Write clear `description` values.** The description field appears in the Skills screen UI picker. A one-sentence summary of what the skill enforces makes it easier to manage a library of skills.
- **Keep rule lists short.** Long rule sections can dilute the agent's attention. Prefer concise, actionable bullet points over lengthy prose in the `## Rules` section.

---

## Troubleshooting

**Skill not loading**

Check that the skill is enabled in the web UI (Settings → Skills) or in `~/.huginn/skills/installed.json`. A skill file present on disk but absent from the manifest (or marked `enabled: false`) is silently ignored.

**Skill silently skipped**

If the skill is enabled in the manifest but has no effect, verify that the frontmatter block starts with exactly `---` on the first line and that the `name` field is present. A frontmatter parse error causes the skill to be skipped with no user-facing error message. Validate your YAML syntax with a linter if in doubt.

**Auto-discovered file not loading**

Confirm the file is in the workspace root — the directory from which you run `huginn`. Huginn does not search subdirectories for auto-discovered files. Also check for typos in the filename; the names are matched exactly (e.g., `CLAUDE.md` is case-sensitive on Linux).

**YAML parse error**

Common causes: unclosed quoted strings, tabs instead of spaces for indentation, or a `huginn:` block written as `huginn.priority: 10` at the top level instead of nested:

```yaml
# Correct
huginn:
  priority: 10

# Incorrect
huginn.priority: 10
```
