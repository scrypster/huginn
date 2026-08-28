import { describe, it, expect, beforeEach, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useAgentsViewState } from '../useAgentsViewState'

const {
  mockApiFetch,
  mockAgents,
  mockLoading,
  mockUpdateAgent,
  mockOpenSpaceDM,
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
  mockUpdateAgent: vi.fn(),
  mockOpenSpaceDM: vi.fn(),
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
    agents: mockAgents,
    loading: ref(mockLoading.value),
    updateAgent: mockUpdateAgent,
    removeAgent: vi.fn(),
    fetchAgents: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('../../../composables/useSpaces', () => ({
  useSpaces: () => ({
    openDM: mockOpenSpaceDM,
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
  mockUpdateAgent.mockReset()
  mockOpenSpaceDM.mockReset()
  mockRouterPush.mockReset()
  mockRouterReplace.mockReset()
  mockConnectionsList.mockReset()
  mockAgentsGet.mockReset()
  mockAgentsUpdate.mockReset()
  mockValidateCapabilityMatrix.mockReset()
  mockAgents.value = []
  mockConnectionsList.mockResolvedValue([])
  mockAgentsGet.mockResolvedValue({})
  mockAgentsUpdate.mockResolvedValue({})
  mockValidateCapabilityMatrix.mockResolvedValue({ valid: true, decisions: [] })
  mockOpenSpaceDM.mockResolvedValue({ id: 'space-steve', kind: 'dm', leadAgent: 'Steve' })
  mockUpdateAgent.mockImplementation((name: string, patch: Record<string, unknown>) => {
    const idx = mockAgents.value.findIndex((a: { name: string }) => a.name === name)
    if (idx >= 0) mockAgents.value[idx] = { ...mockAgents.value[idx], ...patch }
    else mockAgents.value.push({ name, ...patch })
  })
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
    mockOpenSpaceDM.mockResolvedValueOnce({ id: 'space-123' })
    const { state } = mountHarness()
    await state.openDM({ name: 'Alpha' })

    expect(mockOpenSpaceDM).toHaveBeenCalledWith('Alpha')
    expect(mockRouterPush).toHaveBeenCalledWith('/space/space-123')
  })

  it('openDM falls back to agent route when lookup fails', async () => {
    mockOpenSpaceDM.mockResolvedValueOnce(null)
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

  it('save of a new agent updates the agent list and opens that DM without a reload', async () => {
    const { state } = mountHarness('new')
    await flushPromises()

    state.form.value.name = 'Steve'
    state.form.value.model = 'qwen2.5-coder:7b'
    state.form.value.system_prompt = 'You are Steve, a coder. Use tools.'
    state.form.value.description = ''

    await state.save()
    await flushPromises()

    expect(mockAgentsUpdate).toHaveBeenCalled()
    expect(mockAgents.value.map((a: { name: string }) => a.name)).toContain('Steve')
    expect(mockOpenSpaceDM).toHaveBeenCalledWith('Steve')
    expect(mockRouterPush).toHaveBeenCalledWith('/space/space-steve')
  })

  it('save derives a description from the system prompt instead of persisting empty', async () => {
    const { state } = mountHarness('new')
    await flushPromises()

    state.form.value.name = 'Steve'
    state.form.value.model = 'qwen2.5-coder:7b'
    state.form.value.system_prompt = 'You are Steve, a coder. Use tools.'
    state.form.value.description = ''

    await state.save()
    await flushPromises()

    const payload = mockAgentsUpdate.mock.calls[0]?.[1] as { description?: string }
    expect(payload.description).toBe('a coder')
    expect(payload.description).not.toBe('No description')
    expect(mockAgents.value[0]?.description).toBe('a coder')
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

  it('selectedModelUnreliableTools warns for 7b even when supportsTools is true', async () => {
    const { state } = mountHarness('new')
    await flushPromises()
    state.availableModels.value = [
      { name: 'qwen2.5-coder:7b', supportsTools: true, supportsDelegation: false, tier: 'low' },
      { name: 'qwen2.5-coder:14b', supportsTools: true, supportsDelegation: true, tier: 'medium' },
    ]

    state.form.value.model = 'qwen2.5-coder:7b'
    expect(state.selectedModelUnreliableTools.value).toBe(true)
    expect(state.MODEL_TOOL_WARNING).toBe(
      'This model is unlikely to use tools or delegate. Grants will not do what you expect.',
    )
    expect(state.showLocalAccessToolWarning.value).toBe(false)

    state.form.value.local_tools = ['*']
    expect(state.showLocalAccessToolWarning.value).toBe(true)

    state.form.value.model = 'qwen2.5-coder:14b'
    expect(state.selectedModelUnreliableTools.value).toBe(false)
    expect(state.showLocalAccessToolWarning.value).toBe(false)
  })

  it('selectedModelUnreliableTools warns when supportsTools is false', async () => {
    const { state } = mountHarness('new')
    await flushPromises()
    state.availableModels.value = [
      { name: 'custom-coder', supportsTools: false },
    ]
    state.form.value.model = 'custom-coder'
    expect(state.selectedModelUnreliableTools.value).toBe(true)
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

  describe('delete agent', () => {
    let fetchMock: ReturnType<typeof vi.fn>
    let deleteResponse: { ok: boolean; status: number; json: () => Promise<unknown> }

    beforeEach(() => {
      deleteResponse = { ok: true, status: 200, json: async () => ({ deleted: true }) }
      fetchMock = vi.fn((url: string) => {
        // loadMuninnInfo's vault-status probe also runs on mount for an
        // existing agent name; give it a harmless default so it doesn't
        // consume the delete response the test configured.
        if (typeof url === 'string' && url.includes('/vault-status')) {
          return Promise.resolve({ ok: true, status: 200, json: async () => ({}) })
        }
        return Promise.resolve(deleteResponse)
      })
      vi.stubGlobal('fetch', fetchMock)
    })

    it('confirmDelete opens the confirm banner without calling the delete API', async () => {
      const { state } = mountHarness('Alpha')
      state.form.value.name = 'Alpha'
      await flushPromises()
      fetchMock.mockClear()
      expect(state.showDeleteConfirm.value).toBe(false)

      state.confirmDelete()

      expect(state.showDeleteConfirm.value).toBe(true)
      expect(fetchMock).not.toHaveBeenCalledWith(
        '/api/v1/agents/Alpha',
        expect.objectContaining({ method: 'DELETE' }),
      )
    })

    it('deleteAgent calls DELETE /api/v1/agents/{name} and routes away on success', async () => {
      const { state } = mountHarness('Alpha')
      state.form.value.name = 'Alpha'
      state.confirmDelete()

      await state.deleteAgent()

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v1/agents/Alpha',
        expect.objectContaining({ method: 'DELETE' }),
      )
      expect(mockRouterPush).toHaveBeenCalledWith('/agents')
    })

    it('deleteAgent surfaces the 409 lead-block message verbatim and keeps the agent', async () => {
      const leadBlockMsg = 'cannot delete agent "Alpha": assigned as lead agent in spaces: [general]'
      deleteResponse = { ok: false, status: 409, json: async () => ({ error: leadBlockMsg }) }
      const { state } = mountHarness('Alpha')
      state.form.value.name = 'Alpha'
      state.confirmDelete()

      await state.deleteAgent()

      expect(state.saveMsg.value).toBe(leadBlockMsg)
      expect(state.saveError.value).toBe(true)
      expect(state.showDeleteConfirm.value).toBe(false)
      expect(mockRouterPush).not.toHaveBeenCalledWith('/agents')
    })
  })
})

describe('addApprovedTool', () => {
  it('adds a trimmed unique grant (unattended-run escape hatch)', () => {
    const { state } = mountHarness()
    state.form.value.approved_tools = ['bash']
    state.addApprovedTool('  run_tests  ')
    state.addApprovedTool('bash') // duplicate ignored
    state.addApprovedTool('   ') // blank ignored
    expect(state.form.value.approved_tools).toEqual(['bash', 'run_tests'])
  })
})
