import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MessageActions from '../MessageActions.vue'
import type { ChatMessage, ToolCallRecord } from '../../composables/useSessions'

function makeMsg(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello world',
    ...overrides,
  }
}

describe('MessageActions', () => {
  it('shows copy button for all messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg(), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-copy"]').exists()).toBe(true)
  })

  it('shows retry button for user messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-retry"]').exists()).toBe(true)
  })

  it('hides retry button for assistant messages', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-retry"]').exists()).toBe(false)
  })

  it('hides save-to-memory when agent has no vault', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
  })

  it('shows "Saved ✓" when agent already called muninn_remember', () => {
    const toolCalls: ToolCallRecord[] = [
      { id: 't1', name: 'muninn_remember', args: {}, done: true },
    ]
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', toolCalls }), agentVaultName: 'my-vault' },
    })
    expect(wrapper.text()).toContain('Saved')
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
  })

  it('shows save button when vault set and agent did not call memory tools', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', toolCalls: [] }), agentVaultName: 'my-vault' },
    })
    expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(true)
  })

  it('emits retry with message content on retry click', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user', content: 'retry me' }), agentVaultName: '' },
    })
    await wrapper.find('[data-testid="msg-retry"]').trigger('click')
    expect(wrapper.emitted('retry')?.[0]).toEqual(['retry me'])
  })

  it('emits save-memory with vault and content on save click', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'assistant', content: 'remember this' }), agentVaultName: 'vault-x' },
    })
    await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
    expect(wrapper.emitted('save-memory')?.[0]).toEqual([{ vault: 'vault-x', content: 'remember this' }])
  })
})
