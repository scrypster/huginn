import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockApiStats = vi.fn()
const mockApiCost = vi.fn()
const mockApiHealth = vi.fn()
const mockApiSessionsList = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    stats: (...args: unknown[]) => mockApiStats(...args),
    cost: (...args: unknown[]) => mockApiCost(...args),
    health: (...args: unknown[]) => mockApiHealth(...args),
    sessions: {
      list: (...args: unknown[]) => mockApiSessionsList(...args),
    },
  },
}))

import StatsView from '../StatsView.vue'

// ── Sample data ───────────────────────────────────────────────────────────────

const sampleStats = {
  last_prompt_tokens: 1234,
  last_completion_tokens: 567,
}

const sampleCost = { session_total_usd: 0.0025 }

const sampleHealth = { status: 'ok', version: '1.2.3' }

const sampleSessions = [
  { session_id: 'sess-1', agent: 'Coder', model: 'claude-sonnet-4-6', message_count: 10, status: 'active' },
  { session_id: 'sess-2', agent: 'Planner', model: 'claude-opus-4-6', message_count: 5, status: 'idle' },
  { session_id: 'sess-3', agent: 'Coder', model: 'claude-sonnet-4-6', message_count: 8, status: 'active' },
]

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(StatsView)
}

beforeEach(() => {
  mockApiStats.mockReset().mockResolvedValue(sampleStats)
  mockApiCost.mockReset().mockResolvedValue(sampleCost)
  mockApiHealth.mockReset().mockResolvedValue(sampleHealth)
  mockApiSessionsList.mockReset().mockResolvedValue(sampleSessions)
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('StatsView', () => {
  it('renders without error', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('calls all API endpoints on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockApiStats).toHaveBeenCalled()
    expect(mockApiCost).toHaveBeenCalled()
    expect(mockApiHealth).toHaveBeenCalled()
    expect(mockApiSessionsList).toHaveBeenCalled()
  })

  it('shows "stats" label in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('stats')
  })

  it('shows loading state while fetching', async () => {
    let resolve!: (v: unknown) => void
    mockApiStats.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('Loading stats...')
    resolve(sampleStats)
    await flushPromises()
  })

  it('shows total sessions count', async () => {
    const w = mountView()
    await flushPromises()
    // sampleSessions has 3 sessions
    expect(w.text()).toContain('3')
  })

  it('shows active sessions count', async () => {
    const w = mountView()
    await flushPromises()
    // 2 sessions with status 'active'
    expect(w.text()).toContain('2')
  })

  it('shows total messages count', async () => {
    const w = mountView()
    await flushPromises()
    // 10 + 5 + 8 = 23
    expect(w.text()).toContain('23')
  })

  it('shows formatted cost', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('$0.0025')
  })

  it('shows prompt token count', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('1,234')
  })

  it('shows completion token count', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('567')
  })

  it('shows server health status', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('ok')
  })

  it('shows server version', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('1.2.3')
  })

  it('shows top agents section', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Top Agents')
    expect(w.text()).toContain('Coder')
  })

  it('shows top models section', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Top Models')
    expect(w.text()).toContain('claude-sonnet-4-6')
  })

  it('shows "—" for cost when no cost data', async () => {
    mockApiCost.mockResolvedValueOnce(null)
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('—')
  })

  it('shows "Unavailable" for health when health call fails', async () => {
    mockApiHealth.mockResolvedValueOnce(null)
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Unavailable')
  })

  it('shows "No session data." for top agents when no sessions', async () => {
    mockApiSessionsList.mockResolvedValueOnce([])
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('No session data.')
  })

  it('shows a refresh button', async () => {
    const w = mountView()
    await flushPromises()
    const btn = w.findAll('button').find(b => b.text() === 'refresh')
    expect(btn).toBeDefined()
  })

  it('refresh re-fetches all stats', async () => {
    const w = mountView()
    await flushPromises()
    mockApiStats.mockReset().mockResolvedValue({ last_prompt_tokens: 999 })
    await (w.vm as any).fetchAll()
    await flushPromises()
    expect(mockApiStats).toHaveBeenCalledTimes(1)
  })

  it('shows lastRefreshed time after successful fetch', async () => {
    const w = mountView()
    await flushPromises()
    // lastRefreshed should be set to a non-empty time string
    expect((w.vm as any).lastRefreshed).not.toBe('')
  })

  it('barWidth returns 100% for the top item', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const items = [{ name: 'Coder', count: 5 }, { name: 'Planner', count: 2 }]
    expect(vm.barWidth(5, items)).toBe('100%')
  })

  it('barWidth returns proportional width for second item', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const items = [{ name: 'Coder', count: 4 }, { name: 'Planner', count: 2 }]
    expect(vm.barWidth(2, items)).toBe('50%')
  })
})
