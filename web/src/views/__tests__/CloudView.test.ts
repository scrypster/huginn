import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockCloudStatus = vi.fn()
const mockCloudConnect = vi.fn()
const mockCloudDisconnect = vi.fn()

vi.mock('../../composables/useCloud', () => {
  const { ref } = require('vue')
  const status = ref({ registered: false, connected: false })
  const loading = ref(false)
  const connecting = ref(false)
  const disconnecting = ref(false)
  const error = ref<string | null>(null)

  return {
    useCloud: () => ({
      status,
      loading,
      connecting,
      disconnecting,
      error,
      fetchStatus: (...args: unknown[]) => mockCloudStatus(...args),
      connect: (...args: unknown[]) => mockCloudConnect(...args),
      disconnect: (...args: unknown[]) => mockCloudDisconnect(...args),
    }),
  }
})

import CloudView from '../CloudView.vue'

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(CloudView)
}

beforeEach(() => {
  mockCloudStatus.mockReset().mockResolvedValue(undefined)
  mockCloudConnect.mockReset().mockResolvedValue(undefined)
  mockCloudDisconnect.mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('CloudView', () => {
  it('renders without error', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('calls fetchStatus on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockCloudStatus).toHaveBeenCalled()
  })

  it('shows "cloud" label in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('cloud')
  })

  it('shows a refresh button', async () => {
    const w = mountView()
    await flushPromises()
    const btn = w.find('button')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('refresh')
  })

  it('clicking refresh button calls fetchStatus', async () => {
    const w = mountView()
    await flushPromises()
    mockCloudStatus.mockReset().mockResolvedValue(undefined)
    const refreshBtn = w.findAll('button').find(b => b.text().includes('refresh'))
    expect(refreshBtn).toBeDefined()
    await refreshBtn!.trigger('click')
    expect(mockCloudStatus).toHaveBeenCalled()
  })

  it('shows Connection section header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Connection')
  })

  it('shows Actions section header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Actions')
  })

  it('shows "Not connected" status when not registered and not connected', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Not connected')
  })

  it('shows "Connect to HuginnCloud" button when not registered', async () => {
    const w = mountView()
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect'))
    expect(connectBtn).toBeDefined()
    expect(connectBtn!.text()).toContain('Connect to HuginnCloud')
  })

  it('calls connect when Connect button is clicked', async () => {
    const w = mountView()
    await flushPromises()
    const connectBtn = w.findAll('button').find(b => b.text().includes('Connect to HuginnCloud'))
    expect(connectBtn).toBeDefined()
    await connectBtn!.trigger('click')
    expect(mockCloudConnect).toHaveBeenCalled()
  })
})
