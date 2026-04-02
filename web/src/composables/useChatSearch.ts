import { ref, computed, nextTick, type Ref } from 'vue'
import type { ChatMessage } from './useSessions'

/**
 * Composable encapsulating in-chat search (Ctrl+F / Cmd+F) state and methods.
 *
 * @param messages — reactive message list to search through
 * @param messagesEl — ref to the scrollable messages container (for scrollIntoView)
 */
export function useChatSearch(
  messages: Ref<ChatMessage[]>,
  messagesEl: Ref<HTMLElement | undefined>,
) {
  const chatSearchOpen = ref(false)
  const chatSearchQuery = ref('')
  const chatSearchIndex = ref(0)
  const chatSearchInputEl = ref<HTMLInputElement | null>(null)

  const chatSearchMatches = computed((): string[] => {
    const q = chatSearchQuery.value.trim().toLowerCase()
    if (!q) return []
    return messages.value
      .filter(m => m.content?.toLowerCase().includes(q) && (m.role as string) !== 'tool_call' && (m.role as string) !== 'tool_result')
      .map(m => m.id)
  })

  function openChatSearch() {
    chatSearchOpen.value = true
    chatSearchIndex.value = 0
    nextTick(() => chatSearchInputEl.value?.focus())
  }

  function closeChatSearch() {
    chatSearchOpen.value = false
    chatSearchQuery.value = ''
    chatSearchIndex.value = 0
  }

  function scrollToSearchMatch() {
    const id = chatSearchMatches.value[chatSearchIndex.value]
    if (!id) return
    const el = messagesEl.value?.querySelector(`[data-msg-id="${id}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }

  function nextChatSearchMatch() {
    if (!chatSearchMatches.value.length) return
    chatSearchIndex.value = (chatSearchIndex.value + 1) % chatSearchMatches.value.length
    scrollToSearchMatch()
  }

  function prevChatSearchMatch() {
    if (!chatSearchMatches.value.length) return
    chatSearchIndex.value = (chatSearchIndex.value - 1 + chatSearchMatches.value.length) % chatSearchMatches.value.length
    scrollToSearchMatch()
  }

  return {
    chatSearchOpen,
    chatSearchQuery,
    chatSearchIndex,
    chatSearchInputEl,
    chatSearchMatches,
    openChatSearch,
    closeChatSearch,
    nextChatSearchMatch,
    prevChatSearchMatch,
  }
}
