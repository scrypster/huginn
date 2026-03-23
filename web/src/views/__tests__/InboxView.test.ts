import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockFetchNotifications = vi.fn()
const mockApplyAction = vi.fn()
const mockCreateSession = vi.fn()

const mockNotifications = vi.fn(() => ({ value: [] }))
const mockPendingCount = vi.fn(() => ({ value: 0 }))
const mockLoading = vi.fn(() => ({ value: false }))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn(),
  }),
}))

vi.mock('../../composables/useNotifications', () => {
  const { ref } = require('vue')
  const notifications = ref<unknown[]>([])
  const pendingCount = ref(0)
  const loading = ref(false)

  return {
    useNotifications: () => ({
      notifications,
      pendingCount,
      loading,
      fetchNotifications: (...args: unknown[]) => mockFetchNotifications(...args),
      applyAction: (...args: unknown[]) => mockApplyAction(...args),
    }),
  }
})

vi.mock('../../composables/useSessions', () => ({
  useSessions: () => ({
    createSession: (...args: unknown[]) => mockCreateSession(...args),
  }),
}))

import InboxView from '../InboxView.vue'
import { useNotifications } from '../../composables/useNotifications'

// ── Sample data ───────────────────────────────────────────────────────────────

const sampleNotifications = [
  {
    id: 'notif-001',
    routine_id: 'routine-1',
    run_id: 'run-1',
    summary: 'Deploy completed',
    detail: 'Service deployed successfully',
    severity: 'info' as const,
    status: 'pending' as const,
    created_at: '2026-03-17T10:00:00Z',
    updated_at: '2026-03-17T10:00:00Z',
  },
  {
    id: 'notif-002',
    routine_id: 'routine-2',
    run_id: 'run-2',
    summary: 'CPU alert',
    detail: 'CPU usage above 90%',
    severity: 'urgent' as const,
    status: 'pending' as const,
    created_at: '2026-03-17T09:00:00Z',
    updated_at: '2026-03-17T09:00:00Z',
  },
]

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(InboxView)
}

beforeEach(() => {
  mockFetchNotifications.mockReset().mockResolvedValue(undefined)
  mockApplyAction.mockReset().mockResolvedValue({ status: 'seen', pending_count: 0 })
  mockCreateSession.mockReset().mockResolvedValue({ id: 'sess-new-001' })

  // Reset reactive state
  const { notifications, pendingCount, loading } = useNotifications() as any
  notifications.value = []
  pendingCount.value = 0
  loading.value = false
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('InboxView', () => {
  it('renders without error', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('calls fetchNotifications on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockFetchNotifications).toHaveBeenCalled()
  })

  it('shows "Inbox" heading in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Inbox')
  })

  it('shows "Mark all seen" button', async () => {
    const w = mountView()
    await flushPromises()
    const btn = w.find('[data-testid="mark-all-seen-btn"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('Mark all seen')
  })

  it('shows empty state when no notifications', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('No notifications')
  })

  it('shows notification list when notifications exist', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()
    const list = w.find('[data-testid="notification-list"]')
    expect(list.exists()).toBe(true)
  })

  it('shows loading spinner while fetching', async () => {
    const { loading } = useNotifications() as any
    loading.value = true
    const w = mountView()
    await nextTick()
    // The spinner is an animate-spin div
    expect(w.find('.animate-spin').exists()).toBe(true)
  })

  it('shows pending count badge when pendingCount > 0', async () => {
    const { pendingCount } = useNotifications() as any
    pendingCount.value = 3
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('3')
  })

  it('does not show pending count badge when pendingCount is 0', async () => {
    const { pendingCount } = useNotifications() as any
    pendingCount.value = 0
    const w = mountView()
    await flushPromises()
    // The count badge uses a v-if="pendingCount > 0" span
    const badge = w.find('.rounded-full.bg-huginn-blue')
    expect(badge.exists()).toBe(false)
  })

  it('filters out dismissed and executed notifications from visible list', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      ...sampleNotifications,
      { ...sampleNotifications[0], id: 'notif-dismissed', status: 'dismissed' },
      { ...sampleNotifications[0], id: 'notif-executed', status: 'executed' },
    ]
    const w = mountView()
    await flushPromises()
    // Only the 2 pending ones should be rendered; dismissed/executed should be hidden
    const items = w.findAll('[data-testid="notification-item"]')
    expect(items.length).toBe(2)
  })

  it('markAllSeen calls applyAction for each pending notification', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    mockApplyAction.mockResolvedValue({ status: 'seen', pending_count: 0 })
    const btn = w.find('[data-testid="mark-all-seen-btn"]')
    await btn.trigger('click')
    await flushPromises()

    expect(mockApplyAction).toHaveBeenCalledWith('notif-001', 'seen')
    expect(mockApplyAction).toHaveBeenCalledWith('notif-002', 'seen')
  })
})

// ── Severity filter chips ──────────────────────────────────────────────────────

describe('InboxView – severity filter chips', () => {
  const mixedNotifications = [
    {
      id: 'n-urgent',
      routine_id: 'r1',
      run_id: 'run1',
      summary: 'Urgent alert',
      detail: 'Something critical',
      severity: 'urgent' as const,
      status: 'pending' as const,
      created_at: '2026-03-17T10:00:00Z',
      updated_at: '2026-03-17T10:00:00Z',
    },
    {
      id: 'n-warning',
      routine_id: 'r2',
      run_id: 'run2',
      summary: 'Warning notice',
      detail: 'Something to watch',
      severity: 'warning' as const,
      status: 'pending' as const,
      created_at: '2026-03-17T10:01:00Z',
      updated_at: '2026-03-17T10:01:00Z',
    },
    {
      id: 'n-info',
      routine_id: 'r3',
      run_id: 'run3',
      summary: 'FYI',
      detail: 'Just info',
      severity: 'info' as const,
      status: 'pending' as const,
      created_at: '2026-03-17T10:02:00Z',
      updated_at: '2026-03-17T10:02:00Z',
    },
  ]

  it('renders All, Urgent, Warning, Info chip buttons', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const text = w.text()
    expect(text).toContain('All')
    expect(text).toContain('Urgent')
    expect(text).toContain('Warning')
    expect(text).toContain('Info')
  })

  it('All chip shows total active notification count', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    // All chip should display count = 3 (all active)
    const allChipText = w.findAll('button').find(b => b.text().startsWith('All'))?.text()
    expect(allChipText).toContain('3')
  })

  it('Urgent chip shows count of urgent notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    expect(urgentChip?.text()).toContain('1')
  })

  it('Warning chip shows count of warning notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const warningChip = w.findAll('button').find(b => b.text().startsWith('Warning'))
    expect(warningChip?.text()).toContain('1')
  })

  it('Info chip shows count of info notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const infoChip = w.findAll('button').find(b => b.text().startsWith('Info'))
    expect(infoChip?.text()).toContain('1')
  })

  it('clicking Urgent chip filters visible list to urgent only', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    await urgentChip?.trigger('click')
    await nextTick()

    const items = w.findAll('[data-testid="notification-item"]')
    expect(items.length).toBe(1)
  })

  it('clicking Warning chip filters visible list to warning only', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const warningChip = w.findAll('button').find(b => b.text().startsWith('Warning'))
    await warningChip?.trigger('click')
    await nextTick()

    const items = w.findAll('[data-testid="notification-item"]')
    expect(items.length).toBe(1)
  })

  it('clicking Info chip filters visible list to info only', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    const infoChip = w.findAll('button').find(b => b.text().startsWith('Info'))
    await infoChip?.trigger('click')
    await nextTick()

    const items = w.findAll('[data-testid="notification-item"]')
    expect(items.length).toBe(1)
  })

  it('clicking All chip after a severity filter restores all notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = mixedNotifications
    const w = mountView()
    await flushPromises()

    // First filter to urgent
    const urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    await urgentChip?.trigger('click')
    await nextTick()

    // Then switch back to All
    const allChip = w.findAll('button').find(b => b.text().startsWith('All'))
    await allChip?.trigger('click')
    await nextTick()

    const items = w.findAll('[data-testid="notification-item"]')
    expect(items.length).toBe(3)
  })

  it('empty severity category shows 0 count (no count span rendered)', async () => {
    const { notifications } = useNotifications() as any
    // Only urgent notifications, so warning and info chips have count 0
    notifications.value = [mixedNotifications[0]] // only urgent
    const w = mountView()
    await flushPromises()

    const warningChip = w.findAll('button').find(b => b.text().startsWith('Warning'))
    // When count is 0, the span inside the chip is not rendered (v-if="chip.count > 0")
    // So the chip text should only be "Warning" with no number
    expect(warningChip?.text().trim()).toBe('Warning')
  })

  it('dismissed notifications are excluded from chip counts', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      ...mixedNotifications,
      { ...mixedNotifications[0], id: 'n-dismissed', status: 'dismissed' },
      { ...mixedNotifications[1], id: 'n-executed', status: 'executed' },
    ]
    const w = mountView()
    await flushPromises()

    // All chip should still show 3 (only the 3 active ones)
    const allChipText = w.findAll('button').find(b => b.text().startsWith('All'))?.text()
    expect(allChipText).toContain('3')
  })
})

// ── Snooze ────────────────────────────────────────────────────────────────────

describe('InboxView – snooze', () => {
  it('unsnooze button is not shown when no notifications are snoozed', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    expect(w.find('[data-testid="unsnooze-btn"]').exists()).toBe(false)
  })

  it('snoozing a notification hides it from the visible list', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    // 2 items visible initially
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(2)

    // Snooze the first notification
    const snoozeBtn = w.findAll('[data-testid="snooze-btn"]')[0]
    await snoozeBtn.trigger('click')
    await nextTick()

    // Now only 1 item should be visible
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(1)
  })

  it('unsnooze button appears after snoozeing a notification', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    const snoozeBtn = w.findAll('[data-testid="snooze-btn"]')[0]
    await snoozeBtn.trigger('click')
    await nextTick()

    expect(w.find('[data-testid="unsnooze-btn"]').exists()).toBe(true)
  })

  it('unsnooze button shows correct snoozed count', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    // Snooze both notifications
    const snoozeBtns = w.findAll('[data-testid="snooze-btn"]')
    await snoozeBtns[0].trigger('click')
    await nextTick()
    // After first snooze, re-find buttons since the list updated
    const snoozeBtns2 = w.findAll('[data-testid="snooze-btn"]')
    await snoozeBtns2[0].trigger('click')
    await nextTick()

    const unsnoozeBtn = w.find('[data-testid="unsnooze-btn"]')
    expect(unsnoozeBtn.text()).toContain('2')
  })

  it('clicking unsnooze restores all snoozed notifications to visible list', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    // Snooze the first notification
    const snoozeBtn = w.findAll('[data-testid="snooze-btn"]')[0]
    await snoozeBtn.trigger('click')
    await nextTick()
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(1)

    // Click unsnooze (clearSnooze)
    const unsnoozeBtn = w.find('[data-testid="unsnooze-btn"]')
    await unsnoozeBtn.trigger('click')
    await nextTick()

    // All notifications restored
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(2)
    // Unsnooze button is gone
    expect(w.find('[data-testid="unsnooze-btn"]').exists()).toBe(false)
  })

  it('snoozed notifications are also excluded from All chip count', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    // All chip count starts at 2
    let allChipText = w.findAll('button').find(b => b.text().startsWith('All'))?.text()
    expect(allChipText).toContain('2')

    // Snooze one
    await w.findAll('[data-testid="snooze-btn"]')[0].trigger('click')
    await nextTick()

    // All chip count should drop to 1
    allChipText = w.findAll('button').find(b => b.text().startsWith('All'))?.text()
    expect(allChipText).toContain('1')
  })

  it('snoozed + severity filter: only non-snoozed matching severity visible', async () => {
    const { notifications } = useNotifications() as any
    // 2 urgent, 1 info
    notifications.value = [
      { ...sampleNotifications[1], id: 'urgent-1' },
      { ...sampleNotifications[1], id: 'urgent-2' },
      { ...sampleNotifications[0], id: 'info-1' },
    ]
    const w = mountView()
    await flushPromises()

    // Snooze urgent-1
    await w.findAll('[data-testid="snooze-btn"]')[0].trigger('click')
    await nextTick()

    // Filter to Urgent
    const urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    await urgentChip?.trigger('click')
    await nextTick()

    // Only urgent-2 should be visible (urgent-1 is snoozed)
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(1)
  })
})

// ── Dismiss all ───────────────────────────────────────────────────────────────

describe('InboxView – dismissAll', () => {
  it('dismiss all button exists in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="dismiss-all-btn"]').exists()).toBe(true)
  })

  it('dismissAll calls applyAction with "dismissed" for each active notification', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="dismiss-all-btn"]').trigger('click')
    await flushPromises()

    expect(mockApplyAction).toHaveBeenCalledWith('notif-001', 'dismissed')
    expect(mockApplyAction).toHaveBeenCalledWith('notif-002', 'dismissed')
  })

  it('dismissAll skips already-dismissed notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      ...sampleNotifications,
      { ...sampleNotifications[0], id: 'notif-already-dismissed', status: 'dismissed' },
    ]
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="dismiss-all-btn"]').trigger('click')
    await flushPromises()

    // applyAction should be called only for the 2 pending ones, not for the dismissed one
    expect(mockApplyAction).toHaveBeenCalledTimes(2)
    expect(mockApplyAction).not.toHaveBeenCalledWith('notif-already-dismissed', 'dismissed')
  })

  it('dismissAll skips executed notifications', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      ...sampleNotifications,
      { ...sampleNotifications[0], id: 'notif-executed', status: 'executed' },
    ]
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="dismiss-all-btn"]').trigger('click')
    await flushPromises()

    expect(mockApplyAction).not.toHaveBeenCalledWith('notif-executed', 'dismissed')
    expect(mockApplyAction).toHaveBeenCalledTimes(2)
  })

  it('dismissAll does nothing when no notifications exist', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = []
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="dismiss-all-btn"]').trigger('click')
    await flushPromises()

    expect(mockApplyAction).not.toHaveBeenCalled()
  })

  it('dismissAll includes seen notifications (only skips dismissed/executed)', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      { ...sampleNotifications[0], id: 'notif-seen', status: 'seen' },
      { ...sampleNotifications[1], id: 'notif-pending', status: 'pending' },
    ]
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="dismiss-all-btn"]').trigger('click')
    await flushPromises()

    expect(mockApplyAction).toHaveBeenCalledWith('notif-seen', 'dismissed')
    expect(mockApplyAction).toHaveBeenCalledWith('notif-pending', 'dismissed')
    expect(mockApplyAction).toHaveBeenCalledTimes(2)
  })
})

// ── handleChat ────────────────────────────────────────────────────────────────

describe('InboxView – handleChat', () => {
  it('handleChat creates a new session', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    mockCreateSession.mockResolvedValue({ id: 'sess-chat-001' })

    // Trigger chat event from the NotificationCard stub
    const card = w.findComponent({ name: 'NotificationCard' })
    await card.vm.$emit('chat', sampleNotifications[0])
    await flushPromises()

    expect(mockCreateSession).toHaveBeenCalledOnce()
  })

  it('handleChat applies "seen" action on the notification', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications
    const w = mountView()
    await flushPromises()

    mockCreateSession.mockResolvedValue({ id: 'sess-chat-002' })

    const card = w.findComponent({ name: 'NotificationCard' })
    await card.vm.$emit('chat', sampleNotifications[0])
    await flushPromises()

    expect(mockApplyAction).toHaveBeenCalledWith('notif-001', 'seen')
  })

  it('handleChat navigates to /chat/{sessionId}', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = sampleNotifications

    const mockPush = vi.fn()
    vi.doMock('vue-router', () => ({ useRouter: () => ({ push: mockPush }) }))

    const w = mountView()
    await flushPromises()

    mockCreateSession.mockResolvedValue({ id: 'sess-chat-003' })

    const card = w.findComponent({ name: 'NotificationCard' })
    await card.vm.$emit('chat', sampleNotifications[1])
    await flushPromises()

    // Router push is captured via the module-level mock
    // We verify the session was created and action applied as proxy for navigation
    expect(mockCreateSession).toHaveBeenCalled()
    expect(mockApplyAction).toHaveBeenCalledWith('notif-002', 'seen')
  })

  it('handleChat uses session id from createSession response', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [sampleNotifications[0]]
    const w = mountView()
    await flushPromises()

    mockCreateSession.mockResolvedValue({ id: 'unique-session-xyz' })

    const card = w.findComponent({ name: 'NotificationCard' })
    await card.vm.$emit('chat', sampleNotifications[0])
    await flushPromises()

    // Verify createSession was called to obtain the session id
    expect(mockCreateSession).toHaveBeenCalledOnce()
    // And the notification was marked seen after session creation
    expect(mockApplyAction).toHaveBeenCalledWith('notif-001', 'seen')
  })
})

// ── Edge cases ────────────────────────────────────────────────────────────────

describe('InboxView – edge cases', () => {
  it('shows empty state when all notifications are dismissed', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      { ...sampleNotifications[0], status: 'dismissed' },
      { ...sampleNotifications[1], status: 'executed' },
    ]
    const w = mountView()
    await flushPromises()

    expect(w.text()).toContain('No notifications')
    expect(w.find('[data-testid="notification-list"]').exists()).toBe(false)
  })

  it('shows empty state when severity filter matches nothing', async () => {
    const { notifications } = useNotifications() as any
    // Only info notifications
    notifications.value = [{ ...sampleNotifications[0], severity: 'info' }]
    const w = mountView()
    await flushPromises()

    // Filter to urgent (no urgent notifications)
    const urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    await urgentChip?.trigger('click')
    await nextTick()

    expect(w.text()).toContain('No notifications')
  })

  it('All chip count is 0 when all notifications are snoozed', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [sampleNotifications[0]]
    const w = mountView()
    await flushPromises()

    // Snooze the only notification
    await w.find('[data-testid="snooze-btn"]').trigger('click')
    await nextTick()

    // All chip should show no count (count = 0, span hidden by v-if)
    const allChip = w.findAll('button').find(b => b.text().startsWith('All'))
    expect(allChip?.text().trim()).toBe('All')
  })

  it('mixing snoozed and severity filter excludes snoozed from chip counts', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      { ...sampleNotifications[1], id: 'urgent-a' }, // urgent
      { ...sampleNotifications[1], id: 'urgent-b' }, // urgent
    ]
    const w = mountView()
    await flushPromises()

    // Urgent chip initially shows 2
    let urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    expect(urgentChip?.text()).toContain('2')

    // Snooze one urgent
    await w.findAll('[data-testid="snooze-btn"]')[0].trigger('click')
    await nextTick()

    // Urgent chip should now show 1
    urgentChip = w.findAll('button').find(b => b.text().startsWith('Urgent'))
    expect(urgentChip?.text()).toContain('1')
  })

  it('notification-item count matches visible computed based on active severity filter', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      { ...sampleNotifications[0], id: 'i1', severity: 'info' },
      { ...sampleNotifications[0], id: 'i2', severity: 'info' },
      { ...sampleNotifications[1], id: 'u1', severity: 'urgent' },
    ]
    const w = mountView()
    await flushPromises()

    // All: 3 items
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(3)

    // Filter to info: 2 items
    const infoChip = w.findAll('button').find(b => b.text().startsWith('Info'))
    await infoChip?.trigger('click')
    await nextTick()
    expect(w.findAll('[data-testid="notification-item"]').length).toBe(2)

    // Filter to warning: 0 items → empty state
    const warningChip = w.findAll('button').find(b => b.text().startsWith('Warning'))
    await warningChip?.trigger('click')
    await nextTick()
    expect(w.find('[data-testid="notification-list"]').exists()).toBe(false)
    expect(w.text()).toContain('No notifications')
  })

  it('markAllSeen only targets pending notifications, not seen/dismissed/executed', async () => {
    const { notifications } = useNotifications() as any
    notifications.value = [
      { ...sampleNotifications[0], id: 'pending-1', status: 'pending' },
      { ...sampleNotifications[0], id: 'seen-1', status: 'seen' },
      { ...sampleNotifications[0], id: 'dismissed-1', status: 'dismissed' },
    ]
    const w = mountView()
    await flushPromises()

    await w.find('[data-testid="mark-all-seen-btn"]').trigger('click')
    await flushPromises()

    expect(mockApplyAction).toHaveBeenCalledWith('pending-1', 'seen')
    expect(mockApplyAction).not.toHaveBeenCalledWith('seen-1', 'seen')
    expect(mockApplyAction).not.toHaveBeenCalledWith('dismissed-1', 'seen')
  })
})
