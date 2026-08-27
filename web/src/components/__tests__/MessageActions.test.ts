import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import MessageActions from '../MessageActions.vue'
import type { ChatMessage, ToolCallRecord } from '../../composables/useSessions'

// Mock useApi so tests don't make real HTTP calls
vi.mock('../../composables/useApi', () => ({
  api: {
    muninn: {
      remember: vi.fn(),
    },
  },
}))

import { api } from '../../composables/useApi'
const mockRemember = vi.mocked(api.muninn.remember)

function makeMsg(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello world',
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

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
    expect(wrapper.find('[data-testid="msg-saved-indicator"]').exists()).toBe(true)
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

  describe('Save to Memory button feedback', () => {
    it('shows "Saving…" while the API call is in flight', async () => {
      let resolve!: () => void
      mockRemember.mockReturnValue(new Promise(r => { resolve = r }))

      const wrapper = mount(MessageActions, {
        props: { msg: makeMsg({ role: 'assistant', content: 'remember this' }), agentVaultName: 'vault-x' },
      })

      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
      // While in flight the button should show "Saving…"
      expect(wrapper.find('[data-testid="msg-save-memory"]').text()).toBe('Saving…')

      resolve()
      await flushPromises()
    })

    it('shows "Saved ✓" indicator after successful save', async () => {
      mockRemember.mockResolvedValue({})

      const wrapper = mount(MessageActions, {
        props: { msg: makeMsg({ role: 'assistant', content: 'remember this' }), agentVaultName: 'vault-x' },
      })

      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="msg-saved-indicator"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="msg-saved-indicator"]').text()).toContain('Saved')
      expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
    })

    it('calls api.muninn.remember with correct vault and content', async () => {
      mockRemember.mockResolvedValue({})

      const wrapper = mount(MessageActions, {
        props: { msg: makeMsg({ role: 'assistant', content: 'store this' }), agentVaultName: 'my-vault' },
      })

      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
      await flushPromises()

      expect(mockRemember).toHaveBeenCalledOnce()
      expect(mockRemember).toHaveBeenCalledWith('my-vault', 'store this')
    })

    it('shows "Save failed" on API error', async () => {
      mockRemember.mockRejectedValue(new Error('network error'))

      const wrapper = mount(MessageActions, {
        props: { msg: makeMsg({ role: 'assistant', content: 'oops' }), agentVaultName: 'vault-x' },
      })

      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="msg-save-error"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="msg-save-error"]').text()).toBe('Save failed')
      expect(wrapper.find('[data-testid="msg-save-memory"]').exists()).toBe(false)
    })

    it('does not call API a second time if already saving', async () => {
      let resolve!: () => void
      mockRemember.mockReturnValue(new Promise(r => { resolve = r }))

      const wrapper = mount(MessageActions, {
        props: { msg: makeMsg({ role: 'assistant', content: 'double click' }), agentVaultName: 'vault-x' },
      })

      // Trigger two clicks rapidly
      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')
      await wrapper.find('[data-testid="msg-save-memory"]').trigger('click')

      resolve()
      await flushPromises()

      expect(mockRemember).toHaveBeenCalledOnce()
    })
  })
})

  it('shows Reply when showReply is set', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user' }), agentVaultName: '', showReply: true },
    })
    expect(wrapper.find('[data-testid="msg-reply"]').exists()).toBe(true)
    await wrapper.find('[data-testid="msg-reply"]').trigger('click')
    expect(wrapper.emitted('reply')).toBeTruthy()
  })

  it('hides Reply by default so session chat is unchanged', () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg({ role: 'user' }), agentVaultName: '' },
    })
    expect(wrapper.find('[data-testid="msg-reply"]').exists()).toBe(false)
  })

  it('shows Details diagnose only when asked', async () => {
    const wrapper = mount(MessageActions, {
      props: { msg: makeMsg(), agentVaultName: '', showDiagnose: true },
    })
    expect(wrapper.find('[data-testid="msg-diagnose"]').exists()).toBe(true)
    await wrapper.find('[data-testid="msg-diagnose"]').trigger('click')
    expect(wrapper.emitted('diagnose')).toBeTruthy()
  })

