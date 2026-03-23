# Changelog

All notable changes to Huginn are documented here.

## [Unreleased]

### Added
- Cloud-initiated agent execution via `MsgRunAgent` / `MsgAgentResult` relay messages
- Local tools allowlist on agent definitions — agents now default-deny external tools unless explicitly granted
- `Manage Local Access` modal in web UI with full tool catalog and shell warning
- Category navigation in the Skills Browse tab
- Spaces: channel system prompt injection via `BuildChannelContext`
- Spaces: `ThreadEvent` type, emitter, and server broadcast wiring

### Fixed
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
