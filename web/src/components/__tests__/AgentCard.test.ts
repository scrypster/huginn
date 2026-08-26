import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentCard from '../AgentCard.vue'
import type { AgentSummary } from '../../composables/useAgents'
import { MODEL_TOOL_WARNING } from '../../views/agents/modelToolCapabilities'

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

  it('never renders the raw "No description" placeholder', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ system_prompt: 'You are Steve, a coder. Use tools.' }) },
    })
    expect(wrapper.text()).toContain('a coder')
    expect(wrapper.text()).not.toContain('No description')
  })

  it('falls back to a default line when no description or prompt is available', () => {
    const wrapper = mount(AgentCard, { props: { agent: makeAgent() } })
    expect(wrapper.text()).toContain('Ready to chat')
    expect(wrapper.text()).not.toContain('No description')
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

  it('hides Memory badges when advertiseMemory is false', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ vault_name: 'my-vault' }), advertiseMemory: false },
    })
    expect(wrapper.text()).not.toContain('Memory')
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

  it('shows the tools warning for a 7b model', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ name: 'Steve', model: 'qwen2.5-coder:7b' }) },
    })
    expect(wrapper.get('[data-testid="model-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)
  })

  it('shows the tools warning when supportsTools is false', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ name: 'Custom', model: 'custom-coder' }), supportsTools: false },
    })
    expect(wrapper.get('[data-testid="model-tools-warning"]').text()).toBe(MODEL_TOOL_WARNING)
  })

  it('hides the tools warning for a 14b model with tools', () => {
    const wrapper = mount(AgentCard, {
      props: { agent: makeAgent({ name: 'Chris', model: 'qwen2.5-coder:14b' }), supportsTools: true },
    })
    expect(wrapper.find('[data-testid="model-tools-warning"]').exists()).toBe(false)
  })
})
