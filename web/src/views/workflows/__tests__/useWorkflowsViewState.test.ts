import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref, defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useWorkflowsViewState } from '../useWorkflowsViewState'

const mockWorkflows = ref<any[]>([])
const mockLoading = ref(false)
const mockLiveEvents = ref<Record<string, any[]>>({})
const mockFetchWorkflows = vi.fn().mockResolvedValue(undefined)

vi.mock('../../../composables/useWorkflows', () => ({
  useWorkflows: () => ({
    workflows: mockWorkflows,
    loading: mockLoading,
    liveEvents: mockLiveEvents,
    fetchWorkflows: mockFetchWorkflows,
    fetchTemplates: vi.fn().mockResolvedValue([]),
    createWorkflow: vi.fn(),
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
})
