import { ref } from 'vue'

export interface ActiveToolCall {
  id: string
  name: string
  args: Record<string, unknown>
}

/**
 * Composable for chat streaming state: streaming flag, active tool calls,
 * run ID tracking, and the streaming watchdog timer.
 */
export function useChatStreaming() {
  const activeToolCalls = ref<ActiveToolCall[]>([])
  const expandedToolCalls = ref<Set<string>>(new Set())
  const expandedMsgCalls = ref<Set<string>>(new Set())
  const streaming = ref(false)
  const currentRunId = ref('')
  const notifyStreaming = ref(false)

  // ── Streaming watchdog ────────────────────────────────────────────────
  // If no token/done/error arrives within 60s of starting a run, reset streaming
  // so the user is not permanently locked out of sending.
  const STREAMING_WATCHDOG_MS = 60_000
  let streamingWatchdog: ReturnType<typeof setTimeout> | null = null

  function startStreamingWatchdog() {
    if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
    streamingWatchdog = setTimeout(() => {
      if (streaming.value) {
        console.warn('[chat] streaming watchdog: no activity for 60s — resetting streaming state')
        streaming.value = false
        activeToolCalls.value = []
      }
      streamingWatchdog = null
    }, STREAMING_WATCHDOG_MS)
  }

  function clearStreamingWatchdog() {
    if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
  }

  function toggleMsgToolCalls(msgId: string) {
    if (expandedMsgCalls.value.has(msgId)) expandedMsgCalls.value.delete(msgId)
    else expandedMsgCalls.value.add(msgId)
  }

  /** Reset all streaming state (used on session switch). */
  function resetStreaming() {
    clearStreamingWatchdog()
    streaming.value = false
    currentRunId.value = ''
    notifyStreaming.value = false
    activeToolCalls.value = []
  }

  return {
    activeToolCalls,
    expandedToolCalls,
    expandedMsgCalls,
    streaming,
    currentRunId,
    notifyStreaming,
    startStreamingWatchdog,
    clearStreamingWatchdog,
    toggleMsgToolCalls,
    resetStreaming,
  }
}
