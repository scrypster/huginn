# Changelog

All notable changes to Huginn are documented here.

## [Unreleased]

### Fixed
- **bash `~` / `$HOME`** — the bash tool now expands `~` and `$HOME` in the command string the way a shell does (process home, not the session temp HOME). `ls ~` lists the real home (or a test fake home). If home cannot be resolved, the tool returns a loud expansion error instead of empty-success.
- **Permission-deny leftover JSON** — after a tool is denied, leftover harness JSON such as `{"name":"gh_issue_create",...}` is stripped from VisibleAssistantContent and oneshot `agentOutput` so it never appears in the visible answer.

## [0.4.0] - 2026-06-10

### Added
- **Agent handoff overhaul** — `wait_for_threads` tool lets a lead agent block until its delegates finish and collect every full result in one call, instead of polling `recall_thread_result`
- **Sub-agent heartbeat** — threads report live activity (`thinking (turn 3/50)`, running tool, waiting for help) with a "possibly stalled" flag after 2 quiet minutes, surfaced in `list_team_status`, `wait_for_threads`, `thread_status` events, and the thread-panel UI
- **WebSocket resume/replay** — per-session event ring buffer with a `resume` protocol message; reconnecting clients replay missed events instead of losing them. New `chat_cancel` message for explicit run cancellation
- **New LLM providers** — DeepSeek and Z.ai (GLM) as named providers, plus a generic Custom (OpenAI-compatible) provider with manual endpoint + key (#122)
- Cloud-initiated agent execution via `MsgRunAgent` / `MsgAgentResult` relay messages
- Local tools allowlist on agent definitions — agents now default-deny external tools unless explicitly granted
- `Manage Local Access` modal in web UI with full tool catalog and shell warning
- Category navigation in the Skills Browse tab
- Spaces: channel system prompt injection via `BuildChannelContext`
- Spaces: `ThreadEvent` type, emitter, and server broadcast wiring

### Changed
- Agent chat runs now derive from the server lifecycle rather than the client connection — a dropped tab or network blip no longer cancels an in-flight response
- Follow-up synthesis is status-honest — it reports delegate failures and unfinished work instead of always claiming completion, and includes files modified, key decisions, and artifacts
- OpenAI-compatible endpoint URLs are now version-aware (an endpoint already ending in `/v1`, `/v4`, etc. is no longer broken by an appended `/v1`)

### Fixed
- Agents created/updated/deleted via the API now refresh the live registry immediately, instead of being invisible to delegation and rosters until restart (#124)
- Duplicate follow-up synthesis when a lead agent collected a delegate's result via `wait_for_threads`
- Relay chat sessions now have an idle timeout and a non-blocking token pump, so a hung backend or congested hub can't stall the LLM stream; dropped-message accounting added
- Budget-truncated delegation context and dropped sibling updates are now disclosed to the agent instead of silently lost
- Session `Exists()` scan error; cache key now includes model
- Swarm max concurrency enforcement
- AWS token TTL expiry boundary check
- Backend startup health warning on first launch
- Prompt tool timeout; bash timeout now reads from config
- Machine ID generation, config race, and outbox wiring in TUI + serve paths

---

## Earlier Work

Huginn was developed through an intensive internal build sprint (February – March 2026) covering:

- **Core agent loop** — streaming, tool dispatch, MCP client, parallel swarm execution
- **Session & memory** — SQLite-backed sessions, MuninnDB integration, context compaction
- **Web UI** — full Vue 3 / Vite frontend: chat, agents, spaces, skills, workflows, connections, models, settings
- **TUI** — BubbleTea terminal UI with full feature parity
- **Connections** — OAuth broker, PKCE flow, token refresh, 20+ provider integrations
- **Skills system** — registry, hot-reload, community marketplace
- **Workflows** — cron scheduler, dead-letter queue, delivery retry with jitter
- **Relay / HuginnCloud** — WebSocket satellite, outbox with sequence recovery, JWT auth
- **Security hardening** — input validation, SSRF protection, rate limiting, permission gates

Full git history is available for detailed per-commit changes.
