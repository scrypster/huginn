import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentPicker from '../AgentPicker.vue'

// Mock the api composable
vi.mock('@/composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn(),
    },
  },
}))

// Also mock the relative import path used inside the component
vi.mock('../../composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn(),
    },
  },
}))

import { api } from '../../composables/useApi'

const mockAgents = [
  { name: 'atlas', color: '#ff5733', icon: '🤖', model: 'claude-3' },
  { name: 'hermes', color: '#33ff57', icon: 'H', model: 'gpt-4' },
  { name: 'zeus', color: '#3357ff', icon: '', model: 'claude-2' },
]

describe('AgentPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.agents.list).mockResolvedValue(mockAgents as any)
  })

  it('renders the input with @agent-name placeholder', () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    const input = wrapper.find('input')
    expect(input.exists()).toBe(true)
    expect(input.attributes('placeholder')).toBe('@agent-name')
  })

  it('shows dropdown with filtered agents on focus', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    // Wait for onMounted to resolve agents
    await vi.waitFor(() => {
      // The internal agents ref should be populated after the mock resolves
      return (wrapper.vm as any).agents?.length > 0
    })
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    // Dropdown should be open now
    const items = wrapper.findAll('[class*="cursor-pointer"]')
    expect(items.length).toBeGreaterThan(0)
  })

  it('filters agents when typing in the input', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: 'her' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    const items = wrapper.findAll('[class*="cursor-pointer"]')
    // Only 'hermes' matches 'her'
    expect(items.length).toBe(1)
    expect(items[0]!.text()).toContain('hermes')
  })

  it('emits update:modelValue and select:agent when an agent is clicked', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    const items = wrapper.findAll('[class*="cursor-pointer"]')
    expect(items.length).toBeGreaterThan(0)
    await items[0]!.trigger('mousedown')
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('select:agent')).toBeTruthy()
    const emittedAgent = wrapper.emitted('select:agent')![0]![0] as any
    expect(emittedAgent).toHaveProperty('name')
  })

  it('shows validation error when value does not match any known agent', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: 'unknown-agent' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    await wrapper.vm.$nextTick()
    const errorMsg = wrapper.find('p')
    expect(errorMsg.exists()).toBe(true)
    expect(errorMsg.text()).toContain('unknown-agent')
  })

  it('does not show validation error when modelValue is empty', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    await wrapper.vm.$nextTick()
    const errorMsg = wrapper.find('p')
    expect(errorMsg.exists()).toBe(false)
  })

  it('does not show validation error when modelValue exactly matches an agent name', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: 'atlas' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    await wrapper.vm.$nextTick()
    const errorMsg = wrapper.find('p')
    expect(errorMsg.exists()).toBe(false)
  })

  it('renders agent color avatar in the dropdown', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    // Avatar divs have inline style with background color
    const avatars = wrapper.findAll('[style*="background"]')
    expect(avatars.length).toBeGreaterThan(0)
  })

  it('renders agent icon in the dropdown avatar', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    // atlas has icon '🤖'
    expect(wrapper.html()).toContain('🤖')
  })

  it('falls back to first letter of agent name when icon is empty', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: 'zeu' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    // zeus has no icon, should show 'Z'
    expect(wrapper.html()).toContain('Z')
  })

  it('emits update:valid true when value is empty', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    await wrapper.vm.$nextTick()
    const validEmits = wrapper.emitted('update:valid')
    // Should have emitted at least once
    expect(validEmits).toBeTruthy()
    const lastEmit = validEmits![validEmits!.length - 1]
    expect(lastEmit![0]).toBe(true)
  })

  it('shows agent model in the dropdown list', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    expect(wrapper.html()).toContain('claude-3')
  })

  it('closes dropdown on Escape key', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    // Dropdown should be open
    expect((wrapper.vm as any).open).toBe(true)
    await input.trigger('keydown', { key: 'Escape' })
    expect((wrapper.vm as any).open).toBe(false)
  })

  it('moves cursor down on ArrowDown key', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    expect((wrapper.vm as any).cursor).toBe(0)
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect((wrapper.vm as any).cursor).toBe(1)
  })

  it('selects agent on Enter key when dropdown is open', async () => {
    const wrapper = mount(AgentPicker, { props: { modelValue: '' } })
    await vi.waitFor(() => (wrapper.vm as any).agents?.length > 0)
    const input = wrapper.find('input')
    await input.trigger('focus')
    await wrapper.vm.$nextTick()
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('select:agent')).toBeTruthy()
  })
})
