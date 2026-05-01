# Huginn Dominance Roadmap v2 (Final)

**Date:** 2026-04-27
**Revision:** v2 — incorporates creator feedback + Haiku codebase audit
**Objective:** Close the gap with OpenClaw by shipping what matters: proactive agents, polished desktop UX, and real integrations. Honor the work that already exists.

---

## Key Decisions (Resolved)

### 1. Purpose vs SystemPrompt vs Description

These are three distinct things. They are not the same.

| Field | What it is | Who sees it | Max size |
|-------|-----------|-------------|----------|
| `SystemPrompt` | The agent's full persona, instructions, and behavioral rules | The agent itself (injected into every LLM call) | Unlimited |
| `Description` | A short summary of what this agent does | **Other agents** (used for delegation decisions in channels) | 500 bytes |
| *Purpose (proposed)* | What this agent should proactively do on a schedule | The heartbeat workflow generator | ~500 chars |

`SystemPrompt` is "who you are." `Description` is "what others see about you." `Purpose` would be "what you do when nobody is talking to you."

**Decision:** Do NOT add a new `purpose` field. Instead, repurpose `Description` to serve double duty — it already describes what the agent does, and that is exactly the input the heartbeat generator needs. When a user writes a description like "Monitor GitHub repos for security advisories and alert me daily," that is both what other agents need to know AND what a heartbeat workflow should execute. Adding a third free-text field would confuse users ("what's the difference between my agent's description and its purpose?"). Keep it simple: one description, optionally powers a heartbeat.

### 2. The Inbox: Keep It, Narrow It

The inbox should stay, but its role should be crystal clear.

**Reasoning:**
- Agents are people. People DM you — they do not file tickets into your inbox. Agent-to-human communication belongs in DMs. This matches the creator's vision.
- But **workflows are not people.** A cron job that runs at 3am and finds a problem needs somewhere to write its output. That is what the inbox is: a **workflow notification store**, not an agent communication channel.
- The inbox is already built exactly this way — workflow-only writes, never written by agents directly. The code already enforces this boundary.
- Removing the inbox would mean workflows have nowhere to report results except DMs (which would make agents feel like bots, not people) or email (which requires external config).
- For HuginnCloud mobile: push notifications will be wired to DMs and channel posts, not the inbox. The inbox is a desktop-only workflow log.

**Decision:** Keep the inbox. Rename it to "Activity Log" or "Workflow Log" in the UI to make its purpose unambiguous. It is not a communication tool — it is where workflows report results. Agents communicate via DMs and channels.

### 3. Cross-Agent Memory: Already Built

The `MemoryReplicator` in `/internal/agent/memory_replicator.go` implements exactly the "group text" model the creator described. When a lead agent remembers something, it automatically fans out to all channel members' vaults. No LLM cost. Deduplication via `replicated_concept:` tags. SQLite-backed retry queue with exponential backoff.

**What does NOT need to happen:** Rebuilding this, designing it, or "auditing" it as a roadmap task.

**What might need to happen:** Surfacing replication status in the UI (a small badge or indicator showing "3 memories shared to #ops-room"), and documenting the feature so users know it exists.

### 4. Web UI vs HuginnCloud Mobile

These are two completely separate products with different constraints:

| | Huginn (local) | HuginnCloud |
|---|---|---|
| Platform | Desktop browser (localhost) | Ionic mobile app (iOS/Android) |
| Responsive design | Not needed | Required |
| Push notifications | Browser notifications (optional) | FCM/APNS (required, future) |
| Offline | N/A (local server) | Needs offline queue |
| Twilio/Voice | NAT problem (see below) | Cloud relay solves it |

**Rule:** When this roadmap says "UI," it means the desktop web UI. Mobile is HuginnCloud scope and not in this plan unless explicitly noted.

### 5. Twilio Voice: The NAT Problem

Twilio needs to reach a webhook to deliver call events. A local Huginn installation is behind NAT — Twilio cannot reach it. Options:

1. **ngrok/tunneling** — works but fragile, not a product-quality solution
2. **HuginnCloud relay** — the cloud relay already proxies HTTP; it could accept Twilio webhooks and forward them to the local instance. This is the right architecture but requires HuginnCloud.
3. **Outbound-only voice** — Huginn calls out via Twilio API, no inbound webhook needed. Limited but works locally.

**Decision:** Defer Twilio voice integration. It is a HuginnCloud feature, not a local Huginn feature. When HuginnCloud is ready, the relay already has the infrastructure to proxy webhooks. Add it to the HuginnCloud roadmap, not this one. Keep it as a "Future" item here.

### 6. SendGrid

The delivery system already references SendGrid credentials (`"api_key", "from" for SendGrid` in the `CredentialResolver` comment). The `delivery.go` infrastructure supports it conceptually. But there is no formal `ProviderSendGrid` in the connections catalog.

**Decision:** Add SendGrid as a connection provider. It is low effort (add the provider constant, add credential fields to the connection wizard, wire the API key into the existing delivery pipeline). Ship it in Phase 3 alongside other polish work.

---

## Phase 1: Alive (Weeks 1-3)

**Goal:** Make agents feel proactive. A user creates an agent, writes a description, and the agent starts doing things on its own.

### Tasks

1. **Heartbeat opt-in toggle on AgentDef**
   - Add `HeartbeatEnabled bool` and `HeartbeatCron string` to `AgentDef` (`internal/agents/config.go`)
   - `HeartbeatEnabled` defaults to `false`. `HeartbeatCron` defaults to `"0 */6 * * *"` (every 6 hours) when not set.
   - This is per-agent opt-in, not global. The creator is right — 10 agents all running heartbeats every 30 minutes is wasteful and expensive. Each agent opts in independently, with its own cadence.

2. **Heartbeat workflow auto-generation**
   - When an agent is saved with `HeartbeatEnabled: true` and a non-empty `Description`:
     - Generate a workflow YAML: single step, agent runs with its description as the prompt context, delivers to the user's DM with that agent (not the inbox)
     - Use `HeartbeatCron` as the schedule
     - Write to `~/.huginn/workflows/heartbeat-{agent-name}.yaml`
   - When `HeartbeatEnabled` is toggled off, delete/disable the generated workflow
   - When `Description` or `HeartbeatCron` changes, regenerate the workflow

3. **Web UI: Heartbeat controls in agent editor**
   - Add a "Heartbeat" section to the agent editor panel
   - Toggle: "Run this agent on a schedule"
   - When toggled on, show a cron input (with human-readable presets: "Every hour", "Every 6 hours", "Daily", "Weekly") and a note: "Your agent's description will be used as its task"
   - Disable the toggle when `Description` is empty (with tooltip: "Add a description first")

4. **Heartbeat delivery defaults to agent DM**
   - The generated workflow should deliver results as a DM from the agent to the user — reinforcing the "agent as a person" metaphor
   - If the agent is in a channel, optionally deliver to the channel instead (channel members are notified, memory replication fans out automatically)

### Design Notes
- The heartbeat is just a workflow with syntactic sugar. No new runtime concepts.
- The existing `Heartbeater` in `internal/relay/heartbeat.go` is infrastructure-level (machine health). Agent-level heartbeats are a different concept using the workflow engine.
- Start with conservative defaults (6-hour cadence). Users can tune per-agent. This prevents the "10 agents chattering every 30 minutes" problem.

### Estimated Effort
- Backend (AgentDef + workflow generation): 3-4 days
- Web UI (heartbeat controls): 2-3 days
- Testing + edge cases (disable/re-enable, description changes): 2 days
- **Total: ~1.5 weeks**

---

## Phase 2: Memory Polish (Weeks 3-5)

**Goal:** Surface the memory system that already exists. Do not rebuild MuninnDB inside Huginn.

### Tasks

1. **Memory configuration in Web UI agent editor**
   - The TUI wizard has memory configuration (memory type, vault description, memory mode). The Web UI does not. Port this.
   - Memory type selector: None / Context Notes / MuninnDB
   - When MuninnDB: show vault name (auto-generated, editable), memory mode (passive/conversational/immersive), vault description textarea
   - This is just wiring UI to fields that already exist on `AgentDef`

2. **Replication status indicator**
   - In channel views, show a subtle indicator when memory replication is active ("Memories shared across N agents")
   - In agent settings, show replication status: last successful replication, retry queue depth
   - This surfaces the existing `MemoryReplicator` — no new backend work, just API endpoints to expose queue stats

3. **Document the memory architecture**
   - Users need to understand: what gets replicated, what does not, how vaults work, what memory modes mean
   - This is in-app help text and/or a docs page, not a feature
   - Critical for adoption — the system is powerful but invisible

4. **Replication gap: sub-agent memories**
   - Currently only the lead agent's memories fan out. If a sub-agent (delegatee) learns something important during a delegated task, it stays in the sub-agent's vault only.
   - Evaluate: should sub-agents' `muninn_decide` and `muninn_remember` calls also trigger replication to the parent channel? The infrastructure supports it — the replicator just needs to be wired into sub-agent sessions.
   - This is an evaluation + design task, not necessarily a build task in this phase.

### Design Notes
- The creator is right: do not build a MuninnDB browser inside Huginn. MuninnDB has its own UI for data exploration. Huginn should show operational status (is memory working? what is being shared?) not raw data.
- The vault description field is important — it grounds the agent's memory in purpose. Make it prominent in the UI.

### Estimated Effort
- Web UI memory config: 3-4 days
- Replication status API + UI: 2-3 days
- Documentation/help text: 1-2 days
- Sub-agent replication evaluation: 1-2 days (design only)
- **Total: ~2 weeks**

---

## Phase 3: Desktop UX Polish (Weeks 5-8)

**Goal:** Make the desktop web UI best-in-class. No mobile work — that is HuginnCloud.

### Tasks

1. **Rename Inbox to "Activity Log"**
   - Rename in UI only (keep the backend unchanged)
   - Move it from a primary navigation item to a secondary one (settings area or a collapsible panel)
   - Make it clear this is where workflows report results, not where agents talk to you
   - Add filtering: by workflow, by agent, by severity, by date range

2. **DM experience polish**
   - DMs are the primary human-agent communication channel. They should feel like iMessage, not a ticket system.
   - Typing indicators, read receipts, message timestamps, clear agent identity (avatar, color, name)
   - Quick actions: pin a message, copy, retry (re-send the user's last message)
   - Thread panel should show tool call chips (WS4 — already merged in PR #59)

3. **Channel experience polish**
   - Channels are where teams of agents collaborate. Show member list with agent descriptions.
   - When an agent posts to a channel, show its avatar and name. Other channel members should be visually present.
   - Show memory replication indicator: "Alice remembered X — shared with 3 channel members"

4. **Agent cards on nav/home screen**
   - Each agent should have a card showing: name, avatar, description, last active, heartbeat status (if enabled), memory status (connected/disconnected)
   - Clicking a card opens the DM with that agent
   - This replaces the current list view with something that feels alive

5. **SendGrid connection provider**
   - Add `ProviderSendGrid Provider = "sendgrid"` to connections catalog
   - Credential fields: API key, default from address
   - Wire into the existing `CredentialResolver` in delivery.go (the infrastructure already references SendGrid credentials)
   - Add to the connection wizard in the Web UI

6. **Desktop browser notifications**
   - Use the Web Notifications API for the local web UI
   - Trigger on: new DM from agent, workflow failure, heartbeat alert
   - Opt-in via browser permission prompt
   - This is the desktop equivalent of mobile push — free to implement, no infrastructure needed

### Design Notes
- No responsive/mobile design work. This UI runs on a desktop browser at localhost.
- The goal is to make the existing features feel polished and intentional, not to add new capabilities.
- The tool call chip (WS4) is already merged — build on it, do not redo it.

### Estimated Effort
- Inbox rename + filtering: 2-3 days
- DM polish: 3-4 days
- Channel polish: 2-3 days
- Agent cards: 2-3 days
- SendGrid connection: 1-2 days
- Browser notifications: 2-3 days
- **Total: ~3 weeks**

---

## Phase 4: Personal Life Connections (Parallel, Lowest Priority)

**Goal:** Close the gap with OpenClaw's personal-life integrations. Ship connection providers that make Huginn useful outside of work.

**This phase runs in parallel with Phases 1-3. It is the lowest priority but has no dependencies on the other phases.**

### Tasks

1. **Calendar connection (Google Calendar, Outlook)**
   - OAuth flow already exists for Google. Extend to Calendar API scopes.
   - Read: upcoming events, free/busy. Write: create events, RSVP.
   - Agent tools: `calendar_today`, `calendar_create`, `calendar_find_free`

2. **Todoist / task manager connection**
   - API key connection type (already supported)
   - Agent tools: `tasks_list`, `task_create`, `task_complete`
   - Natural fit for heartbeat agents: "Review my tasks every morning and remind me of priorities"

3. **Weather API connection**
   - Simple API key connection
   - Agent tools: `weather_current`, `weather_forecast`
   - Useful for heartbeat: "Good morning — here is your day: 3 meetings, 72F, rain at 4pm"

4. **Smart home (Home Assistant) connection**
   - Home Assistant runs locally — perfect fit for local Huginn
   - Long-lived access token (API key connection type)
   - Agent tools: `ha_states`, `ha_call_service`, `ha_scene_activate`

5. **Finance/budget read-only connections**
   - Plaid or similar for bank account read access
   - Read-only by design — agents should never move money
   - Agent tools: `accounts_balance`, `transactions_recent`, `spending_summary`

### Design Notes
- Each connection follows the existing pattern: add a `Provider` constant, define credential fields, implement tools, add to the connection wizard.
- Prioritize connections that work well with heartbeats (calendar + weather + tasks = killer morning briefing agent).
- Do not try to ship all of these. Pick 2-3 that matter most and do them well.
- The creator wants these to steal users from OpenClaw. Calendar + tasks + weather is the minimum viable "personal AI assistant" stack.

### Estimated Effort
- Per connection: 3-5 days (OAuth ones are slower, API key ones are faster)
- Recommended first batch: Calendar + Todoist + Weather = ~2 weeks
- Second batch: Home Assistant + Finance = ~2 weeks
- **Total: ~4 weeks (but spread across the entire roadmap timeline)**

---

## Future (Not in This Roadmap)

These are real features with real value, but they belong in future planning, not this cycle.

### HuginnCloud Mobile (FCM/APNS)
- The relay already sends `MsgNotificationSync` to HuginnCloud. The missing piece is cloud-to-device push via FCM (Android) and APNS (iOS).
- Requires: Capacitor Push Notifications plugin in the Ionic app, FCM project setup, APNS certificate, server-side push sender in the cloud relay.
- This is a HuginnCloud cost (AWS resources). Ship Huginn standalone first.

### Twilio Voice Integration
- Requires HuginnCloud relay to accept inbound Twilio webhooks (local Huginn is behind NAT).
- Needs research on voice models (OpenAI Realtime API, ElevenLabs, etc.) and whether they can work with a relay architecture.
- Outbound-only voice (Huginn calls you) is technically possible locally but limited.
- Defer until HuginnCloud is operational and voice model landscape is clearer.

### MuninnDB Semantic Triggers
- MuninnDB could fire webhooks back into Huginn when memories change ("when a memory about security is created, notify the security agent").
- The inbound webhook infrastructure exists (`POST /api/v1/workflows/{id}/webhook`). MuninnDB would need to support outbound triggers.
- This is a MuninnDB feature request, not a Huginn feature.

### Sub-Agent Memory Replication
- Pending the evaluation in Phase 2 Task 4. If the design is clean, implement in a future phase.

---

## What's Already Built (Don't Rebuild)

These systems exist, work, and should be honored — not redesigned.

### Cross-Agent Memory Replication (`/internal/agent/memory_replicator.go`)
- 820 lines, fully implemented
- Intercepts `muninn_remember`, `muninn_decide`, `muninn_evolve` tool calls
- Fans out to all channel members' vaults in parallel (pool of 3 MCP clients, max 8 concurrent)
- Deduplication via `replicated_concept:` entity tags — no echo loops
- SQLite-backed retry queue: exponential backoff (5s to 1h), 5 attempts, 7-day purge
- Each replica tagged: `["replicated:true", "source:AgentName", "channel:channel-name"]`
- **This is the "group text" model — lead agent remembers, everyone gets it, zero LLM cost**

### Delegation Context Injection
- Sub-agents receive `## Why You Were Chosen` with rationale
- Dependency summaries (files modified, decisions made, status) passed at dispatch
- Channel context (member list, recent activity) injected automatically
- Session summaries: LLM-generated, persisted, reloaded as "Recent Work Context"

### Notification/Inbox Infrastructure
- Workflow-only writes (agents never write to inbox directly)
- Agent DMs via SQLite Spaces (kind=dm) — natural person-to-person feel
- Channel posting via `deliver_to: space` in workflow YAML
- Email delivery via SMTP or Gmail OAuth connections
- Delivery retry with exponential backoff and jitter
- SSRF protection on webhook URLs

### Relay Protocol
- Mature: notification sync, session done notify, HTTP proxy, PTY shell, remote agent execution
- Heartbeat (infrastructure-level): machine health, disk, CPU, uptime
- Broadcasts to relay even when no local WS clients connected

### Inbound Webhooks
- `POST /api/v1/workflows/{id}/webhook` — seeds workflow scratchpad
- Already validated and working — can be extended for future integrations (Twilio, etc.)

---

## Summary Timeline

| Phase | Focus | Weeks | Priority | Dependencies |
|-------|-------|-------|----------|-------------|
| 1: Alive | Heartbeat agents | 1-3 | Highest | None |
| 2: Memory Polish | Surface existing memory system | 3-5 | High | None |
| 3: Desktop UX | Polish DMs, channels, notifications | 5-8 | High | Phase 1 (heartbeat status cards) |
| 4: Connections | Personal life integrations | 1-8 (parallel) | Lowest | None |

**Total estimated effort:** 8-10 weeks for Phases 1-3, with Phase 4 interleaved as capacity allows.
