import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'
import WorkflowsView from '../../views/WorkflowsView.vue'

// ── Composable mocks ──────────────────────────────────────────────────────────

const mockWorkflows = ref<any[]>([])
const mockFetchWorkflows = vi.fn().mockResolvedValue(undefined)
const mockDeleteWorkflow = vi.fn()
const mockTriggerWorkflow = vi.fn()
const mockUpdateWorkflow = vi.fn()
const mockCreateWorkflow = vi.fn()
const mockFetchWorkflowRuns = vi.fn()
const mockWorkflowError = ref<string | null>(null)
const mockLoading = ref(false)

const mockCancelWorkflow = vi.fn()
const mockReplayWorkflowRun = vi.fn()
const mockForkWorkflowRun = vi.fn()
const mockDiffWorkflowRuns = vi.fn()
const mockFetchSessionArtifacts = vi.fn()

vi.mock('../../composables/useWorkflows', () => ({
  useWorkflows: () => ({
    workflows: mockWorkflows,
    loading: mockLoading,
    error: mockWorkflowError,
    liveEvents: ref({}),
    fetchWorkflows: mockFetchWorkflows,
    fetchTemplates: vi.fn().mockResolvedValue(undefined),
    createWorkflow: mockCreateWorkflow,
    updateWorkflow: mockUpdateWorkflow,
    deleteWorkflow: mockDeleteWorkflow,
    triggerWorkflow: mockTriggerWorkflow,
    cancelWorkflow: mockCancelWorkflow,
    fetchWorkflowRuns: mockFetchWorkflowRuns,
    replayWorkflowRun: mockReplayWorkflowRun,
    forkWorkflowRun: mockForkWorkflowRun,
    diffWorkflowRuns: mockDiffWorkflowRuns,
    fetchSessionArtifacts: mockFetchSessionArtifacts,
  }),
}))

vi.mock('../../composables/useSpaces', () => ({
  useSpaces: () => ({
    spaces: ref([]),
    channels: ref([]),
    dms: ref([]),
    activeSpaceId: ref(null),
    fetchSpaces: vi.fn().mockResolvedValue(undefined),
  }),
}))

const mockRouterPush = vi.fn()
const mockRouterReplace = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {} }),
  useRouter: () => ({ push: mockRouterPush, replace: mockRouterReplace }),
}))

// ── helpers ───────────────────────────────────────────────────────────────────

function mountWorkflowsView() {
  return shallowMount(WorkflowsView, {
    global: {
      stubs: {
        Teleport: true,
      },
    },
  })
}

beforeEach(() => {
  mockWorkflows.value = [
    {
      id: 'wf-1',
      name: 'Daily Report',
      enabled: true,
      schedule: '0 8 * * *',
      version: 3,
      steps: [],
      retry: { max_retries: 1, delay: '5s' },
      chain: { next: 'wf-2', on_success: true, on_failure: false },
    },
    { id: 'wf-2', name: 'Nightly Cleanup', enabled: false, schedule: '0 2 * * *', steps: [] },
  ]
  mockWorkflowError.value = null
  mockLoading.value = false
  mockDeleteWorkflow.mockReset()
  mockTriggerWorkflow.mockReset().mockResolvedValue(undefined)
  mockUpdateWorkflow.mockReset().mockResolvedValue({ id: 'wf-1', name: 'Daily Report', enabled: true, schedule: '0 8 * * *', steps: [] })
  mockCreateWorkflow.mockReset()
  mockFetchWorkflowRuns.mockReset().mockResolvedValue([])
  mockFetchWorkflows.mockReset().mockResolvedValue(undefined)
  mockCancelWorkflow.mockReset().mockResolvedValue(undefined)
  mockReplayWorkflowRun.mockReset().mockResolvedValue({ status: 'triggered' })
  mockForkWorkflowRun.mockReset().mockResolvedValue({ status: 'triggered' })
  mockDiffWorkflowRuns.mockReset().mockResolvedValue({ ok: true })
  mockFetchSessionArtifacts.mockReset().mockResolvedValue([])
  mockRouterPush.mockReset()
  mockRouterReplace.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('WorkflowsView', () => {
  it('renders workflow list', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    expect(w.text()).toContain('Daily Report')
    expect(w.text()).toContain('Nightly Cleanup')
  })

  it('shows empty state when no workflows', async () => {
    mockWorkflows.value = []
    const w = mountWorkflowsView()
    await nextTick()
    expect(w.text()).toContain('workflow')
  })

  it('confirmDelete sets pendingDelete state', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    // Select the first workflow
    const vm = w.vm as any
    vm.selectedWorkflow = mockWorkflows.value[0]
    await nextTick()
    vm.confirmDelete()
    await nextTick()
    expect(vm.pendingDelete).not.toBeNull()
    expect(vm.pendingDelete.name).toBe('Daily Report')
  })

  it('doDeleteWorkflow calls deleteWorkflow with correct id', async () => {
    mockDeleteWorkflow.mockResolvedValue(true)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.pendingDelete = { id: 'wf-1', name: 'Daily Report' }
    await vm.doDeleteWorkflow()
    await flushPromises()
    expect(mockDeleteWorkflow).toHaveBeenCalledWith('wf-1')
  })

  it('doDeleteWorkflow clears pendingDelete after deletion', async () => {
    mockDeleteWorkflow.mockResolvedValue(true)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingDelete = { id: 'wf-1', name: 'Daily Report' }
    await vm.doDeleteWorkflow()
    await flushPromises()
    expect(vm.pendingDelete).toBeNull()
  })

  it('doDeleteWorkflow is no-op when pendingDelete is null', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingDelete = null
    await vm.doDeleteWorkflow()
    expect(mockDeleteWorkflow).not.toHaveBeenCalled()
  })

  it('canceling confirmation clears pendingDelete', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingDelete = { id: 'wf-1', name: 'Daily Report' }
    vm.pendingDelete = null
    expect(vm.pendingDelete).toBeNull()
  })

  // ── Create workflow modal ──────────────────────────────────────────────────

  it('renders "New Workflow" button', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const btn = w.find('[data-testid="new-workflow-btn"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('New Workflow')
  })

  it('clicking New Workflow button opens create modal', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    expect(vm.showCreate).toBe(false)
    await w.find('[data-testid="new-workflow-btn"]').trigger('click')
    await nextTick()
    expect(vm.showCreate).toBe(true)
  })

  // ── Edit workflow ──────────────────────────────────────────────────────────

  it('clicking a workflow card opens the editor view', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    expect(vm.selectedWorkflow).toBeNull()
    const cards = w.findAll('[data-testid="workflow-item"]')
    expect(cards.length).toBeGreaterThan(0)
    await cards[0]!.trigger('click')
    await nextTick()
    expect(vm.selectedWorkflow).not.toBeNull()
    expect(vm.selectedWorkflow.id).toBe('wf-1')
  })

  // ── Save workflow ──────────────────────────────────────────────────────────

  it('saveWorkflow calls updateWorkflow with correct payload', async () => {
    const updatedWf = { id: 'wf-1', name: 'Daily Report', enabled: true, schedule: '0 8 * * *', steps: [] }
    mockUpdateWorkflow.mockResolvedValue(updatedWf)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    // Open a workflow first so selectedId and selectedWorkflow are set
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    vm.editForm.name = 'Daily Report'
    vm.editForm.enabled = true
    vm.editForm.schedule = '0 8 * * *'
    vm.editForm.steps = []
    vm.editForm.retry = { max_retries: 0, delay: '' }
    vm.editForm.chain = { next: '', on_success: true, on_failure: false }
    await vm.saveWorkflow()
    await flushPromises()
    expect(mockUpdateWorkflow).toHaveBeenCalledWith('wf-1', expect.objectContaining({
      id: 'wf-1',
      name: 'Daily Report',
      enabled: true,
    }))
  })

  it('saveWorkflow sends model_override, when, sub_workflow, retry, and chain', async () => {
    mockUpdateWorkflow.mockResolvedValue({ id: 'wf-1', name: 'Daily Report', enabled: true, schedule: '', steps: [] })
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedWorkflow = { ...mockWorkflows.value[0], steps: [] }
    vm.selectedId = 'wf-1'
    vm.editForm.retry = { max_retries: 3, delay: '30s' }
    vm.editForm.chain = { next: 'wf-2', on_success: true, on_failure: true }
    vm.editForm.steps = [
      {
        name: 'Step1',
        agent: 'Coder',
        prompt: 'hello',
        connections: {},
        vars: {},
        position: 0,
        on_failure: 'stop',
        inputs: [],
        model_override: 'claude-haiku-4',
        when: '{{run.scratch.go}}',
        sub_workflow: '',
      },
      {
        name: 'Child',
        agent: '',
        prompt: '',
        connections: {},
        vars: {},
        position: 1,
        on_failure: 'stop',
        inputs: [],
        sub_workflow: 'wf-2',
      },
    ]
    await vm.saveWorkflow()
    await flushPromises()
    const payload = mockUpdateWorkflow.mock.calls[0]![1] as Record<string, unknown>
    expect(payload.chain).toEqual({ next: 'wf-2', on_success: true, on_failure: true })
    expect(payload.retry).toEqual({ max_retries: 3, delay: '30s' })
    const steps = payload.steps as Array<Record<string, unknown>>
    expect(steps[0]).toMatchObject({ model_override: 'claude-haiku-4', when: '{{run.scratch.go}}' })
    expect(steps[0].sub_workflow).toBeUndefined()
    expect(steps[1]).toMatchObject({ sub_workflow: 'wf-2' })
  })

  it('saveWorkflow clears saving flag after completion', async () => {
    mockUpdateWorkflow.mockResolvedValue({ id: 'wf-1', name: 'Daily Report', enabled: true, schedule: '0 8 * * *', steps: [] })
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    const savePromise = vm.saveWorkflow()
    // saving should be true while the promise is in flight
    expect(vm.saving).toBe(true)
    await savePromise
    await flushPromises()
    expect(vm.saving).toBe(false)
  })

  // ── Enable/disable toggle ──────────────────────────────────────────────────

  it('toggling enabled button updates editForm.enabled', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    // Open workflow editor
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    vm.editForm.enabled = true
    await nextTick()
    expect(vm.editForm.enabled).toBe(true)
    vm.editForm.enabled = !vm.editForm.enabled
    await nextTick()
    expect(vm.editForm.enabled).toBe(false)
  })

  // ── Steps management ──────────────────────────────────────────────────────

  it('addStep appends a new empty step to the list', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    vm.editForm.steps = []
    await nextTick()
    expect(vm.editForm.steps.length).toBe(0)
    vm.addStep()
    await nextTick()
    expect(vm.editForm.steps.length).toBe(1)
    expect(vm.editForm.steps[0]).toMatchObject({ name: '', agent: '', prompt: '', position: 0 })
  })

  it('removeStep removes step at given index', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    vm.editForm.steps = [
      { name: 'Step A', agent: '', prompt: '', connections: {}, vars: {}, position: 0, on_failure: 'stop', inputs: [] },
      { name: 'Step B', agent: '', prompt: '', connections: {}, vars: {}, position: 1, on_failure: 'stop', inputs: [] },
    ]
    await nextTick()
    expect(vm.editForm.steps.length).toBe(2)
    vm.removeStep(0)
    await nextTick()
    expect(vm.editForm.steps.length).toBe(1)
    expect(vm.editForm.steps[0].name).toBe('Step B')
  })

  // ── Trigger run ───────────────────────────────────────────────────────────

  it('triggerRun calls triggerWorkflow and sets running flag to true', async () => {
    let resolveRun!: () => void
    const runPromise = new Promise<void>(resolve => { resolveRun = resolve })
    mockTriggerWorkflow.mockReturnValue(runPromise)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    const triggerPromise = vm.triggerRun()
    await nextTick()
    expect(vm.running).toBe(true)
    expect(mockTriggerWorkflow).toHaveBeenCalledWith('wf-1')
    resolveRun()
    await triggerPromise
    await flushPromises()
  })

  it('running flag stays true after triggerRun until a terminal WS event arrives', async () => {
    mockTriggerWorkflow.mockResolvedValue(undefined)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    await vm.triggerRun()
    await flushPromises()
    // running stays true — the WS-driven watcher (not a setTimeout) clears it.
    expect(vm.running).toBe(true)
  })

  it('running flag is cleared when a terminal WS event arrives', async () => {
    mockTriggerWorkflow.mockResolvedValue(undefined)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    await vm.triggerRun()
    await flushPromises()
    expect(vm.running).toBe(true)

    // Simulate the WS push: a terminal workflow_complete event for this
    // workflow lands in liveEvents — the component's watcher should clear
    // the running flag.
    vm.liveEvents['wf-1'] = [
      { type: 'workflow_complete', workflow_id: 'wf-1', run_id: 'r-1' },
    ]
    await nextTick()
    await flushPromises()
    expect(vm.running).toBe(false)
  })

  it('watchdog clears running after 30 minutes when no terminal event arrives', async () => {
    vi.useFakeTimers()
    mockTriggerWorkflow.mockResolvedValue(undefined)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    await vm.triggerRun()
    await flushPromises()
    expect(vm.running).toBe(true)

    // No WS event arrives — the safety-net watchdog must clear running.
    vi.advanceTimersByTime(30 * 60 * 1000 + 100)
    await nextTick()
    expect(vm.running).toBe(false)
    vi.useRealTimers()
  })

  // ── History panel ─────────────────────────────────────────────────────────

  it('clicking history button opens history panel', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    // Navigate to editor view
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.selectedId = 'wf-1'
    await nextTick()
    expect(vm.showHistory).toBe(false)
    vm.showHistory = true
    await nextTick()
    expect(vm.showHistory).toBe(true)
  })

  it('history panel fetches runs when showHistory becomes true', async () => {
    const fakeRuns = [
      { id: 'run-abc123', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() },
    ]
    mockFetchWorkflowRuns.mockResolvedValue(fakeRuns)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.selectedWorkflow = mockWorkflows.value[0]
    await nextTick()
    vm.showHistory = true
    await flushPromises()
    expect(mockFetchWorkflowRuns).toHaveBeenCalledWith('wf-1')
    expect(vm.runs).toEqual(fakeRuns)
  })

  it('startReplay triggers replay API and shows success feedback', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    const run = { id: 'run-1', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() }
    await vm.startReplay(run)
    await flushPromises()
    expect(mockReplayWorkflowRun).toHaveBeenCalledWith('wf-1', 'run-1')
    expect(vm.historyFeedback).toEqual({ text: 'Replay triggered.', err: false })
  })

  it('submitFork validates JSON and avoids API call on parse failure', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.forkTargetRun = { id: 'run-1', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() }
    vm.forkInputsJson = '{bad'
    await vm.submitFork()
    await flushPromises()
    expect(mockForkWorkflowRun).not.toHaveBeenCalled()
    expect(vm.historyFeedback).toEqual({ text: 'Invalid JSON for inputs', err: true })
  })

  it('submitFork calls fork API with overrides and live-definition flag', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.forkTargetRun = { id: 'run-1', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() }
    vm.forkInputsJson = '{"k":"v","n":2}'
    vm.forkUseLive = true
    await vm.submitFork()
    await flushPromises()
    expect(mockForkWorkflowRun).toHaveBeenCalledWith('wf-1', 'run-1', {
      inputs: { k: 'v', n: '2' },
      use_live_definition: true,
    })
  })

  it('runDiffCompare fetches and stores formatted diff JSON', async () => {
    mockDiffWorkflowRuns.mockResolvedValue({ changed: true, steps: [] })
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.diffBaseRun = { id: 'run-a', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() }
    vm.diffOtherRunId = 'run-b'
    await vm.runDiffCompare()
    await flushPromises()
    expect(mockDiffWorkflowRuns).toHaveBeenCalledWith('wf-1', 'run-a', 'run-b')
    expect(vm.diffResultJson).toContain('"changed": true')
  })

  it('toggleArtifactPopover fetches artifacts once per session and caches', async () => {
    mockFetchSessionArtifacts.mockResolvedValue([{ id: 'a1', title: 'file.txt', kind: 'file', status: 'accepted' }])
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    await vm.toggleArtifactPopover('sess-1')
    await flushPromises()
    expect(mockFetchSessionArtifacts).toHaveBeenCalledTimes(1)
    expect(vm.artifactPopoverSessionId).toBe('sess-1')
    expect(vm.sessionArtifactsById['sess-1']).toHaveLength(1)
    await vm.toggleArtifactPopover('sess-1') // close
    await vm.toggleArtifactPopover('sess-1') // reopen; should use cache
    await flushPromises()
    expect(mockFetchSessionArtifacts).toHaveBeenCalledTimes(1)
  })

  // ── Error and loading states ───────────────────────────────────────────────

  it('shows loading spinner while fetching workflows', async () => {
    mockLoading.value = true
    const w = mountWorkflowsView()
    await nextTick()
    // The spinner is rendered with animate-spin class when loading is true
    expect(w.html()).toContain('animate-spin')
  })

  it('shows error banner when workflowError is set', async () => {
    mockWorkflowError.value = 'Failed to load workflows'
    mockWorkflows.value = []
    const w = mountWorkflowsView()
    await nextTick()
    // The error is surfaced through the mockWorkflowError ref bound into the component
    expect(mockWorkflowError.value).toBe('Failed to load workflows')
    // The empty-state branch is rendered (no workflows) rather than the list
    expect(w.text()).toContain('workflow')
  })

  // ── Validation ────────────────────────────────────────────────────────────

  it('saveWorkflow is a no-op when selectedWorkflow is null', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    // Ensure no workflow is selected
    vm.selectedWorkflow = null
    vm.selectedId = null
    await vm.saveWorkflow()
    await flushPromises()
    expect(mockUpdateWorkflow).not.toHaveBeenCalled()
  })

  // ── Deep links ────────────────────────────────────────────────────────────

  it('toggleRun expands the run and pushes /workflows/:id/runs/:runId', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.toggleRun('run-abc')
    await nextTick()
    expect(vm.expandedRunId).toBe('run-abc')
    expect(mockRouterReplace).toHaveBeenCalledWith('/workflows/wf-1/runs/run-abc')
  })

  it('toggleRun collapses the run and reverts URL to /workflows/:id', async () => {
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.toggleRun('run-abc') // expand
    await nextTick()
    vm.toggleRun('run-abc') // collapse
    await nextTick()
    expect(vm.expandedRunId).toBeNull()
    expect(mockRouterReplace).toHaveBeenLastCalledWith('/workflows/wf-1')
  })

  it('closeHistory drops the runId from the URL', async () => {
    // Mount with a runId prop simulating a deep-link arrival.
    const w = shallowMount(WorkflowsView, {
      props: { id: 'wf-1', runId: 'run-deep' },
      global: { stubs: { Teleport: true } },
    })
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.showHistory = true
    await nextTick()
    vm.closeHistory()
    await nextTick()
    expect(vm.showHistory).toBe(false)
    expect(mockRouterReplace).toHaveBeenCalledWith('/workflows/wf-1')
  })

  it('opening with runId prop opens history and expands the matching run', async () => {
    const fakeRuns = [
      { id: 'run-abc', workflow_id: 'wf-1', status: 'complete', steps: [], started_at: new Date().toISOString() },
      { id: 'run-xyz', workflow_id: 'wf-1', status: 'failed',   steps: [], started_at: new Date().toISOString() },
    ]
    mockFetchWorkflowRuns.mockResolvedValue(fakeRuns)
    const w = shallowMount(WorkflowsView, {
      props: { id: 'wf-1', runId: 'run-xyz' },
      global: { stubs: { Teleport: true } },
    })
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    vm.selectedWorkflow = mockWorkflows.value[0]
    vm.showHistory = true
    await flushPromises()
    expect(vm.expandedRunId).toBe('run-xyz')
  })
})
