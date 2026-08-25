import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { MODEL_TOOL_WARNING } from '../agents/modelToolCapabilities'

const { mockModelsAvailable } = vi.hoisted(() => ({
  mockModelsAvailable: vi.fn().mockResolvedValue({ models: [], builtin_models: [], provider_models: [] }),
}))

// We need to stub useAgents so we can control the agents list.
vi.mock('../../composables/useAgents', () => {
  const { ref } = require('vue')
  const agents = ref<any[]>([])
  const loading = ref(false)
  return {
    useAgents: () => ({
      agents,
      loading,
      fetchAgents: vi.fn(),
      updateAgent: vi.fn(),
      removeAgent: vi.fn(),
      getAgentByName: vi.fn(),
      wireWS: vi.fn(),
    }),
    get _agents() { return agents },
    get _loading() { return loading },
  }
})

// Stub apiFetch used by openDM
vi.mock('../../composables/useApi', async (importOriginal) => {
  const orig = await importOriginal<any>()
  const mockedApiFetch = vi.fn().mockImplementation(async (path: string) => {
    if (path.startsWith('/api/v1/spaces/dm/')) return { id: 'space-123' }
    return {}
  })
  return {
    ...orig,
    apiFetch: mockedApiFetch,
    api: {
      ...orig.api,
      connections: {
        ...orig.api.connections,
        list: vi.fn().mockResolvedValue([]),
      },
      system: {
        ...orig.api.system,
        tools: vi.fn().mockResolvedValue([]),
      },
      muninn: {
        ...orig.api.muninn,
        status: vi.fn().mockResolvedValue({ connected: false }),
        vaults: vi.fn().mockResolvedValue({ vaults: [] }),
      },
      agents: {
        ...orig.api.agents,
        capabilityMatrix: vi.fn().mockResolvedValue({ connections: [], providers: [] }),
        validateCapabilityMatrix: vi.fn().mockResolvedValue({ valid: true, decisions: [] }),
      },
      models: {
        ...orig.api.models,
        available: (...args: unknown[]) => mockModelsAvailable(...args),
      },
    },
  }
})

import AgentsView from '../AgentsView.vue'
import { useAgents as _useAgents } from '../../composables/useAgents'
import { apiFetch } from '../../composables/useApi'

const router = createRouter({ history: createMemoryHistory(), routes: [
  { path: '/agents/:agentName?', component: AgentsView, props: true },
  { path: '/space/:id', component: { template: '<div />' } },
] })

describe('AgentsView', () => {
  beforeEach(async () => {
    await router.push('/agents')
    await router.isReady()
    ;((_useAgents() as any).agents as any).value = []
    ;((_useAgents() as any).loading as any).value = false
    vi.mocked(apiFetch).mockImplementation(async (path: string) => {
      if (path.startsWith('/api/v1/spaces/dm/')) return { id: 'space-123' }
      return {}
    })
    mockModelsAvailable.mockReset().mockResolvedValue({ models: [], builtin_models: [], provider_models: [] })
  })

  it('shows empty state when agents list is empty and not loading', async () => {
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Select an agent')
    expect(wrapper.find('[data-testid="agent-card"]').exists()).toBe(false)
  })

  it('renders card grid when agents exist', async () => {
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
      { name: 'Beta',  color: '#0ff', icon: 'B', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="agent-card"]')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('Select an agent')
  })

  it('openDM navigates to /space/:id on success', async () => {
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/space/space-123')
  })

  it('openDM falls back to /agents/:name if DM fetch fails', async () => {
    // Override the mock so DM fetch fails while other calls (e.g. skills load) succeed.
    vi.mocked(apiFetch).mockImplementation(async (path: string) => {
      if (path.startsWith('/api/v1/spaces/dm/')) throw new Error('fail')
      return {}
    })
    ;((_useAgents() as any).agents as any).value = [
      { name: 'Alpha', color: '#ff0', icon: 'A', model: 'gpt-4' },
    ]
    const wrapper = mount(AgentsView, {
      global: { plugins: [router] },
      props: { agentName: undefined },
    })
    await flushPromises()
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/agents/Alpha')
  })

  const editorMount = (props: { agentName?: string } = { agentName: 'new' }) => mount(AgentsView, {
    global: {
      plugins: [router],
      stubs: { Teleport: true, Transition: false },
    },
    props,
  })

  it('shows the tools warning when picking qwen2.5-coder:7b but not 14b', async () => {
    mockModelsAvailable.mockResolvedValue({
      models: [
        { name: 'qwen2.5-coder:7b', supportsTools: true, supportsDelegation: false, tier: 'low', details: { parameter_size: '7.6B' } },
        { name: 'qwen2.5-coder:14b', supportsTools: true, supportsDelegation: true, tier: 'medium', details: { parameter_size: '14.8B' } },
      ],
      builtin_models: [],
      provider_models: [],
    })
    const wrapper = editorMount()
    await flushPromises()

    await wrapper.get('[data-testid="open-model-picker"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="pick-model-qwen2.5-coder:7b"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="model-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)
    expect(wrapper.find('[data-testid="local-access-model-tools-warning"]').exists()).toBe(false)

    await wrapper.get('[data-testid="local-access-allow-all-btn"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="local-access-model-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)

    await wrapper.get('[data-testid="open-model-picker"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="pick-model-qwen2.5-coder:14b"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="model-tools-warning"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="local-access-model-tools-warning"]').exists()).toBe(false)
  })

  it('shows the picker warning copy when supportsTools is false', async () => {
    mockModelsAvailable.mockResolvedValue({
      models: [
        { name: 'custom-coder', supportsTools: false, supportsDelegation: false, tier: 'low' },
      ],
      builtin_models: [],
      provider_models: [],
    })
    const wrapper = editorMount()
    await flushPromises()

    await wrapper.get('[data-testid="open-model-picker"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="model-picker-tools-warning"]').exists()).toBe(false)
    await wrapper.get('[data-testid="pick-model-custom-coder"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)

    await wrapper.get('[data-testid="open-model-picker"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-picker-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)
  })
})
