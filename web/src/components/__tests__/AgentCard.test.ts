import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentCard from '../AgentCard.vue'
import type { AgentSummary } from '../../composables/useAgents'

function makeAgent(overrides: Partial<AgentSummary> = {}): AgentSummary {
  return {
    name: 'TestAgent',
    color: '#58a6ff',
    icon: 'T',
    model: 'claude-3',
    ...overrides,
  }
}

describe('AgentCard', () => {
  it('renders agent name', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('TestAgent')
  })

  it('renders description when provided', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ description: 'My helpful agent' }) },
    })
    expect(wrapper.text()).toContain('My helpful agent')
  })

  it('shows "No description" when description is absent', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('No description')
  })

  it('shows heartbeat badge when heartbeat_enabled is true', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ heartbeat_enabled: true }) },
    })
    expect(wrapper.text()).toContain('Heartbeat')
  })

  it('hides heartbeat badge when heartbeat_enabled is false', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ heartbeat_enabled: false }) },
    })
    expect(wrapper.text()).not.toContain('Heartbeat')
  })

  it('shows memory badge when vault_name is set', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ vault_name: 'my-vault' }) },
    })
    expect(wrapper.text()).toContain('Memory')
    expect(wrapper.text()).not.toContain('No memory')
  })

  it('shows "No memory" when vault_name is absent', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('No memory')
  })

  it('emits "edit" on edit button click without bubbling to card click', async () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    const editBtn = wrapper.find('[data-testid="agent-card-edit"]')
    await editBtn.trigger('click')
    expect(wrapper.emitted('edit')).toHaveLength(1)
    expect(wrapper.emitted('click')).toBeFalsy()
  })

  it('emits "click" when card body is clicked', async () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    await wrapper.find('[data-testid="agent-card"]').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
  })
})
