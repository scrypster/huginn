# CLI Reference

Complete reference for all Huginn flags, subcommands, and environment variables.

---

## Flags

These flags apply to the root `huginn` command and to subcommands where noted.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--version` | bool | false | Print version and exit |
| `--print`, `-p` | string | — | Non-interactive: run message and print response, then exit |
| `--agent` | string | — | Run with a specific named agent (e.g. `Chris`, `Steve`, `Mark`) |
| `--model` | string | — | Override the coder agent model for this session (overrides `coder_model` in config). Does not affect planner or reasoner models. |
| `--endpoint` | string | — | OpenAI-compatible backend endpoint, overrides config |
| `--headless` | bool | false | Headless mode — no TUI, output to stdout |
| `--cwd` | string | — | Working directory override (headless mode) |
| `--command` | string | — | Slash command to run on start (headless mode) |
| `--json` | bool | false | Output JSON; use with `--headless --print` |
| `--workspace` | string | — | Path to `huginn.workspace.json`, overrides workspace auto-detection |
| `--max-turns` | int | 0 | Max agentic loop iterations (0 = use config default of 50) |
| `--no-tools` | bool | false | Disable all tool use — plain chat mode |
| `--dangerously-skip-permissions` | bool | false | Skip all permission prompts; allows all tool use without approval |
| `--no-tray` | bool | false | Disable system tray icon (use with `huginn tray`) |

---

## Subcommands

### `huginn tray`

Launch the web server and system tray icon. The web UI is available at `http://localhost:8421` by default.

```bash
huginn tray [options]
```

| Option | Description |
|--------|-------------|
| `--server` | Skip onboarding wizard; start immediately. Use for Docker or unattended server deployments. |
| `--attach <address>` | Attach to a Huginn server already running at the given address instead of starting a new one. |
| `--no-tray` | Start the web server without registering a system tray icon. |

---

### `huginn serve`

Start the Huginn HTTP server only, with no system tray icon.

```bash
huginn serve [options]
```

| Option | Description |
|--------|-------------|
| `--foreground` | Run in the foreground; do not daemonize. |
| `--daemon` | Run as a background daemon. |

---

### `huginn init`

Initialize a Huginn configuration file at `~/.huginn/config.json` with safe defaults. Safe to run on an existing installation — will not overwrite values already set.

```bash
huginn init
```

---

### `huginn pull`

Download a model into `~/.huginn/models/`. Shorthand for `huginn models pull`.

```bash
huginn pull <model-name-or-url>
```

**Example:**

```bash
huginn pull qwen2.5-coder:14b
```

---

### `huginn models`

Model management subcommands.

```bash
huginn models <subcommand>
```

| Subcommand | Usage | Description |
|------------|-------|-------------|
| `list` | `huginn models list` | List all installed models with their size and source |
| `pull` | `huginn models pull <name>` | Download a model by name |
| `delete` | `huginn models delete <name>` | Uninstall a model and remove its files |

---

### `huginn runtime`

Control the managed Ollama runtime (Phase 3 / managed backend). Used when `backend.type` is `"managed"`.

```bash
huginn runtime [subcommand]
```

---

### `huginn relay`

Cloud relay management. The relay allows HuginnCloud to reach a locally running Huginn instance securely without requiring inbound firewall rules.

```bash
huginn relay <subcommand>
```

| Subcommand | Description |
|------------|-------------|
| `register` | Register this machine with HuginnCloud and obtain a relay token |
| `start` | Start the relay server in the foreground |
| `status` | Check relay connection status and display machine ID |
| `unregister` | Unregister this machine from HuginnCloud |
| `install` | Install the relay as a system service (launchd on macOS, systemd on Linux) |
| `uninstall` | Remove the relay system service |

---

### `huginn connect`

Run the HuginnCloud browser-based connect and registration flow. Opens a browser window to authenticate and link this machine to a HuginnCloud account.

```bash
huginn connect
```

---

### `huginn cloud`

Cloud integration management commands.

```bash
huginn cloud [subcommand]
```

---

### `huginn agents`

Agent management subcommands. Agents are named, configured personas that can be invoked with `--agent <name>` or switched to inside the TUI.

```bash
huginn agents <subcommand>
```

| Subcommand | Usage | Description |
|------------|-------|-------------|
| `list` | `huginn agents list` | Show all configured agents with their model assignments |
| `edit` | `huginn agents edit` | Create or modify an agent interactively |
| `use` | `huginn agents use <name>` | Set the active agent (persisted to config) |
| `show` | `huginn agents show <name>` | Display full details for a named agent |

---

### `huginn skill`

Skill package management. Skills are Markdown files that inject persona, constraints, or domain knowledge into the agent context.

```bash
huginn skill <subcommand>
```

| Subcommand | Usage | Description |
|------------|-------|-------------|
| `list` | `huginn skill list` | List installed skills with their enabled status and source |
| `search` | `huginn skill search [query]` | Search the registry by name, description, or tag. Omit query to list all. |
| `info` | `huginn skill info <name>` | Show details and prompt content for a skill (installed or in registry) |
| `install` | `huginn skill install <target>` | Install a skill (see install targets below) |
| `enable` | `huginn skill enable <name>` | Enable a disabled skill |
| `disable` | `huginn skill disable <name>` | Disable a skill without uninstalling it |
| `uninstall` | `huginn skill uninstall <name>` | Remove a skill file and manifest entry |
| `update` | `huginn skill update [name]` | Re-fetch registry skills from source. Omit name to update all. |
| `validate` | `huginn skill validate <path>` | Validate a SKILL.md file for correct format |
| `create` | `huginn skill create` | Create a starter SKILL.md template at `~/.huginn/skills/my-skill.md` |

#### Install targets

```bash
# Registry skill (by name)
huginn skill install nil-guard

# GitHub repository
huginn skill install github:scrypster/huginn-skills/skills/official/go-expert

# Local file path
huginn skill install ./my-skill.md
huginn skill install /absolute/path/to/skill.md
```

GitHub installs prompt for confirmation before downloading since they are not verified by the registry.

---

### `huginn stats`

Display usage and cost statistics for the current session or across all sessions.

```bash
huginn stats
```

---

### `huginn export`

Export sessions to a file.

```bash
huginn export
```

---

### `huginn logs`

View application logs.

```bash
huginn logs
```

---

### `huginn report-bug`

Print the bug report URL and the location of any crash dump files.

```bash
huginn report-bug
```

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `HUGINN_CLOUD_URL` | Override HuginnCloud relay base URL. Default: `https://api.huginncloud.com` |
| `HUGINN_JWT_SECRET` | JWT secret for relay authentication |
| `HUGINN_OAUTH_BROKER_URL` | Override OAuth broker URL |
| `HUGINN_CLOUD_TOKEN` | Pre-issued fleet token for headless HuginnCloud connections (CI/CD, Docker) |

---

## API Key Formats

The `backend.api_key` config field and related credential fields support three formats:

| Format | Example | Description |
|--------|---------|-------------|
| Literal | `"sk-ant-api03-..."` | Key value stored directly in config |
| Env var | `"$ANTHROPIC_API_KEY"` | `$` prefix — value is expanded from the named environment variable at runtime |
| Keychain | `"keyring:<service>:<user>"` | Reads the credential from the OS keychain (macOS Keychain, GNOME Keyring, etc.) |

The keychain format is the most secure option for persistent installations because the key never touches the config file.

---

## Examples

```bash
# Ask a question without launching TUI
huginn --print "what does the auth middleware do?"
huginn -p "summarize internal/payment/gateway.go"

# Run a specific agent
huginn --agent Steve "implement the login handler"
huginn --agent Chris "design the payment module architecture"

# Use a cloud model instead of local
huginn --model claude-sonnet-4-5 --endpoint https://api.anthropic.com

# Headless mode for CI
huginn --headless --print "run the test suite and report failures"

# JSON output for piping
huginn --headless --json --print "list all API endpoints"

# Server mode (skip onboarding, for Docker/servers)
huginn tray --server

# Server without system tray (headless Linux servers)
huginn tray --no-tray

# Attach CLI to an already-running server
huginn tray --attach localhost:8421

# Start relay and check its status
huginn relay register
huginn relay start
huginn relay status

# Install relay as a persistent system service
huginn relay install

# Pull a model before first use
huginn pull qwen2.5-coder:14b

# Manage agents
huginn agents list
huginn agents use Chris

# Install a skill from the registry
huginn skill install nil-guard

# Headless fleet deployment with pre-issued token
HUGINN_CLOUD_TOKEN=fleet-token-here huginn tray --server --no-tray
```
