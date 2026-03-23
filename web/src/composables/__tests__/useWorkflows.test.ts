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

    it('registers message handler and adds workflow events to liveEvents', () => {
      const { result: { wireWS, liveEvents } } = withSetup(() => useWorkflows())
      const handlers: Record<string, (e: MessageEvent) => void> = {}
      const ws = { addEventListener: (ev: string, h: any) => { handlers[ev] = h }, removeEventListener: vi.fn() }
      wireWS(ws as any)
      handlers['message']!(new MessageEvent('message', {
        data: JSON.stringify({ type: 'workflow_started', workflow_id: 'wf-ws-1' }),
      }))
      expect(liveEvents.value['wf-ws-1']).toHaveLength(1)
      expect(liveEvents.value['wf-ws-1']![0].type).toBe('workflow_started')
    })
  })
})
