# Browser Notifications — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fire desktop notifications when agents respond or workflows complete while the browser tab is in the background.

**Architecture:** A module-level singleton composable (`useBrowserNotifications.ts`) holds permission and enabled state (same pattern as `useNotifications.ts`). Three WS event handlers wire into it: `done` in `ChatView.vue`, `notification_new` in `useNotifications.ts`, and `agent_follow_up` in `ChatView.vue`. A new "Notifications" tab is added to `SettingsView.vue` using the existing `ToggleRow` component.

**Tech Stack:** Vue 3 Composition API, Web Notifications API, Vitest, localStorage.

---

## File Changes

| File | Change |
|------|--------|
| `web/src/composables/useBrowserNotifications.ts` | New: module-level singleton, `notify()`, `toggle()`, `requestPermission()` |
| `web/src/composables/useNotifications.ts` | Wire `notify()` in `notification_new` handler |
| `web/src/views/ChatView.vue` | Wire `notify()` in `done` and `agent_follow_up` handlers |
| `web/src/views/SettingsView.vue` | Add "Notifications" tab with toggle |

---

### Task 1: `useBrowserNotifications` Composable

**Files:**
- Create: `web/src/composables/useBrowserNotifications.ts`
- Create: `web/src/composables/__tests__/useBrowserNotifications.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `web/src/composables/__tests__/useBrowserNotifications.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

// We need to reset modules between tests to get fresh singleton state.
async function fresh() {
  vi.resetModules()
  const mod = await import('../useBrowserNotifications')
  return mod.useBrowserNotifications
}

describe('useBrowserNotifications', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('isSupported is false when Notification API is absent', async () => {
    const origNotif = (globalThis as any).Notification
    delete (globalThis as any).Notification
    const useBrowserNotifications = await fresh()
    const { isSupported } = useBrowserNotifications()
    expect(isSupported).toBe(false)
    ;(globalThis as any).Notification = origNotif
  })

  it('notify does nothing when document.hidden is false', async () => {
    Object.defineProperty(document, 'hidden', { value: false, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'granted' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'granted'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify does nothing when enabled is false', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'granted' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = false
    permission.value = 'granted'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify does nothing when permission is not granted', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'default' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'default'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify fires Notification when enabled, granted, and tab hidden', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const instances: any[] = []
    function MockNotification(title: string, opts: any) { instances.push({ title, opts }) }
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'granted'
    notify('Agent', 'Hello', 'session-done-123')
    expect(instances).toHaveLength(1)
    expect(instances[0]!.title).toBe('Agent')
    expect(instances[0]!.opts.tag).toBe('session-done-123')
  })

  it('enabled and permission are module-level singletons', async () => {
    const MockNotification = vi.fn()
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const a = useBrowserNotifications()
    const b = useBrowserNotifications()
    a.enabled.value = true
    expect(b.enabled.value).toBe(true) // same ref
  })

  it('enabled persists to localStorage', async () => {
    const MockNotification = vi.fn()
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle } = useBrowserNotifications()
    toggle(false)
    expect(localStorage.getItem('huginn:notifications:enabled')).toBe('false')
  })

  it('toggle calls requestPermission when permission is default', async () => {
    const requestPermission = vi.fn().mockResolvedValue('granted')
    function MockNotification() {}
    MockNotification.permission = 'default'
    MockNotification.requestPermission = requestPermission
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle, permission } = useBrowserNotifications()
    permission.value = 'default'
    toggle(true)
    expect(requestPermission).toHaveBeenCalledOnce()
  })

  it('toggle sets enabled=false without calling requestPermission', async () => {
    const requestPermission = vi.fn()
    function MockNotification() {}
    MockNotification.permission = 'granted'
    MockNotification.requestPermission = requestPermission
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle, enabled, permission } = useBrowserNotifications()
    permission.value = 'granted'
    enabled.value = true
    toggle(false)
    expect(enabled.value).toBe(false)
    expect(requestPermission).not.toHaveBeenCalled()
  })

  it('permission initialized from Notification.permission on construction', async () => {
    function MockNotification() {}
    MockNotification.permission = 'denied'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { permission } = useBrowserNotifications()
    expect(permission.value).toBe('denied')
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd web && npx vitest run src/composables/__tests__/useBrowserNotifications.test.ts 2>&1 | tail -15
```
Expected: FAIL — module not found.

- [ ] **Step 3: Create the composable**

Create `web/src/composables/useBrowserNotifications.ts`:

```ts
import { ref } from 'vue'

// Module-level singletons — shared across all callers (same pattern as useNotifications.ts).
// Declared outside the function so every useBrowserNotifications() call returns the same refs.
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
    if (!document.hidden) return // tab is active — in-app UI is sufficient
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

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd web && npx vitest run src/composables/__tests__/useBrowserNotifications.test.ts
```
Expected: all 9 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useBrowserNotifications.ts web/src/composables/__tests__/useBrowserNotifications.test.ts
git commit -m "feat(ui): add useBrowserNotifications singleton composable"
```

---

### Task 2: Wire into `useNotifications.ts`

**Files:**
- Modify: `web/src/composables/useNotifications.ts`

The existing `notification_new` handler at line 90:
```ts
ws.on?.('notification_new', (payload: { notification: Notification }) => {
  notifications.value.unshift(payload.notification)
})
```

- [ ] **Step 1: Write the failing test**

In `web/src/composables/__tests__/useNotifications.test.ts`, append:

```ts
it('notification_new fires browser notification when tab hidden', async () => {
  Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
  const instances: any[] = []
  function MockNotif(title: string, opts: any) { instances.push({ title, opts }) }
  MockNotif.permission = 'granted'
  ;(globalThis as any).Notification = MockNotif
  vi.resetModules()
  const { setToken } = await import('../useApi')
  setToken('test-token')
  const { useBrowserNotifications } = await import('../useBrowserNotifications')
  const notif = useBrowserNotifications()
  notif.enabled.value = true
  notif.permission.value = 'granted'
  const { useNotifications } = await import('../useNotifications')
  const { wireWS } = useNotifications()
  const handlers: Record<string, Function> = {}
  wireWS({ on: (ev: string, cb: Function) => { handlers[ev] = cb } } as any)
  handlers['notification_new']!({ notification: { id: 'n1', summary: 'Alert', detail: 'msg' } })
  expect(instances.some(i => i.title === 'Alert')).toBe(true)
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run src/composables/__tests__/useNotifications.test.ts -t "fires browser notification" 2>&1 | tail -10
```
Expected: FAIL.

- [ ] **Step 3: Add the import and wire notify**

In `web/src/composables/useNotifications.ts`, add the import at the top (after existing imports):

```ts
import { useBrowserNotifications } from './useBrowserNotifications'
```

Then update the `notification_new` handler (line 90) to:

```ts
ws.on?.('notification_new', (payload: { notification: Notification }) => {
  notifications.value.unshift(payload.notification)
  const { notify } = useBrowserNotifications()
  notify(
    payload.notification.summary,
    payload.notification.detail ?? '',
    `notif-${payload.notification.id}`,
    () => router?.push('/logs')
  )
})
```

Note: `router` is not available in this composable. Pass it as a parameter or use `window.location` as fallback. The simplest approach: pass `onClick` only when router is available. Since `useNotifications` doesn't have a router, use `undefined` for `onClick` and the notification still fires — navigation on click is a nice-to-have, not required for the feature to work. Update to:

```ts
ws.on?.('notification_new', (payload: { notification: Notification }) => {
  notifications.value.unshift(payload.notification)
  const { notify } = useBrowserNotifications()
  notify(
    payload.notification.summary,
    payload.notification.detail ?? '',
    `notif-${payload.notification.id}`
  )
})
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run src/composables/__tests__/useNotifications.test.ts
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useNotifications.ts
git commit -m "feat(ui): wire browser notification on notification_new WS event"
```

---

### Task 3: Wire into `ChatView.vue`

**Files:**
- Modify: `web/src/views/ChatView.vue`

Two locations: the `done` handler (line ~1445) and the `agent_follow_up` handler (line ~1685).

- [ ] **Step 1: Add import**

In `ChatView.vue`, after the existing composable imports (~line 828), add:

```ts
import { useBrowserNotifications } from '../composables/useBrowserNotifications'
```

- [ ] **Step 2: Destructure notify in setup**

After the `useBrowserNotifications` import is used, destructure `notify` once in the component setup (before the `watch(wsRef, ...)` block):

```ts
const { notify } = useBrowserNotifications()
```

- [ ] **Step 3: Wire into `done` handler**

In the `done` handler (line ~1487, after `fetchStatus()`), append before the closing `}`):

```ts
    // Browser notification — only fires when tab is hidden (checked inside notify())
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

- [ ] **Step 4: Wire into `agent_follow_up` handler**

In the `agent_follow_up` handler (line ~1715, after `scrollToBottom()`), append before the closing `}`):

```ts
    // agentName is already extracted at the top of this handler as:
    // const agentName = p?.agent as string | undefined
    notify(
      agentName ?? 'Agent',
      'Has a follow-up for you',
      `follow-up-${props.sessionId}`,
      () => router.push(`/chat/${props.sessionId}`)
    )
```

- [ ] **Step 5: Build check**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: no TypeScript errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "feat(ui): wire browser notification on done and agent_follow_up"
```

---

### Task 4: Settings Toggle

**Files:**
- Modify: `web/src/views/SettingsView.vue`

The settings view uses tabs. Current tabs array (line ~420): `general, tools, webui, integrations, mcp, about`. Add a `notifications` tab.

- [ ] **Step 1: Add the tab to the tabs array**

In `SettingsView.vue`, find the `tabs` array (line ~420) and add a notifications entry. First add an SVG icon constant alongside the existing icons (find where `const IconGeneral = ...` or similar is defined, then add):

```ts
const IconNotifications = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>`
```

Then in the `tabs` array, add before the `about` entry:

```ts
{ id: 'notifications', label: 'Notifications', icon: IconNotifications },
```

- [ ] **Step 2: Add the import**

In `SettingsView.vue` script setup, add the import:

```ts
import { useBrowserNotifications } from '../composables/useBrowserNotifications'
```

- [ ] **Step 3: Destructure in setup**

After the existing composable setup in the script, add:

```ts
const notif = useBrowserNotifications()
```

- [ ] **Step 4: Add the notifications tab panel**

In the template, after the `<div v-if="activeTab === 'mcp'"` section and before the `<div v-if="activeTab === 'about'"` section, add:

```html
<!-- ── Notifications ───────────────────────────────────────────── -->
<div v-if="activeTab === 'notifications'" class="space-y-6">
  <p class="text-xs text-huginn-muted">Control when Huginn sends desktop notifications to your operating system.</p>

  <section class="space-y-3">
    <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Desktop Notifications</h3>

    <div v-if="!notif.isSupported" class="text-xs text-huginn-muted px-1">
      Browser notifications are not supported in this browser.
    </div>

    <template v-else>
      <ToggleRow
        :modelValue="notif.enabled.value"
        label="Desktop notifications"
        hint="Get notified when agents respond or workflows complete, even when this tab is in the background."
        @update:modelValue="notif.toggle($event)"
      />

      <p v-if="notif.permission.value === 'denied'" class="text-[11px] text-amber-400 px-1">
        Notifications are blocked in browser settings. To enable, update your browser's site permissions for this page.
      </p>
    </template>
  </section>
</div>
```

- [ ] **Step 5: Build check**

```bash
cd web && npm run build 2>&1 | tail -20
```
Expected: no TypeScript errors, build succeeds.

- [ ] **Step 6: Final test run**

```bash
cd web && npx vitest run src/composables/__tests__/useBrowserNotifications.test.ts src/composables/__tests__/useNotifications.test.ts
```
Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/SettingsView.vue
git commit -m "feat(ui): add Notifications settings tab with desktop notification toggle"
```
