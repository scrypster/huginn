# UX Polish — 6 Issues Design Spec

## Overview

Six targeted UX improvements identified from user testing screenshots. All are small, isolated fixes. No new subsystems, no breaking changes.

---

## Issue 1 — Chat Input Bottom Controls

### Problem
The `ChatToolbar` renders formatting buttons (Bold, Italic, Code, BulletList, OrderedList, Blockquote, CodeBlock), a keyboard hint, and the Send button all on one horizontal strip at the bottom of the editor. At small widths or with many buttons, this can feel cluttered and visually busy.

### Fix
Hide the text-formatting toolbar buttons behind a toggle. Replace the full toolbar with:
- A compact `Format` icon button (pilcrow or `¶` / pencil) on the left that toggles visibility of formatting buttons
- When collapsed (default): only the Format toggle + keyboard hint + Send button visible
- When expanded: all formatting buttons appear (same as today)
- Collapsed state persists via `localStorage` key `huginn_toolbar_expanded`

**Files to change:**
- `web/src/components/ChatEditor/ChatToolbar.vue` — add `expanded` ref, wrap formatting buttons in `v-show`, add toggle button
- No other files needed

---

## Issue 2 — Auto-Approve Countdown Visibility

### Problem
Delegation preview banners can auto-approve after a server-configured timeout (default 30s). Users don't know the approval will happen automatically.

### Current state (reviewed)
`ChatView.vue` already has:
- `previewNowMs` ref (line ~1145) — reactive `Date.now()`
- A `setInterval` ticker at 1000ms that updates `previewNowMs`
- `previewCountdownText(preview)` — already returns `"Auto-approves in Xs"` text
- `previewProgressStyle(preview)` — already uses `previewNowMs` for animated progress bar

**The countdown display is already implemented.** The only gap is:

### Fix
When the server fires the auto-approval and `delegation_preview_ack_result` arrives (handled in `onDelegationPreviewAckResult`), emit a brief transient notification in the chat: `"✓ Auto-approved — [agentName] took over"` — shown as a small status chip in the message area, auto-dismissed after 4s.

**Files to change:**
- `web/src/views/ChatView.vue` — add transient auto-approve notification in `onDelegationPreviewAckResult` handler; add `autoApproveNotices` ref (array of `{agentName, id}`) rendered as dismissible chips in the delegation preview area

---

## Issue 3 — "View Thread" Broken Click

### Problem
`openThreadDetailById(threadId)` in `ChatView.vue` (lines 2088–2099) fetches `thread.parentMessageId` from live state. If the thread isn't in live state (e.g. after page reload, or for older messages), `parentMessageId` is undefined and the function falls back to the global panel which does nothing meaningful.

### Fix
`GET /api/v1/sessions/{sessionId}/threads/{threadId}` already exists. Use it as the fallback:

1. In `openThreadDetailById(threadId)` in `ChatView.vue`: if `getThreadById(threadId)?.parentMessageId` is missing, fetch the thread from `GET /api/v1/sessions/{currentSessionId}/threads/{threadId}` to get `parentMessageId`, then call `threadDetail.open(parentMessageId, agentName)`.
2. While the async fetch resolves, show a brief loading state (disable the button with opacity).

**Files to change:**
- `web/src/views/ChatView.vue` — update `openThreadDetailById` to async-fetch thread if `parentMessageId` is missing from live state

---

## Issue 4 — Self-Delegation Messaging

### Problem
When an agent delegates to itself (same `fromAgent === toAgent`), the UI shows "Delegated to @elena" or activity rows labeled "Elena delegated to Elena" which is confusing.

### Fix
Detect self-delegation at the display layer. In `ChatView.vue`:
- In delegated thread activity rows (lines 560–565): if `d.agentId === msg.agent` (where `msg` is the parent assistant message containing `delegatedThreads`), replace "Delegated to @{agentId}" with "Handling directly"
- This handles multi-level delegation chains correctly by comparing against the parent message's agent, not a session-level agent

For `ThreadDetail.vue`: The handoff divider already suppresses when consecutive messages share the same agent (`item.msg.agent !== prevMsg.agent`). Self-delegation messages from the same agent won't trigger the divider — no change needed.

No backend changes needed; this is purely display logic.

**Files to change:**
- `web/src/views/ChatView.vue` — guard delegation label: `d.agentId === msg.agent ? "Handling directly" : "Delegated to @" + d.agentId`

---

## Issue 5 — Thinking Indicator in Chat Area

### Problem
When a delegated agent is working, only the sidebar shows a glowing dot. The main chat message list shows no indicator that anything is happening — especially confusing when the delegated agent hasn't yet produced any output.

### Current state
`ChatView.vue` lines 517–532 show tool-call bouncing dots and lines 533–539 show a follow-up thinking indicator. These are attached to a specific message (`msg.streaming`). Before the first token arrives, there's no message to attach them to.

### Fix
Add a "delegated agent thinking" row to the message list:
- `getSessionThreads(sessionId)` already exists and returns `LiveThread[]`
- `isRunning(thread)` already checks non-terminal statuses
- Show indicator when: `isRunning(thread) && thread.streamingContent === ''`
- Only show for threads that have a `parentMessageId` matching a chat message (i.e., delegated threads triggered from a message, not top-level threads)
- Placement: immediately after the delegation activity row for that thread, not floating at the bottom
- Content: small avatar dot (agent color from registry) + agent name + "thinking" with 3 bouncing dots matching existing style (classes: `animate-bounce`, staggered `delay-75`/`delay-150`)
- Dismiss automatically when `streamingContent` becomes non-empty (Vue reactivity handles this)

**Files to change:**
- `web/src/views/ChatView.vue` — in the delegation activity row rendering loop, add a thinking indicator row after each `d` entry when its corresponding thread `isRunning && streamingContent === ''`; use `getThreadById(d.threadId)` to get live state

---

## Issue 6 — Agent Permission Hot-Reload

### Problem
When a user changes an agent's model, permissions, or tools via the UI, the server must be restarted for changes to take effect. This creates a disruptive workflow.

### Current state
- `applyToolbelt()` in `internal/agent/config.go` is called per-request, so capability checks are always current
- `AgentDef` struct is loaded once at startup via `LoadAgentsFromBase()` and stored in-memory
- `UpdateModels()` and `UpdateFallbackAPIKey()` already support hot-reload for API keys and model names
- `SaveAgent()` writes atomically to `~/.huginn/agents/{name}.yaml`

### Fix
Wire a file-system watcher to the agents directory. When a `.yaml` or `.json` file changes:
1. Call `LoadAgentsFromBase(agentsDir)` to re-read all agent configs
2. Call `BuildRegistryWithUsername()` to rebuild the registry
3. Call `SetAgentRegistry()` on the Orchestrator to swap in the new registry
4. Existing in-flight requests complete normally (they hold a reference to the old registry object)

Implementation approach: Use the existing polling-based `WorkflowsWatcher` pattern from `internal/scheduler/workflows_watcher.go`. Create `internal/agent/agents_watcher.go` that polls `~/.huginn/agents/` every 2s, calls the reload callback on hash change.

Wire the watcher in `main.go` / server startup, passing the Orchestrator's `SetAgentRegistry` as the callback.

**Files to change:**
- `internal/agent/agents_watcher.go` — new file, polling watcher for agents directory
- `main.go` — start agents watcher after server init

---

## Out of Scope

- Persisting toolbar state server-side (localStorage is sufficient)
- WebSocket push for agent config changes to connected clients (not needed — changes apply on next request)
- Changing the auto-approve timeout default (server-side config, separate concern)
