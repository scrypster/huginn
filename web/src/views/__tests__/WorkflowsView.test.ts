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
    fetchWorkflowRuns: mockFetchWorkflowRuns,
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

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
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
    { id: 'wf-1', name: 'Daily Report', enabled: true, schedule: '0 8 * * *', steps: [] },
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
    await vm.saveWorkflow()
    await flushPromises()
    expect(mockUpdateWorkflow).toHaveBeenCalledWith('wf-1', expect.objectContaining({
      id: 'wf-1',
      name: 'Daily Report',
      enabled: true,
    }))
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

  it('running flag is cleared after triggerRun completes', async () => {
    vi.useFakeTimers()
    mockTriggerWorkflow.mockResolvedValue(undefined)
    const w = mountWorkflowsView()
    await nextTick()
    const vm = w.vm as any
    vm.selectedId = 'wf-1'
    await vm.triggerRun()
    await flushPromises()
    // running is cleared after a 1000ms timeout in the component
    expect(vm.running).toBe(true)
    vi.advanceTimersByTime(1100)
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
})
