# Huginn Dominance Roadmap: Phased Implementation Plan

**Date:** 2026-04-27
**Objective:** Close the gap with OpenClaw and surpass it by leveraging Huginn's existing multi-agent orchestration, workflow engine, and memory system — packaging what already works into a world-class UX.

---

## Codebase Audit Findings (Pre-Roadmap)

Before building, these are the facts from the code:

### MuninnDB Wiring Status
- **What IS wired:**
  - Per-agent vaults with auto-naming (`huginn:agent:<username>:<agentname>`)
  - `muninn_recall` and `muninn_where_left_off` are called during orchestrator prefetch (semantic prefetch with caching, TTL, LRU eviction)
  - Memory mode instruction injection into system prompt when vault tools are registered (passive/conversational/immersive modes)
  - `muninn_evolve` and `muninn_consolidate` are referenced in system prompt instructions
  - Vault health endpoint per agent (`/api/v1/agents/{name}/vault-status`)
  - Agent wizard in TUI includes memory configuration step

- **What is NOT wired:**
  - No cross-agent memory pushing. The "lead agent pushing memories to other agents" pattern is not implemented. Each agent operates in its own vault silo.
  - No semantic triggers from MuninnDB firing back into Huginn (e.g., "when a memory about X changes, notify agent Y"). The `muninn_replay_enrichment` and `muninn_retry_enrich` tools exist in MuninnDB but are not consumed by Huginn.
  - No memory browser/inspector in the Web UI. Vault status is health-only (ok/degraded/unavailable + tool count).
  - The Web UI agent editor has no memory configuration — only the TUI wizard does.

### Workflow Engine Status
- Fully functional: cron triggers, YAML definitions, step chaining, variable passing, retry/backoff, per-step timeouts, failure handling, sub-workflows, conditional steps, streaming output
- WorkflowsWatcher polls directory for YAML changes every 2s
- No concept of "heartbeat" as a packaged UX feature — it is just a workflow with a cron trigger

### Notification/Relay Status
- Relay protocol is mature: `notification_sync`, `session_done_notify`, HTTP proxy passthrough, PTY shell, remote agent execution
- `BroadcastNotification` forwards to relay even when no local WS clients are connected
- FCM/APNS: NOT wired. The relay sends `MsgNotificationSync` to HuginnCloud, but the cloud-to-device push (FCM/APNS) is not implemented. Capacitor Push Notifications plugin is not installed in the Ionic app.

### Agent Config Status
- No `purpose` field on `AgentDef`. Has: name, model, backstory, color, icon, vault_name, memory_mode, tools, connections
- No auto-generated workflow from agent creation

---

## Phase 1: Alive (Weeks 1-3)

**Goal:** Make Huginn feel proactive and alive out of the box. Users create an agent, give it a purpose, and it starts monitoring/reporting without manual workflow authoring.

### Tasks

1. **Add `purpose` field to `AgentDef`** (`internal/agents/config.go`)
   - Add `Purpose string` to the struct with `json:"purpose,omitempty" yaml:"purpose,omitempty"`
   - Purpose is a natural-language description of what this agent should proactively do ("Monitor my GitHub repos for security advisories and alert me daily")
   - Validation: optional, max 500 chars

2. **Agent Purpose UI in Web agent editor** (`web/src/views/AgentsView.vue`)
   - Add a "Purpose" textarea to the agent editor left panel, below backstory
   - Label: "What should this agent proactively do?" with helper text: "Leave blank for a conversational-only agent"
   - When purpose is set and saved, auto-generate a heartbeat workflow (Task 3)

3. **Auto-generate heartbeat workflow on agent save** (`internal/server/handlers.go` or new `handlers_heartbeat.go`)
   - When an agent is saved/created with a non-empty `purpose` field:
     - Generate a YAML workflow file: `workflows/heartbeat-{agent-name}.yaml`
     - Default cron: `0 */4 * * *` (every 4 hours) — user can customize later
     - Single step: runs the agent with prompt derived from purpose text
     - Notification targets: inbox (default), agent_dm if user has a DM space
   - When purpose is cleared, disable (don't delete) the heartbeat workflow
   - This is NOT a new engine — it writes a standard workflow YAML that the existing WorkflowsWatcher picks up

4. **"Active Heartbeats" section on agent detail page**
   - Show the auto-generated workflow with its cron schedule, last run time, next run time
   - Quick toggle to enable/disable
   - Link to full workflow editor for advanced customization
   - Show last 3 heartbeat outputs inline

5. **Wire native push notifications in HuginnCloud Ionic app**
   - Install `@capacitor/push-notifications` in the Ionic project
   - On app launch: request permission, get device token, send to HuginnCloud backend
   - HuginnCloud receives `MsgNotificationSync` from satellite relay, maps to FCM (Android) / APNS (iOS) push
   - Badge count from pending notification count
   - This is the single most impactful mobile gap — without it, "always-on" is meaningless on mobile

### Design Notes
- Heartbeat workflows are standard YAML — they work identically whether accessed via local UI or HuginnCloud relay
- Push notifications flow: Agent runs on satellite -> notification stored -> `MsgNotificationSync` to HuginnCloud -> FCM/APNS to device
- The agent purpose field should be included in the relay's agent sync so HuginnCloud mobile can display it

### Estimated Effort
- Tasks 1-4: ~1 week (mostly UI + a thin server handler that writes YAML)
- Task 5: ~1.5 weeks (FCM/APNS setup, HuginnCloud backend handler, Ionic plugin wiring, Apple/Google developer console config)

---

## Phase 2: Memory (Weeks 4-6)

**Goal:** Make MuninnDB a visible, auditable, and cross-agent asset instead of an opaque backend.

### Tasks

1. **AUDIT: Cross-agent memory sharing** (investigation, not code yet)
   - Map every agent's vault. Determine: are there shared vaults? Is the lead agent pattern documented anywhere?
   - Check if delegation (agent A delegates to agent B) passes memory context or if each agent only sees its own vault
   - Document findings. The "lead agent pushing memories to other agents" requires either: (a) a shared vault, (b) cross-vault writes via the lead agent's tools, or (c) a new memory-relay mechanism
   - **Decision needed from MJ:** Should cross-agent memory be (a) shared vault, (b) lead agent writes to other vaults, or (c) event-driven (MuninnDB notifies Huginn of changes)?

2. **Wire MuninnDB semantic triggers into Huginn**
   - MuninnDB has `muninn_replay_enrichment` — after a memory is enriched (entity extraction, linking), fire a webhook or event
   - Add a webhook endpoint in Huginn: `POST /api/v1/hooks/muninn` that receives enrichment events
   - When received: check if the enriched memory matches any agent's purpose/interests, and if so, queue a notification or trigger a heartbeat run
   - This makes memory a proactive trigger, not just passive storage

3. **Memory Inspector UI** (`web/src/views/MemoryView.vue` — new view)
   - Tree browser: vaults -> memories, with search
   - For each memory: show concept, content, entities, links, decay score, last accessed
   - Edit/evolve/forget actions directly from UI
   - Entity graph visualization (stretch — can be Phase 3 if complex)
   - Link to the agent that owns each vault

4. **Add memory configuration to Web agent editor**
   - Port the TUI wizard's memory step to the web UI
   - Memory mode selector (passive/conversational/immersive) with explanation of each
   - Vault name display (auto-generated, read-only unless advanced mode)
   - Vault health indicator inline (leverage existing `/api/v1/agents/{name}/vault-status`)

5. **Cross-agent memory write tool** (pending audit from Task 1)
   - If decision is (b): add a `muninn_share` tool that lets the lead agent write a memory to another agent's vault with proper attribution
   - If decision is (a): document shared vault setup pattern
   - If decision is (c): implement in conjunction with Task 2

### Design Notes
- Memory Inspector must work through HuginnCloud relay — all reads/writes go through the satellite's REST API, proxied via `MsgHTTPRequest/MsgHTTPResponse`
- The Ionic app should show a simplified memory view (recent memories, search) — full tree browser is desktop-first

### Estimated Effort
- Task 1: 2-3 days (audit + decision document)
- Task 2: 3-4 days (webhook endpoint + trigger logic)
- Task 3: 4-5 days (new Vue view with tree browser, search, CRUD)
- Task 4: 1-2 days (port from TUI, mostly UI)
- Task 5: 2-3 days (depends on decision)

---

## Phase 3: Polish (Weeks 7-9)

**Goal:** Make the local Huginn app buttery smooth and the mobile app feel native-quality.

### Tasks

1. **Web UI performance audit and fixes**
   - Profile Vue app: identify re-renders, large bundle chunks, slow composables
   - Lazy-load heavy views (WorkflowsView, MemoryView, ConnectionsView)
   - Add skeleton loaders for all data-fetching views
   - Optimize WebSocket message handling (batch DOM updates, debounce inbox refreshes)

2. **Mobile UX polish pass** (HuginnCloud Ionic app)
   - Haptic feedback on key actions (send message, approve permission, dismiss notification)
   - Pull-to-refresh on inbox, thread list, agent list
   - Offline indicator + graceful degradation when satellite is unreachable
   - Dark mode consistency audit (every screen)
   - Keyboard avoidance on chat input (common Ionic issue)
   - App icon + splash screen refinement

3. **Inbox as the command center**
   - Unify notifications, heartbeat outputs, workflow results, and agent DMs into a single prioritized inbox
   - Add filters: by agent, by severity, by type (heartbeat/workflow/dm/system)
   - Swipe actions: archive, snooze, open thread
   - Badge counts that sync between web, mobile, and system tray
   - ProposedAction buttons render inline (approve/reject/customize) — this already exists partially, make it reliable

4. **Skills discoverability UX**
   - Current: 410 skills in a flat list. The creator has already curated the top 100.
   - Add: categories/tags, "featured" section (top 20), search with fuzzy matching
   - Agent-contextual suggestions: when editing an agent's tools, suggest skills that match the agent's purpose/backstory
   - "One-click enable" that adds the skill to the agent's toolbelt and saves

5. **Workflow builder improvements**
   - Visual indication of heartbeat workflows vs. custom workflows
   - "Duplicate workflow" action
   - Workflow run history with output viewer (not just last run)
   - Quick-create from templates: "Daily digest", "PR monitor", "Inbox triage"

### Design Notes
- All polish must be tested through the relay path — if it feels smooth locally but janky through HuginnCloud, it doesn't count
- Mobile polish is high-signal for the App Store — reviews will make or break HuginnCloud adoption

### Estimated Effort
- Tasks 1-2: ~1.5 weeks (profiling + targeted fixes, not a rewrite)
- Tasks 3-4: ~1 week (mostly UI reorganization)
- Task 5: ~0.5 weeks (incremental improvements to existing workflow view)

---

## Phase 4: Expand (Weeks 10-13)

**Goal:** Add the high-value personal-life integrations that differentiate Huginn from work-only tools, and nail the "always-on" positioning.

### Tasks

1. **Google Calendar deep integration**
   - Existing: partial Google Calendar tools
   - Add: event creation, modification, conflict detection, daily briefing prompt
   - Heartbeat template: "Morning briefing" — runs at 7am, summarizes today's calendar + yesterday's unread notifications
   - This is the single highest-value personal integration

2. **Twilio SMS/Voice** (backlog, but high impact)
   - OAuth connection for Twilio
   - Tools: send SMS, make call (text-to-speech), receive SMS webhook
   - Use case: agent can text you when something urgent happens and push notification isn't enough

3. **Obsidian vault integration** (backlog)
   - Read/write markdown files in Obsidian vault path
   - Tools: search notes, create note, append to daily note
   - Natural pairing with MuninnDB — agent can externalize memories to Obsidian for human review

4. **Plaid financial data** (backlog, long-tail)
   - OAuth connection for Plaid
   - Tools: account balances, recent transactions, spending categories
   - Heartbeat template: "Weekly spending summary"
   - Note: PCI/compliance considerations — scope carefully

5. **"Always-on" marketing/positioning** (not a build task)
   - Update huginn.dev landing page to lead with "always-on AI assistant" narrative
   - Hero section: show heartbeat concept — "Your agents work while you sleep"
   - Add comparison table: Huginn vs OpenClaw, emphasizing multi-agent, workflows, memory, privacy
   - Document the heartbeat pattern in user docs with examples
   - This is copy/marketing work, not engineering — but it's load-bearing for adoption

### Honest Assessment: Spotify
The creator asked whether Spotify is worth integrating. My honest answer: **no, not in the near term.** Music control via AI assistant is a novelty feature with low daily utility. The user can already control Spotify from their phone/desktop — an AI intermediary adds friction, not value. The only compelling use case (mood-based playlist generation) is better served by Spotify's own AI DJ. Drop it from the roadmap. If user demand surfaces organically, revisit.

### Design Notes
- All new connections must register in the connections catalog (`internal/connections/catalog/`) with proper OAuth flow
- Twilio webhooks need to work when satellite is behind NAT — HuginnCloud relay should support inbound webhook proxying (new relay message type, or use existing `MsgHTTPRequest`)
- Obsidian integration is local-only by nature — works great for the local Huginn app, needs a file-sync story for HuginnCloud mobile

### Estimated Effort
- Task 1: ~1 week (extend existing Google connection)
- Task 2: ~1 week (new connection + tools)
- Task 3: ~0.5 weeks (file read/write tools, straightforward)
- Task 4: ~1 week (OAuth + financial tools, careful scoping)
- Task 5: ~2-3 days (copywriting + static site updates)

---

## Priority Rationale

| Phase | Why Now |
|-------|---------|
| Phase 1: Alive | This is the single biggest perception gap. OpenClaw feels proactive because it has push. Huginn has a better engine but no packaging. Purpose + heartbeat + push closes this in weeks, not months. |
| Phase 2: Memory | MuninnDB is Huginn's deepest moat — no competitor has Ebbinghaus decay, Hebbian learning, or per-agent semantic memory. But it's invisible to users. Making it visible and cross-agent turns a backend feature into a selling point. |
| Phase 3: Polish | The creator said "buttery smooth." This is where you earn App Store ratings and word-of-mouth. No amount of features compensates for jank. |
| Phase 4: Expand | New integrations are exciting but secondary. Calendar and Twilio are the only ones that meaningfully change daily usage. Obsidian and Plaid are nice-to-haves. |

---

## What This Plan Does NOT Include (Intentionally)

- **Home Assistant / IoT integration** — niche audience, complex setup, high support burden. Backlog.
- **Health data (Apple Health, Fitbit)** — privacy minefield, low daily utility for most users. Backlog.
- **Expanding skills marketplace beyond 410** — the creator is right: quality over quantity. The 13k community skills are mostly low quality. Focus on discoverability of the good ones.
- **TUI improvements** — the TUI is functional. The web UI and mobile app are where users live. TUI is for power users who already love it.
- **New agent execution modes** — the DAG delegation + parallel swarm is already ahead of competitors. Don't over-engineer what works.

---

## Key Decisions Needed From MJ

1. **Cross-agent memory model:** Shared vaults, lead-agent writes, or event-driven? (Phase 2, Task 1)
2. **Heartbeat default cadence:** Every 4 hours suggested — too frequent? Too infrequent? Should it be agent-purpose-dependent?
3. **FCM/APNS priority:** Is HuginnCloud App Store submission imminent, or is push a "wire it up for when we're ready" task?
4. **Twilio vs. email for urgent alerts:** Email delivery already exists in the notification system. Is Twilio SMS worth the integration cost, or is push notification + email sufficient for urgent delivery?
