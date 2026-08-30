import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { apiFetch as ApiFetchType } from '../useApi'
import type { RunRecord } from '../useCheckpoints'

vi.mock('../useApi', () => ({
  apiFetch: vi.fn(),
  ensureToken: vi.fn().mockResolvedValue('tok'),
  getToken: vi.fn().mockReturnValue('tok'),
}))

function makeRun(overrides: Partial<RunRecord> = {}): RunRecord {
  return {
    ThreadID: 'thread-1',
    AgentID: 'Coder',
    TaskSummary: 'Fix the bug',
    Status: 'completed',
    PreSnapshot: 'abc123',
    PostSnapshot: 'def456',
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

// useCheckpoints keeps its state at module scope (shared singleton, same
// pattern as useSessions.ts) so every test resets the module registry and
// re-imports fresh — otherwise `loaded`/`runs` would leak across tests.
async function freshComposable() {
  vi.resetModules()
  const apiModule = await import('../useApi')
  const { useCheckpoints } = await import('../useCheckpoints')
  return { apiFetch: apiModule.apiFetch as unknown as typeof ApiFetchType & ReturnType<typeof vi.fn>, ...useCheckpoints() }
}

describe('useCheckpoints', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('fetches runs and exposes them via runs/loading state', async () => {
    const { apiFetch, fetchRuns, runs, loading, error } = await freshComposable()
    const run = makeRun()
    vi.mocked(apiFetch).mockResolvedValueOnce([run])

    const p = fetchRuns()
    expect(loading.value).toBe(true)
    await p

    expect(loading.value).toBe(false)
    expect(error.value).toBe('')
    expect(runs.value).toEqual([run])
    expect(apiFetch).toHaveBeenCalledWith('/api/v1/checkpoints/?limit=100')
  })

  it('sets error on a failed fetch', async () => {
    const { apiFetch, fetchRuns, error, loading } = await freshComposable()
    vi.mocked(apiFetch).mockRejectedValueOnce(new Error('boom'))
    await fetchRuns()
    expect(error.value).toBe('boom')
    expect(loading.value).toBe(false)
  })

  it('correlates a run to its thread id via getRunForThread', async () => {
    const { apiFetch, fetchRuns, getRunForThread } = await freshComposable()
    const runA = makeRun({ ThreadID: 'thread-a' })
    const runB = makeRun({ ThreadID: 'thread-b', Status: 'capture_failed', CaptureError: 'snapshot failed' })
    vi.mocked(apiFetch).mockResolvedValueOnce([runA, runB])
    await fetchRuns()

    expect(getRunForThread('thread-a')?.ThreadID).toBe('thread-a')
    expect(getRunForThread('thread-b')?.Status).toBe('capture_failed')
    expect(getRunForThread('thread-nonexistent')).toBeUndefined()
  })

  it('ensureLoaded fetches once and is a no-op on repeat calls', async () => {
    const { apiFetch, ensureLoaded } = await freshComposable()
    vi.mocked(apiFetch).mockResolvedValue([makeRun()])
    await ensureLoaded()
    await ensureLoaded()
    await ensureLoaded()
    expect(apiFetch).toHaveBeenCalledTimes(1)
  })

  it('revert posts the honesty-relevant options and refreshes the list', async () => {
    const { apiFetch, revert } = await freshComposable()
    const revertResult = {
      Restored: ['a.go'],
      Deleted: [],
      SkippedEdited: ['c.go'],
      NotRestorable: [],
      Failed: {},
      Warning: '',
      NothingCaptured: false,
    }
    vi.mocked(apiFetch)
      .mockResolvedValueOnce(revertResult) // POST revert
      .mockResolvedValueOnce([makeRun({ Status: 'reverted' })]) // refresh fetchRuns

    const result = await revert('thread-1', { all: false, allow_after_push: true })

    expect(apiFetch).toHaveBeenNthCalledWith(1, '/api/v1/checkpoints/thread-1/revert', {
      method: 'POST',
      body: JSON.stringify({ all: false, only_paths: [], allow_after_push: true }),
    })
    expect(result).toEqual(revertResult)
    // Refetched the ledger after reverting.
    expect(apiFetch).toHaveBeenNthCalledWith(2, '/api/v1/checkpoints/?limit=100')
  })

  it('fetchDiff performs a raw text fetch with bearer auth, not apiFetch/json', async () => {
    const { fetchDiff } = await freshComposable()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      text: () => Promise.resolve('diff --git a/x b/x\n+hello'),
    })
    vi.stubGlobal('fetch', fetchMock)

    const text = await fetchDiff('thread-1')

    expect(text).toContain('+hello')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/checkpoints/thread-1/diff',
      expect.objectContaining({ headers: { Authorization: 'Bearer tok' } }),
    )
    vi.unstubAllGlobals()
  })

  it('fetchDiff throws with the response body on a non-ok response', async () => {
    const { fetchDiff } = await freshComposable()
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      text: () => Promise.resolve('checkpoint: no run recorded for this thread'),
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchDiff('missing')).rejects.toThrow(/no run recorded/)
    vi.unstubAllGlobals()
  })
})
