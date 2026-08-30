import { ref, computed, type Ref } from 'vue'
import type { ChatMessage } from './useSessions'

/**
 * Composable for unread-message tracking and the "jump to unread" pill.
 *
 * @param sessionId — reactive session ID (regular chat)
 * @param messages — reactive message list (session or space timeline)
 * @param messagesEl — ref to the scrollable messages container
 * @param spaceId — space timeline id in /space/:id mode. When set, unread is
 *   keyed on the space (or its active session), not the empty route sessionId.
 */
export function useUnreadTracking(
  sessionId: Ref<string | undefined>,
  messages: Ref<ChatMessage[]>,
  messagesEl: Ref<HTMLElement | undefined>,
  spaceId?: Ref<string | undefined>,
) {
  const lastSeenMessageCount = ref<Record<string, number>>({})
  const atBottom = ref(true)

  // Space mode has no route sessionId; key on the timeline so a scrolled-up
  // DM/channel can show a jump pill > 0.
  const trackingKey = computed(() => spaceId?.value || sessionId.value)

  const unreadCount = computed(() => {
    if (!trackingKey.value) return 0
    const seen = lastSeenMessageCount.value[trackingKey.value] ?? 0
    const total = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
    return Math.max(0, total - seen)
  })

  function onMessagesScroll() {
    const el = messagesEl.value
    if (!el) return
    const threshold = 80
    atBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    if (atBottom.value && trackingKey.value) {
      markCurrentSessionSeen()
    }
  }

  function markCurrentSessionSeen() {
    if (!trackingKey.value) return
    const count = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
    lastSeenMessageCount.value = { ...lastSeenMessageCount.value, [trackingKey.value]: count }
  }

  function jumpToUnread() {
    if (!trackingKey.value) return
    const seen = lastSeenMessageCount.value[trackingKey.value] ?? 0
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
