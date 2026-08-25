import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'

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

const mockOpenSpaceDM = vi.hoisted(() => vi.fn())

vi.mock('../../composables/useSpaces', () => ({
  useSpaces: () => ({
    openDM: mockOpenSpaceDM,
  }),
  wireSpaceWS: vi.fn(),
}))

// Stub remaining API used by the agent editor
vi.mock('../../composables/useApi', async (importOriginal) => {
  const orig = await importOriginal<any>()
  const mockedApiFetch = vi.fn().mockResolvedValue({})
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
        available: vi.fn().mockResolvedValue({ models: [], builtin_models: [], provider_models: [] }),
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
    vi.mocked(apiFetch).mockResolvedValue({})
    mockOpenSpaceDM.mockReset()
    mockOpenSpaceDM.mockResolvedValue({ id: 'space-123', kind: 'dm', leadAgent: 'Alpha' })
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
    expect(wrapper.text()).not.toContain('No description')
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
    mockOpenSpaceDM.mockResolvedValueOnce(null)
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
})
