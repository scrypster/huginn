import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp } from 'vue'
import { setToken } from '../useApi'
import { useWorkflows, type WorkflowRun } from '../useWorkflows'

function ok(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function makeWorkflow(overrides: Record<string, unknown> = {}) {
  return {
    id: 'wf-1',
    name: 'Test Workflow',
    enabled: true,
    schedule: '',
    steps: [],
    ...overrides,
  }
}

describe('useWorkflows', () => {
  beforeEach(() => {
    setToken('test-token')
    // Reset module-level state by clearing the workflows list
    const { workflows, liveEvents } = useWorkflows()
    workflows.value = []
    liveEvents.value = {}
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ── fetchWorkflows ──────────────────────────────────────────────────────────

  describe('fetchWorkflows', () => {
    it('populates the workflows list from the API response', async () => {
      const list = [makeWorkflow({ id: 'wf-1' }), makeWorkflow({ id: 'wf-2', name: 'Second' })]
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(list))

      const { workflows, fetchWorkflows } = useWorkflows()
      await fetchWorkflows()

      expect(workflows.value).toHaveLength(2)
      expect(workflows.value[0]!.id).toBe('wf-1')
      expect(workflows.value[1]!.id).toBe('wf-2')
    })

    it('sends Authorization header', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok([]))
      const { fetchWorkflows } = useWorkflows()
      await fetchWorkflows()

      const [, opts] = spy.mock.calls[0]!
      const headers = opts?.headers as Record<string, string>
      expect(headers['Authorization']).toBe('Bearer test-token')
    })

    it('sets workflows to empty array when response is not an array', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ error: 'oops' }))
      const { workflows, fetchWorkflows } = useWorkflows()
      workflows.value = [makeWorkflow() as any]
      await fetchWorkflows()
      expect(workflows.value).toHaveLength(0)
    })
  })

  // ── createWorkflow ──────────────────────────────────────────────────────────

  describe('createWorkflow', () => {
    it('returns a workflow with an id', async () => {
      const created = makeWorkflow({ id: 'wf-new' })
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(created))

      const { createWorkflow } = useWorkflows()
      const result = await createWorkflow({ name: 'Test Workflow' })

      expect(result.id).toBe('wf-new')
    })

    it('prepends the new workflow to the list', async () => {
      const existing = makeWorkflow({ id: 'wf-old' })
      const created = makeWorkflow({ id: 'wf-new' })

      const { workflows, createWorkflow } = useWorkflows()
      workflows.value = [existing as any]

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(created))
      await createWorkflow({ name: 'New Workflow' })

      expect(workflows.value).toHaveLength(2)
      expect(workflows.value[0]!.id).toBe('wf-new')
    })

    it('sends POST to /api/v1/workflows', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(makeWorkflow()))
      const { createWorkflow } = useWorkflows()
      await createWorkflow({ name: 'Test' })

      const [url, opts] = spy.mock.calls[0]!
      expect(url).toBe('/api/v1/workflows')
      expect(opts?.method).toBe('POST')
    })

    it('throws on non-ok response', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 500 }))
      const { createWorkflow } = useWorkflows()
      await expect(createWorkflow({ name: 'Bad' })).rejects.toThrow('create failed: 500')
    })

    it('serializes step inputs correctly in POST body', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(makeWorkflow()))
      const { createWorkflow } = useWorkflows()

      const stepWithInputs = {
        position: 0,
        inputs: [{ from_step: 'step1', as: 'result' }],
      }
      await createWorkflow({ name: 'Workflow With Inputs', steps: [stepWithInputs] })

      const [, opts] = spy.mock.calls[0]!
      const body = JSON.parse(opts?.body as string)
      expect(body.steps[0].inputs).toEqual([{ from_step: 'step1', as: 'result' }])
    })

    it('serializes step notify shape correctly in POST body', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(makeWorkflow()))
      const { createWorkflow } = useWorkflows()

      const stepWithNotify = {
        position: 0,
        notify: {
          on_success: true,
          deliver_to: [{ type: 'space', space_id: 'sp-1' }],
        },
      }
      await createWorkflow({ name: 'Workflow With Notify', steps: [stepWithNotify] })

      const [, opts] = spy.mock.calls[0]!
      const body = JSON.parse(opts?.body as string)
      expect(body.steps[0].notify).toEqual({
        on_success: true,
        deliver_to: [{ type: 'space', space_id: 'sp-1' }],
      })
    })
  })

  // ── updateWorkflow ──────────────────────────────────────────────────────────

  describe('updateWorkflow', () => {
    it('replaces the matching workflow in the list', async () => {
      const original = makeWorkflow({ id: 'wf-1', name: 'Original' })
      const updated = makeWorkflow({ id: 'wf-1', name: 'Updated' })

      const { workflows, updateWorkflow } = useWorkflows()
      workflows.value = [original as any]

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(updated))
      await updateWorkflow('wf-1', updated as any)

      expect(workflows.value[0]!.name).toBe('Updated')
    })

    it('sends PUT to /api/v1/workflows/{id}', async () => {
      const wf = makeWorkflow({ id: 'wf-1' })
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(wf))

      const { workflows, updateWorkflow } = useWorkflows()
      workflows.value = [wf as any]

      await updateWorkflow('wf-1', wf as any)

      const [url, opts] = spy.mock.calls[0]!
      expect(url).toBe('/api/v1/workflows/wf-1')
      expect(opts?.method).toBe('PUT')
    })

    it('guard: does NOT add to list when response lacks id', async () => {
      const wf = makeWorkflow({ id: 'wf-1' })
      const responseWithoutId = { name: 'No ID' }

      const { workflows, updateWorkflow } = useWorkflows()
      workflows.value = [wf as any]

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(responseWithoutId))
      await updateWorkflow('wf-1', wf as any)

      // list unchanged — guard prevents replacing when updated.id is missing
      expect(workflows.value).toHaveLength(1)
      expect(workflows.value[0]!.id).toBe('wf-1')
    })
  })

  // ── deleteWorkflow ──────────────────────────────────────────────────────────

  describe('deleteWorkflow', () => {
    it('removes the workflow from the list', async () => {
      const { workflows, deleteWorkflow } = useWorkflows()
      workflows.value = [makeWorkflow({ id: 'wf-1' }) as any, makeWorkflow({ id: 'wf-2' }) as any]

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({}))
      await deleteWorkflow('wf-1')

      expect(workflows.value).toHaveLength(1)
      expect(workflows.value[0]!.id).toBe('wf-2')
    })

    it('sends DELETE to /api/v1/workflows/{id}', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({}))
      const { workflows, deleteWorkflow } = useWorkflows()
      workflows.value = [makeWorkflow({ id: 'wf-1' }) as any]

      await deleteWorkflow('wf-1')

      const [url, opts] = spy.mock.calls[0]!
      expect(url).toBe('/api/v1/workflows/wf-1')
      expect(opts?.method).toBe('DELETE')
    })
  })

  // ── WorkflowRun status types ────────────────────────────────────────────────

  describe('WorkflowRun status types', () => {
    it("'partial' is a valid WorkflowRun status", () => {
      const status = 'partial' satisfies WorkflowRun['status']
      expect(status).toBe('partial')
    })

    it("'running', 'complete', 'failed', 'partial' are all valid statuses", () => {
      const statuses: WorkflowRun['status'][] = ['running', 'complete', 'failed', 'partial']
      expect(statuses).toHaveLength(4)
    })
  })

  // ── fetchTemplates ───────────────────────────────────────────────────────────

  describe('fetchTemplates', () => {
    it('fetches from /api/v1/workflows/templates and returns array', async () => {
      const templates = [{ id: 't1', name: 'Template 1' }]
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(templates))
      const { fetchTemplates } = useWorkflows()
      const result = await fetchTemplates()
      expect(spy.mock.calls[0]![0]).toBe('/api/v1/workflows/templates')
      expect(result).toHaveLength(1)
    })

    it('returns empty array when response is not an array', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ error: 'oops' }))
      const { fetchTemplates } = useWorkflows()
      const result = await fetchTemplates()
      expect(result).toEqual([])
    })

    it('returns empty array when fetch throws', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
      const { fetchTemplates } = useWorkflows()
      const result = await fetchTemplates()
      expect(result).toEqual([])
    })
  })

  // ── triggerWorkflow ──────────────────────────────────────────────────────────

  describe('triggerWorkflow', () => {
    it('POSTs to /api/v1/workflows/:id/run', async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ status: 'triggered' }))
      const { triggerWorkflow } = useWorkflows()
      await triggerWorkflow('wf-trigger-1')
      expect(spy.mock.calls[0]![0]).toBe('/api/v1/workflows/wf-trigger-1/run')
      expect((spy.mock.calls[0]![1] as RequestInit).method).toBe('POST')
    })

    it('throws when response is not ok', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 422 }))
      const { triggerWorkflow } = useWorkflows()
      await expect(triggerWorkflow('wf-trigger-2')).rejects.toThrow('trigger failed')
    })
  })

  // ── fetchWorkflowRuns ────────────────────────────────────────────────────────

  describe('fetchWorkflowRuns', () => {
    it('fetches from /api/v1/workflows/:id/runs and returns array', async () => {
      const runs = [{ id: 'run-1', status: 'complete' }]
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok(runs))
      const { fetchWorkflowRuns } = useWorkflows()
      const result = await fetchWorkflowRuns('wf-runs-1')
      expect(spy.mock.calls[0]![0]).toBe('/api/v1/workflows/wf-runs-1/runs')
      expect(result).toHaveLength(1)
    })

    it('returns empty array when fetch throws', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
      const { fetchWorkflowRuns } = useWorkflows()
      const result = await fetchWorkflowRuns('wf-runs-2')
      expect(result).toEqual([])
    })
  })

  // ── wireWS ───────────────────────────────────────────────────────────────────

  describe('wireWS', () => {
    function withSetup<T>(composable: () => T): { result: T; unmount: () => void } {
      let result!: T
      const app = createApp({ setup() { result = composable(); return () => null } })
      app.mount(document.createElement('div'))
      return { result, unmount: () => app.unmount() }
    }

    /**
     * makeFakeHuginnWS returns a minimal stand-in for the HuginnWS shape:
     * tests register handlers via on(type, fn) and trigger them via
     * fire(type, msg). Mirrors the real HuginnWS pub/sub contract.
     */
    function makeFakeHuginnWS() {
      const handlers: Record<string, Array<(m: any) => void>> = {}
      return {
        on(type: string, fn: (m: any) => void) {
          if (!handlers[type]) handlers[type] = []
          handlers[type]!.push(fn)
        },
        off(type: string, fn: (m: any) => void) {
          handlers[type] = (handlers[type] ?? []).filter(f => f !== fn)
        },
        fire(type: string, msg: any) {
          for (const fn of handlers[type] ?? []) fn(msg)
        },
        handlers,
      }
    }

    it('registers message handlers via ws.on() and routes workflow events into liveEvents', () => {
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-ws-1' })

      expect(liveEvents.value['wf-ws-1']).toHaveLength(1)
      expect(liveEvents.value['wf-ws-1']![0]!.type).toBe('workflow_started')
    })

    it('registers handlers for ALL workflow event types', () => {
      withSetup(() => {
        const { wireWS } = useWorkflows()
        const ws = makeFakeHuginnWS()
        wireWS(ws as any)

        // Every workflow_* event must be subscribed; otherwise the live
        // execution panel will silently drop events.
        const expected = [
          'workflow_started',
          'workflow_step_started',
          'workflow_step_token',
          'workflow_step_complete',
          'workflow_complete',
          'workflow_failed',
          'workflow_partial',
          'workflow_skipped',
          'workflow_cancelled',
        ]
        for (const t of expected) {
          expect(ws.handlers[t], `missing handler for ${t}`).toBeDefined()
          expect(ws.handlers[t]!.length).toBeGreaterThan(0)
        }
      })
    })

    it('ignores messages without a workflow_id (no cross-talk with other event types)', () => {
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      ws.fire('workflow_started', { type: 'workflow_started' /* no workflow_id */ })
      expect(liveEvents.value).toEqual({})
    })

    it('clears prior liveEvents for the same workflow on workflow_started (fresh-run hygiene)', async () => {
      // Stub fetch because workflow_complete triggers a workflows refetch.
      vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.resolve(ok([])))
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      // Simulate events from a completed prior run.
      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-fresh', run_id: 'run-1' })
      ws.fire('workflow_step_complete', { type: 'workflow_step_complete', workflow_id: 'wf-fresh', run_id: 'run-1', position: 0, status: 'success' })
      ws.fire('workflow_complete', { type: 'workflow_complete', workflow_id: 'wf-fresh', run_id: 'run-1' })
      expect(liveEvents.value['wf-fresh']!.length).toBeGreaterThanOrEqual(3)

      // A new run starts: prior events for this workflow are dropped.
      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-fresh', run_id: 'run-2' })

      expect(liveEvents.value['wf-fresh']).toHaveLength(1)
      expect(liveEvents.value['wf-fresh']![0]!.run_id).toBe('run-2')
      await Promise.resolve()
    })

    it('does NOT clear liveEvents for a different workflow on workflow_started', () => {
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-a', run_id: 'a-1' })
      ws.fire('workflow_step_complete', { type: 'workflow_step_complete', workflow_id: 'wf-a', run_id: 'a-1', position: 0, status: 'success' })
      // Now wf-b starts — should not affect wf-a.
      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-b', run_id: 'b-1' })

      expect(liveEvents.value['wf-a']).toHaveLength(2)
      expect(liveEvents.value['wf-b']).toHaveLength(1)
    })

    it('caps liveEvents per workflow at 100 entries (oldest dropped)', () => {
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      // workflow_started clears events; we only want to test the cap on
      // continuing-run events, so use workflow_step_complete only.
      for (let i = 0; i < 105; i++) {
        ws.fire('workflow_step_complete', {
          type: 'workflow_step_complete',
          workflow_id: 'wf-cap',
          run_id: `r-${i}`,
          position: i,
        })
      }
      expect(liveEvents.value['wf-cap']).toHaveLength(100)
      // Oldest dropped → first remaining is r-5.
      expect(liveEvents.value['wf-cap']![0]!.run_id).toBe('r-5')
      expect(liveEvents.value['wf-cap']![99]!.run_id).toBe('r-104')
    })

    it('refetches workflows on terminal events (complete/failed/partial/cancelled)', async () => {
      // Each fetch consumes the response body, so we return a fresh Response per call.
      const fetchSpy = vi
        .spyOn(globalThis, 'fetch')
        .mockImplementation(() => Promise.resolve(ok([])))
      const { result: { wireWS } } = withSetup(() => useWorkflows())
      const ws = makeFakeHuginnWS()
      wireWS(ws as any)

      const before = fetchSpy.mock.calls.length
      ws.fire('workflow_complete', { type: 'workflow_complete', workflow_id: 'wf-t' })
      ws.fire('workflow_failed', { type: 'workflow_failed', workflow_id: 'wf-t' })
      ws.fire('workflow_partial', { type: 'workflow_partial', workflow_id: 'wf-t' })
      ws.fire('workflow_cancelled', { type: 'workflow_cancelled', workflow_id: 'wf-t' })
      // workflow_started should NOT trigger a refetch.
      ws.fire('workflow_started', { type: 'workflow_started', workflow_id: 'wf-t' })

      // Each terminal event triggers exactly one fetch.
      expect(fetchSpy.mock.calls.length - before).toBe(4)
      // Drain microtasks so the promise resolutions complete and don't surface
      // as unhandled rejections in afterEach.
      await Promise.resolve()
    })
  })

  describe('replay / fork / diff', () => {
    it('replayWorkflowRun POSTs replay endpoint', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify({ status: 'triggered' }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
      )
      const { replayWorkflowRun } = useWorkflows()
      const out = await replayWorkflowRun('wf-1', 'run-abc')
      expect(out.status).toBe('triggered')
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/v1/workflows/wf-1/runs/run-abc/replay',
        expect.objectContaining({ method: 'POST' }),
      )
      fetchSpy.mockRestore()
    })

    it('forkWorkflowRun POSTs JSON body', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify({ status: 'triggered' }), { status: 200 }),
      )
      const { forkWorkflowRun } = useWorkflows()
      await forkWorkflowRun('wf-1', 'run-abc', { inputs: { a: '1' }, use_live_definition: true })
      const [, init] = fetchSpy.mock.calls[0]!
      expect(init?.method).toBe('POST')
      expect(JSON.parse(String(init?.body))).toEqual({ inputs: { a: '1' }, use_live_definition: true })
      fetchSpy.mockRestore()
    })

    it('diffWorkflowRuns GETs diff endpoint', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
        new Response(JSON.stringify({ ok: true }), { status: 200 }),
      )
      const { diffWorkflowRuns } = useWorkflows()
      const d = await diffWorkflowRuns('wf-1', 'run-a', 'run-b')
      expect(d).toEqual({ ok: true })
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/v1/workflows/wf-1/runs/run-a/diff/run-b',
        expect.anything(),
      )
      fetchSpy.mockRestore()
    })

    it('fetchSessionArtifacts GETs session artifacts list', async () => {
      const rows = [{ id: 'a1', kind: 'file', title: 'out.csv', agent_name: 'x', session_id: 's1', status: 'accepted', created_at: '', updated_at: '' }]
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(rows))
      const { fetchSessionArtifacts } = useWorkflows()
      const got = await fetchSessionArtifacts('sess-1')
      expect(got).toEqual(rows)
      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/v1/sessions/sess-1/artifacts',
        expect.anything(),
      )
      fetchSpy.mockRestore()
    })
  })
})
