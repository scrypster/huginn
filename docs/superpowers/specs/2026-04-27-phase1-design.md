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

Three codepaths currently describe an agent to other agents:
- `BuildRoster()` in `internal/agents/roster.go` uses the first 60 characters of `SystemPrompt` via `extractPersonaBlurb()`
- `BuildSpaceContextBlock()` in `internal/agent/context.go` uses the `Description` field
- `BuildDMCrossSpaceContextBlock()` in `internal/agent/context.go` also formats member descriptions for DM-based cross-space awareness

All three diverge. None includes tools or connections. This is a maintenance landmine that gets worse as the agent count grows.

### Design

**`BuildCapabilityCard(agent AgentDef) string`** — a new function in `internal/agents/capability_card.go` (package `agents`) that generates a deterministic, runtime capability card from live `AgentDef` state.

Format:
```
[Agent Name] [capable, tools: yes]
Role: <first sentence of SystemPrompt, truncated at 200 chars>
Tools: filesystem, web_search, github (local) | browser, code_interpreter (toolbelt)
Skills: daily-briefing, git-monitor, pr-reviewer
Connections: GitHub, Google Calendar, Notion
Memory: conversational
```

Rules:
- **Model tier and tool support** are preserved from the existing roster format — `[capable, tools: yes]` / `[medium, tools: yes]` / `[low, tools: no]`. These are load-bearing for the lead agent's delegation decisions (don't assign complex reasoning to a low-tier model; don't assign tool-dependent tasks to a tools:no agent). The `ModelInfoFn` parameter from `BuildRoster()` is passed through to `BuildCapabilityCard()`.
- If the user has set a non-empty `Description` override on the agent, use it as the Role line instead of the SystemPrompt extraction.
- Connections are derived from Toolbelt provider names. Provider slugs (e.g. `"github"`) are title-cased for display (`"GitHub"`). A simple lookup map handles common providers; unknown providers use the slug as-is.
- Tools list is split: local tools (from `LocalTools`) labeled `(local)` vs toolbelt providers labeled `(toolbelt)`.
- Skills list from `Skills` field on `AgentDef`.
- Memory mode from `MemoryMode` field. The lead agent uses this to know whether a sub-agent retains context: `passive` = stateless, `conversational` = recent context, `immersive` = deep persistent memory. This affects whether the lead agent needs to supply explicit context vs letting the sub-agent recall on its own.
- Generated at runtime from live state — cannot go stale.

**Package placement:** `BuildCapabilityCard()` lives in `internal/agents/` (same package as `BuildRoster()`). Placing it in `internal/agent/` would create an import cycle (`agents` → `agent` → `agents`).

**Refactoring:**

All three functions are refactored to call `BuildCapabilityCard()`:
- `BuildRoster()` in `internal/agents/roster.go`
- `BuildSpaceContextBlock()` in `internal/agent/context.go`
- `BuildDMCrossSpaceContextBlock()` in `internal/agent/context.go`

One function, one source of truth.

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
HeartbeatCron    string `json:"heartbeat_cron,omitempty"    yaml:"heartbeat_cron,omitempty"`
```

- `HeartbeatEnabled`: default `false`
- `HeartbeatCron`: default `"0 */4 * * *"` (every 4 hours when enabled)

**Workflow YAML auto-generation:**

When `HeartbeatEnabled` is set to `true` on agent save, the server writes:
`~/.huginn/workflows/heartbeat-{sanitized-agent-name}.yaml`

WorkflowsWatcher polls every 2 seconds and picks it up automatically. No new engine. No new scheduler. A standard workflow YAML that the existing engine runs.

When `HeartbeatEnabled` is set to `false`, the `enabled` field in the YAML is set to `false`. The file is not deleted — custom cron settings are preserved for re-enable.

**Generated YAML structure:**

```yaml
# MANAGED BY HUGINN — changes to cron/enabled will be overwritten by the UI.
# To customize fully: copy to a new filename (e.g. my-ares-heartbeat.yaml) and Huginn will stop managing it.
name: "Heartbeat: Ares"
description: "Auto-generated heartbeat for Ares"
enabled: true
schedule: "0 */4 * * *"
notification:
  on_success: true
  on_failure: true
  severity: info
  deliver_to:
    - type: agent_dm
      from: "Ares"
steps:
  - name: "Check in"
    agent: "Ares"
    prompt: |
      You are checking in with your user. Use your tools and memory to assess whether
      anything warrants their attention right now.

      Respond as you would in a direct message to a colleague — conversational, direct, 2-4 sentences.
      Do not use bullet points, markdown tables, headers, or report formatting.
      Do not say "Heartbeat:" or "Status update:" or anything that sounds like a log entry.
      If there is nothing to report, say so in one sentence and stop.

      Good: "Nothing unusual in your repos today. The PR you opened yesterday is still waiting on review."
      Bad: "**Heartbeat Report**\n- Repos: OK\n- PRs: 1 open\n- Actions: None required"
    position: 0
    on_failure: stop
```

Key decisions in this YAML:
- `on_success: true` — the DM is always delivered, including nothing-to-report runs.
- `on_failure: true` — failure also delivers a DM so the user knows the agent couldn't check in.
- `deliver_to: [{type: agent_dm, from: "Ares"}]` — delivery goes to the agent's DM space only. No `inbox` entry is created. Heartbeat runs do **not** appear in the Activity Log.
- The prompt is the step's `prompt` field — it layers on top of the agent's existing SystemPrompt persona. The agent reads its own persona first, then the heartbeat instruction. This means a strongly-conditioned SystemPrompt ("always respond with structured output") could fight the heartbeat instruction. The heartbeat prompt is injected last so it takes precedence at inference time. If an agent's SystemPrompt contains explicit formatting instructions that conflict, this is a configuration problem to document, not to silently override.

**Managed file pattern:**

The YAML has a `# MANAGED BY HUGINN` header. Huginn overwrites the file (updating `enabled` and `schedule`) when the user changes settings in the UI. If the user renames the file (or copies it to a new filename), Huginn stops managing it — the user owns it entirely and the UI shows it as a custom workflow.

**Heartbeat YAML lifecycle:**

- **Agent rename:** The server detects that the agent name changed (compare old vs new name on save). It renames `heartbeat-{old-name}.yaml` to `heartbeat-{new-name}.yaml` and updates the internal `name`, `agent`, and `from` fields. The old file is not left behind.
- **Agent deletion:** Agent deletion removes `heartbeat-{name}.yaml` if it exists and is still marked `# MANAGED BY HUGINN`. User-customized files (renamed) are not touched.

**Nothing-to-report DMs:**

A nothing-to-report message is delivered as a normal DM — it increments the unread count in the agent DM space. This is intentional: the user should see "Quiet day." in their DM list, not receive silence. If users find this noisy at 4-hour intervals, Phase 3 adds a quiet mode toggle that suppresses nothing-to-report messages after N consecutive ones. The default cadence (every 4 hours) is the user's choice — the UI presets go down to Daily and Weekly for users who want lower volume.

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
- Workflow run results (success/failure/output) — for workflows that deliver to `inbox`
- Workflow errors and retries
- System-level notifications (agent errors, vault health issues, connection failures)
- `severity: urgent` alerts that require acknowledgment

**What does not live in the Activity Log:**
- Heartbeat outputs — heartbeat workflows deliver to `agent_dm` only, not `inbox`. They do not appear here.
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
| `internal/agents/roster.go` | Refactor `BuildRoster()` to call `BuildCapabilityCard()` |
| `internal/agent/context.go` | Refactor `BuildSpaceContextBlock()` and `BuildDMCrossSpaceContextBlock()` to call `BuildCapabilityCard()` |
| `internal/server/handlers.go` (or new `handlers_heartbeat.go`) | On agent save: generate/update/disable/rename/delete heartbeat YAML |
| `web/src/views/AgentsView.vue` | Add heartbeat toggle + cron presets to agent editor |
| `web/src/` (nav) | Rename Inbox → Activity Log, move to secondary nav |

### New files

| File | Purpose |
|------|---------|
| `internal/agents/capability_card.go` | `BuildCapabilityCard()` function (package `agents`) |
| `~/.huginn/workflows/heartbeat-{name}.yaml` | Auto-generated per-agent heartbeat workflow (runtime, not in repo) |

---

## Data Flow

**Heartbeat execution:**
```
UI toggle on → server writes heartbeat-{name}.yaml → WorkflowsWatcher picks up (≤2s)
→ cron fires → workflow runner calls agent with heartbeat prompt (appended after system prompt)
→ agent uses tools/memory → workflow completes
→ dispatchNotification(type: agent_dm, from: agent name) → DM space receives message
→ user sees DM from agent (no Activity Log entry)
```

**Delegation with capability cards:**
```
Lead agent starts orchestration → BuildRoster() calls BuildCapabilityCard() for each channel member
→ lead agent sees team directory (names, tiers, tools, connections, memory modes)
→ delegates task to sub-agent with "Why You Were Chosen" rationale
→ sub-agent executes with its own system prompt + task context (no card about itself)
```

---

## Failure Modes

1. **Capability card staleness:** Cannot happen — cards are generated at runtime from live AgentDef. If an agent's tools change, the next delegation sees the updated card immediately.

2. **Heartbeat notification fatigue:** Mitigated by conversational prompt design (short natural DMs), explicit nothing-to-report path, and user-selectable cadence presets. Phase 3 adds quiet mode if needed.

3. **Roster/channel context divergence:** Eliminated by unifying all three codepaths (`BuildRoster`, `BuildSpaceContextBlock`, `BuildDMCrossSpaceContextBlock`) to `BuildCapabilityCard()`. The maintenance landmine is removed.

4. **Managed YAML conflict:** If the user edits a managed file manually, Huginn overwrites their changes on next UI save. The header comment warns them. The escape hatch (copy/rename to a new filename) is the explicit path for full ownership.

5. **Heartbeat prompt vs SystemPrompt conflict:** If an agent's SystemPrompt contains explicit formatting instructions that conflict with the heartbeat prompt, the heartbeat instruction takes precedence (injected last). Persistent conflicts are a configuration issue — document in user-facing notes that heartbeat works best with conversational agents.

6. **Agent rename/delete lifecycle:** Addressed explicitly — rename migrates the YAML, delete removes it. Managed-file check prevents accidental removal of user-customized heartbeat workflows.

---

## Out of Scope (Phase 1)

- Push notifications (FCM/APNS) — Phase 1 backend only; Ionic wiring is separate work. Note: without push, heartbeat DMs on mobile are only visible when the user opens the app. The "proactive and alive" feeling is desktop-first until FCM/APNS is wired.
- Memory Inspector UI — Phase 2
- Cross-agent memory model changes — MemoryReplicator already handles lead-agent fan-out; sub-agent fan-out is a Phase 2 decision
- Heartbeat quiet mode — Phase 3
- Heartbeat delivery to channels or inbox — Phase 3 if needed
- Skills discoverability improvements — Phase 3
