import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useDeliveryQueue } from '../useDeliveryQueue'
import { setToken, getToken } from '../useApi'

// Reset module-level shared state before each test
beforeEach(() => {
  const { badgeCount, actionableEntries, loading } = useDeliveryQueue()
  badgeCount.value = 0
  actionableEntries.value = []
  loading.value = false
  setToken('')
  vi.resetAllMocks()
})

describe('useDeliveryQueue', () => {
  it('fetchBadge updates badgeCount', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ count: 3 }),
    } as Response)
    const { badgeCount, fetchBadge } = useDeliveryQueue()
    await fetchBadge()
    expect(badgeCount.value).toBe(3)
  })

  it('dismissEntry removes entry from list', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true } as Response)
    const { actionableEntries, dismissEntry } = useDeliveryQueue()
    actionableEntries.value = [
      {
        id: 'e1', workflow_id: 'w1', run_id: 'r1', endpoint: 'x', channel: 'webhook',
        status: 'failed', attempt_count: 5, max_attempts: 5, retry_window_s: 480,
        next_retry_at: '', created_at: '',
      }
    ]
    await dismissEntry('e1')
    expect(actionableEntries.value).toHaveLength(0)
  })

  it('fetchBadge waits for a token before requesting, so it never fires pre-auth', async () => {
    // No token set yet (simulates the badge poll racing App.vue's initApp()).
    expect(getToken()).toBe('')
    const calls: string[] = []
    global.fetch = vi.fn(async (input: RequestInfo | URL) => {
      calls.push(String(input))
      if (String(input) === '/api/v1/token') {
        return { ok: true, json: async () => ({ token: 'fresh-token' }) } as Response
      }
      return { ok: true, json: async () => ({ count: 2 }) } as Response
    })
    const { badgeCount, fetchBadge } = useDeliveryQueue()
    await fetchBadge()
    // Token endpoint must be hit (and resolved) before the badge endpoint —
    // never the reverse, which is what produces the pre-auth 401.
    expect(calls.indexOf('/api/v1/token')).toBeLessThan(calls.indexOf('/api/v1/delivery-queue/badge'))
    expect(getToken()).toBe('fresh-token')
    expect(badgeCount.value).toBe(2)
  })

  it('handleBadgeUpdate sets count', () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [],
    } as Response)
    const { badgeCount, handleBadgeUpdate } = useDeliveryQueue()
    handleBadgeUpdate(7)
    expect(badgeCount.value).toBe(7)
  })
})
