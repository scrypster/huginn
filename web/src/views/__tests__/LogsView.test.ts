import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockApiLogs = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    logs: (...args: unknown[]) => mockApiLogs(...args),
  },
}))

import LogsView from '../LogsView.vue'

// ── Sample data ───────────────────────────────────────────────────────────────

const jsonLog1 = JSON.stringify({ time: '2026-03-17T10:00:00Z', level: 'INFO', msg: 'Server started', port: 8080 })
const jsonLog2 = JSON.stringify({ time: '2026-03-17T10:01:00Z', level: 'ERROR', msg: 'Connection refused', host: 'db.local' })
const jsonLog3 = JSON.stringify({ time: '2026-03-17T10:02:00Z', level: 'WARN', msg: 'High memory usage', used: '90%' })
const jsonLog4 = JSON.stringify({ time: '2026-03-17T10:03:00Z', level: 'DEBUG', msg: 'Cache miss', key: 'user:123' })

const sampleLines = [jsonLog1, jsonLog2, jsonLog3, jsonLog4]

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(LogsView)
}

beforeEach(() => {
  vi.useFakeTimers()
  mockApiLogs.mockReset().mockResolvedValue({ lines: sampleLines })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('LogsView', () => {
  it('renders without error', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('calls api.logs on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockApiLogs).toHaveBeenCalledWith(500)
  })

  it('shows "logs" label in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('logs')
  })

  it('shows loading state while fetching', async () => {
    let resolve!: (v: unknown) => void
    mockApiLogs.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('Loading logs...')
    resolve({ lines: sampleLines })
    await flushPromises()
  })

  it('shows error message when api call fails', async () => {
    mockApiLogs.mockRejectedValueOnce(new Error('Server unavailable'))
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Server unavailable')
  })

  it('renders log messages from the API response', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Server started')
    expect(w.text()).toContain('Connection refused')
  })

  it('shows line count in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('4 / 4 lines')
  })

  it('shows level filter pills (ALL, ERROR, WARN, INFO, DEBUG)', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('ALL')
    expect(w.text()).toContain('ERROR')
    expect(w.text()).toContain('WARN')
    expect(w.text()).toContain('INFO')
    expect(w.text()).toContain('DEBUG')
  })

  it('shows no-match message when no lines match filter', async () => {
    mockApiLogs.mockResolvedValueOnce({ lines: [] })
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('No log entries match.')
  })

  it('shows a refresh button', async () => {
    const w = mountView()
    await flushPromises()
    const refreshBtn = w.findAll('button').find(b => b.text() === 'refresh')
    expect(refreshBtn).toBeDefined()
  })

  it('clicking refresh re-fetches logs', async () => {
    const w = mountView()
    await flushPromises()
    mockApiLogs.mockReset().mockResolvedValue({ lines: [jsonLog1] })
    const refreshBtn = w.findAll('button').find(b => b.text() === 'refresh')
    await refreshBtn!.trigger('click')
    await flushPromises()
    expect(mockApiLogs).toHaveBeenCalledTimes(1)
  })

  it('auto-refresh checkbox exists and is checked by default', async () => {
    const w = mountView()
    await flushPromises()
    const checkbox = w.find('input[type="checkbox"]')
    expect(checkbox.exists()).toBe(true)
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
  })

  it('auto-refresh starts interval timer on mount', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')
    mountView()
    await flushPromises()
    expect(setIntervalSpy).toHaveBeenCalled()
  })

  it('renders parsed time from JSON log lines', async () => {
    const w = mountView()
    await flushPromises()
    // extractTime('2026-03-17T10:00:00Z') → '10:00:00'
    expect(w.text()).toContain('10:00:00')
  })
})
