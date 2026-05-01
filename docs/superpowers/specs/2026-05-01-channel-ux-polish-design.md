# Channel UX Polish — Enterprise-Grade Agent Experience

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 6 compounding UX failures that together prevent users from confidently delegating work to agents in channels.

**Architecture:** Pure frontend for Issues 1, 2, 5, 6. Frontend + backend WS event for Issue 4. Frontend save-flow fix for Issues 3+6.

**Tech Stack:** Vue 3, TypeScript, Go, WebSocket event bus, CSS (Tailwind)

---

## Issue 1: Horizontal Scroll in Chat Pane

**Root cause:** Missing `min-w-0 overflow-hidden` cascade in nested flex containers. Every flex level needs it explicitly or intrinsic content width propagates upward. Code blocks (`<pre>`) and long delegation task descriptions widen the column.

**Fix:**
- `style.css`: Add `overflow-x: auto; max-width: 100%` to `.md-content`. Add `overflow-x: auto` to `.md-content pre` and `.md-content table`.
- `ChatView.vue`: Add `min-w-0 overflow-hidden` to the assistant message bubble wrapper and the delegation strip parent `<div>`.
- `ThreadDetail.vue`: Add `min-w-0 overflow-hidden` to message content containers.

---

## Issue 2: "0 replies" on Active Delegation

**Root cause:** Reply count badge and "working…" state are two independent `v-if` conditions. When `replyCount` is explicitly `0` (not null/undefined), the `?? 1` fallback doesn't fire. The badge shows "0 replies" even while thread is running.

**Fix:** Use `d.done` as single source of truth:
- `!d.done` → always "working…"
- `d.done && replyCount < 1` → "completed"
- `d.done && replyCount >= 1` → "N reply/replies"

---

## Issue 3+6: Wildcard Validation Blocking Save (Root Cause of Permission Not Taking Effect)

**Root cause:** A `provider: "*"` wildcard entry in the agent toolbelt (legacy config) triggers a validation error. This error blocks the **entire save button** — including LOCAL ACCESS changes. So when the user grants filesystem access, the save silently fails due to an unrelated connections validation error.

**Fix — three parts:**
1. **Auto-clean on load:** Strip `provider: "*"` entries when agent editor loads. Show info banner: "Removed legacy wildcard connection. Save to persist."
2. **Decouple save validation:** Toolbelt validation only runs when toolbelt has actually changed. LOCAL ACCESS saves proceed regardless.
3. **Actionable error copy + Remove button:** Update wildcard error message to explain what it is and how to fix. Add inline "Remove" button.

---

## Issue 4: No Visibility into Agent Permission Denials

**Root cause:** Delegated thread tool dispatch in `spawn.go`'s `runOnce` method (the path that powers delegation strips) has no broadcast for permission denials. Errors go back to the agent as tool results, processed silently. Users only see a vague synthesis message minutes later.

**Fix — backend:**
- In `spawn.go` `runOnce`, after the `RequireApprovalToken` / permission check that returns an error string, broadcast a `thread_permission_denied` WS event: `{thread_id, agent_id, tool, session_id}`.
- Backend dedup: `map[string]bool` keyed by `toolName` per `runOnce` call — only broadcast the first denial per tool per thread.

**Fix — frontend:**
- Add `permissionDenials?: Array<{agentId, tool, threadId}>` to `ChatMessage` (follows existing `delegationErrors`/`delegationWarnings` pattern).
- Register WS handler for `thread_permission_denied` — find parent message by matching `delegatedThreads[].threadId`, attach denial.
- Render amber inline card below delegation strip:
  ```
  🔒 Elena needs bash access to continue   [Grant]
  ```
- "Grant" routes to agent editor (`/agents/{agentId}`).
- Frontend dedup: Set keyed on `threadId:tool`.
- After `d.done`: fade card to a subtle informational note (not alarming).

---

## Issue 5: Raw JSON in Thread Panel

**Root cause:** Memory tool calls (`muninn_recall`, `muninn_remember`, etc.) appear in every thread and render as raw JSON in the thread detail panel. These are internal infrastructure, not user-meaningful content.

**Fix:**
- Define `INTERNAL_TOOLS` Set in `ThreadDetail.vue` with all muninn tool names.
- Add `isInternal: boolean` to `ToolGroup` type — true when all calls in the group are internal.
- Render internal groups as: `🧠 Memory: checked context` (italic, muted, not a chip).
- `summarizeMemoryOp()` helper: "checked context" / "saved to memory" / "resumed session" based on tool name pattern.
- Non-internal tool groups render exactly as today.

---

## Implementation Tasks

### Task 1 — CSS Overflow Containment (Issue 1)
Files: `web/src/style.css`, `web/src/views/ChatView.vue`

### Task 2 — Fix "0 replies" Display (Issue 2)
Files: `web/src/views/ChatView.vue`

### Task 3 — Wildcard Auto-Clean + Save Decoupling (Issues 3+6)
Files: `web/src/views/agents/useAgentsViewState.ts`, `web/src/views/agents/useAgentCapabilityMatrix.ts`, `web/src/views/AgentsView.vue`

### Task 4 — Backend: thread_permission_denied WS Event (Issue 4)
Files: `internal/threadmgr/spawn.go`

### Task 5 — Frontend: Permission Denial Cards (Issue 4)
Files: `web/src/views/ChatView.vue`, `web/src/composables/useSessions.ts`

### Task 6 — Filter Internal Tool Results in Thread Panel (Issue 5)
Files: `web/src/components/ThreadDetail.vue`

### Task 7 — ThreadDetail Overflow + Card Polish (Issues 1+4 polish)
Files: `web/src/components/ThreadDetail.vue`, `web/src/views/ChatView.vue`
