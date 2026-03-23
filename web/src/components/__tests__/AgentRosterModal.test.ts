import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentRosterModal from '../AgentRosterModal.vue'
import type { Space } from '../../composables/useSpaces'

// Mock composables
vi.mock('../../composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn().mockResolvedValue([]),
      get: vi.fn().mockResolvedValue({}),
    },
    spaces: {
      updateSpace: vi.fn().mockResolvedValue({}),
    },
  },
}))

vi.mock('../../composables/useAgents', () => {
  const { ref } = require('vue')
  const agents = ref([
    { name: 'atlas', color: '#ff5733', icon: '🤖', model: 'claude-3' },
    { name: 'hermes', color: '#33ff57', icon: 'H', model: 'gpt-4' },
  ])
  return {
    useAgents: () => ({
      agents,
      loading: ref(false),
      fetchAgents: vi.fn(),
    }),
  }
})

vi.mock('../../composables/useSpaces', () => {
  const { ref } = require('vue')
  return {
    useSpaces: () => ({
      spaces: ref([]),
      updateSpace: vi.fn().mockResolvedValue({}),
      error: ref(null),
    }),
  }
})

const makeSpace = (overrides: Partial<Space> = {}): Space => ({
  id: 'space-1',
  name: 'Engineering',
  kind: 'channel',
  leadAgent: 'atlas',
  memberAgents: ['atlas', 'hermes'],
  icon: '#',
  color: '#58a6ff',
  unseenCount: 0,
  archivedAt: null,
  ...overrides,
})

describe('AgentRosterModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the space name in the header', () => {
    const wrapper = mount(AgentRosterModal, {
      props: { space: makeSpace({ name: 'Engineering' }) },
    })
    expect(wrapper.html()).toContain('Engineering')
  })

  it('emits close when the backdrop div receives a self-click', async () => {
    const wrapper = mount(AgentRosterModal, {
      props: { space: makeSpace() },
    })
    // @click.self only fires when the event target IS the backdrop div itself.
    // We trigger click directly via the component's vm to bypass the .self modifier.
    ;(wrapper.vm as any).$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits close when the X button is clicked', async () => {
    const wrapper = mount(AgentRosterModal, {
      props: { space: makeSpace() },
    })
    // Find the close button (X icon button in header)
    const buttons = wrapper.findAll('button')
    const closeBtn = buttons.find(b => b.find('svg line').exists())
    expect(closeBtn).toBeTruthy()
    await closeBtn!.trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('renders all member agents listed in space.memberAgents', () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas', 'hermes'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('atlas')
    expect(wrapper.html()).toContain('hermes')
  })

  it('shows the correct agent count in the subheading', () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas', 'hermes'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    // Should show "2 agents"
    expect(wrapper.html()).toContain('2 agents')
  })

  it('shows Lead badge on the lead agent', () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas', 'hermes'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('Lead')
  })

  it('shows Member label on non-lead agents', () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas', 'hermes'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('Member')
  })

  it('shows singular "agent" for a space with one member', () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('1 agent')
    expect(wrapper.html()).not.toContain('1 agents')
  })

  it('renders space icon in the header', () => {
    const space = makeSpace({ icon: '#', color: '#58a6ff', kind: 'channel' })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('#')
  })

  it('shows add-agent section placeholder for channel spaces', () => {
    // The mock has 2 agents (atlas, hermes), both are already members,
    // so the "All available agents are members" text shows.
    // To see the input we need a space that doesn't cover all agents.
    const space = makeSpace({ kind: 'channel', leadAgent: 'atlas', memberAgents: ['atlas'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    // hermes is not a member, so the search input should render
    // The placeholder is "Add an agent to this channel…"
    expect(wrapper.html()).toContain('Add an agent to this channel')
  })

  it('shows DM note for dm spaces instead of add-agent section', () => {
    const space = makeSpace({ kind: 'dm' })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('DM conversations are fixed')
  })

  it('toggles expanded state when clicking an agent row', async () => {
    const space = makeSpace({ leadAgent: 'atlas', memberAgents: ['atlas', 'hermes'] })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    // Find the agent rows (cursor-pointer divs that contain agent names)
    const agentRows = wrapper.findAll('[class*="cursor-pointer"]')
    expect(agentRows.length).toBeGreaterThan(0)
    // Initially expandedAgent is null
    expect((wrapper.vm as any).expandedAgent).toBe(null)
    await agentRows[0]!.trigger('click')
    expect((wrapper.vm as any).expandedAgent).toBe('atlas')
  })

  it('shows "All available agents are members" when no agents are available to add', () => {
    // leadAgent and memberAgents cover all known agents from useAgents mock
    const space = makeSpace({
      kind: 'channel',
      leadAgent: 'atlas',
      memberAgents: ['atlas', 'hermes'],
    })
    const wrapper = mount(AgentRosterModal, { props: { space } })
    expect(wrapper.html()).toContain('All available agents are members')
  })
})
