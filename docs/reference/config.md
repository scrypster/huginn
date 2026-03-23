# Config Reference

Huginn reads `~/.huginn/config.json`. The file is created automatically on first run with safe defaults. All fields are optional — omit any field to use its default.

For machine-specific overrides without modifying the primary config, see [Config Files](#config-files) below.

---

## General

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `theme` | string | `""` | UI theme name |
| `workspace_path` | string | `""` | Path to `huginn.workspace.json` workspace config file |
| `machine_id` | string | auto-generated | Stable machine identifier used for HuginnCloud relay registration |
| `scheduler_enabled` | bool | `true` | Enable or disable all workflow and routine scheduling. Setting to `false` pauses all automations without deleting them. |
| `active_agent` | string | `""` | The currently active agent name; persisted across restarts |
| `active_session_id` | string | `""` | The currently active session ID; persisted across restarts |

---

## Models

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `planner_model` | string | `"qwen3-coder:30b"` | Model for the planner agent slot (Chris) |
| `coder_model` | string | `"qwen2.5-coder:14b"` | Model for the coder agent slot (Steve) |
| `reasoner_model` | string | `"deepseek-r1:14b"` | Model for the reasoner agent slot (Mark) |
| `default_model` | string | `""` | Optional override applied to all agent slots. Overridden by `planner_model`, `coder_model`, and `reasoner_model` when those are set. |
| `ollama_base_url` | string | `"http://localhost:11434"` | Ollama API endpoint |
| `embedding_model` | string | `"nomic-embed-text"` | Model used for semantic code search embeddings. Pulled from Ollama. |

---

## Backend

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `backend.type` | string | `"external"` | `"external"` (cloud API or Ollama) or `"managed"` (built-in llama.cpp) |
| `backend.provider` | string | `"ollama"` | `"anthropic"`, `"openai"`, `"openrouter"`, `"ollama"` |
| `backend.endpoint` | string | — | API endpoint when `type` is `"external"` |
| `backend.api_key` | string | — | API credential. Supports three formats — see below. |
| `backend.builtin_model` | string | — | Model name when `type` is `"managed"` |

### API key formats

The `backend.api_key` field (and any other credential field) supports three formats:

| Format | Example | Description |
|--------|---------|-------------|
| Literal | `"sk-ant-api03-..."` | Key value stored directly in the config file |
| Env var | `"$ANTHROPIC_API_KEY"` | `$` prefix — value is expanded from the named environment variable at runtime |
| Keychain | `"keyring:<service>:<user>"` | Reads the credential from the OS keychain (macOS Keychain, GNOME Keyring, etc.) |

The keychain format is recommended for persistent installations because the key never touches the config file on disk.

### Provider examples

**Anthropic:**
```json
{
  "backend": {
    "type": "external",
    "provider": "anthropic",
    "endpoint": "https://api.anthropic.com",
    "api_key": "$ANTHROPIC_API_KEY"
  },
  "coder_model": "claude-sonnet-4-5"
}
```

**Anthropic via OS keychain:**
```json
{
  "backend": {
    "type": "external",
    "provider": "anthropic",
    "endpoint": "https://api.anthropic.com",
    "api_key": "keyring:huginn:anthropic"
  },
  "coder_model": "claude-sonnet-4-5"
}
```

**OpenAI:**
```json
{
  "backend": {
    "type": "external",
    "provider": "openai",
    "endpoint": "https://api.openai.com",
    "api_key": "$OPENAI_API_KEY"
  },
  "coder_model": "gpt-4o"
}
```

**OpenRouter:**
```json
{
  "backend": {
    "type": "external",
    "provider": "openrouter",
    "api_key": "$OPENROUTER_API_KEY"
  },
  "coder_model": "anthropic/claude-sonnet-4-5"
}
```

---

## Agentic Loop

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_turns` | int | `50` | Max tool-use iterations per agent turn |
| `bash_timeout_secs` | int | `120` | Timeout for bash tool commands |
| `tools_enabled` | bool | `true` | Enable or disable all tool use |
| `allowed_tools` | []string | `[]` (all) | Tool whitelist; empty = all allowed |
| `disallowed_tools` | []string | `[]` | Tool blacklist |
| `diff_review_mode` | string | `"auto"` | When to show diffs: `"always"`, `"never"`, `"auto"` |
| `git_stage_on_write` | bool | `false` | Auto-stage files the agent writes |
| `context_limit_kb` | int | `128` | Max context window in KB (~32k tokens at 4 bytes/token). Compaction triggers when the context fill ratio exceeds `compact_trigger`. |

---

## Code Intelligence

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `semantic_search` | bool | `false` | Enable HNSW vector search alongside BM25 for code navigation. Requires Ollama and a configured `embedding_model`. |
| `brave_api_key` | string | `""` | Brave Search API key. When set, agents can search the web. Empty = web search disabled. |

---

## Web UI

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `web.enabled` | bool | `true` | Enable the web server |
| `web.port` | int | `8421` | HTTP port (0 = dynamic allocation) |
| `web.auto_open` | bool | `false` | Open browser automatically on start |
| `web.bind` | string | `"127.0.0.1"` | Bind address (`"0.0.0.0"` to expose on network) |
| `web.allowed_origins` | []string | `[]` | CORS allowed origins for the web server. Empty = same-origin only. |
| `web.trusted_proxies` | []string | `[]` | Trusted proxy IPs for `X-Forwarded-For` header processing. |

---

## Context & Memory

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `compact_mode` | string | `"auto"` | Context compaction strategy: `"auto"`, `"never"`, `"always"` |
| `compact_trigger` | float | `0.70` | Context fill ratio (0.0–1.0) that triggers compaction. Evaluated against `context_limit_kb`. |
| `notepads_enabled` | bool | `false` | Enable the notepads feature |
| `notepads_max_tokens` | int | `0` | Max tokens injected per notepad into context. `0` = no limit. |
| `vision_enabled` | bool | `false` | Enable image and screenshot input |
| `max_image_size_kb` | int | `0` | Max image size in KB for vision input. `0` = use built-in default. |

---

## MCP Servers

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `mcp_servers` | array of objects | `[]` | MCP server configurations; each entry has `name`, `command`, and `args` |

```json
{
  "mcp_servers": [
    {
      "name": "my-server",
      "command": "npx",
      "args": ["-y", "@my/mcp-server"],
      "env": { "API_KEY": "$MY_API_KEY" }
    }
  ]
}
```

---

## OAuth Connections

Configure OAuth app credentials for GitHub, Google, Slack, Jira, and Bitbucket integrations.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `oauth.google` | object (`{client_id, client_secret}`) | `{}` | Google OAuth app credentials |
| `oauth.github` | object (`{client_id, client_secret}`) | `{}` | GitHub OAuth app credentials |
| `oauth.slack` | object (`{client_id, client_secret}`) | `{}` | Slack OAuth app credentials |
| `oauth.jira` | object (`{client_id, client_secret}`) | `{}` | Jira OAuth app credentials |
| `oauth.bitbucket` | object (`{client_id, client_secret}`) | `{}` | Bitbucket OAuth app credentials |

```json
{
  "oauth": {
    "github": {
      "client_id": "Iv1.abc123",
      "client_secret": "$GITHUB_CLIENT_SECRET"
    }
  }
}
```

---

## Cloud

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `cloud.url` | string | `"https://app.huginncloud.com"` | HuginnCloud web app URL used for the browser connect flow |

> **Note:** The WebSocket relay endpoint is set via the `HUGINN_CLOUD_URL` environment variable, not a config key. See [CLI Reference](cli.md) for details.

---

## Config Files

### Primary config

```
~/.huginn/config.json
```

Created automatically on first run. All keys are optional.

### Local override file

```
~/.huginn/config.local.json
```

Optional. Keys present in this file override the corresponding keys in `config.json` without modifying the primary file. This is the recommended place for machine-specific secrets, alternate endpoints, or developer overrides that should not be committed to a shared dotfiles repository.

**Example `~/.huginn/config.local.json`:**

```json
{
  "backend": {
    "api_key": "sk-ant-api03-..."
  },
  "web": {
    "port": 9000
  }
}
```

---

## Workspace Config

Workspace config lives in a `huginn.workspace.json` file, either in the project root or at the path set by `workspace_path` in the primary config (or `--workspace` on the CLI).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `memory_vault` | string | `"default"` | MuninnDB vault name scoped to this workspace. Agents operating in this workspace read and write memory to the named vault. |

**Example `huginn.workspace.json`:**

```json
{
  "memory_vault": "my-project"
}
```

Place the file in the project root and commit it to version control so all contributors share the same vault name and workspace settings.
