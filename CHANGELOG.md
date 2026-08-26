# Changelog

All notable changes to Huginn are documented here.

## [Unreleased]

### Added
- Space-routed DMs/channels now load REST threads after timeline hydrate (active session and every `session_id` on the timeline) so ThreadPanel, previews, and A2A strips use the same helpers as session mode

### Fixed
- Settings → Tools no longer presents `tools_enabled` as a master off switch for `huginn serve`; the copy matches serve (builtins still register; allow/deny still apply; deny wins on conflict)
- Chat tool chips say **failed** instead of green **done** when a tool is denied, missing, or the assistant text is `TOOL_FAIL` / `DELEGATE_FAIL`
- `TOOL_FAIL` / `DELEGATE_FAIL` assistant text renders as a system error line, not teammate speech (bare hydrated tokens and `TOKEN: reason` both chip)
- Channel sidebar previews stay plaintext so `snake_case` and `TOOL_FAIL` keep their underscores
- New-agent form no longer opens as "Unsaved changes" (color picker first-paint `@change`) and hides Delete
- Parked Memory (empty vaults / disconnected) no longer badges agent cards or the channel header
- Version badge collapses `vv0.4.0-try-all` to `v0.4.0-try-all` (About, profile popover, and Stats SERVER — no extra `v` prefix)
- Settings → Stats no longer reports 0 messages / 0 tokens when transcripts exist: `Append` persists `message_count` / `last_message_id` / `updated_at` with the message, existing sessions are backfilled from `messages`, token usage is stamped on the assistant row and written to `cost_history` when known, and Last LLM Call shows "—" instead of a fake 0
- `/chat/:name` for an agent name (e.g. `/#/chat/Steve`) now redirects to that agent's DM space instead of treating the name as a new empty session
- Composer `@` mention picker now dismisses on Escape via TipTap suggestion `onExit`, instead of only hiding the popup
- Local Access “Allow all” now asks for confirmation before enabling God Mode (`local_tools: ["*"]`, including shell)
- Harness announcement lines (`Delegated to @…`, auto-approved, completed delegated work, `TOOL_FAIL` / `DELEGATE_FAIL`) render as system/delegation rows instead of teammate speech; A2A tools are omitted from the “N tool calls” chip
- Chat composer stays editable while an agent is responding so a second message can be queued without clearing the in-flight bubble
- Space-mode unreads now match the Slack surface: the in-chat jump pill keys off the space timeline, opening one space no longer clears unseen for unvisited spaces, and sidebar DM previews prefetch a last-message snippet without stripping TOOL_FAIL underscores
- Stats and Settings routes keep the Channels/DMs sidebar visible instead of unmounting the context panel
- Chat sidebar no longer flashes “No channels yet” / “No agents configured” while spaces are still loading
- Channel header "N agents / Manage agents" chip now opens the agent roster modal (add, remove, set lead) instead of only the read-only member panel; DMs stay read-only and the panel chevron still toggles the rail
- Desktop notifications now fire for space-mode agent replies when the Huginn tab is in the background
- Composer `@` picker lists only agents in the active space roster (channel = lead + members, DM = that agent); leftover `@Name` of a non-member is dropped with a "not in this channel" hint instead of silently hitting the lead
- Cmd+K global search now finds channel/DM (space) messages and opens `/#/space/:id` instead of leftover `/#/chat/:sessionId` chrome
- Persist inbound user messages at accept so mid-turn harness announcements no longer appear before the prompt after reload
- Mid-text `@Name` of someone not in the space no longer addresses them or extra-spawns a thread; leftover is dropped with the same "not in this channel" hint as a leading leftover. Spaces with a roster only address or extra-spawn roster names

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
