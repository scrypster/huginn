import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useAgentsViewState } from '../useAgentsViewState'

const {
  mockApiFetch,
  mockAgents,
  mockLoading,
  mockRouterPush,
  mockRouterReplace,
} = vi.hoisted(() => ({
  mockApiFetch: vi.fn(),
  mockAgents: { value: [] as any[] },
  mockLoading: { value: false },
  mockRouterPush: vi.fn(),
  mockRouterReplace: vi.fn(),
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
    connections: { list: vi.fn().mockResolvedValue([]) },
    system: { tools: vi.fn().mockResolvedValue([]) },
    agents: {
      get: vi.fn().mockResolvedValue({}),
      update: vi.fn().mockResolvedValue({}),
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

  it('toggleConnectionsAllowAll toggles wildcard toolbelt entry', () => {
    const { state } = mountHarness()

    state.form.value.toolbelt = []
    state.toggleConnectionsAllowAll()
    expect(state.form.value.toolbelt).toEqual([{ connection_id: '*', provider: '*', profile: '', approval_gate: false }])

    state.toggleConnectionsAllowAll()
    expect(state.form.value.toolbelt).toEqual([])
  })
})
