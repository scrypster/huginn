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
  // Hard ceiling: even with an activity probe re-arming the watchdog, a run that
  // has produced no token/done for this long is force-reset so the composer is
  // never permanently locked.
  const STREAMING_WATCHDOG_CEILING_MS = 600_000
  let streamingWatchdog: ReturnType<typeof setTimeout> | null = null

  /**
   * Arm (or re-arm) the streaming watchdog.
   *
   * @param activityProbe optional callback consulted when the timer fires; if it
   * returns true (thinking/status/tool events are still flowing for this run),
   * the watchdog re-arms instead of resetting, up to STREAMING_WATCHDOG_CEILING_MS.
   * Callers re-invoke this on each token, which restarts the ceiling.
   */
  function startStreamingWatchdog(activityProbe?: () => boolean) {
    if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
    const armedAt = Date.now()
    const arm = () => {
      streamingWatchdog = setTimeout(() => {
        streamingWatchdog = null
        if (!streaming.value) return
        const withinCeiling = Date.now() - armedAt < STREAMING_WATCHDOG_CEILING_MS
        if (withinCeiling && activityProbe?.()) { arm(); return }
        console.warn('[chat] streaming watchdog: no activity for 60s — resetting streaming state')
        streaming.value = false
        activeToolCalls.value = []
      }, STREAMING_WATCHDOG_MS)
    }
    arm()
  }

  function clearStreamingWatchdog() {
    if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
  }

  // ── Elapsed timer ─────────────────────────────────────────────────────
  // Tracks seconds since the current stream started. Shown in the status bar
  // after 10s so users know a long-running agent hasn't frozen.
  const streamingElapsed = ref(0)
  let elapsedInterval: ReturnType<typeof setInterval> | null = null

  function startElapsedTimer() {
    streamingElapsed.value = 0
    if (elapsedInterval !== null) { clearInterval(elapsedInterval); elapsedInterval = null }
    elapsedInterval = setInterval(() => { streamingElapsed.value++ }, 1000)
  }

  function stopElapsedTimer() {
    if (elapsedInterval !== null) { clearInterval(elapsedInterval); elapsedInterval = null }
    streamingElapsed.value = 0
  }

  function formatElapsed(s: number): string {
    if (s < 60) return `${s}s`
    return `${Math.floor(s / 60)}m ${s % 60}s`
  }

  function toggleMsgToolCalls(msgId: string) {
    if (expandedMsgCalls.value.has(msgId)) expandedMsgCalls.value.delete(msgId)
    else expandedMsgCalls.value.add(msgId)
  }

  /** Reset all streaming state (used on session switch). */
  function resetStreaming() {
    clearStreamingWatchdog()
    stopElapsedTimer()
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
    streamingElapsed,
    startStreamingWatchdog,
    clearStreamingWatchdog,
    startElapsedTimer,
    stopElapsedTimer,
    formatElapsed,
    toggleMsgToolCalls,
    resetStreaming,
  }
}
