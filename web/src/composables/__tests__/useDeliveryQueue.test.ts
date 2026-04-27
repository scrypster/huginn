import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useDeliveryQueue } from '../useDeliveryQueue'

// Reset module-level shared state before each test
beforeEach(() => {
  const { badgeCount, actionableEntries, loading } = useDeliveryQueue()
  badgeCount.value = 0
  actionableEntries.value = []
  loading.value = false
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
