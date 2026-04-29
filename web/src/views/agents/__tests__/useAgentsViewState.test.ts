import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useAgentsViewState } from '../useAgentsViewState'

const {
  mockApiFetch,
  mockAgents,
  mockLoading,
  mockRouterPush,
  mockRouterReplace,
  mockConnectionsList,
} = vi.hoisted(() => ({
  mockApiFetch: vi.fn(),
  mockAgents: { value: [] as any[] },
  mockLoading: { value: false },
  mockRouterPush: vi.fn(),
  mockRouterReplace: vi.fn(),
  mockConnectionsList: vi.fn().mockResolvedValue([]),
}))

vi.mock('../../../composables/useApi', () => ({
  getToken: () => 'token',
  apiFetch: mockApiFetch,
  api: {
    models: { available: vi.fn().mockResolvedValue({ models: [], builtin_models: [], provider_models: [] }) },
    muninn: {
      status: vi.fn().mockResolvedValue({ connected: false, endpoint: '', username: '' }),
      vaults: vi.fn().mockResolvedValue({ vaults: [] }),
      createVault: vi.fn().mockResolvedValue({}),
    },
    connections: { list: mockConnectionsList },
    system: { tools: vi.fn().mockResolvedValue([]) },
    agents: {
      get: vi.fn().mockResolvedValue({}),
      update: vi.fn().mockResolvedValue({}),
      capabilityMatrix: vi.fn().mockResolvedValue({ connections: [], providers: [] }),
      validateCapabilityMatrix: vi.fn().mockResolvedValue({ valid: true, decisions: [] }),
    },
  },
}))

vi.mock('../../../composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref(mockAgents.value),
    loading: ref(mockLoading.value),
    updateAgent: vi.fn(),
    removeAgent: vi.fn(),
    fetchAgents: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('../../../composables/useSkills', () => ({
  useInstalledSkills: () => ({
    skills: ref([{ name: 'skill-a' }]),
    load: vi.fn().mockResolvedValue(undefined),
  }),
}))

function mountHarness(agentName?: string) {
  let state: any
  const agentNameRef = ref<string | undefined>(agentName)
  const router = { push: mockRouterPush, replace: mockRouterReplace }

  const Harness = defineComponent({
    setup() {
      state = useAgentsViewState(agentNameRef, router as any)
      return () => null
    },
  })

  mount(Harness)
  return { state, agentNameRef }
}

beforeEach(() => {
  mockApiFetch.mockReset()
  mockRouterPush.mockReset()
  mockRouterReplace.mockReset()
  mockConnectionsList.mockReset()
  mockConnectionsList.mockResolvedValue([])
})

describe('useAgentsViewState', () => {
  it('openDM routes to DM space when lookup succeeds', async () => {
    mockApiFetch.mockResolvedValueOnce({ id: 'space-123' })
    const { state } = mountHarness()
    await state.openDM({ name: 'Alpha' })

    expect(mockApiFetch).toHaveBeenCalledWith('/api/v1/spaces/dm/Alpha')
    expect(mockRouterPush).toHaveBeenCalledWith('/space/space-123')
  })

  it('openDM falls back to agent route when lookup fails', async () => {
    mockApiFetch.mockRejectedValueOnce(new Error('failed'))
    const { state } = mountHarness()
    await state.openDM({ name: 'Alpha' })

    expect(mockRouterPush).toHaveBeenCalledWith('/agents/Alpha')
  })

  it('toggleConnectionsAllowAll toggles explicit assignable connections', async () => {
    mockConnectionsList.mockResolvedValueOnce([
      { id: 'conn-1', provider: 'github', account_label: 'GitHub' },
    ])
    const { state } = mountHarness()
    await flushPromises()

    state.form.value.toolbelt = []
    state.toggleConnectionsAllowAll()
    expect(state.form.value.toolbelt).toEqual([{ connection_id: 'conn-1', provider: 'github', approval_gate: false }])

    state.toggleConnectionsAllowAll()
    expect(state.form.value.toolbelt).toEqual([])
  })
})
