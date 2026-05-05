import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { useChatSearch } from '../useChatSearch'

describe('useChatSearch', () => {
  it('computes case-insensitive matches and excludes tool roles', () => {
    const messages = ref([
      { id: 'u1', role: 'user', content: 'Alpha hello' },
      { id: 'a1', role: 'assistant', content: 'HELLO there' },
      { id: 't1', role: 'tool_call', content: 'hello from tool' },
      { id: 't2', role: 'tool_result', content: 'hello from result' },
    ] as any)
    const messagesEl = ref<HTMLElement | undefined>(undefined)

    const search = useChatSearch(messages as any, messagesEl)
    search.chatSearchQuery.value = 'hello'

    expect(search.chatSearchMatches.value).toEqual(['u1', 'a1'])
  })

  it('openChatSearch focuses input and closeChatSearch resets state', async () => {
    const messages = ref([{ id: 'm1', role: 'assistant', content: 'hello' }] as any)
    const messagesEl = ref<HTMLElement | undefined>(undefined)
    const search = useChatSearch(messages as any, messagesEl)

    search.chatSearchQuery.value = 'hello'
    search.chatSearchIndex.value = 1
    const focus = vi.fn()
    search.chatSearchInputEl.value = { focus } as any

    search.openChatSearch()
    await nextTick()
    expect(search.chatSearchOpen.value).toBe(true)
    expect(search.chatSearchIndex.value).toBe(0)
    expect(focus).toHaveBeenCalledTimes(1)

    search.closeChatSearch()
    expect(search.chatSearchOpen.value).toBe(false)
    expect(search.chatSearchQuery.value).toBe('')
    expect(search.chatSearchIndex.value).toBe(0)
  })

  it('next/prev wraps index and scrolls selected match into view', () => {
    const messages = ref([
      { id: 'm1', role: 'assistant', content: 'hello one' },
      { id: 'm2', role: 'assistant', content: 'hello two' },
    ] as any)
    const messagesContainer = document.createElement('div')
    const msg1 = document.createElement('div')
    const msg2 = document.createElement('div')
    const scroll1 = vi.fn()
    const scroll2 = vi.fn()
    ;(msg1 as any).scrollIntoView = scroll1
    ;(msg2 as any).scrollIntoView = scroll2
    ;(messagesContainer as any).querySelector = vi.fn((selector: string) => {
      if (selector === '[data-msg-id="m1"]') return msg1
      if (selector === '[data-msg-id="m2"]') return msg2
      return null
    })

    const messagesEl = ref<HTMLElement | undefined>(messagesContainer)
    const search = useChatSearch(messages as any, messagesEl)
    search.chatSearchQuery.value = 'hello'

    // Start at first match, go next -> second.
    search.nextChatSearchMatch()
    expect(search.chatSearchIndex.value).toBe(1)
    expect(scroll2).toHaveBeenCalledTimes(1)

    // Next wraps to first.
    search.nextChatSearchMatch()
    expect(search.chatSearchIndex.value).toBe(0)
    expect(scroll1).toHaveBeenCalledTimes(1)

    // Prev wraps back to second.
    search.prevChatSearchMatch()
    expect(search.chatSearchIndex.value).toBe(1)
    expect(scroll2).toHaveBeenCalledTimes(2)
  })
})
