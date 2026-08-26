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
  mockAgentsGet,
  mockAgentsUpdate,
  mockValidateCapabilityMatrix,
} = vi.hoisted(() => ({
  mockApiFetch: vi.fn(),
  mockAgents: { value: [] as any[] },
  mockLoading: { value: false },
  mockRouterPush: vi.fn(),
  mockRouterReplace: vi.fn(),
  mockConnectionsList: vi.fn().mockResolvedValue([]),
  mockAgentsGet: vi.fn().mockResolvedValue({}),
  mockAgentsUpdate: vi.fn().mockResolvedValue({}),
  mockValidateCapabilityMatrix: vi.fn().mockResolvedValue({ valid: true, decisions: [] }),
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
      get: (...args: unknown[]) => mockAgentsGet(...args),
      update: (...args: unknown[]) => mockAgentsUpdate(...args),
      capabilityMatrix: vi.fn().mockResolvedValue({ connections: [], providers: [] }),
      validateCapabilityMatrix: (...args: unknown[]) => mockValidateCapabilityMatrix(...args),
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
  mockAgentsGet.mockReset()
  mockAgentsUpdate.mockReset()
  mockValidateCapabilityMatrix.mockReset()
  mockConnectionsList.mockResolvedValue([])
  mockAgentsGet.mockResolvedValue({})
  mockAgentsUpdate.mockResolvedValue({})
  mockValidateCapabilityMatrix.mockResolvedValue({ valid: true, decisions: [] })
})

describe('useAgentsViewState', () => {
  it('new agent form is not dirty and does not advertise as existing', async () => {
    const { state } = mountHarness('new')
    await flushPromises()
    expect(state.dirty.value).toBe(false)
    expect(state.isNewAgent.value).toBe(true)
    expect(state.advertiseMemory.value).toBe(false)
  })

  it('color input change matching the default does not mark dirty', async () => {
    const { state } = mountHarness('new')
    await flushPromises()
    expect(state.dirty.value).toBe(false)
    state.onColorInputChange()
    expect(state.dirty.value).toBe(false)
    state.form.value.color = '#111111'
    state.onColorInputChange()
    expect(state.dirty.value).toBe(true)
  })

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

  it('toggleLocalAllowAll enable without confirm does not write wildcard', () => {
    const { state } = mountHarness()
    state.form.value.local_tools = []
    state.dirty.value = false

    state.toggleLocalAllowAll()

    expect(state.form.value.local_tools).toEqual([])
    expect(state.form.value.local_tools).not.toEqual(['*'])
    expect(state.showLocalAllowAllConfirm.value).toBe(true)
    expect(state.dirty.value).toBe(false)
  })

  it('confirmLocalAllowAll writes wildcard and marks dirty; cancel does not write', () => {
    const { state } = mountHarness()
    state.form.value.local_tools = ['read_file']
    state.dirty.value = false

    state.toggleLocalAllowAll()
    state.cancelLocalAllowAll()
    expect(state.form.value.local_tools).toEqual(['read_file'])
    expect(state.showLocalAllowAllConfirm.value).toBe(false)
    expect(state.dirty.value).toBe(false)

    state.toggleLocalAllowAll()
    state.confirmLocalAllowAll()
    expect(state.form.value.local_tools).toEqual(['*'])
    expect(state.showLocalAllowAllConfirm.value).toBe(false)
    expect(state.dirty.value).toBe(true)
  })

  it('toggleLocalAllowAll disable path immediately clears local tools', () => {
    const { state } = mountHarness()
    state.form.value.local_tools = ['*']
    state.dirty.value = false

    state.toggleLocalAllowAll()

    expect(state.form.value.local_tools).toEqual([])
    expect(state.showLocalAllowAllConfirm.value).toBe(false)
    expect(state.dirty.value).toBe(true)
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

  it('loadAgent strips wildcard toolbelt entries and marks wildcardStripped', async () => {
    mockAgentsGet.mockResolvedValue({
      name: 'Alpha',
      model: 'claude-sonnet-4-6',
      system_prompt: '',
      toolbelt: [
        { connection_id: '*', provider: '*', approval_gate: false },
        { connection_id: 'conn-1', provider: 'github', approval_gate: false },
      ],
      local_tools: [],
      skills: [],
    })

    const { state } = mountHarness('Alpha')
    await flushPromises()

    expect(state.form.value.toolbelt).toEqual([
      { connection_id: 'conn-1', provider: 'github', approval_gate: false },
    ])
    expect(state.wildcardStripped.value).toBe(true)
  })

  it('saveLocalAccessModal bypasses toolbelt validation when saving existing agent', async () => {
    const { state } = mountHarness('Alpha')
    state.form.value.name = 'Alpha'
    state.form.value.model = 'claude-sonnet-4-6'
    state.form.value.toolbelt = [{ connection_id: 'stale', provider: 'github', approval_gate: false }]
    state.modalLocalTools.value = ['read_file']

    await state.saveLocalAccessModal()

    expect(mockValidateCapabilityMatrix).not.toHaveBeenCalled()
    expect(mockAgentsUpdate).toHaveBeenCalled()
  })
})
