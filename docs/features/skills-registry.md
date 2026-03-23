# Skills Registry

## What it is

The Skills Registry is Huginn's community hub for discovering and installing reusable agent behaviors. Browse curated skills, install them with a single command, and Huginn injects their instructions into every agent's system prompt automatically.

Skills are discovered from `https://skills.huginncloud.com/index.json` — a public registry of community-contributed SKILL.md files. You can also install skills directly from any GitHub repository or from a local file path.

---

## How to use it

### Browse available skills

```bash
# List all registry skills (searches name, description, category, tags)
huginn skill search

# Filter by keyword
huginn skill search golang
huginn skill search "code review"
huginn skill search security
```

Output:
```
SKILL               AUTHOR       DESCRIPTION
nil-guard           huginn-team  Always add nil guards before dereferencing pointers
go-expert           huginn-team  Expert Go coding conventions and idioms
security-reviewer   huginn-team  Flag common security issues in code review
```

### See skill details

```bash
huginn skill info nil-guard
```

Prints the full description, author, category, and the skill's system prompt fragment and rules — so you know exactly what will be injected before you install it.

### Install a skill

```bash
# From the registry (by name)
huginn skill install nil-guard

# From GitHub (any public repo — see warning below)
huginn skill install github:scrypster/huginn-skills/skills/official/go-expert

# From a local file
huginn skill install ./path/to/my-skill.md
```

Registry skills install silently. GitHub installs prompt for confirmation first:

```
⚠  This skill is not in the Huginn registry. Install anyway? [y/N]
```

After install, the skill is immediately enabled. No restart required — the next agent request picks it up automatically.

### List installed skills

```bash
huginn skill list
```

Output shows name, enabled/disabled status, and install source:

```
SKILL          STATUS    SOURCE
nil-guard      enabled   registry
go-expert      enabled   github:scrypster/huginn-skills/skills/official/go-expert
my-local-rule  enabled   local
```

### Enable and disable skills

```bash
# Disable without uninstalling
huginn skill disable nil-guard

# Re-enable
huginn skill enable nil-guard
```

Disabled skills remain on disk and in the manifest but are not injected into agent prompts.

### Update installed skills

```bash
# Update all registry-sourced skills
huginn skill update

# Update a specific skill
huginn skill update nil-guard
```

This force-refreshes the registry index and re-downloads the SKILL.md file for each registry-sourced skill. GitHub and local skills are not affected by `update`.

### Uninstall a skill

```bash
huginn skill uninstall nil-guard
```

Removes the skill file from `~/.huginn/skills/` and removes its entry from the manifest.

---

## huginn-skills: The official community repo

The official Huginn community skills collection lives at **`github.com/scrypster/huginn-skills`**. It is curated by the Huginn team and mirrors the skills available in the registry.

Install any skill from this repo directly:

```bash
# Install the official Go expert skill
huginn skill install github:scrypster/huginn-skills/skills/official/go-expert

# Install any skill from the community collection
huginn skill install github:scrypster/huginn-skills/skills/community/sql-reviewer
```

GitHub path format: `github:<user>/<repo>/<path>` — Huginn fetches the `SKILL.md` file at `https://raw.githubusercontent.com/<user>/<repo>/main/<path>/SKILL.md`.

> **Security note**: GitHub installs always show a confirmation prompt. Only install skills you trust — a skill is injected verbatim into your agent's system prompt.

To browse the full collection before installing, visit the repository on GitHub.

---

## Writing and publishing your own skill

### Create a skill from a template

```bash
huginn skill create
```

This creates `~/.huginn/skills/my-skill.md` with a starter template:

```markdown
---
name: my-skill
author: your-name
source: local
huginn:
  priority: 5
---

Describe what this skill makes the agent an expert in.
Add specific instructions, idioms, and domain knowledge here.

## Rules

Add non-negotiable rules that the agent must follow.
```

### Validate before sharing

```bash
huginn skill validate ~/.huginn/skills/my-skill.md
```

Output on success:
```
✓ Valid SKILL.md
  name:   my-skill
  author:  your-name
```

### Publish to the community

To share your skill with the community, open a pull request against `github.com/scrypster/huginn-skills`. Place your SKILL.md under `skills/community/<your-skill-name>/SKILL.md`. The registry index is updated when PRs are merged.

---

## Configuration

| Path | Description |
|------|-------------|
| `~/.huginn/skills/*.md` | Installed skill files |
| `~/.huginn/skills/installed.json` | Manifest tracking name, enabled state, and source for each skill |
| `~/.huginn/cache/skills-index.json` | Cached registry index (refreshed every 24 hours) |

### Manifest format

`~/.huginn/skills/installed.json` is managed automatically by the `huginn skill` commands. Each entry records the skill name, whether it is enabled, and where it came from:

```json
{
  "skills": [
    { "name": "nil-guard",  "enabled": true,  "source": "registry" },
    { "name": "go-expert",  "enabled": true,  "source": "github:scrypster/huginn-skills/skills/official/go-expert" },
    { "name": "my-local",   "enabled": false, "source": "local" }
  ]
}
```

Source values:
| Value | Meaning |
|-------|---------|
| `"registry"` | Installed from the official registry |
| `"github:user/repo/..."` | Installed from a GitHub repository |
| `"local"` | Installed from a local file path |

### Registry index cache

The registry index is fetched once and cached for 24 hours. After the cache expires, the next `huginn skill search` or `install` command refreshes it automatically. `huginn skill update` always force-refreshes regardless of cache age.

---

## Tips & common patterns

- **Start with `huginn skill search`** — browse without committing. Run `huginn skill info <name>` to read the full prompt before installing.
- **Skills compose** — multiple skills can be active simultaneously. Each adds its fragment to the system prompt in priority order. Keep individual skills focused to avoid conflicting instructions.
- **Project skills override globals** — a skill with the same `name` in `.huginn/skills/` (inside your repo) takes precedence over the globally installed version for that project.
- **Disable instead of uninstall** — use `huginn skill disable` to temporarily turn off a skill without losing its configuration.
- **Pin community skills** — if you rely on a specific community skill, copy its `.md` file into `.huginn/skills/` in your repo. This pins the exact version and makes it part of your codebase.

---

## Troubleshooting

**`huginn skill search` returns nothing**

The registry index could not be fetched. Check network connectivity. If the problem persists, delete the cache to force a fresh fetch:
```bash
rm ~/.huginn/cache/skills-index.json
huginn skill search
```

**Installed skill has no effect**

1. Confirm it is enabled: `huginn skill list` — status column should show `enabled`.
2. Validate the file is parseable: `huginn skill validate ~/.huginn/skills/<name>.md`.
3. If using the web UI, use the reload button in Settings → Skills.

**GitHub install fails**

The install path must contain a valid `SKILL.md` at `main/<path>/SKILL.md` in the repository. Check that the path is correct and the repository is public.

**`huginn skill update` skips a skill**

`update` only re-fetches skills with `"source": "registry"`. GitHub-sourced or local skills are not touched. To update a GitHub-sourced skill, run `huginn skill install github:...` again.

---

## See Also

- [Skills](skills.md) — SKILL.md format, frontmatter fields, tool definitions, auto-discovered rule files
- [Custom Agents](custom-agents.md) — assign specific skills to individual agents via the `skills` field
- [CLI Reference](../reference/cli.md) — full `huginn skill` subcommand reference
