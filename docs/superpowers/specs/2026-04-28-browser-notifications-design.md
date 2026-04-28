# Browser Notifications — Design Spec

**Date:** 2026-04-28
**Status:** Draft
**Phase:** 3 — Desktop UX Polish

---

## Problem

Huginn agents send DMs and heartbeat messages, but users only see them if they have the browser tab open and are watching the chat. When the tab is in the background or the user is in a different app, there's no signal that something happened. The result: users miss agent responses, workflow failures, and heartbeat check-ins.

The Web Notifications API solves this for desktop browsers — no infrastructure, no mobile push, no service worker required.

---

## Scope

**In scope:**
- `useBrowserNotifications` composable: permission management + `notify()` function
- Trigger browser notifications on: new agent `done` (session complete), `notification_new` (workflow notification), `agent_follow_up` (agent follow-up final reply persisted)
- Only fire when `document.hidden` is true (tab not active)
- User preference: enable/disable toggle in SettingsView
- Permission request: triggered by the enable toggle (not on app load)
- Notification click: focuses the tab and navigates to the relevant session/space

**Out of scope:**
- Sound/vibration (browser support inconsistent; too intrusive for a first pass)
- Service worker / persistent notifications (requires HTTPS + SW registration)
- Per-agent notification filters (Phase 4)
- Mobile push (FCM/APNS — HuginnCloud scope)

---

## Architecture

### 1. `useBrowserNotifications` Composable

New file: `web/src/composables/useBrowserNotifications.ts`

This composable follows the **module-level singleton pattern** used by `useNotifications.ts` and `useAgents.ts`. The `enabled` and `permission` refs are declared at module scope (outside the function body) so all callers share the same state. Multiple `useBrowserNotifications()` calls return the same reactive refs.

**State (module-level, outside function body):**
- `enabled: Ref<boolean>` — persisted in `localStorage` as `huginn:notifications:enabled`
- `permission: Ref<NotificationPermission>` — `'default' | 'granted' | 'denied'`, initialized from `Notification.permission` if the API exists

**Functions:**
- `requestPermission()`: calls `Notification.requestPermission()`, updates `permission` ref, sets `enabled = true` on grant
- `notify(title, body, tag, onClick)`: fires `new Notification(title, { body, tag, icon: '/favicon.ico' })` if `enabled && permission === 'granted' && document.hidden`
- `isSupported`: computed — `typeof Notification !== 'undefined'`

```ts
// Module-level singletons — shared across all callers (same pattern as useNotifications.ts)
const _isSupported = typeof Notification !== 'undefined'
const enabled = ref(localStorage.getItem('huginn:notifications:enabled') === 'true')
const permission = ref<NotificationPermission>(_isSupported ? Notification.permission : 'denied')

export function useBrowserNotifications() {
  async function requestPermission() {
    if (!_isSupported) return
    const result = await Notification.requestPermission()
    permission.value = result
    if (result === 'granted') enabled.value = true
    localStorage.setItem('huginn:notifications:enabled', String(result === 'granted'))
  }

  function notify(title: string, body: string, tag: string, onClick?: () => void) {
    if (!_isSupported || !enabled.value || permission.value !== 'granted') return
    if (!document.hidden) return   // tab is active — in-app UI is sufficient
    const n = new Notification(title, { body, tag, icon: '/favicon.ico' })
    if (onClick) n.onclick = () => { window.focus(); onClick() }
  }

  function toggle(value: boolean) {
    if (value && permission.value !== 'granted') {
      requestPermission()
    } else {
      enabled.value = value
      localStorage.setItem('huginn:notifications:enabled', String(value))
    }
  }

  return { isSupported: _isSupported, enabled, permission, requestPermission, notify, toggle }
}
```

---

### 2. WS Event Triggers

Wire notifications into the existing WS handlers. Three trigger points:

#### a. `done` event (agent finishes responding)

In the `done` WS handler in `ChatView.vue` (around line 1445), after marking streaming as complete. The `done` handler already uses `props.sessionId` and `getMessages(props.sessionId)` — use these same references, not `messages.value` or a bare `sessionId` variable:

```ts
// At top of ChatView.vue setup (outside registerWS):
const { notify } = useBrowserNotifications()

// Inside the 'done' registerWS handler, after flushPendingToolResults:
if (props.sessionId) {
  const msgs = getMessages(props.sessionId)
  const last = msgs.at(-1)
  const agentName = last?.agent ?? 'Agent'
  const preview = last?.content?.slice(0, 80) ?? ''
  notify(
    agentName,
    preview || 'Finished responding',
    `session-done-${props.sessionId}`,
    () => router.push(`/chat/${props.sessionId}`)
  )
}
```

Use `tag: \`session-done-${props.sessionId}\`` — the browser deduplicates notifications with the same tag, so rapid `done` events in one session produce only one notification.

#### b. `notification_new` event (workflow notification)

In `useNotifications.ts`, in the `wireWS` function. The existing `notification_new` handler receives `(payload: { notification: Notification })` — access fields via `payload.notification`:

```ts
const { notify } = useBrowserNotifications()

// In wireWS, add to the notification_new handler:
ws.on?.('notification_new', (payload: { notification: Notification }) => {
  notifications.value.unshift(payload.notification)
  // Browser notification — fires only when tab is hidden
  notify(
    payload.notification.summary,
    payload.notification.detail ?? '',
    `notif-${payload.notification.id}`,
    () => router.push('/logs')
  )
})
```

#### c. `agent_follow_up` event (agent follow-up final reply)

In `ChatView.vue`'s `agent_follow_up` handler (`registerWS(ws, 'agent_follow_up', ...)` around line 1685):

```ts
// At the end of the agent_follow_up handler.
// The handler already extracts: const agentName = p?.agent as string | undefined
// Use that existing local variable rather than msg.agent (WSMessage has no agent property):
notify(
  agentName ?? 'Agent',
  'Has a follow-up for you',
  `follow-up-${props.sessionId}`,
  () => router.push(`/chat/${props.sessionId}`)
)
```

---

### 3. Settings Toggle

In `SettingsView.vue`, add a "Notifications" section:

```html
<div class="space-y-3">
  <p class="text-xs font-semibold text-[var(--color-text-muted)] uppercase tracking-widest">Notifications</p>

  <div v-if="!notif.isSupported" class="text-xs text-[var(--color-text-muted)]">
    Browser notifications are not supported in this browser.
  </div>

  <template v-else>
    <div class="flex items-center justify-between">
      <div>
        <p class="text-xs text-[var(--color-text)]">Desktop notifications</p>
        <p class="text-[11px] text-[var(--color-text-muted)] mt-0.5">
          Get notified when agents respond or workflows complete, even when this tab is in the background.
        </p>
      </div>
      <button
        @click="notif.toggle(!notif.enabled.value)"
        class="relative inline-flex h-5 w-9 rounded-full transition-colors"
        :class="notif.enabled.value ? 'bg-[var(--color-accent)]' : 'bg-[var(--color-border)]'"
      >
        <span class="sr-only">Toggle notifications</span>
        <span class="inline-block w-4 h-4 rounded-full bg-white shadow transform transition-transform mt-0.5"
          :class="notif.enabled.value ? 'translate-x-4' : 'translate-x-0.5'" />
      </button>
    </div>

    <p v-if="notif.permission.value === 'denied'" class="text-[11px] text-amber-400">
      Notifications are blocked in browser settings. To enable, update your browser's site permissions for this page.
    </p>
  </template>
</div>
```

`notif` is `useBrowserNotifications()` called in `SettingsView.vue` setup.

**Toggle behavior:**
- Off → On: if `permission === 'default'`, calls `requestPermission()` (triggers browser prompt). If `permission === 'granted'`, just sets `enabled = true`. If `permission === 'denied'`, shows the blocked message (can't programmatically prompt again).
- On → Off: sets `enabled = false`, persists to localStorage. Does not revoke the browser permission (can't do this programmatically).

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useBrowserNotifications.ts` | New composable: module-level singleton state, `notify()`, `toggle()` |
| `web/src/views/ChatView.vue` | Wire `notify()` in `done` and `agent_follow_up` handlers (using `props.sessionId` + `getMessages`) |
| `web/src/composables/useNotifications.ts` | Wire `notify()` in `notification_new` handler (using `payload.notification`) |
| `web/src/views/SettingsView.vue` | Add Notifications section with toggle |

No backend changes. No DB migrations.

---

## Tests

**Frontend (Vitest):**
- `notify does nothing when document.hidden is false`
- `notify does nothing when enabled is false`
- `notify does nothing when permission is not granted`
- `notify does nothing when Notification API is unsupported`
- `notify fires Notification when enabled, granted, and tab hidden`
- `notify deduplicates via tag (same tag replaces previous)`
- `toggle calls requestPermission when permission is default`
- `toggle sets enabled=false without calling requestPermission`
- `enabled persists to localStorage`
- `permission initialized from Notification.permission on construction`
- `enabled and permission are module-level singletons (two useBrowserNotifications() calls share the same ref)`

---

## Success Criteria

- A "Desktop notifications" toggle appears in Settings
- Toggling on triggers the browser permission prompt (if not yet granted)
- After granting, agent responses received while the tab is hidden fire a desktop notification
- Notification title is the agent's name; body is the first 80 chars of the response
- Clicking the notification focuses the tab and navigates to the relevant session
- Notifications with the same tag (same session) coalesce — no rapid-fire spam
- Toggling off stops notifications immediately; state persists across reload
- If the browser blocks notifications, a clear message explains how to re-enable
- When the tab is active, no notification fires (in-app UI is sufficient)
- `npm run build` clean; no TypeScript errors
