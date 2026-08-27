import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref, defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useWorkflowsViewState } from '../useWorkflowsViewState'

const mockWorkflows = ref<any[]>([])
const mockLoading = ref(false)
const mockLiveEvents = ref<Record<string, any[]>>({})
const mockFetchWorkflows = vi.fn().mockResolvedValue(undefined)
const mockDropWorkflow = vi.fn()


vi.mock('../../../composables/useWorkflows', () => ({
  useWorkflows: () => ({
    workflows: mockWorkflows,
    loading: mockLoading,
    liveEvents: mockLiveEvents,
    fetchWorkflows: mockFetchWorkflows,
    fetchTemplates: vi.fn().mockResolvedValue([]),
    createWorkflow: vi.fn(),
    dropWorkflow: (...args: unknown[]) => mockDropWorkflow(...args),
    updateWorkflow: vi.fn(),
    deleteWorkflow: vi.fn(),
    triggerWorkflow: vi.fn(),
    cancelWorkflow: vi.fn(),
    fetchWorkflowRuns: vi.fn().mockResolvedValue([]),
    replayWorkflowRun: vi.fn(),
    forkWorkflowRun: vi.fn(),
    diffWorkflowRuns: vi.fn(),
    fetchSessionArtifacts: vi.fn().mockResolvedValue([]),
  }),
}))

vi.mock('../../../composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref([{ name: 'agent-1', description: 'test agent' }]),
  }),
}))

vi.mock('../../../composables/useDeliveryQueue', () => ({
  useDeliveryQueue: () => ({
    actionableEntries: ref([]),
    retryEntry: vi.fn(),
    dismissEntry: vi.fn(),
  }),
}))

vi.mock('../../../composables/useApi', () => ({
  getToken: () => 'token',
}))

function mountHarness(initial: { id?: string; runId?: string } = {}) {
  let state: any
  const id = ref<string | undefined>(initial.id)
  const runId = ref<string | undefined>(initial.runId)
  const router = { push: vi.fn(), replace: vi.fn() }

  const Harness = defineComponent({
    setup() {
      state = useWorkflowsViewState({ id, runId }, router as any)
      return () => null
    },
  })

  mount(Harness)
  return { state, id, runId, router }
}

beforeEach(() => {
  mockWorkflows.value = [
    {
      id: 'wf-1',
      name: 'Workflow One',
      enabled: true,
      schedule: '',
      steps: [{ name: 'Step 1', agent: 'agent-1', prompt: 'hello', position: 0 }],
    },
  ]
  mockLiveEvents.value = {}
  mockFetchWorkflows.mockReset().mockResolvedValue(undefined)
  mockDropWorkflow.mockReset().mockResolvedValue({ id: 'dropped', name: 'Dropped', enabled: false, schedule: '', steps: [] })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ json: async () => [] }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useWorkflowsViewState', () => {
  it('initializes selectedId from route id ref', async () => {
    const { state } = mountHarness({ id: 'wf-1' })
    await nextTick()
    expect(state.selectedId.value).toBe('wf-1')
  })

  it('flattens token events into a single token batch row', async () => {
    const { state } = mountHarness({ id: 'wf-1' })
    state.selectedId.value = 'wf-1'
    mockLiveEvents.value = {
      'wf-1': [
        { type: 'workflow_step_token', workflow_id: 'wf-1', run_id: 'run-1', token: 'Hello ' },
        { type: 'workflow_step_token', workflow_id: 'wf-1', run_id: 'run-1', token: 'world' },
        { type: 'workflow_complete', workflow_id: 'wf-1', run_id: 'run-1' },
      ],
    }
    await nextTick()

    const rows = state.displayedLiveEvents.value
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({ kind: 'token_batch', text: 'Hello world', count: 2 })
    expect(rows[1]).toMatchObject({ type: 'workflow_complete' })
  })

  it('openWorkflow seeds stepAgentDetails from known agents', () => {
    const { state, router } = mountHarness()
    state.openWorkflow(mockWorkflows.value[0])

    expect(state.stepAgentDetails.value[0]).toMatchObject({ name: 'agent-1' })
    expect(router.push).toHaveBeenCalledWith('/workflows/wf-1')
  })

  it('addStep stacks a teammate and pipes the previous output', () => {
    const { state } = mountHarness()
    state.openWorkflow(mockWorkflows.value[0])
    state.addStep()
    const steps = state.editForm.value.steps
    expect(steps).toHaveLength(2)
    expect(steps[1].name).toBe('step-2')
    expect(steps[1].inputs).toEqual([{ from_step: 'Step 1', as: 'prev' }])
    expect(steps[1].prompt).toContain('{{inputs.prev}}')
    expect(state.pipelinePreview.value[0]).toBe('@agent-1')
    expect(state.pipelinePreview.value[1]).toBe('step-2')
  })

  it('scheduleMode once clears cron; repeat seeds a weekday default', () => {
    const { state } = mountHarness()
    state.openWorkflow(mockWorkflows.value[0])
    state.scheduleMode.value = 'once'
    expect(state.editForm.value.schedule).toBe('')
    state.scheduleMode.value = 'repeat'
    expect(state.editForm.value.schedule).toBe('0 8 * * 1-5')
  })

  it('onFileDrop imports yaml and opens the workflow', async () => {
    const { state, router } = mountHarness()
    const file = new File(['id: dropped\nname: Dropped\n'], 'dropped.yaml', { type: 'text/yaml' })
    const ev = { dataTransfer: { files: [file], types: ['Files'] } } as unknown as DragEvent
    await state.onFileDrop(ev)
    expect(mockDropWorkflow).toHaveBeenCalledWith('dropped.yaml', expect.stringContaining('id: dropped'))
    expect(router.push).toHaveBeenCalledWith('/workflows/dropped')
    expect(state.dropError.value).toBe(false)
  })
})
