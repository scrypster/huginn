import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { useUnreadTracking } from '../useUnreadTracking'

function setScrollMetrics(el: HTMLElement, scrollHeight: number, scrollTop: number, clientHeight: number): void {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: scrollHeight })
  Object.defineProperty(el, 'scrollTop', { configurable: true, value: scrollTop, writable: true })
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: clientHeight })
}

describe('useUnreadTracking', () => {
  it('computes unread count from user/assistant messages only', () => {
    const sessionId = ref('sess-1')
    const messages = ref([
      { id: 'u1', role: 'user', content: 'hi' },
      { id: 'a1', role: 'assistant', content: 'hello' },
      { id: 't1', role: 'tool', content: 'ignored' } as any,
    ])
    const messagesEl = ref<HTMLElement | undefined>(undefined)

    const unread = useUnreadTracking(sessionId, messages as any, messagesEl)
    expect(unread.unreadCount.value).toBe(2)
  })

  it('marks session seen when scrolled near bottom', () => {
    const sessionId = ref('sess-2')
    const messages = ref([
      { id: 'u1', role: 'user', content: 'hi' },
      { id: 'a1', role: 'assistant', content: 'hello' },
    ])
    const el = document.createElement('div')
    setScrollMetrics(el, 1000, 925, 40) // gap = 35 < threshold(80)
    const messagesEl = ref<HTMLElement | undefined>(el)

    const unread = useUnreadTracking(sessionId, messages as any, messagesEl)
    unread.onMessagesScroll()

    expect(unread.atBottom.value).toBe(true)
    expect(unread.unreadCount.value).toBe(0)
  })

  it('jumpToUnread scrolls first unread message into view and marks seen', () => {
    const sessionId = ref('sess-3')
    const messages = ref([
      { id: 'u1', role: 'user', content: 'first' },
      { id: 'a1', role: 'assistant', content: 'second' },
      { id: 'a2', role: 'assistant', content: 'third' },
    ])
    const el = document.createElement('div')
    const firstUnread = document.createElement('div')
    firstUnread.setAttribute('data-msg-id', 'a2')
    const scrollIntoView = vi.fn()
    ;(firstUnread as any).scrollIntoView = scrollIntoView
    el.appendChild(firstUnread)
    ;(el as any).querySelector = vi.fn((sel: string) => {
      if (sel === '[data-msg-id="a2"]') return firstUnread
      return null
    })

    const messagesEl = ref<HTMLElement | undefined>(el)
    const unread = useUnreadTracking(sessionId, messages as any, messagesEl)

    // Pretend user has already seen first two.
    unread.markCurrentSessionSeen()
    messages.value.push({ id: 'a2', role: 'assistant', content: 'third' } as any)
    expect(unread.unreadCount.value).toBe(1)

    unread.jumpToUnread()
    expect(scrollIntoView).toHaveBeenCalledTimes(1)
    expect(unread.unreadCount.value).toBe(0)
  })
})
