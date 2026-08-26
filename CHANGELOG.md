# Changelog

All notable changes to Huginn are documented here.

## [Unreleased]

### Added
- Space-routed DMs/channels now load REST threads after timeline hydrate (active session and every `session_id` on the timeline) so ThreadPanel, previews, and A2A strips use the same helpers as session mode
- CLI `--print` / `--agent` oneshot now wires A2A delegation (`delegate_to_agent`, `wait_for_threads`, `list_team_status`, `recall_thread_result`) with an ephemeral session, ThreadManager, and SpawnThread. Preview is always auto-approved (`HUGINN_DELEGATION_PREVIEW=off`). Named agents resolve from `~/.huginn/agents/*.{yaml,json}`.
- Model picker warns when the selected model cannot reliably use tools (7b / `supportsTools: false`)
- User `@Name` in a channel or DM addresses that agent for the turn when they are on the space roster (stamped lead no longer swallows the mention). A leftover `@Name` of someone not in the roster still does not address them or extra-spawn a thread

### Fixed
- Residual playbook speech (`<wait for X to finish>`, "Once X has finished:", re-typed or invented tool JSON, echoed result objects) is stripped from CLI `agentOutput` and the web assistant bubble after tools ran; invented names are never executed and non-tool code fences stay intact
- Fenced or bare JSON invocations of granted tools mixed with prose (the qwen2.5-coder "playbook" shape) now promote and execute in order — glue prose stays visible, fences never paint in chat, unknown names stay inert, and a lead that delegates without `wait_for_threads` gets one automatic barrier so specialist results are not abandoned
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
- Switching DMs no longer paints a quiet room as busy: the responding banner, preparing-context line, and update-active-work strip stay on the space that owns the in-flight run
- Switching DMs no longer dumps another space's follow-up cards, thread completion cards, permission prompts, warnings, or thread-help toasts onto the room you opened — those events write to the owner space's timeline and only surface permission/toasts when that space is in view
- Assistant or user `@Name` of someone not in the space no longer extra-spawns a thread via CreateFromMentions (Tess DM `@Steve` stays Tess-only; channel roster members still spawn; standalone session-mode is unchanged)
- `delegate_to_agent` targeting someone not in the space roster now fails visibly (DELEGATE_FAIL) instead of spawning (Tess-only DM cannot spawn Steve; channel roster members still delegate; standalone session-mode is unchanged)
- Creating an agent now refreshes the chat sidebar and persists a derived description from the system prompt instead of leaving DMs stale and cards showing "No description"
- CLI `--print` / `--agent NAME MSG` now run the agentic tool loop (`ChatWithAgent` / `RunLoop`) instead of a bare ChatCompletion. `--print` honors `--agent`, `--model` (via SwapModel), `--no-tools`, `--max-turns`, and `--dangerously-skip-permissions` without requiring `--headless`. `--json` emits `agentOutput` plus `toolsCalled` `{name, args, result}` for each tool.
- Local Qwen 14b (Ollama) tool calls that arrive as JSON-in-content instead of structured `tool_calls` are promoted so the agent loop actually executes them. One or more whitespace-separated `{"name","arguments"}` objects in a single content blob are all promoted (Winston oneshot sent two).
- Empty agent toolbelt no longer grants every connection provider. `AllowedProviders` fails closed; `provider: "*"` / `connection_id: "*"` remains explicit allow-all. Server auto-approve (`NewGate(true, nil)` skipAll) no longer bypasses an empty toolbelt.
- In-flight responding status names the addressed `@mention` agent instead of always showing the channel lead; per-space run chrome from owner-scoped events is unchanged
- Mixed JSON-in-content tool calls (and streamed JSON tokens) are stripped from the user-visible assistant bubble so harness invocations never render as chat text.
- Streamed leftover after JSON-in-content (`}PONG`) stays one bubble — the first character is not dropped or forked into a nameless `ONG` row, and the sidebar preview keeps `PONG`.
- Model tool warning persists on agent cards, the editor, and chat header/composer (not only the create-agent picker)
- **bash `~` / `$HOME`** — the bash tool now expands `~` and `$HOME` in the command string the way a shell does (process home, not the session temp HOME). `ls ~` lists the real home (or a test fake home). If home cannot be resolved, the tool returns a loud expansion error instead of empty-success.
- **Permission-deny leftover JSON** — after a tool is denied, leftover harness JSON such as `{"name":"gh_issue_create",...}` is stripped from VisibleAssistantContent and oneshot `agentOutput` so it never appears in the visible answer.
- Follow-up JSON-in-content tool calls with missing arguments (e.g. `{"name":"wait_for_threads"}` after `delegate_to_agent`) are promoted and executed instead of becoming the oneshot `agentOutput`. `{"function_name":"bash"}` leftovers are treated the same; mixed JSON+prose is still stripped, not executed. Visible `agentOutput` also drops leftover `TOOL_FAIL` / `DELEGATE_FAIL` tokens and bare harness tool-name lines (names stay in `toolsCalled`).

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
