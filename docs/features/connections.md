# Connections

## What it is

Integrations that give agents real-time access to external services. Huginn supports two distinct categories:

- **OAuth integrations** — Google, Slack, Jira, and Bitbucket. Huginn handles the full OAuth2 PKCE flow, stores tokens securely in your OS keychain, and refreshes them automatically.
- **CLI integrations** — GitHub (`gh`), AWS (`aws`), and Google Cloud (`gcloud`). These use the CLIs you have already installed and authenticated. Huginn detects them and exposes them to agents automatically — no credential storage required.

Any MCP-compatible server can also be connected to expose custom tools — databases, internal APIs, and scripts.

Once configured, agents can query issues, pull requests, messages, and other service data as part of any task, without you pasting content manually.

---

## How to use it

### OAuth integrations (Google, Slack, Jira, Bitbucket)

Open the Huginn web UI and go to **Connections**. Click **Connect** next to any provider. A browser window opens to the provider's login page. After you approve, the token is stored in your OS keychain and the connection is active.

Agents use the connection automatically once it is established:

```
Summarize the open Jira tickets assigned to me
```

```
Post a message in #deploys on Slack: deployment complete
```

### CLI integrations (GitHub, AWS, Google Cloud)

Install the CLI and authenticate it the usual way. Huginn detects it automatically — no additional setup needed in Huginn.

```sh
# GitHub
gh auth login

# AWS
aws configure

# Google Cloud
gcloud auth login
```

After authenticating the CLI, Huginn's Connections page shows it as active. Agents can then run CLI commands as part of any task.

### MCP servers

Add any MCP-compatible server to the `mcp_servers` array in `~/.huginn/config.json`. The server starts automatically when Huginn launches, and its tools are available to all agents:

```json
{
  "mcp_servers": [
    {
      "name": "my-database",
      "command": "npx",
      "args": ["-y", "@my/mcp-database-server"],
      "env": { "DB_URL": "$DATABASE_URL" }
    }
  ]
}
```

---

## Configuration

### Token storage

OAuth tokens are stored in two layers automatically — no config file required:

| Layer | What | Where |
|-------|------|-------|
| Connection metadata | Provider, account label, scopes, expiry | `~/.huginn/connections.json` |
| OAuth tokens | access_token, refresh_token | OS keychain (macOS Keychain, GNOME Keyring, or Windows Credential Manager) |

On systems without a keychain (CI, Docker, SSH sessions), Huginn falls back to in-memory storage automatically. Tokens work for the session but do not persist across restarts.

### OAuth redirect URI

When registering your own OAuth app with a provider, set the redirect URI to:

```
http://localhost:8421/oauth/callback/<provider>
```

Where `<provider>` is one of: `google`, `slack`, `jira`, `bitbucket`.

### HuginnCloud broker (optional)

If you are using HuginnCloud, OAuth flows are routed through the broker. The broker hosts a certified OAuth app for each provider — you do not need to create your own. The relay handshake is cryptographic: the broker never receives your tokens, only a signed relay JWT that only your local instance can verify.

---

## Connecting via HuginnCloud

When Huginn is registered with HuginnCloud (`huginn cloud register`), the connect flow for OAuth integrations happens in the HuginnCloud web app instead of a local browser window.

### How it works

1. Open the HuginnCloud web app and go to **Connections**.
2. Click **Connect** next to a provider. A popup opens and takes you to the provider's login page.
3. Sign in and approve the connection. The popup closes automatically.
4. The connection appears as active in the HuginnCloud UI. Agents can use it immediately.

From your perspective it is the same one-click flow. The only difference is that the popup is opened by the cloud app instead of your local Huginn UI.

### Your tokens stay on your machine

HuginnCloud never stores your OAuth tokens. After you approve the connection, your tokens are delivered to your local Huginn instance and written to your OS keychain — the same place they go in the local flow. HuginnCloud only holds a short-lived, encrypted reference long enough to hand the tokens back to your machine.

### Supported providers through the cloud UI

All five OAuth providers are available when connected through HuginnCloud:

- Google (Gmail, Drive, Calendar)
- GitHub
- Slack
- Jira
- Bitbucket

You do not need to create or configure OAuth apps with any of these providers. HuginnCloud hosts certified apps for each one.

---

## Tips & common patterns

- **Agents use connections automatically** — you do not need to tell the agent which integration to use. If the task involves Slack, the agent uses the Slack connection. If it involves Jira, it uses Jira.
- **Tokens refresh silently** — Huginn refreshes OAuth tokens lazily when a tool needs them. If a token is expired, it is refreshed before the request goes out. You will not see auth errors unless the refresh itself fails.
- **CLI tools use your existing auth** — GitHub, AWS, and Google Cloud CLIs manage their own credentials. Huginn does not store or touch them. If you are already authenticated in your terminal, the agent can use those CLIs.
- **Multiple accounts** — some providers support multiple connected accounts. The Connections page shows each account separately; agents disambiguate by account label.
- **MCP servers can expose any tool** — databases, internal APIs, custom scripts, or anything else with an MCP server. Use `$ENV_VAR` syntax in the `env` field to pass secrets without hardcoding them.

---

## Troubleshooting

**OAuth redirect failure / "redirect_uri mismatch"**

Your OAuth app's registered redirect URI does not match Huginn's callback URL. Set it to `http://localhost:8421/oauth/callback/<provider>` in your OAuth app settings (for example, Slack → App Settings → OAuth & Permissions → Redirect URLs).

**Connection shows as active but agent gets auth errors**

The OAuth token may have expired and the refresh failed. Go to Connections in the web UI, disconnect the provider, and reconnect. This starts a fresh OAuth flow and stores a new token.

**CLI integration not detected**

The CLI binary must be in your `$PATH` and already authenticated. Run the CLI's auth status command manually to confirm:

```sh
gh auth status
aws sts get-caller-identity
gcloud auth list
```

If the command works in your terminal but Huginn still shows it as not detected, restart Huginn — CLI status is checked at startup and on-demand via the Connections page (refresh the page to re-detect).

**MCP server not starting**

Check the `command` and `args` values by running the command manually in a terminal. Check that all required `env` variables are set in your shell. Huginn logs MCP server startup errors — check the terminal output when Huginn launches.

**Tokens lost after restart**

Your system's keychain is likely unavailable (CI environment, Docker container, or SSH session without keychain forwarding). In these environments, Huginn uses in-memory storage and tokens do not survive restarts. Reconnect after each restart, or use HuginnCloud which handles token persistence centrally.
