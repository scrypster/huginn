import { ref, computed, type Ref } from 'vue'
import type { ChatMessage } from './useSessions'

/**
 * Composable for unread-message tracking and the "jump to unread" pill.
 *
 * @param sessionId — reactive getter for the current session ID
 * @param messages — reactive message list
 * @param messagesEl — ref to the scrollable messages container
 */
export function useUnreadTracking(
  sessionId: Ref<string | undefined>,
  messages: Ref<ChatMessage[]>,
  messagesEl: Ref<HTMLElement | undefined>,
) {
  const lastSeenMessageCount = ref<Record<string, number>>({})
  const atBottom = ref(true)

  const unreadCount = computed(() => {
    if (!sessionId.value) return 0
    const seen = lastSeenMessageCount.value[sessionId.value] ?? 0
    const total = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
    return Math.max(0, total - seen)
  })

  function onMessagesScroll() {
    const el = messagesEl.value
    if (!el) return
    const threshold = 80
    atBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    if (atBottom.value && sessionId.value) {
      markCurrentSessionSeen()
    }
  }

  function markCurrentSessionSeen() {
    if (!sessionId.value) return
    const count = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
    lastSeenMessageCount.value = { ...lastSeenMessageCount.value, [sessionId.value]: count }
  }

  function jumpToUnread() {
    if (!sessionId.value) return
    const seen = lastSeenMessageCount.value[sessionId.value] ?? 0
    const relevant = messages.value.filter(m => m.role === 'assistant' || m.role === 'user')
    const firstUnread = relevant[seen]
    if (firstUnread) {
      const el = messagesEl.value?.querySelector(`[data-msg-id="${firstUnread.id}"]`)
      el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    } else {
      messagesEl.value?.scrollTo({ top: messagesEl.value.scrollHeight, behavior: 'smooth' })
    }
    markCurrentSessionSeen()
  }

  return {
    atBottom,
    unreadCount,
    onMessagesScroll,
    markCurrentSessionSeen,
    jumpToUnread,
  }
}
