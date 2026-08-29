import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import type { RunRecord } from '../../../composables/useCheckpoints'

const runs = ref<RunRecord[]>([])
const loading = ref(false)
const error = ref('')
const fetchRuns = vi.fn().mockResolvedValue(undefined)
const fetchDiff = vi.fn()
const revert = vi.fn()

vi.mock('../../../composables/useCheckpoints', () => ({
  useCheckpoints: () => ({ runs, loading, error, fetchRuns, fetchDiff, revert }),
}))

import SettingsCheckpointsTab from '../SettingsCheckpointsTab.vue'

function makeRun(overrides: Partial<RunRecord> = {}): RunRecord {
  return {
    ThreadID: 'thread-1',
    AgentID: 'Coder',
    TaskSummary: 'Fix the bug',
    Status: 'completed',
    PreSnapshot: 'abc',
    PostSnapshot: 'def',
    TouchedPaths: ['a.go', 'b.go'],
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

describe('SettingsCheckpointsTab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    runs.value = []
    loading.value = false
    error.value = ''
  })

  it('shows an empty state with no runs recorded', async () => {
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.text()).toContain('No runs recorded yet')
    expect(fetchRuns).toHaveBeenCalled()
  })

  it('renders newest-first with agent, when, and files-touched count', async () => {
    runs.value = [
      makeRun({ ThreadID: 't-old', CreatedAt: '2026-08-01T00:00:00Z' }),
      makeRun({ ThreadID: 't-new', CreatedAt: '2026-08-02T00:00:00Z' }),
    ]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    const rows = w.findAll('[data-testid="checkpoint-run-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('t-new')
    expect(rows[1].text()).toContain('t-old')
    expect(rows[0].text()).toContain('2 files touched')
  })

  it('shows a "protected" chip for a completed run', async () => {
    runs.value = [makeRun()]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-chip-protected"]').exists()).toBe(true)
  })

  it('shows a "capture failed — not protected" chip and disables actions for a capture_failed run', async () => {
    runs.value = [makeRun({ Status: 'capture_failed', CaptureError: 'disk full' })]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-chip-capture-failed"]').text()).toContain('not protected')
    const diffBtn = w.find('[data-testid="checkpoint-view-diff"]')
    const revertBtn = w.find('[data-testid="checkpoint-revert-open"]')
    expect((diffBtn.element as HTMLButtonElement).disabled).toBe(true)
    expect((revertBtn.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows a "reverted" chip for a reverted run', async () => {
    runs.value = [makeRun({ Status: 'reverted' })]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-chip-reverted"]').exists()).toBe(true)
  })

  it('shows an additional "pushed" chip alongside the status chip', async () => {
    runs.value = [makeRun({ Pushed: true })]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-chip-protected"]').exists()).toBe(true)
    expect(w.find('[data-testid="checkpoint-chip-pushed"]').exists()).toBe(true)
  })

  it('expands the diff view on click and renders the fetched unified diff', async () => {
    runs.value = [makeRun()]
    fetchDiff.mockResolvedValue('diff --git a/a.go b/a.go\n+added line\n-removed line')
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()

    await w.find('[data-testid="checkpoint-view-diff"]').trigger('click')
    await flushPromises()

    expect(fetchDiff).toHaveBeenCalledWith('thread-1')
    const body = w.find('[data-testid="checkpoint-diff-body"]')
    expect(body.exists()).toBe(true)
    expect(body.text()).toContain('added line')
    expect(body.text()).toContain('removed line')
  })

  it('opens the revert dialog for a revertable run', async () => {
    runs.value = [makeRun()]
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-revert-dialog"]').exists()).toBe(false)
    await w.find('[data-testid="checkpoint-revert-open"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="checkpoint-revert-dialog"]').exists()).toBe(true)
  })

  it('surfaces a load error', async () => {
    error.value = 'failed to reach checkpoints API'
    const w = mount(SettingsCheckpointsTab)
    await flushPromises()
    expect(w.find('[data-testid="checkpoints-load-error"]').text()).toContain('failed to reach checkpoints API')
  })
})
