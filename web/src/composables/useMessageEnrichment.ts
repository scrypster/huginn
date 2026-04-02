import { computed, type Ref } from 'vue'
import type { SpaceMessage } from './useSpaceTimeline'
import type { ChatMessage } from './useSessions'

// ── Pure utility functions ──────────────────────────────────────────────

/** Human-friendly date label for message dividers ("Today", "Yesterday", "Mon, Mar 15"). */
export function dateLabelFor(ts: string | undefined): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const msgDay = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const diffDays = Math.round((today.getTime() - msgDay.getTime()) / 86400000)
  if (diffDays === 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

/** Check whether two ISO timestamps fall on the same calendar day. */
export function isSameDay(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false
  const da = new Date(a), db = new Date(b)
  return da.getFullYear() === db.getFullYear() &&
    da.getMonth() === db.getMonth() &&
    da.getDate() === db.getDate()
}

/**
 * Adapt SpaceMessage[] (which uses `ts` for timestamp) to the shape that
 * enrichedMessages / the template expect (which uses `createdAt`).
 */
export function adaptSpaceMessages(msgs: SpaceMessage[]): ChatMessage[] {
  return msgs.map(m => ({
    id: m.id,
    role: m.role as 'user' | 'assistant',
    content: m.content,
    agent: m.agent || undefined,
    createdAt: m.ts,
    // stream- prefix means the message is in-flight (status placeholder or live
    // token stream). Show the blinking cursor so the user knows content is arriving.
    streaming: m.id.startsWith('stream-'),
    // done is always true for persisted (API-loaded) tool calls; WS-streamed ones set it explicitly.
    toolCalls: (m.toolCalls ?? []).map(tc => ({ ...tc, done: tc.done ?? true })),
    // Carry through thread data attached by hydration or WS handlers.
    delegatedThreads: (m as any).delegatedThreads,
    threadReplies: (m as any).threadReplies,
    // Follow-up marker for header suppression logic. True when:
    //   (a) WS handler set it during live streaming (isFollowUp), OR
    //   (b) Server returned a persisted follow-up with parent_message_id.
    isFollowUp: !!(m as any).isFollowUp || !!m.parent_message_id,
    // Carry through follow-up streaming flags for token appending.
    followUpStreaming: (m as any).followUpStreaming,
    followUpThinking: (m as any).followUpThinking,
  })) as ChatMessage[]
}

// ── Enriched message type ──────────────────────────────────────────────

export type EnrichedMessage = ChatMessage & {
  showHeader: boolean
  dateLabel?: string
}

/**
 * enrichMessages adds display hints to each message:
 *   showHeader  — false when this is a continuation from same agent (collapses avatar + name)
 *   dateLabel   — set to "Today" / "Yesterday" / "Mon, Mar 15" when a date boundary is crossed
 */
export function enrichMessages(msgs: ChatMessage[]): EnrichedMessage[] {
  const result: EnrichedMessage[] = []
  for (let i = 0; i < msgs.length; i++) {
    const msg = msgs[i]!
    const prev = result[i - 1]

    // Date divider: show when this message is on a different day from the previous
    const ts = (msg as any).createdAt as string | undefined
    const prevTs = prev ? (prev as any).createdAt as string | undefined : undefined
    const dateLabel = (i === 0 || !isSameDay(ts, prevTs)) ? dateLabelFor(ts) : undefined

    // Header suppression: hide avatar+name for continuations from same agent.
    // A message is a "continuation" when all of:
    //   1. Same role as previous
    //   2. Same agent name (assistant) or both user messages
    //   3. No date boundary between them
    //   4. Previous message is not a threadSummary separator
    //   5. Current message is not a follow-up synthesis (isFollowUp flag)
    //   6. Messages are within 60s of each other (time gap → new thought)
    let showHeader = true
    if (prev && !dateLabel) {
      const sameRole = msg.role === prev.role
      const prevIsThreadSummary = !!(prev as any).threadSummary
      const currIsThreadSummary = !!(msg as any).threadSummary
      const currIsFollowUp = !!(msg as any).isFollowUp
      // Time gap check: if > 60s between messages, treat as new thought (show header).
      // This naturally separates Tom's @mention response from Tom's follow-up synthesis,
      // even after page refresh when the isFollowUp flag is lost.
      const prevTime = prev.createdAt ? new Date(prev.createdAt).getTime() : 0
      const currTime = ts ? new Date(ts).getTime() : 0
      const timeGapSec = (prevTime && currTime) ? (currTime - prevTime) / 1000 : 0
      const hasLargeGap = timeGapSec > 60
      if (sameRole && !prevIsThreadSummary && !currIsThreadSummary && !currIsFollowUp && !hasLargeGap) {
        if (msg.role === 'user') {
          showHeader = false
        } else if (msg.role === 'assistant') {
          const sameAgent = (msg.agent || '') === (prev.agent || '')
          if (sameAgent) showHeader = false
        }
      }
    }

    result.push({ ...msg, showHeader, dateLabel } as EnrichedMessage)
  }
  return result
}

/**
 * Composable that wraps enrichMessages in a computed property.
 * Pass in the reactive messages ref and get back enrichedMessages.
 */
export function useMessageEnrichment(messages: Ref<ChatMessage[]>) {
  const enrichedMessages = computed((): EnrichedMessage[] => {
    return enrichMessages(messages.value)
  })

  return { enrichedMessages }
}
