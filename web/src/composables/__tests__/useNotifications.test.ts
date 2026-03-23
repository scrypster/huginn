import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setToken } from '../useApi'
import { useNotifications } from '../useNotifications'

function ok(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function makeNotification(overrides: Record<string, unknown> = {}) {
  return {
    id: 'notif-1',
    routine_id: 'routine-1',
    run_id: 'run-1',
    summary: 'Test notification',
    detail: 'Test detail',
    severity: 'info' as const,
    status: 'pending' as const,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('useNotifications', () => {
  beforeEach(() => {
    setToken('test-token')
    const { notifications } = useNotifications()
    notifications.value = []
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ── fetchNotifications ──────────────────────────────────────────────────────

  describe('fetchNotifications', () => {
    it('populates the notifications list from the API response', async () => {
      const list = [
        makeNotification({ id: 'notif-1' }),
        makeNotification({ id: 'notif-2', status: 'seen' }),
      ]
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(list))

      const { notifications, fetchNotifications } = useNotifications()
      await fetchNotifications()

      expect(notifications.value).toHaveLength(2)
      expect(notifications.value[0]!.id).toBe('notif-1')
      expect(notifications.value[1]!.id).toBe('notif-2')
    })

    it('sends Authorization header', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok([]))
      const { fetchNotifications } = useNotifications()
      await fetchNotifications()

      const [, opts] = spy.mock.calls[0]!
      const headers = opts?.headers as Record<string, string>
      expect(headers['Authorization']).toBe('Bearer test-token')
    })

    it('sets notifications to empty array when response is not an array', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ error: 'oops' }))
      const { notifications, fetchNotifications } = useNotifications()
      notifications.value = [makeNotification() as any]
      await fetchNotifications()
      expect(notifications.value).toHaveLength(0)
    })

    it('computes pendingCount from pending notifications', async () => {
      const list = [
        makeNotification({ id: 'n1', status: 'pending' }),
        makeNotification({ id: 'n2', status: 'seen' }),
        makeNotification({ id: 'n3', status: 'pending' }),
      ]
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(list))

      const { pendingCount, fetchNotifications } = useNotifications()
      await fetchNotifications()

      expect(pendingCount.value).toBe(2)
    })

    it('does not throw when fetch fails — handles errors silently', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('Network error'))
      const { fetchNotifications } = useNotifications()
      await expect(fetchNotifications()).resolves.toBeUndefined()
    })

    it('calls /api/v1/notifications endpoint', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok([]))
      const { fetchNotifications } = useNotifications()
      await fetchNotifications()

      expect(spy.mock.calls[0]![0]).toBe('/api/v1/notifications')
    })

    it('sets loading=true during fetch then false after', async () => {
      let loadingDuringFetch = false
      const { loading, fetchNotifications } = useNotifications()
      vi.spyOn(globalThis, 'fetch').mockImplementationOnce(async () => {
        loadingDuringFetch = loading.value
        return ok([])
      })
      await fetchNotifications()
      expect(loadingDuringFetch).toBe(true)
      expect(loading.value).toBe(false)
    })
  })

  // ── fetchSummary ──────────────────────────────────────────────────────────

  describe('fetchSummary', () => {
    it('updates pendingCount from pending_count field', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ pending_count: 7 }))
      const { pendingCount, fetchSummary } = useNotifications()
      await fetchSummary()
      expect(pendingCount.value).toBe(7)
    })

    it('calls /api/v1/inbox/summary', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ pending_count: 0 }))
      const { fetchSummary } = useNotifications()
      await fetchSummary()
      expect(spy.mock.calls[0]![0]).toBe('/api/v1/inbox/summary')
    })

    it('silently ignores fetch failures', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network error'))
      const { fetchSummary } = useNotifications()
      await expect(fetchSummary()).resolves.toBeUndefined()
    })
  })

  // ── applyAction ───────────────────────────────────────────────────────────

  describe('applyAction', () => {
    it('POSTs to /api/v1/notifications/:id/action', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'seen', pending_count: 0 }))
      const { applyAction } = useNotifications()
      await applyAction('notif-1', 'seen')
      expect(spy.mock.calls[0]![0]).toBe('/api/v1/notifications/notif-1/action')
      expect((spy.mock.calls[0]![1] as RequestInit).method).toBe('POST')
    })

    it('sends action in request body', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'dismissed', pending_count: 0 }))
      const { applyAction } = useNotifications()
      await applyAction('notif-1', 'dismissed')
      const body = JSON.parse((spy.mock.calls[0]![1] as RequestInit).body as string)
      expect(body.action).toBe('dismissed')
    })

    it('includes proposedActionId in body when provided', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'approved', pending_count: 0 }))
      const { applyAction } = useNotifications()
      await applyAction('notif-1', 'approve', 'pa-abc')
      const body = JSON.parse((spy.mock.calls[0]![1] as RequestInit).body as string)
      expect(body.proposed_action_id).toBe('pa-abc')
    })

    it('updates notification status in list', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'seen', pending_count: 0 }))
      const { notifications, applyAction } = useNotifications()
      notifications.value = [makeNotification({ id: 'notif-1', status: 'pending' })] as any
      await applyAction('notif-1', 'seen')
      expect(notifications.value[0]!.status).toBe('seen')
    })

    it('throws when response is not ok', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 400 }))
      const { applyAction } = useNotifications()
      await expect(applyAction('notif-1', 'seen')).rejects.toThrow('action failed')
    })

    it('updates pendingCount from response', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'seen', pending_count: 3 }))
      const { pendingCount, applyAction } = useNotifications()
      await applyAction('notif-1', 'seen')
      expect(pendingCount.value).toBe(3)
    })

    it('does not crash when id not in list, still updates pendingCount', async () => {
      const { notifications, pendingCount, applyAction } = useNotifications()
      notifications.value = []
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'seen', pending_count: 2 }))
      await expect(applyAction('nonexistent', 'seen')).resolves.not.toThrow()
      expect(pendingCount.value).toBe(2)
    })

    it('keeps existing pendingCount when response lacks pending_count', async () => {
      const { pendingCount, applyAction } = useNotifications()
      pendingCount.value = 4
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'seen' }))
      await applyAction('notif-1', 'seen')
      expect(pendingCount.value).toBe(4)
    })
  })

  // ── wireWS ────────────────────────────────────────────────────────────────

  describe('wireWS', () => {
    it('is a no-op when ws is null', () => {
      const { wireWS } = useNotifications()
      expect(() => wireWS(null)).not.toThrow()
    })

    it('is a no-op when ws is undefined', () => {
      const { wireWS } = useNotifications()
      expect(() => wireWS(undefined)).not.toThrow()
    })

    it('notification_new prepends notification to list', () => {
      const { notifications, wireWS } = useNotifications()
      notifications.value = [makeNotification({ id: 'existing' })] as any
      const handlers: Record<string, (p: any) => void> = {}
      wireWS({ on: (ev: string, cb: (p: any) => void) => { handlers[ev] = cb } })
      handlers['notification_new']!({ notification: makeNotification({ id: 'new-one' }) })
      expect(notifications.value[0]!.id).toBe('new-one')
      expect(notifications.value).toHaveLength(2)
    })

    it('notification_update changes status of existing notification', () => {
      const { notifications, wireWS } = useNotifications()
      notifications.value = [makeNotification({ id: 'n1', status: 'pending' })] as any
      const handlers: Record<string, (p: any) => void> = {}
      wireWS({ on: (ev: string, cb: (p: any) => void) => { handlers[ev] = cb } })
      handlers['notification_update']!({ id: 'n1', status: 'seen' })
      expect(notifications.value[0]!.status).toBe('seen')
    })

    it('inbox_badge updates pendingCount', () => {
      const { pendingCount, wireWS } = useNotifications()
      const handlers: Record<string, (p: any) => void> = {}
      wireWS({ on: (ev: string, cb: (p: any) => void) => { handlers[ev] = cb } })
      handlers['inbox_badge']!({ pending_count: 5 })
      expect(pendingCount.value).toBe(5)
    })
  })
})
