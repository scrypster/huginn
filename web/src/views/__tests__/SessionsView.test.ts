import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockApiSessionsList = vi.fn().mockResolvedValue([])

vi.mock('../../composables/useApi', () => ({
  api: {
    sessions: {
      list: (...args: unknown[]) => mockApiSessionsList(...args),
    },
  },
}))

import SessionsView from '../SessionsView.vue'

// ── Sample data ───────────────────────────────────────────────────────────────

const sampleSessions = [
  {
    id: 'sess-abc123def456',
    created_at: '2026-01-15T10:00:00Z',
    updated_at: '2026-01-15T11:00:00Z',
    model: 'claude-sonnet-4-6',
  },
  {
    id: 'sess-xyz789uvw012',
    created_at: '2026-01-14T08:00:00Z',
    updated_at: '2026-01-14T09:30:00Z',
    model: 'claude-opus-4-6',
  },
]

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(SessionsView)
}

beforeEach(() => {
  mockApiSessionsList.mockReset().mockResolvedValue(sampleSessions)
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SessionsView', () => {
  it('calls api.sessions.list on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockApiSessionsList).toHaveBeenCalled()
  })

  it('renders session id slices in the list', async () => {
    const w = mountView()
    await flushPromises()
    // The template shows sess.id?.slice(0, 12) + '...'
    // 'sess-abc123def456'.slice(0, 12) === 'sess-abc123d'
    expect(w.text()).toContain('sess-abc123d')
    expect(w.text()).toContain('sess-xyz789u')
  })

  it('renders the model name for each session', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('claude-sonnet-4-6')
    expect(w.text()).toContain('claude-opus-4-6')
  })

  it('shows loading state while fetching', async () => {
    // Don't resolve immediately
    let resolve!: (v: unknown) => void
    mockApiSessionsList.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('Loading sessions')
    // Resolve so the component settles before teardown
    resolve(sampleSessions)
    await flushPromises()
  })

  it('shows empty state when no sessions are returned', async () => {
    mockApiSessionsList.mockResolvedValueOnce([])
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('No sessions found')
  })

  it('shows error message when api call fails', async () => {
    mockApiSessionsList.mockRejectedValueOnce(new Error('Failed to load sessions'))
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Failed to load sessions')
  })

  it('toggleExpand expands a session when clicked', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.expanded).toBe('')
    vm.toggleExpand('sess-abc123def456')
    await nextTick()
    expect(vm.expanded).toBe('sess-abc123def456')
  })

  it('toggleExpand collapses an already-expanded session', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.toggleExpand('sess-abc123def456')
    await nextTick()
    // Toggle again to collapse
    vm.toggleExpand('sess-abc123def456')
    await nextTick()
    expect(vm.expanded).toBe('')
  })

  it('toggleExpand switches focus to a different session', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.toggleExpand('sess-abc123def456')
    await nextTick()
    vm.toggleExpand('sess-xyz789uvw012')
    await nextTick()
    expect(vm.expanded).toBe('sess-xyz789uvw012')
  })

  it('refresh re-fetches sessions from the api', async () => {
    const w = mountView()
    await flushPromises()
    mockApiSessionsList.mockReset().mockResolvedValue([sampleSessions[0]!])
    await (w.vm as any).refresh()
    await flushPromises()
    expect(mockApiSessionsList).toHaveBeenCalledTimes(1)
    expect((w.vm as any).sessions).toHaveLength(1)
  })

  it('refresh clears previous error on retry', async () => {
    mockApiSessionsList.mockRejectedValueOnce(new Error('Network error'))
    const w = mountView()
    await flushPromises()
    expect((w.vm as any).error).toContain('Network error')

    // Now succeed on retry
    mockApiSessionsList.mockResolvedValueOnce(sampleSessions)
    await (w.vm as any).refresh()
    await flushPromises()
    expect((w.vm as any).error).toBe('')
  })

  it('formatDate returns empty string for undefined', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.formatDate(undefined)).toBe('')
  })

  it('formatDate returns a formatted date string for valid ISO date', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const result = vm.formatDate('2026-01-15T10:00:00Z')
    expect(typeof result).toBe('string')
    expect(result.length).toBeGreaterThan(0)
  })

  it('renders "sessions" label in the header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('sessions')
  })

  it('shows a refresh button', async () => {
    const w = mountView()
    await flushPromises()
    const button = w.find('button')
    expect(button.exists()).toBe(true)
    expect(button.text()).toContain('refresh')
  })
})
