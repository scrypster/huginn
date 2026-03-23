import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ObservationDeck from '../ObservationDeck.vue'
import type { ThreadMessage } from '../../composables/useThreadDetail'

function makeMessage(overrides: Partial<ThreadMessage> = {}): ThreadMessage {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello',
    agent: 'atlas',
    seq: 1,
    created_at: new Date().toISOString(),
    ...overrides,
  }
}

function makeToolCall(toolName: string): ThreadMessage {
  return makeMessage({
    role: 'tool_call',
    content: JSON.stringify({ name: toolName }),
  })
}

describe('ObservationDeck — toggle', () => {
  it('renders the toggle button with agent name', () => {
    const wrapper = mount(ObservationDeck, {
      props: { messages: [], agentName: 'atlas' },
    })
    expect(wrapper.html()).toContain('atlas')
    expect(wrapper.find('button').exists()).toBe(true)
  })

  it('is collapsed by default (steps not visible)', () => {
    const messages = [makeToolCall('read_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    // The steps list is inside v-if="deckOpen" which is false by default
    expect(wrapper.find('ol').exists()).toBe(false)
  })

  it('expands and shows steps when toggle button is clicked', async () => {
    const messages = [makeToolCall('read_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.find('ol').exists()).toBe(true)
  })

  it('collapses again when toggle button is clicked twice', async () => {
    const messages = [makeToolCall('read_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.find('ol').exists()).toBe(true)
    await wrapper.find('button').trigger('click')
    expect(wrapper.find('ol').exists()).toBe(false)
  })
})

describe('ObservationDeck — empty state', () => {
  it('shows "No steps recorded" when expanded with no processable messages', async () => {
    // Only a user message — no tool calls, no assistant messages
    const messages = [makeMessage({ role: 'user', content: 'hi' })]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('No steps recorded')
  })
})

describe('ObservationDeck — tool call steps', () => {
  it('shows a read step when tool name contains "read"', async () => {
    const messages = [makeToolCall('read_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('read')
    expect(wrapper.html()).toContain('file')
  })

  it('shows a search step when tool name contains "search"', async () => {
    const messages = [makeToolCall('search_codebase')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('searched')
  })

  it('shows a bash step when tool name contains "bash"', async () => {
    const messages = [makeToolCall('bash_execute')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('shell')
  })

  it('shows a write step when tool name contains "write"', async () => {
    const messages = [makeToolCall('write_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('edit')
  })

  it('shows a recall/memory step when tool name contains "recall"', async () => {
    const messages = [makeToolCall('recall_memories')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('recalled')
  })

  it('shows delegation step for delegation_chain messages', async () => {
    const messages = [makeMessage({ type: 'delegation_chain', role: 'assistant', content: '' })]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('delegated')
  })

  it('shows response message count for assistant messages with content', async () => {
    const messages = [makeMessage({ role: 'assistant', content: 'Here is my answer' })]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('produced')
    expect(wrapper.html()).toContain('response')
  })

  it('uses agentName in step descriptions', async () => {
    const messages = [makeToolCall('read_file')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'hermes' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('hermes')
  })

  it('shows singular "file" for a single read tool call', async () => {
    const messages = [makeToolCall('read_document')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('1')
    expect(wrapper.html()).toContain('file')
  })

  it('shows plural "files" for multiple read tool calls', async () => {
    const messages = [makeToolCall('read_one'), makeToolCall('get_two'), makeToolCall('fetch_three')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('3')
    expect(wrapper.html()).toContain('files')
  })

  it('shows fallback "used X tools" step when only tool_result messages present', async () => {
    const messages = [
      makeMessage({ role: 'tool_result', content: 'result1' }),
      makeMessage({ role: 'tool_result', content: 'result2' }),
    ]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.html()).toContain('2')
    expect(wrapper.html()).toContain('tools')
  })

  it('renders numbered list items for each step', async () => {
    const messages = [makeToolCall('read_file'), makeToolCall('bash_run')]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    const items = wrapper.findAll('li')
    expect(items.length).toBeGreaterThanOrEqual(2)
  })

  it('handles non-JSON tool call content gracefully', async () => {
    const messages = [makeMessage({ role: 'tool_call', content: 'read_plain_text' })]
    const wrapper = mount(ObservationDeck, {
      props: { messages, agentName: 'atlas' },
    })
    await wrapper.find('button').trigger('click')
    // Should not throw, and may render a step
    expect(wrapper.find('ol').exists()).toBe(true)
  })
})
