# DM Experience Polish — Design Spec

**Date:** 2026-04-28
**Status:** Draft
**Phase:** 3 — Desktop UX Polish

**Prerequisites:** Requires Phase 2 (PR #63 — cross-channel memory replication + MemoryView) to be merged to `develop` before implementation. Phase 2 provides `POST /api/v1/muninn/tool`, the `allowedMuninnTools` whitelist in `handlers_memory.go`, and the MuninnDB proxy infrastructure this spec extends.

---

## Problem

DMs are the primary human-agent communication surface in Huginn, but they feel like a raw chat log rather than a conversation. Three specific gaps:

1. No feedback between "user sends message" and "first token arrives" — the UI is silent for 1-3 seconds.
2. No signal that the agent has seen/started processing the user's message.
3. No quick actions on messages — copying content, retrying a failed send, or manually saving something to the agent's memory all require workarounds.
4. User messages show no timestamp; only assistant messages show one. Asymmetric and confusing.

---

## Scope

**In scope:**
- Thinking bubble: animated `...` shown while waiting for first agent token
- Seen indicator: subtle "Seen" line below user's last message when agent starts responding
- User message timestamps: show `createdAt` on hover (consistent with assistant messages)
- Quick actions bar: copy, retry, save-to-memory — appears on message hover
- Save to Memory: user-initiated `muninn_remember` via the existing MuninnDB proxy; hidden when agent already saved memory during that turn

**Out of scope:**
- Pin messages (dropped in favor of Save to Memory — see design notes)
- Typing indicators for user → agent (no WS infrastructure; AI context makes this low value)
- Cross-device read sync (local-only)
- Mobile/responsive changes (Phase 3 is desktop-only)

---

## Architecture

### 1. Thinking Bubble (frontend only)

**State variable:** Add `agentThinking: boolean` to session state in `useSessions.ts`.

Set `agentThinking = true` when the user submits a message. Set it `false` when:
- The first `token` WS event arrives for this run, OR
- A `status` WS event arrives (server provides its own placeholder), OR
- A `done` event arrives (handles tool-only responses that emit no tokens), OR
- An `error` WS event arrives

**Template:** In `ChatView.vue`, render a thinking bubble below the last user message when `agentThinking && !isStreaming`:

```html
<div v-if="agentThinking" class="flex items-end gap-2 px-4 py-2">
  <div class="thinking-bubble">
    <span class="dot" /><span class="dot" /><span class="dot" />
  </div>
</div>
```

Three bouncing dots, same color as the agent's palette color. CSS `@keyframes` bounce with staggered delays.

No backend changes.

---

### 2. Seen Indicator (frontend only)

**State:** Track `lastSeenMessageId: string | null` per session. When the first `token` event arrives for a new run, set `lastSeenMessageId` to the ID of the last user message.

**Template:** Render a "Seen" line directly below the user message with `id === lastSeenMessageId`:

```html
<p class="text-[10px] text-[var(--color-text-muted)] text-right pr-2 -mt-1">
  Seen · {{ agentName }}
</p>
```

Subtle, right-aligned, sits just below the user bubble. Clears when the next user message is sent.

No backend changes.

---

### 3. User Message Timestamps (frontend only)

Currently `AgentMessageHeader.vue` shows timestamps for assistant messages only. User messages are rendered with no timestamp.

**Change:** Render a timestamp on user messages, visible on hover. In `ChatView.vue`'s user message template, wrap the timestamp in a `group` Tailwind class on the outer container and show it with `opacity-0 group-hover:opacity-100 transition-opacity`:

```html
<span class="text-[10px] text-[var(--color-text-muted)] opacity-0 group-hover:opacity-100 transition-opacity">
  {{ formatTime(msg.createdAt) }}
</span>
```

Use the same `formatTime` helper already used in `AgentMessageHeader.vue` (relative: "3m ago", absolute on hover tooltip).

No backend changes.

---

### 4. Quick Actions Bar (frontend only)

Show a compact action bar on message hover. Appears absolutely positioned to the top-right of the message bubble. Actions vary by message role:

| Action | User messages | Assistant messages |
|--------|:---:|:---:|
| Copy   | ✓  | ✓  |
| Retry  | ✓  | — |
| Save to Memory | — | ✓ (conditional) |

**Copy:** `navigator.clipboard.writeText(msg.content)`. Show a brief "Copied!" tooltip (1.5s).

**Retry:** Re-sends the message content as a new user message in the same session. Calls the existing `sendMessage()` composable with `msg.content`. Appends a fresh exchange — does not delete the original message or its response.

**Save to Memory:** Calls `POST /api/v1/muninn/tool` (existing Phase 2 endpoint) with `muninn_remember`, using the message content as the memory body and the agent's configured `vault_name`. Only shown on assistant messages.

**Visibility rules for "Save to Memory":**
1. The agent must have a `vault_name` configured (no vault → button hidden entirely)
2. The agent must NOT have already called a memory tool (`muninn_remember`, `muninn_decide`, `muninn_evolve`) during this turn — checked by inspecting `msg.toolCalls`. The `ToolCallRecord` interface uses `name` (not `tool`): check `msg.toolCalls?.some(tc => ['muninn_remember','muninn_decide','muninn_evolve'].includes(tc.name))`. If true, the agent already saved to memory; show "Saved ✓" (static, non-clickable) instead.

This prevents duplicate memories and respects the agent's own judgment — if the agent decided something was worth remembering, we surface that to the user rather than offering to do it redundantly.

**Request body for save:**
```json
{
  "vault": "<agent.vault_name>",
  "tool": "muninn_remember",
  "args": {
    "concept": "<first ~60 chars of msg.content, trimmed>",
    "content": "<msg.content>"
  }
}
```

On success: button changes to "Saved ✓" (1 session lifetime — resets on page reload since we don't persist this state client-side). This is intentional: the persistent record lives in MuninnDB, not in the UI. Reloading the page and seeing a "Save to Memory" button again is correct — the memory is safe in the vault regardless. On error: show "Failed" tooltip.

**Backend change:** Add `"muninn_remember"` to the `allowedMuninnTools` map in `internal/server/handlers_memory.go`. The existing comment says "no autonomous write tools" — update the comment to clarify the distinction: tools in this map are user-initiated (via UI), not agent-autonomous. `muninn_remember` is safe to add here for explicit user actions.

```go
var allowedMuninnTools = map[string]bool{
    "muninn_recall":   true,
    "muninn_read":     true,
    "muninn_forget":   true,
    "muninn_remember": true, // user-initiated only; not called autonomously by agents via this endpoint
}
```

**Implementation:** New `MessageActions.vue` component. Receives `msg` prop (includes `toolCalls`) and `agentVaultName` prop. Emits `@retry` and `@save-memory`. Absolutely positioned with `opacity-0 group-hover:opacity-100` on the parent message container.

---

### Design Notes

**Why Save to Memory instead of Pin:**
Pin was dropped in favour of Save to Memory. Pin is a user-side bookmark that agents never see. Save to Memory directly feeds the agent's recall — more aligned with Huginn's memory-first identity. It also reuses the `POST /api/v1/muninn/tool` proxy built in Phase 2, requiring no new backend infrastructure beyond a one-line whitelist change.

**Why not fan out via MemoryReplicator:**
User-initiated saves via this button do NOT trigger cross-channel replication. The `MemoryReplicator` is wired into agent tool calls during sessions, not into the HTTP proxy endpoint. Replication fan-out for user-initiated saves is a potential Phase 4 enhancement.

---

## File Changes

| File | Change |
|------|--------|
| `internal/server/handlers_memory.go` | Add `"muninn_remember": true` to `allowedMuninnTools`; update comment |
| `web/src/composables/useSessions.ts` | Add `agentThinking`, `lastSeenMessageId` state; add `saveToMemory()` composable function |
| `web/src/views/ChatView.vue` | Thinking bubble, seen indicator, user timestamps on hover, pass `agentVaultName` to `MessageActions` |
| `web/src/components/MessageActions.vue` | New component: hover action bar (copy, retry, save-to-memory) |

No DB migrations. No new API routes.

---

## Tests

**Backend:**
- `TestHandleMuninnTool_AllowsMuninnRemember` — verify `muninn_remember` is now in the allowed tools list (existing test infrastructure in `handlers_memory_test.go`)

**Frontend (Vitest):**
- `agentThinking is true after sendMessage, false on first token`
- `agentThinking resets on done event (tool-only responses)`
- `lastSeenMessageId set on first token event`
- `MessageActions hides save-to-memory when agent has no vault`
- `MessageActions shows "Saved ✓" when toolCalls contains muninn_remember`
- `MessageActions shows save button when toolCalls has no memory tools`

---

## Success Criteria

- Sending a message shows the thinking bubble within 50ms; it disappears when the first token, status, done, or error event arrives
- "Seen · AgentName" appears below the user's last message as soon as streaming starts
- Hovering a user message reveals its timestamp
- Hovering any message reveals the action bar; copy works; retry re-sends
- On an assistant message with a configured vault and no memory tool calls: "Save to Memory" button is visible and saves successfully
- On an assistant message where the agent already called `muninn_remember`/`muninn_decide`/`muninn_evolve`: "Saved ✓" indicator shown, button absent
- On an agent with no vault configured: save button absent entirely
- All new tests pass; existing session and muninn tool tests still pass
- `go build ./...` and `npm run build` clean
