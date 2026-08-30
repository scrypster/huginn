import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { RunRecord } from '../../composables/useCheckpoints'

const ensureLoaded = vi.fn().mockResolvedValue(undefined)
const getRunForThread = vi.fn<(id: string) => RunRecord | undefined>()
const fetchRuns = vi.fn().mockResolvedValue(undefined)

vi.mock('../../composables/useCheckpoints', () => ({
  useCheckpoints: () => ({ ensureLoaded, getRunForThread, fetchRuns }),
}))

import CheckpointBadge from '../CheckpointBadge.vue'

function makeRun(overrides: Partial<RunRecord> = {}): RunRecord {
  return {
    ThreadID: 'thread-1',
    AgentID: 'Coder',
    TaskSummary: 'Fix the bug',
    Status: 'completed',
    PreSnapshot: 'abc',
    PostSnapshot: 'def',
    TouchedPaths: ['a.go'],
    Pushed: false,
    PRURL: '',
    CreatedAt: '2026-08-01T00:00:00Z',
    CompletedAt: '2026-08-01T00:05:00Z',
    CaptureError: '',
    IgnoredAtBegin: [],
    IgnoredTouched: [],
    ...overrides,
  }
}

describe('CheckpointBadge', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ensureLoaded.mockResolvedValue(undefined)
    fetchRuns.mockResolvedValue(undefined)
  })

  it('renders nothing when no checkpoint run exists for the thread', async () => {
    getRunForThread.mockReturnValue(undefined)
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-x' } })
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-badge"]').exists()).toBe(false)
  })

  it('shows an honest amber "Not snapshotted" marker when capture failed', async () => {
    getRunForThread.mockReturnValue(makeRun({ Status: 'capture_failed', CaptureError: 'disk full' }))
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1' } })
    await flushPromises()
    const marker = w.find('[data-testid="checkpoint-badge-not-protected"]')
    expect(marker.exists()).toBe(true)
    expect(marker.text()).toContain('Not snapshotted')
    // No undo affordance offered for an unprotected run.
    expect(w.find('[data-testid="checkpoint-badge-undo"]').exists()).toBe(false)
  })

  it('shows "Snapshotted" with an Undo action for a protected run', async () => {
    getRunForThread.mockReturnValue(makeRun())
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1' } })
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-badge-protected"]').text()).toContain('Snapshotted')
    expect(w.find('[data-testid="checkpoint-badge-undo"]').exists()).toBe(true)
  })

  it('shows a reverted marker with no undo action once already reverted', async () => {
    getRunForThread.mockReturnValue(makeRun({ Status: 'reverted' }))
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1' } })
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-badge-reverted"]').text()).toContain('Reverted')
    expect(w.find('[data-testid="checkpoint-badge-undo"]').exists()).toBe(false)
  })

  it('opens the revert dialog when Undo is clicked', async () => {
    getRunForThread.mockReturnValue(makeRun())
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1' } })
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-revert-dialog"]').exists()).toBe(false)
    await w.find('[data-testid="checkpoint-badge-undo"]').trigger('click')
    expect(w.find('[data-testid="checkpoint-revert-dialog"]').exists()).toBe(true)
  })

  it('marks a pushed run distinctly without hiding the undo action', async () => {
    getRunForThread.mockReturnValue(makeRun({ Pushed: true }))
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1' } })
    await flushPromises()
    expect(w.text()).toContain('pushed')
    expect(w.find('[data-testid="checkpoint-badge-undo"]').exists()).toBe(true)
  })

  // F4: a run that finishes AFTER the checkpoints list's first fetch must
  // still get a badge without a page reload. useCheckpoints.fetchRuns is a
  // module-singleton fetch-once composable — CheckpointBadge must re-fetch
  // when ITS OWN thread's `done` flag (mirrors DelegatedThread.done,
  // ChatView's existing WS-driven completion source) transitions to true.
  it('refetches checkpoints when this thread transitions to done after mount', async () => {
    getRunForThread.mockReturnValue(undefined)
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1', done: false } })
    await flushPromises()
    expect(fetchRuns).not.toHaveBeenCalled()

    await w.setProps({ done: true })
    await flushPromises()
    expect(fetchRuns).toHaveBeenCalledTimes(1)
  })

  it('does not refetch on mount when the thread is already done', async () => {
    getRunForThread.mockReturnValue(makeRun())
    mount(CheckpointBadge, { props: { threadId: 'thread-1', done: true } })
    await flushPromises()
    expect(fetchRuns).not.toHaveBeenCalled()
  })

  it('does not refetch again on further no-op done updates', async () => {
    getRunForThread.mockReturnValue(undefined)
    const w = mount(CheckpointBadge, { props: { threadId: 'thread-1', done: false } })
    await w.setProps({ done: true })
    await flushPromises()
    expect(fetchRuns).toHaveBeenCalledTimes(1)

    await w.setProps({ done: true })
    await flushPromises()
    expect(fetchRuns).toHaveBeenCalledTimes(1)
  })
})
