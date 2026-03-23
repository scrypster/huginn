# Multi-Agent Channels: Gap Analysis

**Date:** 2026-03-17
**Author:** Principal Engineering Review
**Status:** Active — drives multi-sprint backlog
**Scope:** Spaces, channel routing, agent coordination, threads, delegation, real-time UX

---

## 1. Executive Summary

Huginn has the **skeleton** of a multi-agent channel system but not the **nervous system**. The data model is sound: spaces, members, lead agents, threads, and memory vaults all exist in SQLite and Go structs. But the wiring between these primitives is incomplete in several critical places, producing a channel experience that is static, uncoordinated, and blind.

**Current state:** A user sends a message in a channel. The server picks an agent (often the first member, ignoring the `lead_agent` field). That agent receives a bare-bones "Space Context" block listing member names with no descriptions. It responds in isolation. If it happens to emit `@agentName` text, a post-hoc mention parser fires a delegation — but there is no pre-routing, no coordination protocol, and no real-time status feedback to the frontend. Threads exist but cannot be created via the REST API. The frontend thread panel does not subscribe to WebSocket events, so it shows stale data until manually reopened.

**Target state:** A Slack-like living workspace where the lead agent automatically triages incoming messages, delegates to specialists with structured awareness of their capabilities, coordinates multi-agent workflows through threads with dependency DAGs, and the frontend reflects every state change in real time — typing indicators, thread status badges, delegation previews, and agent availability.

The gap is not "a few bugs." It is a **missing coordination layer** between well-built primitives.

---

## 2. Critical Blocking Gaps

### Gap 1: Lead Agent Field Is Ignored in Routing

**What's broken:** The `spaces` table stores `lead_agent TEXT` and `Space.LeadAgent` is populated, but `resolveAgent()` in `internal/server/ws.go:400-434` never consults it. Resolution order is: (1) session's `PrimaryAgentID`, (2) first `IsDefault` agent in config, (3) first agent in config. The lead agent concept is write-only.

**Root cause:** `resolveAgent` was written for single-agent sessions and was never extended for space-aware routing. The space-session creation handler (`POST /api/v1/space-sessions`) reads `space.LeadAgent` but only uses it to set the session's primary agent if it exists — a one-time assignment, not dynamic routing.

**Fix needed:**
- When a chat message arrives for a session with a `SpaceID`, resolve the space and prefer `space.LeadAgent` over the generic default-agent fallback.
- If `lead_agent` is set but the slug does not resolve to a configured agent, log a warning and fall back (do not silently serve the wrong agent).
- Validate `lead_agent` existence at space-creation time (currently no validation — `internal/spaces/store.go:90-110`).

**Files:** `internal/server/ws.go:400-434`, `internal/spaces/store.go:85-115`

---

### Gap 2: Space Context Lacks Agent Descriptions / Specialties

**What's broken:** `BuildSpaceContextBlock` (`internal/agent/context.go:264-298`) injects space name, kind, lead agent name, and a comma-separated member list into the system prompt. But it provides **zero information about what each member does**. The lead agent sees "Members: coder, reviewer, researcher" with no way to know who handles what.

**Root cause:** `AgentDef` (`internal/agents/config.go:14-79`) has no `description` or `specialty` field. It has `Slot` (display-only), `VaultDescription` (memory-scoped), and `SystemPrompt` (private to the agent). There is no structured, cross-agent-visible capability descriptor.

The older `BuildChannelContext` function in `internal/spaces/store.go:383-399` **does** accept an `agentDescriptions map[string]string` parameter and formats it properly — but this function has zero callers in the main codebase. It is dead code superseded by `BuildSpaceContextBlock`.

**Fix needed:**
- Add a `Description string` field to `AgentDef` (JSON: `"description"`). This is the agent's one-liner visible to other agents and the UI.
- Wire `BuildSpaceContextBlock` (or replace it) to accept and format descriptions per member.
- Populate descriptions from the agent config when building space context in `internal/server/ws.go:745-746`.
- Deprecate/remove the dead `BuildChannelContext` in `spaces/store.go`.

**Files:** `internal/agents/config.go:14-79`, `internal/agent/context.go:264-298`, `internal/server/ws.go:730-747`, `internal/spaces/store.go:380-399`

---

### Gap 3: Thread Creation API Returns 501

**What's broken:** `POST /api/v1/threads` (`internal/server/handlers_threads.go:292-332`) parses the request body correctly (agent_id, task, depends_on) but unconditionally returns `501 Not Implemented` with the message: "thread creation via HTTP is not yet available; threads are spawned by agent orchestration."

**Root cause:** `SpawnThread` requires orchestrator-level dependencies (agent registry, backend, session store) that were not wired into the HTTP handler context at the time of implementation. The comment at line 327-329 says this explicitly.

**Impact:** The frontend cannot programmatically create threads. Users cannot manually kick off a delegation from the UI. Only the internal mention-parsing path (`threadmgr/mentions.go:66-107`) and the `delegate_to_agent` tool can create threads.

**Fix needed:**
- Wire the thread manager's `Create` + `SpawnThread` into the HTTP handler using the same dependencies available to the WS chat path.
- The server already has `s.tm` (ThreadManager), `s.store`, and agent loader — pass them through.
- Return the created thread in the response body for frontend consumption.

**Files:** `internal/server/handlers_threads.go:292-332`, `internal/threadmgr/spawn.go`

---

### Gap 4: Frontend Thread Panel Is Not Reactive

**What's broken:** `useThreadDetail.ts` (web/src/composables/useThreadDetail.ts) is a pure fetch-on-open composable. It has zero WebSocket subscriptions. When a thread's status changes (Pending -> Running -> Done), the panel shows stale data until the user closes and reopens it.

The `delegationChain` ref (line 33) is initialized as an empty array and never populated — no server event feeds it.

**Root cause:** The backend emits `thread_status` events (`internal/threadmgr/spawn.go:393, 576`) via the broadcast function, but `useThreadDetail.ts` does not subscribe to them. The WS event integration was never completed for the thread panel.

**Fix needed:**
- Subscribe `useThreadDetail` to `thread_status`, `thread_created`, and `thread_updated` WS events.
- On `thread_status` for the currently-open thread, update status badge and re-fetch messages if status transitions to Done/Error.
- Populate `delegationChain` from a new server-sent `thread_delegation_chain` event or include it in the `thread_created` payload.

**Files:** `web/src/composables/useThreadDetail.ts`, `internal/threadmgr/spawn.go:390-400`

---

### Gap 5: No Pre-Routing for @Mentions in User Messages

**What's broken:** When a user types `@researcher analyze this dataset`, the message goes to the lead agent first. The lead agent processes it fully. Then, post-response, `CreateFromMentions` (`internal/threadmgr/mentions.go:66-107`) parses the lead agent's output for `@mentions` and fires delegations. But it does not parse the **user's** input for mentions before routing.

**Root cause:** The mention-parsing path is called after the lead agent responds, not before. There is no pre-routing intercept in the WS chat handler (`internal/server/ws.go:436+`).

**Fix needed:**
- Before dispatching to the lead agent, parse the user message for `@agentName` patterns.
- If a single agent is mentioned and the message is clearly directed at them (heuristic: starts with `@agent` or `@agent` is the only mention), route directly to that agent with the lead agent's space context injected.
- If multiple agents are mentioned, route to the lead agent with an injected instruction: "The user has mentioned @X and @Y. Coordinate delegation to them."
- This gives the user explicit control while keeping the lead agent in the loop for multi-agent coordination.

**Files:** `internal/server/ws.go:436-574`, `internal/threadmgr/mentions.go:30-61`

---

## 3. UX / Experience Gaps

### 3.1 No "Agent Thinking" Indicator

The frontend shows streaming tokens once the LLM starts producing output, but there is no visual signal during the gap between message send and first token. In a multi-agent channel, this gap can be significant (tool calls, memory lookups, delegation setup). The user sees nothing.

**Fix:** Emit an `agent_thinking` WS event when the orchestrator begins processing (before first token). Frontend shows a pulsing indicator with the agent's name and avatar.

### 3.2 No Agent Availability Status

There is no mechanism for the frontend to know whether an agent is idle, busy (processing another request), or in an error state. `useAgents.ts` fetches the agent list but tracks no real-time status.

**Fix:** Maintain an in-memory agent status map on the server. Emit `agent_status_changed` events. Display status dots (green/amber/red) next to member names in the channel sidebar.

### 3.3 No Delegation Preview

When the lead agent decides to delegate, the user sees nothing until the delegated agent starts streaming. There is no "Atlas is assigning this to Coder..." preview.

**Fix:** Emit a `delegation_preview` event from the ThreadManager when a thread is created but before it starts running. Frontend shows a brief toast or inline indicator.

### 3.4 Thread Status Not Visible in Chat Flow

Thread status badges exist in the ThreadDetail drawer but are not reflected inline in the main chat. A delegation that completes or fails is invisible in the message timeline unless the user opens the drawer.

**Fix:** Emit thread status changes as inline chat events (similar to system messages in Slack). "Coder finished task: implement OAuth flow" appears inline.

### 3.5 No Coordination Feedback Loop

After the lead agent delegates to multiple subagents, there is no synthesis step. Each subagent responds independently. The lead agent does not automatically summarize or reconcile results.

**Fix:** After all threads in a delegation batch complete, invoke the lead agent with a synthesis prompt containing the collected results. This is the "coordinator" pattern.

---

## 4. Architecture Deficiencies

### 4.1 BuildSpaceContextBlock Is Structurally Shallow

`BuildSpaceContextBlock` (`internal/agent/context.go:264-298`) produces:

```
## Space Context
**Space:** Engineering (channel)
**Lead Agent:** atlas
**Members:** coder, reviewer, researcher
```

This tells the LLM almost nothing actionable. Compare to what it should produce:

```
## Space Context
**Space:** Engineering (channel)
**Lead Agent:** atlas (you)
**Team:**
- coder: Writes and refactors code. Specializes in Go and TypeScript.
- reviewer: Reviews PRs, finds bugs, suggests improvements.
- researcher: Searches docs, reads codebases, produces summaries.

You are the coordinator. Route specialized tasks to the appropriate team member using @mentions or the delegate_to_agent tool.
```

The function needs agent descriptions, the agent's own identity awareness ("you are atlas"), and explicit delegation instructions.

### 4.2 Dead Code: BuildChannelContext

`BuildChannelContext` in `internal/spaces/store.go:383-399` is architecturally correct (accepts descriptions map) but dead. It should either be resurrected as the canonical builder or deleted. Having two context builders with overlapping intent creates confusion.

### 4.3 Memory Replication Wiring Uncertain

`buildMemReplicationContext` (`internal/server/ws.go:792-819`) constructs a `MemReplicationContext` and attaches it to the Go context. But whether `SetMemoryReplicator` is actually called during server startup is unclear. If not called, the replication context is constructed but never consumed — another write-only pattern.

**Action:** Audit the `serve` path to confirm `SetMemoryReplicator` is invoked. Add a startup log line confirming replication is active.

### 4.4 resolveAgent Is Space-Unaware

`resolveAgent` (`internal/server/ws.go:400-434`) operates entirely on session-level and global-config-level data. It has no `spaceID` parameter and no awareness of space membership. This means:

- A session in Space A could resolve to an agent that is not a member of Space A.
- The lead agent preference is not enforced.
- There is no guardrail preventing cross-space agent leakage.

**Fix:** Add a `resolveAgentForSpace(sessionID, spaceID string)` variant that constrains resolution to space members and prefers the lead agent.

### 4.5 No Coordination Protocol Between Lead and Sub-Agents

The system has delegation (threads) and mention parsing, but no structured coordination protocol. The lead agent cannot:

- Query thread status ("is coder done yet?")
- Send follow-up instructions to a running thread
- Cancel a thread and reassign
- Receive a structured completion signal from a subagent

These are tool-call primitives that should be registered when the agent is operating as a lead in a channel context.

---

## 5. Missing WebSocket Contract

The following events must be added to achieve a real-time channel experience:

| Event | Direction | Payload | Purpose |
|-------|-----------|---------|---------|
| `agent_thinking` | Server -> Client | `{ agent_name, session_id, space_id }` | Pre-stream "thinking" indicator |
| `agent_status_changed` | Server -> Client | `{ agent_name, status: "idle"\|"busy"\|"error", space_id? }` | Real-time agent availability |
| `delegation_preview` | Server -> Client | `{ lead_agent, target_agent, task_summary, thread_id }` | "Lead is delegating to X" preview |
| `delegation_complete` | Server -> Client | `{ thread_id, agent_name, status, summary }` | Inline completion signal |
| `space_member_added` | Server -> Client | `{ space_id, agent_name }` | Real-time roster updates |
| `space_member_removed` | Server -> Client | `{ space_id, agent_name }` | Real-time roster updates |
| `thread_help_resolving` | Server -> Client | `{ thread_id, agent_name }` | Already typed in frontend, never emitted |
| `thread_help_resolved` | Server -> Client | `{ thread_id, resolution }` | Already typed in frontend, never emitted |
| `thread_messages_updated` | Server -> Client | `{ thread_id, new_message_count }` | Trigger re-fetch in open ThreadDetail panel |

**Existing events that work correctly:**
- `agent_message`, `agent_stream_chunk`, `agent_stream_end` — functional
- `thread_status` — emitted by `threadmgr/spawn.go` — functional but frontend does not subscribe

---

## 6. Memory & Context Gaps

### 6.1 No Shared Channel Memory

Each agent has its own vault in MuninnDB. When the lead agent learns something in the channel conversation, subagents do not automatically receive that context. Memory replication infrastructure exists (`workforce.MemReplicationContext`) but its activation in the serve path is unverified.

**Fix:** Confirm replication is wired. Add integration test: lead agent stores a memory, verify subagent can recall it within the same space context.

### 6.2 Channel History Not Scoped to Space

`buildChannelRecentBlock` (`internal/server/ws.go:752-787`) calls `s.store.TailSpaceMessages(spaceID, 20)` — this is correctly scoped. However, the agent's conversation history (`sess.snapshotHistoryTail(50)` at `mcp_agent_chat.go:215`) is session-scoped, not space-scoped. If the session is reused across spaces (unlikely but possible with stale session IDs), history bleeds.

**Fix:** Assert that sessions are always 1:1 with spaces when `SpaceID` is set. Add a guard in `Append` that rejects writes if `sess.Manifest.SpaceID` does not match the expected space.

### 6.3 No Cross-Agent Context Handoff on Delegation

When the lead agent delegates to a subagent via thread creation, the subagent receives the `Task` string but no channel context, no recent history, and no lead agent reasoning. It starts cold.

**Fix:** When spawning a thread, inject: (1) the space context block, (2) the last N messages from the channel, and (3) the lead agent's delegation rationale (extracted from its response or tool call arguments).

---

## 7. Agent Awareness & Coordination

### 7.1 Lead Agent Needs Structured Team Awareness

The lead agent must receive, in its system prompt or as a context injection:

1. **Team roster with descriptions** — who is available and what they do
2. **Current thread status** — which agents are currently running tasks, what tasks, how long ago they started
3. **Its own role** — explicit instruction that it is the coordinator, not a direct executor for specialized tasks
4. **Delegation instructions** — how to use `delegate_to_agent` or `@mentions`, and when to respond directly vs. delegate

### 7.2 Subagents Need Channel Awareness

Subagents executing in threads should receive:

1. **Space context** — they are operating within a channel, not in isolation
2. **Delegation context** — who delegated to them, what the user originally asked, and what the lead agent's reasoning was
3. **Sibling awareness** — if other threads are running in parallel, a brief note on what they are doing (prevents duplicate work)

### 7.3 Missing Coordination Tools

The lead agent should have access to these tools when operating in a channel:

| Tool | Purpose |
|------|---------|
| `list_team_status` | Returns current thread statuses for all subagents in the space |
| `delegate_to_agent` | Already exists (via threadmgr) but needs enriched context injection |
| `recall_thread_result` | Retrieves the output/summary from a completed thread |
| `cancel_thread` | Cancels a running thread (exists internally but not exposed as agent tool) |

---

## 8. Prioritized Roadmap

### P0 — Blockers (Must fix before any enterprise demo)

| # | Item | Effort | Gap Ref |
|---|------|--------|---------|
| 1 | Wire `lead_agent` into agent resolution for space sessions | S | Gap 1 |
| 2 | Add `Description` field to `AgentDef`, wire into space context block | S | Gap 2 |
| 3 | Enrich `BuildSpaceContextBlock` with descriptions, self-identity, delegation instructions | M | Gap 2, 4.1 |
| 4 | Implement thread creation via REST API (remove 501) | M | Gap 3 |
| 5 | Subscribe `useThreadDetail` to WS `thread_status` events | S | Gap 4 |

### P1 — Critical UX (Required for "alive" feeling)

| # | Item | Effort | Gap Ref |
|---|------|--------|---------|
| 6 | Emit `agent_thinking` event, frontend indicator | S | 3.1 |
| 7 | Pre-route user `@mentions` before lead agent processing | M | Gap 5 |
| 8 | Emit `delegation_preview` event, frontend toast | S | 3.3 |
| 9 | Agent status tracking + `agent_status_changed` WS event | M | 3.2 |
| 10 | Inject channel history and delegation rationale into subagent threads | M | 6.3 |
| 11 | Post-delegation synthesis: lead agent summarizes subagent results | M | 3.5 |
| 12 | Inline thread completion messages in chat timeline | S | 3.4 |

### P2 — Polish (World-class finish)

| # | Item | Effort | Gap Ref |
|---|------|--------|---------|
| 13 | `space_member_added/removed` WS events + reactive roster | S | 5 |
| 14 | `list_team_status` tool for lead agent | M | 7.3 |
| 15 | `recall_thread_result` tool for lead agent | S | 7.3 |
| 16 | Verify and integration-test memory replication in serve path | M | 6.1 |
| 17 | Delete dead `BuildChannelContext` in `spaces/store.go` | S | 4.2 |
| 18 | Add `resolveAgentForSpace` to prevent cross-space agent leakage | M | 4.4 |
| 19 | Validate `lead_agent` existence at space creation time | S | Gap 1 |
| 20 | Populate `delegationChain` in `useThreadDetail` from server events | S | Gap 4 |

**Effort key:** S = 1-2 days, M = 3-5 days, L = 1-2 weeks

---

## 9. Definition of "World Class" — Acceptance Criteria

Each criterion is testable. The channel experience is world-class when ALL of these pass.

### Routing & Resolution
- [ ] User sends a message in a channel with a configured `lead_agent`. The lead agent responds (not member[0], not the global default).
- [ ] User sends `@coder fix this bug`. The coder agent receives the message directly with channel context injected. The lead agent does not respond first.
- [ ] Setting `lead_agent` to a non-existent agent slug returns a validation error at space creation time.

### Agent Awareness
- [ ] Lead agent's system prompt contains each team member's name AND description.
- [ ] Lead agent's system prompt explicitly states "You are [name], the lead agent" and includes delegation instructions.
- [ ] Subagent executing a delegated thread receives: the space context, the user's original message, and the lead agent's delegation rationale.

### Real-Time Experience
- [ ] Within 200ms of the user sending a message, the frontend shows "[agent] is thinking..." indicator.
- [ ] When the lead agent creates a delegation, a `delegation_preview` toast appears in the frontend within 500ms.
- [ ] When a thread status changes, the ThreadDetail panel updates without manual close/reopen.
- [ ] Agent availability (idle/busy) is shown as a status indicator next to each member in the sidebar.
- [ ] When a member is added to or removed from a space, the sidebar roster updates in real time.

### Threads & Delegation
- [ ] `POST /api/v1/threads` creates a thread and returns `201` with the thread object.
- [ ] The lead agent can call `list_team_status` and receive a structured JSON of current thread states.
- [ ] After all delegated threads complete, the lead agent is automatically invoked with a synthesis prompt.
- [ ] Thread completion is shown as an inline message in the chat timeline (not only in the side panel).

### Memory & Context
- [ ] Memory stored by the lead agent is accessible to subagents within the same space (replication verified).
- [ ] Channel history (last 20 messages) is injected into every agent's context for the space.
- [ ] Sessions are strictly scoped to their assigned space — no cross-space history bleed.

### Coordination Protocol
- [ ] The lead agent can cancel a running thread and the frontend reflects the cancellation within 1s.
- [ ] The lead agent can retrieve the result of a completed thread via tool call.
- [ ] Multiple subagents running in parallel do not duplicate each other's work (sibling awareness injected).

---

## Appendix: Key File Reference

| File | What it does | Relevant lines |
|------|-------------|----------------|
| `internal/agents/config.go` | `AgentDef` struct — needs `Description` field | 14-79 |
| `internal/agent/context.go` | `BuildSpaceContextBlock` — needs enrichment | 264-298 |
| `internal/agent/agent_prompt.go` | `buildAgentSystemPrompt` — pure string, no ctx param | 20-81 |
| `internal/agent/mcp_agent_chat.go` | `AgentChat` — injects space context at line 197 | 62-260 |
| `internal/server/ws.go` | `resolveAgent` (400-434), space context wiring (540-558), `buildSpaceContextBlock` (730-747) | 389-820 |
| `internal/server/handlers_threads.go` | Thread CRUD — create returns 501 | 292-332 |
| `internal/threadmgr/mentions.go` | `@mention` parsing + `CreateFromMentions` | 1-107 |
| `internal/threadmgr/spawn.go` | Thread lifecycle, `thread_status` broadcast | 390-400, 570-580 |
| `internal/spaces/store.go` | Space CRUD, dead `BuildChannelContext` | 383-399 |
| `internal/workforce/types.go` | `WithSpaceContext`, `MemReplicationContext` | 27-40 |
| `web/src/composables/useThreadDetail.ts` | Thread panel — no WS subscriptions | 1-162 |
| `web/src/composables/useAgents.ts` | Agent list — no real-time status | all |
