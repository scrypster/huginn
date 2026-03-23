# Web UI

The Huginn web interface is a full-featured browser client served locally by the Huginn process. It provides everything available in the TUI plus a persistent session sidebar, a live sub-agent thread panel, Spaces (DMs and Channels), and richer management screens for agents, models, connections, skills, and workflows.

---

## Starting the web UI

### `huginn tray` (recommended)

Starts the Huginn server and a system tray icon. The server listens at `http://localhost:8421` by default. Open that URL in any browser on the same machine.

```bash
huginn tray
```

The tray icon provides quick access to open the UI and quit Huginn.

### `huginn serve`

Starts the server only, without the system tray icon. Use this in headless or Docker environments where a system tray is unavailable.

```bash
huginn serve
```

---

## Layout

The web UI uses a fixed left icon strip (48 px wide) for navigation and a main content area that occupies the rest of the viewport. A responsive grid layout is used within each view.

```
┌────┬──────────────────────────────────────────┐
│    │                                          │
│    │           Main content area              │
│ Nav│           (current view)                 │
│    │                                          │
│    │                                          │
└────┴──────────────────────────────────────────┘
```

Navigation icons in the left strip show badge counters for unread items (inbox, notifications). Clicking an icon switches the main content area to that view. The current view is highlighted in the strip.

---

## Views

### Chat

The primary workspace. Opens by default when you load the UI.

**Session sidebar (left panel)**
- Lists all saved sessions, most recent first.
- Click any session to restore its full message history.
- Click **New Session** to start a fresh conversation.
- Sessions are automatically named from the first message. Rename with `/title <text>` or the rename control in the sidebar.

**Message display (center)**
- Streams agent responses token by token as they arrive over WebSocket.
- Supports Markdown rendering: code blocks, tables, inline code, bold, italic.
- Tool call events (Bash, Glob, Grep, etc.) appear inline, collapsible.
- Agent identity headers identify which agent sent each message.

**Chat input (bottom of center panel)**
- Supports slash commands — type `/` to open the inline picker.
- Supports `@mentions` to address a specific agent directly.
- Multi-line input: `Shift+Enter` inserts a newline.
- Submit: `Enter`.

**Thread panel (right panel)**
- Appears when a sub-agent delegation is in progress.
- Each active sub-agent (Chris, Steve, Mark, or custom) gets its own thread card showing live streaming output.
- Cards persist after completion so you can review what each agent did.
- Artifact cards appear here when an agent produces structured output (code files, reports, data).

**Artifact cards**
- Displayed when an agent produces a structured artifact: a file, a report, a schema, etc.
- Cards show the artifact title, type, and a preview. Click to expand the full content.

---

### Agents

Full agent management screen.

**Agent list**
- Displays all registered agents: the three built-in roles (Chris / planner, Steve / coder, Mark / reasoner) plus any custom agents.
- Each agent card shows name, model, color swatch, and icon.

**Create agent**
- Opens a wizard to configure a new agent: name, model, system prompt, color, icon.
- The wizard also handles memory configuration for the new agent.

**Edit agent**
- Click any agent to open its edit panel.
- Editable fields: name, model, system prompt, color, icon.
- Memory management: view vault health, enable or disable memory, change memory mode.
- Skills assignment: select which skills this agent has access to.
- Tool configuration: enable or disable toolbelt tools and local tools.

**Delete agent**
- Removes the agent definition from `~/.huginn/agents/` and unregisters it from the running process.
- Built-in agents (Chris, Steve, Mark) can be deleted but will be recreated from defaults on next startup unless the defaults are overridden.

---

### Models

Model management for all three model slots and locally-run Ollama models.

**Installed models**
- Lists all models currently available to Huginn, including remote API models and locally pulled Ollama models.

**Pull a model**
- Enter an Ollama model name and click Pull to download it. Progress is shown inline.

**Delete a model**
- Removes a locally installed Ollama model to reclaim disk space.

**Switch active model per slot**
- Three slot selectors: planner, coder, reasoner.
- Changing a slot here is equivalent to editing the model in Settings and takes effect immediately for the current session.

---

### Connections

OAuth provider setup and token management.

**Supported providers**
- GitHub
- Google
- Slack
- Jira
- Bitbucket

**Authorize a provider**
- Click the authorize button next to a provider. Huginn opens the provider's OAuth consent page in a new tab using PKCE flow.
- After consent, the token is stored in the system keychain.

**Token status**
- Each connected provider shows its current token status: active, expiring soon, or expired.
- Expired tokens can be refreshed or re-authorized with one click.

**Revoke access**
- Click the revoke button to delete the stored token and disconnect the provider.

**Multiple accounts**
- Some providers support multiple accounts. Each account appears as a separate row.

---

### Skills

Skill discovery, installation, and management.

**Browse the registry**
- Community skill registry is listed with name, description, author, and category.
- Filter by keyword, category, or tag using the search bar.

**Install a skill**
- Click the Install button on any registry entry. The skill is downloaded to `~/.huginn/skills/` and registered immediately — no restart needed.
- Equivalent CLI: `huginn skill install <name>`.

**Uninstall a skill**
- Click Uninstall to remove the skill file. The command is removed from the `/` picker.

**Enable / disable per session**
- Toggle a skill's enabled state without uninstalling. Disabled skills do not appear in the picker.

**Installed skills list**
- Shows all locally installed skills with their enabled/disabled state and version.

---

### Workflows

Automation workflow builder and run history.

**Create a workflow**
- Click **New Workflow** to open the visual step editor.
- Set a workflow name and optional description.

**Visual step editor**
- Add steps sequentially. Each step configures:
  - **Agent**: which agent runs this step.
  - **Prompt**: the instruction sent to the agent.
  - **Schedule**: cron expression or manual-only.
  - **Output routing**: where the result goes (inbox, another step, webhook).

**Manual trigger**
- Click the Run button on any workflow to trigger it immediately, regardless of schedule.

**Run history**
- Each workflow shows a list of past runs with timestamp, status (success / failure / running), and a link to the output.
- Failed runs show the error reason.

**Delete a workflow**
- Removes the workflow YAML and cancels any scheduled triggers.

**Bundled workflow templates**
- Huginn ships with example workflows in `internal/server/workflows/`:
  - `code-review.yaml`
  - `daily-standup.yaml`
  - `health-check.yaml`
  - `weekly-summary.yaml`

---

### Settings

Global configuration for the running Huginn instance.

**Model selection**
- Planner, coder, and reasoner slot selectors. Changes are saved to `~/.huginn/config.json` and take effect in new sessions.

**Theme**
- Light or dark mode toggle.

**Backend configuration**
- API key management and backend endpoint override for Anthropic or custom backends.

**Tool permissions**
- Enable or disable individual tools (Bash, Glob, Grep, file read/write, fetch, etc.) for all sessions or specific agents.

**Web UI options**
- Port, bind address, auto-open, and origin/proxy settings (see [Configuration](#configuration) below).

---

### Logs

Structured application log viewer.

**Log display**
- All log entries from the running Huginn process, displayed in reverse-chronological order.
- Each entry shows timestamp, level, component, and message.

**Level filtering**
- Filter by log level: debug, info, warn, error.
- Multiple levels can be selected simultaneously.

**Search**
- Full-text search across log messages. Results are highlighted inline.

---

### Stats

Usage metrics and cost tracking.

**Token usage over time**
- Chart of input and output token counts by session or time range.

**Estimated cost**
- Dollar-amount cost estimate calculated from token counts and per-model pricing.

**Model usage breakdown**
- Which models were used, and how much of the token budget each consumed.

**Session statistics**
- Per-session breakdown: total tokens, latency (median / p95), cache hit rate.

---

### Inbox

Notification center for workflow results and system alerts.

**Notification list**
- Each notification shows: source (workflow name or system), timestamp, summary text, and status (unread / read / dismissed).

**Action buttons**
- **Mark read**: removes the unread badge without dismissing.
- **Dismiss**: removes the notification from the list.

**Filter by type**
- Filter to show only workflow results, system alerts, or agent notifications.

**Live badge counter**
- The inbox icon in the left strip shows a live count of unread notifications. The counter updates in real time over the WebSocket connection — no page refresh needed.

---

### Spaces (DMs and Channels)

Persistent collaborative conversation spaces that are separate from main chat sessions.

**Direct Messages (DMs)**
- One-on-one persistent conversation with a specific agent.
- Open from the sidebar or with `/dm <agent-name>`.
- History is preserved across sessions. Each DM has its own message thread.

**Channels**
- Multi-agent collaboration rooms. Multiple agents can participate in the same channel.
- Open from the sidebar or with `/channel <name>`.
- Useful for ongoing project discussions where you want to address different agents in the same thread.

**Workstreams**
- Space-specific workflows that run in the context of a channel or DM.
- Visible in the channel sidebar alongside message history.

**Differences from main chat sessions**
- Spaces are always persistent and named; they do not expire or rotate.
- Spaces do not share context with main chat sessions.
- Multiple users or devices connected to the same Huginn instance can participate in the same channel via HuginnCloud relay.

---

## Profile popover

Click the person icon at the bottom of the left navigation strip to open the profile popover.

| Item | Description |
|------|-------------|
| Machine ID | The unique identifier for this Huginn installation |
| Cloud connection status | Green dot = satellite relay active; red dot = not connected |
| "Connect to Huginn Cloud" | Initiates the HuginnCloud registration and relay setup flow |
| Cloud account info | Email and plan details when connected |

---

## Configuration

Web UI configuration lives in the `"web"` section of `~/.huginn/config.json`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `web.port` | int | `8421` | HTTP port for the web server |
| `web.auto_open` | bool | `false` | Automatically open the browser when `huginn tray` starts |
| `web.bind` | string | `"127.0.0.1"` | Bind address. Set to `"0.0.0.0"` to expose on the network (Docker, remote access) |
| `web.allowed_origins` | []string | `[]` | CORS allowed origins. Empty means same-origin only |
| `web.trusted_proxies` | []string | `[]` | IP addresses of trusted reverse proxies for `X-Forwarded-For` handling |

**Example — expose on the local network with auto-open:**

```json
{
  "web": {
    "port": 8421,
    "auto_open": true,
    "bind": "0.0.0.0",
    "allowed_origins": ["http://192.168.1.100:8421"],
    "trusted_proxies": []
  }
}
```

**Example — behind a reverse proxy (nginx, Caddy):**

```json
{
  "web": {
    "port": 8421,
    "bind": "127.0.0.1",
    "trusted_proxies": ["127.0.0.1"]
  }
}
```

---

## Troubleshooting

**`404 page not found` when opening the browser**

The binary was not compiled with the frontend assets embedded. Rebuild with the embed tag:

```bash
go build -tags embed_frontend -o huginn .
```

**`address already in use` on startup**

Port 8421 is taken by another process. Change the port:

```json
{ "web": { "port": 9000 } }
```

Then open `http://localhost:9000`.

**`Connection refused` at localhost:8421**

Huginn is not running. Start it:

```bash
huginn tray
```

**Cloud status shows "Not connected"**

The satellite relay to HuginnCloud is not active. See [HuginnCloud](huginncloud.md) for setup instructions.

**Thread panel does not appear**

The thread panel only renders when a sub-agent delegation is active. If no delegation is in progress, the panel is hidden. It is also only available in the web UI — in the TUI, sub-agent output appears inline in the terminal.

**WebSocket disconnects frequently**

If you are behind a reverse proxy, ensure it is configured to allow long-lived WebSocket connections (no short idle timeout). For nginx, add:

```nginx
proxy_read_timeout 3600;
proxy_send_timeout 3600;
```
