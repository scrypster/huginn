import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { RunRecord, RevertResult } from '../../composables/useCheckpoints'

const revert = vi.fn<(threadId: string, opts: unknown) => Promise<RevertResult>>()

vi.mock('../../composables/useCheckpoints', () => ({
  useCheckpoints: () => ({ revert }),
}))

import CheckpointRevertDialog from '../CheckpointRevertDialog.vue'

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

function fullResult(overrides: Partial<RevertResult> = {}): RevertResult {
  return {
    Restored: ['a.go', 'b.go'],
    Deleted: ['c.go'],
    SkippedEdited: ['d.go'],
    NotRestorable: ['e.db (database row)'],
    Failed: { 'f.go': 'permission denied' },
    Warning: '1 file(s) were hand-edited after this run and were left alone.',
    NothingCaptured: false,
    ...overrides,
  }
}

describe('CheckpointRevertDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('disables the Revert button until the confirm checkbox is checked', async () => {
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })
    const btn = w.find('[data-testid="checkpoint-revert-confirm-btn"]')
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)

    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    expect((btn.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('never calls revert on click while the button is disabled', async () => {
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })
    await w.find('[data-testid="checkpoint-revert-confirm-btn"]').trigger('click')
    expect(revert).not.toHaveBeenCalled()
  })

  it('requires the extra pushed checkbox before Revert is enabled on a pushed run', async () => {
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun({ Pushed: true }) } })
    const btn = w.find('[data-testid="checkpoint-revert-confirm-btn"]')

    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    // Base confirm alone is not enough for a pushed run.
    expect((btn.element as HTMLButtonElement).disabled).toBe(true)

    await w.find('[data-testid="checkpoint-revert-allow-pushed"]').setValue(true)
    expect((btn.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('states files-only / hand-edit-preserving semantics in the confirmation copy', () => {
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })
    expect(w.text()).toContain('working tree')
    expect(w.text()).toContain('left alone')
    expect(w.text()).toContain('Preserve hand-edits')
    expect(w.text()).toContain('Restore everything')
  })

  it('shows the pushed warning copy only for a pushed run', () => {
    const notPushed = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun({ Pushed: false }) } })
    expect(notPushed.find('[data-testid="checkpoint-revert-allow-pushed"]').exists()).toBe(false)

    const pushed = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun({ Pushed: true }) } })
    expect(pushed.text()).toContain('already pushed')
    expect(pushed.find('[data-testid="checkpoint-revert-allow-pushed"]').exists()).toBe(true)
  })

  it('calls revert with all/allow_after_push mapped from the chosen options', async () => {
    revert.mockResolvedValue(fullResult())
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun({ Pushed: true }) } })

    await w.find('[data-testid="checkpoint-revert-mode-all"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-allow-pushed"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-confirm-btn"]').trigger('click')
    await flushPromises()

    expect(revert).toHaveBeenCalledWith('thread-1', { all: true, allow_after_push: true })
  })

  it('renders every honesty field of the RevertResult after a revert — not a truncated toast', async () => {
    revert.mockResolvedValue(fullResult())
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })

    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-confirm-btn"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid="checkpoint-result-restored-count"]').text()).toBe('2')
    expect(w.find('[data-testid="checkpoint-result-deleted-count"]').text()).toBe('1')
    expect(w.text()).toContain('a.go')
    expect(w.text()).toContain('c.go')
    expect(w.find('[data-testid="checkpoint-result-skipped-edited"]').text()).toContain('d.go')
    expect(w.find('[data-testid="checkpoint-result-not-restorable"]').text()).toContain('e.db')
    expect(w.find('[data-testid="checkpoint-result-failed"]').text()).toContain('f.go')
    expect(w.find('[data-testid="checkpoint-result-failed"]').text()).toContain('permission denied')
    expect(w.find('[data-testid="checkpoint-result-warning"]').text()).toContain('hand-edited')
  })

  it('leads with a NothingCaptured warning instead of a success headline', async () => {
    revert.mockResolvedValue(fullResult({
      Restored: [], Deleted: [], SkippedEdited: [], NotRestorable: [], Failed: {}, NothingCaptured: true,
      Warning: 'Nothing was captured for this run.',
    }))
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })
    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-confirm-btn"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-testid="checkpoint-result-nothing-captured"]').exists()).toBe(true)
  })

  it('surfaces a revert error inline instead of throwing silently', async () => {
    revert.mockRejectedValue(new Error('checkpoint: this run was already pushed'))
    const w = mount(CheckpointRevertDialog, { props: { threadId: 'thread-1', run: makeRun() } })
    await w.find('[data-testid="checkpoint-revert-confirm"]').setValue(true)
    await w.find('[data-testid="checkpoint-revert-confirm-btn"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('already pushed')
    // Still on the confirmation state, not silently swallowed as a result.
    expect(w.find('[data-testid="checkpoint-result-restored-count"]').exists()).toBe(false)
  })
})
