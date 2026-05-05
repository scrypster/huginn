# World-Class Thread Orchestration UX — Design Spec

**Date:** 2026-05-01  
**Status:** In Progress  
**Area:** Channels / DM delegation workflow

---

## Problem

The control plane is now reliably tool-first (`delegate_to_agent`), but the user-facing thread experience still needs stronger human-oriented feedback in three critical moments:

1. While work is active, users need to understand _who_ is doing what without parsing a noisy mixed stream.
2. When users interject guidance, they need explicit delivery receipts so they trust that guidance reached the right delegates.
3. When a thread is resolved but revisited later, users need clear reopen semantics that feel Slack-like instead of ambiguous dead-end input.

---

## Goals

- Make delegated work easy to scan as a "team conversation" while keeping timeline fidelity.
- Provide explicit interjection delivery metadata (`delivered_to_agent`, `shared_with_active`) in UI feedback.
- Formalize lifecycle language in ThreadDetail with clear states:
  - `active`
  - `needs_input`
  - `resolved`
  - `expired` (stale resolved thread)
- Support reopen flow that naturally pivots users back into main chat with thread context.

---

## Scope

**In scope (this tranche):**
- ThreadDetail view toggle: merged timeline vs. agent lanes
- Lifecycle badge in ThreadDetail with `Active`, `Needs Input`, `Resolved`, `Expired`
- Delivery receipts for thread interjections (backend ACK payload + frontend rendering)
- Reopen CTA for expired/resolved threads that prefills follow-up prompt in main composer
- Regression tests for new ThreadDetail behavior and threadmgr receipt metadata

**Out of scope (next tranche):**
- Explicit main-chat "update active work" vs "new request" routing control
- Thread interjection target modes (lead-only / selected delegate / broadcast-all UI controls)
- Per-thread inactivity policy and server-side expiry persistence

---

## Architecture

### 1) Agent-Lane Rendering (Frontend)

`ThreadDetail` keeps timeline mode as source-of-truth rendering and adds a second "Agent lanes" mode that groups non-tool messages by author (`You`, `@Agent`) for rapid human scanning.

Tool-call detail remains available in timeline mode to preserve high-fidelity execution debugging.

### 2) Thread Lifecycle State (Frontend)

Thread state is derived from `threadStatus` + recency of thread messages:

- `blocked` => `Needs Input`
- terminal (`done`, `completed`, `completed-with-timeout`, `error`, `cancelled`) =>
  - `Resolved` when recently active
  - `Expired` when stale beyond expiry threshold (48h in UI)
- other statuses => `Active`

Lifecycle drives:
- top badge copy/color
- read-only hint copy
- follow-up vs reopen CTA text

### 3) Interjection Delivery Receipt (Backend + Frontend)

`threadmgr.InjectReceipt(threadID)` computes:
- target delegate (`delivered_to_agent`)
- active sibling delegate count (`shared_with_active`)

`ws` `thread_inject_ack` now carries:

```json
{
  "thread_id": "<id>",
  "delivered_to_agent": "<agent>",
  "shared_with_active": 2
}
```

`ThreadDetail.onInjectAck()` renders explicit success copy, e.g.:
- "Delivered to @atlas."
- "Delivered to @atlas; shared with 2 active delegates."

### 4) Reopen Flow (Frontend)

When a terminal thread is resolved/expired, CTA emits `start-follow-up` with a prefilled draft prompt.
`ChatView` closes detail and injects draft text into `ChatEditor`, preserving the Slack-like "continue in main channel" loop.

---

## File Changes

| File | Change |
|------|--------|
| `internal/threadmgr/manager.go` | Add `InjectReceipt()` helper for interjection receipt metadata |
| `internal/threadmgr/try_send_input_test.go` | Add receipt coverage for target + active sibling count |
| `internal/server/ws.go` | Extend `thread_inject_ack` payload with delivery metadata |
| `web/src/components/ThreadDetail.vue` | Add lane mode, lifecycle badge/state mapping, receipt copy, reopen semantics |
| `web/src/components/__tests__/ThreadDetail.test.ts` | Add tests for lane mode, receipt rendering, expired/reopen behavior |
| `web/src/views/ChatView.vue` | Forward ack payload to detail; consume follow-up draft into main composer |
| `web/src/components/ChatEditor/useEditor.ts` | Add `setText()` helper |
| `web/src/components/ChatEditor/ChatEditor.vue` | Expose `setText()` to parent views |

---

## Validation Plan

- Go:
  - `go test ./internal/threadmgr ./internal/server`
- Web:
  - `pnpm --dir web vitest run src/components/__tests__/ThreadDetail.test.ts`
  - `pnpm --dir web vitest run src/views/__tests__/ChatView.test.ts`

---

## Success Criteria

- Users can switch between timeline and agent lanes in thread detail.
- Users see immediate, explicit confirmation of where thread guidance was delivered.
- Terminal threads clearly communicate resolved vs expired state.
- Reopening thread work preps the main-chat composer with contextual follow-up text.
- Regression suites pass for touched backend and frontend paths.
