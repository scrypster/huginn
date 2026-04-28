# Phase 1 Design Spec: Capability Cards + Heartbeat + Activity Log

**Date:** 2026-04-27
**Phase:** 1 — Alive
**Objective:** Make Huginn feel proactive and alive. Agents should feel like colleagues, not tools. "It should feel like you are talking to people."

---

## Overview

Phase 1 ships three coupled systems:

1. **Capability Cards** — deterministic, always-accurate public resumes that let lead agents understand what other agents in a channel can do, enabling intelligent delegation
2. **Heartbeat System** — per-agent opt-in cron that runs the agent on a schedule and delivers a conversational update via DM, not a report
3. **Activity Log** — the existing Inbox renamed and repositioned as a secondary audit trail, freeing the primary interface for human-agent DMs and spaces

These three are coupled: heartbeat agents need capability cards to delegate properly in multi-agent channels, and the Activity Log repositioning makes DMs the primary interaction surface.

---

## 1. Capability Cards

### Problem

Two codepaths currently describe an agent to other agents:
- `BuildRoster()` uses the first 60 characters of `SystemPrompt`
- `BuildSpaceContextBlock()` uses the `Description` field

They diverge. Neither includes tools or connections. This is a maintenance landmine.

### Design

**`BuildCapabilityCard(agent AgentDef) string`** — a new function that generates a deterministic, runtime capability card from live `AgentDef` state.

Format:
```
[Agent Name]
Role: <first sentence of SystemPrompt, truncated at 200 chars>
Tools: filesystem, web_search, github (local) | browser, code_interpreter (toolbelt)
Skills: daily-briefing, git-monitor, pr-reviewer
Connections: GitHub, Google Calendar, Notion
Memory: conversational
```

Rules:
- If the user has set a non-empty `Description` override on the agent, use it as the Role line instead of the SystemPrompt extraction
- Connections are derived from Toolbelt provider names — provider names only, no credentials or tokens
- Tools lists are split: local tools (registered via `LocalTools`) vs toolbelt providers
- Skills list comes from the `Skills` field on `AgentDef`
- Memory mode comes from `MemoryMode` field
- Generated at runtime from live state — cannot go stale

**Refactoring:**

Both `BuildRoster()` and `BuildSpaceContextBlock()` are refactored to call `BuildCapabilityCard()`. One function, one source of truth.

**Who sees capability cards:**

The card is **outward-facing only** — it is a public resume, not self-knowledge.

- **Lead agent** receives the capability cards of all other agents in the roster before orchestrating. This lets it make intelligent delegation decisions.
- **Sub-agents** do not receive their own card. They already know who they are via their system prompt. They receive their task + "Why You Were Chosen" rationale as today.

The mental model: a team directory. The lead agent gets the directory. Sub-agents don't get a page about themselves.

**`AgentDef.Description` field:**

Kept as-is. Becomes an optional user override. When empty (the default for all existing agents), `BuildCapabilityCard()` generates the role line from SystemPrompt. When set, it overrides the role line. This is a non-breaking change — all existing agents behave identically.

---

## 2. Heartbeat System

### Design

**Two new fields on `AgentDef`** (`internal/agents/config.go`):

```go
HeartbeatEnabled bool   `json:"heartbeat_enabled,omitempty" yaml:"heartbeat_enabled,omitempty"`
HeartbeatCron    string `json:"heartbeat_cron,omitempty" yaml:"heartbeat_cron,omitempty"`
```

- `HeartbeatEnabled`: default `false`
- `HeartbeatCron`: default `"0 */4 * * *"` (every 4 hours when enabled)

**Workflow YAML auto-generation:**

When `HeartbeatEnabled` is set to `true` on agent save, the server writes:
`~/.huginn/workflows/heartbeat-{agent-name}.yaml`

WorkflowsWatcher polls every 2 seconds and picks it up automatically. No new engine. No new scheduler. A standard workflow YAML that the existing engine runs.

When `HeartbeatEnabled` is set to `false`, the workflow is **disabled** (not deleted). The file persists so a custom cron is not lost on re-enable.

**Managed file pattern:**

The generated YAML has a header comment:
```yaml
# MANAGED BY HUGINN - changes to cron/enabled will be overwritten by the UI.
# To customize fully: copy to a new filename (e.g. my-ares-heartbeat.yaml) and Huginn will stop managing it.
```

Huginn updates the managed file when the user changes the cron in the UI. If the user copies it to a new filename, Huginn stops touching it — the user owns it entirely.

**Heartbeat prompt design:**

The workflow step runs the agent with an injected instruction block:

```
You are checking in with your user. Use your tools and memory to assess whether anything
warrants their attention right now.

Respond as you would in a direct message to a colleague — conversational, direct, 2-4 sentences.
Do not use bullet points, markdown tables, headers, or report formatting.
Do not say "Heartbeat:" or "Status update:" or anything that sounds like a log entry.
If there is nothing to report, say so in one sentence and stop.

Good: "Nothing unusual in your repos today. The PR you opened yesterday is still waiting on review."
Bad: "**Heartbeat Report**\n- Repos: OK\n- PRs: 1 open\n- Actions: None required"
```

The agent has full access to its tools and memory — it can actually check things, not just echo.

**Delivery:**

`deliver_to: agent_dm` — result is delivered to the agent's DM space. The user sees a message from the agent (e.g., Ares), not a workflow notification entry. DM space is created automatically if it does not exist (existing behavior).

**Notification fatigue:**

Nothing-to-report cases are delivered as a single short sentence — the channel stays alive without flooding. Phase 3 can add quiet mode (suppress after N consecutive nothing-to-report messages) if needed.

**Web UI — agent editor:**

- Toggle: `"Send me regular updates"` (off by default, shown in agent editor)
- When toggled on: cron preset picker appears — Every 4 hours / Twice daily / Daily / Weekly
- Raw cron expression shown below presets for power users
- Last run time + next run time shown inline when enabled

---

## 3. Activity Log

### Design

**Rename:** Inbox → **Activity Log**

**Navigation:** Moved to secondary navigation. Not the first thing you see. Accessible but not primary.

**What lives in the Activity Log:**
- Workflow run results (success/failure/output)
- Workflow errors and retries
- System-level notifications (agent errors, vault health issues, connection failures)
- `severity: urgent` alerts that require acknowledgment

**What does not live in the Activity Log:**
- Heartbeat outputs (→ agent DMs)
- General agent-to-user communication (→ agent DMs or spaces)
- Conversational thread results (→ threads)

**Mental model:** Activity Log is your server logs made readable. You check it when something seems wrong, not as your daily interface. Agent DMs and spaces are the actual interaction surface.

**Backend:** No structural changes. The existing `Notification` struct, severity levels, `ProposedActions`, and `dispatchNotification()` are unchanged. This is a naming and navigation change in the web UI only.

**Mobile (Ionic app):** Activity Log accessible from menu — not the home screen. Home screen shows recent DMs and spaces.

---

## Architecture

### Files affected

| File | Change |
|------|--------|
| `internal/agents/config.go` | Add `HeartbeatEnabled`, `HeartbeatCron` to `AgentDef` |
| `internal/agent/agent_dispatcher.go` | Refactor `BuildRoster()` to use `BuildCapabilityCard()` |
| `internal/workforce/space_context.go` (or equivalent) | Refactor `BuildSpaceContextBlock()` to use `BuildCapabilityCard()` |
| `internal/server/handlers.go` (or new `handlers_heartbeat.go`) | On agent save: generate/update/disable heartbeat YAML |
| `web/src/views/AgentsView.vue` | Add heartbeat toggle + cron presets to agent editor |
| `web/src/` (nav) | Rename Inbox → Activity Log, move to secondary nav |

### New files

| File | Purpose |
|------|---------|
| `internal/agent/capability_card.go` | `BuildCapabilityCard()` function |
| `~/.huginn/workflows/heartbeat-{name}.yaml` | Auto-generated per-agent heartbeat workflow (runtime, not in repo) |

---

## Data Flow

**Heartbeat execution:**
```
UI toggle on → server writes heartbeat-{name}.yaml → WorkflowsWatcher picks up (≤2s)
→ cron fires → workflow runner calls agent with heartbeat prompt
→ agent uses tools/memory → workflow runner calls dispatchNotification(deliver_to: agent_dm)
→ DM space receives message → user sees message from agent
```

**Delegation with capability cards:**
```
Lead agent starts orchestration → BuildRoster() calls BuildCapabilityCard() for each channel member
→ lead agent sees team directory → delegates task to sub-agent with "Why You Were Chosen" rationale
→ sub-agent executes with its own system prompt + task context (no card about itself)
```

---

## Failure Modes

1. **Capability card staleness:** Cannot happen — cards are generated at runtime from live AgentDef. If an agent's tools change, the next delegation sees the updated card immediately.

2. **Heartbeat notification fatigue:** Mitigated by conversational prompt design (short, natural DMs) and explicit nothing-to-report path. Phase 3 adds quiet mode if needed.

3. **Roster/channel context divergence:** Eliminated by unifying both codepaths to `BuildCapabilityCard()`. The maintenance landmine is removed.

4. **Managed YAML conflict:** If user edits a managed file manually, Huginn overwrites their changes on next UI save. The header comment warns them. The escape hatch (copy to new filename) is the explicit path for full ownership.

---

## Out of Scope (Phase 1)

- Push notifications (FCM/APNS) — Phase 1 backend only; Ionic wiring is separate work
- Memory Inspector UI — Phase 2
- Cross-agent memory model changes — MemoryReplicator already handles lead-agent fan-out; sub-agent fan-out is a Phase 2 decision
- Heartbeat quiet mode — Phase 3
- Heartbeat delivery to channels or inbox — Phase 3 if needed
- Skills discoverability improvements — Phase 3
