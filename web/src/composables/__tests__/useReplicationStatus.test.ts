import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'

// Mock apiFetch so tests don't need a real server
vi.mock('../useApi', () => ({
  apiFetch: vi.fn(),
  setToken: vi.fn(),
}))

import { apiFetch } from '../useApi'
import { useReplicationStatus } from '../useReplicationStatus'

describe('useReplicationStatus', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('chipText is empty string initially (before first poll)', () => {
    const spaceId = ref<string | undefined>(undefined)
    const { chipText } = useReplicationStatus(spaceId)
    expect(chipText.value).toBe('')
  })

  it('chipText is empty when not connected', async () => {
    vi.mocked(apiFetch).mockResolvedValue({ pending: 0, failed: 0, dead: 0, connected: false })
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    // Advance to run the immediate poll
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(chipText.value).toBe('')
  })

  it('chipText shows syncing when pending > 0', async () => {
    vi.mocked(apiFetch).mockResolvedValue({ pending: 3, failed: 0, dead: 0, connected: true })
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(chipText.value).toContain('syncing')
  })

  it('chipText shows synced when pending=0, failed=0, dead=0, connected=true', async () => {
    vi.mocked(apiFetch).mockResolvedValue({ pending: 0, failed: 0, dead: 0, connected: true })
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(chipText.value).toContain('synced')
  })

  it('chipText shows issues when failed > 0', async () => {
    vi.mocked(apiFetch).mockResolvedValue({ pending: 0, failed: 2, dead: 0, connected: true })
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(chipText.value).toContain('issues')
  })

  it('swallows fetch errors silently — chip stays empty', async () => {
    vi.mocked(apiFetch).mockRejectedValue(new Error('network fail'))
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(chipText.value).toBe('')
  })

  it('clears chip when spaceId changes to undefined', async () => {
    vi.mocked(apiFetch).mockResolvedValue({ pending: 0, failed: 0, dead: 0, connected: true })
    const spaceId = ref<string | undefined>('space-1')
    const { chipText } = useReplicationStatus(spaceId)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    spaceId.value = undefined
    await nextTick()
    expect(chipText.value).toBe('')
  })
})
