import { ref } from 'vue'
import { api, getToken } from './useApi'

// hydrationQueueOverflowed is set to true when any session's pre-hydration WS
// event queue exceeds MAX_HYDRATION_QUEUE_SIZE. Components can watch this ref
// to show a user-visible warning (e.g. an amber toast) and then reset it to
// false once the warning has been acknowledged or auto-dismissed.
export const hydrationQueueOverflowed = ref(false)

export interface Session {
  id: string
  agent_id: string
  agent?: string // primary agent name from session manifest
  state: string
  created_at: string
  updated_at: string
  title?: string
}

export interface ToolCallRecord {
  id: string
  name: string
  args: Record<string, unknown>
  result?: string
  done: boolean
}

export interface DelegatedThread {
  threadId: string
  agentId: string
  msgId?: string          // parent message ID for fetching thread messages (GET /api/v1/messages/{id}/thread)
  done?: boolean
  replyCount?: number     // actual thread reply count from DB (for badge label)
  inlineSummary?: string  // thread completion summary shown inline (Slack-style thread preview)
}

export interface ThreadReply {
  id: string
  agent: string
  content: string
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  agent?: string          // which agent sent this message (for per-message attribution)
  createdAt?: string      // ISO timestamp for message header display
  streaming?: boolean
  toolCalls?: ToolCallRecord[]
  delegatedThreads?: DelegatedThread[]  // threads spawned by this message
  threadReplies?: ThreadReply[]         // inline thread replies from agent_follow_up (Slack-style)
  replyCount?: number     // thread reply count (for badge display after hydration)
}

// Module-level shared state (singleton across all component instances)
const sessions = ref<Session[]>([])
const loading = ref(false)
const messagesBySession = ref<Record<string, ChatMessage[]>>({})

// Hydration pattern: "hydrate-then-subscribe"
//
// When a session is first opened (or on page reload), fetchMessages() loads the
// full message history from the server REST API and stores it in messagesBySession.
// While that HTTP fetch is in flight, any WS events for the same session are
// buffered in preHydrationQueue rather than being applied immediately — applying
// them before the history is loaded would cause duplicates or out-of-order messages.
// Once the fetch completes (or times out), all buffered handlers are flushed in
// order, after which WS events flow normally.
//
// Safety limits:
//   - Per-session queue is capped at MAX_HYDRATION_QUEUE_SIZE events. If the cap
//     is exceeded the oldest event is dropped (FIFO eviction) to bound memory use.
//   - The REST fetch has a 30-second AbortController timeout. If it times out,
//     the session is still marked hydrated and the queue is flushed so live WS
//     streaming is never permanently blocked.
const hydrated = new Set<string>()
const preHydrationQueue = new Map<string, Array<() => void>>()

/** Maximum WS events buffered per session while a history fetch is in flight. */
const MAX_HYDRATION_QUEUE_SIZE = 500

// queueIfHydrating queues handler for later execution if sessionId is currently
// being hydrated (fetch in progress). Returns true if queued, false if the
// session is already hydrated and the caller should process immediately.
// If the queue for the session exceeds MAX_HYDRATION_QUEUE_SIZE, the oldest
// handler is dropped to prevent unbounded memory growth when the server hangs.
function queueIfHydrating(sessionId: string, handler: () => void): boolean {
  const q = preHydrationQueue.get(sessionId)
  if (q !== undefined) {
    if (q.length >= MAX_HYDRATION_QUEUE_SIZE) {
      // Drop the oldest buffered event (FIFO eviction) to cap memory use.
      q.shift()
      console.warn(`[useSessions] hydration queue for session ${sessionId} exceeded ${MAX_HYDRATION_QUEUE_SIZE} events; dropping oldest`)
      // Signal UI components to show a user-visible warning. Reset to false
      // after the session finishes hydrating (see flushQueue below).
      hydrationQueueOverflowed.value = true
    }
    q.push(handler)
    return true
  }
  return false
}

export function useSessions() {
  async function fetchSessions() {
    loading.value = true
    try {
      const data = await api.sessions.list() as unknown
      if (Array.isArray(data)) {
        sessions.value = data as Session[]
      } else {
        sessions.value = (data as { sessions?: Session[] }).sessions ?? []
      }
    } catch {
      // ignore — server may not be fully ready
    } finally {
      loading.value = false
    }
  }

  async function createSession(spaceId?: string): Promise<Session> {
    const data = await api.sessions.create(spaceId) as unknown as { session_id?: string; id?: string }
    const id = data.id ?? data.session_id ?? crypto.randomUUID()
    const session: Session = {
      id,
      agent_id: 'default',
      state: 'idle',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
    sessions.value.unshift(session)
    return session
  }

  async function deleteSession(id: string) {
    try {
      await fetch(`/api/v1/sessions/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${getToken()}` },
      })
    } catch { /* ignore */ }
    sessions.value = sessions.value.filter(s => s.id !== id)
    delete messagesBySession.value[id]
    // Clean up hydration state so the session can be re-fetched if re-created.
    hydrated.delete(id)
    preHydrationQueue.delete(id) // discard any buffered handlers — session is gone
  }

  async function renameSession(id: string, title: string) {
    const sess = sessions.value.find(s => s.id === id)
    const prev = sess?.title
    if (sess) sess.title = title          // optimistic update
    try {
      await api.sessions.rename(id, title)
    } catch {
      if (sess) sess.title = prev         // revert on error
    }
  }

  function getMessages(sessionId: string): ChatMessage[] {
    if (!messagesBySession.value[sessionId]) {
      messagesBySession.value[sessionId] = []
    }
    return messagesBySession.value[sessionId]
  }

  // fetchMessages loads persisted message history from the server for a session.
  // Uses the "hydrate-then-subscribe" pattern described at the top of this file:
  // WS events received while the fetch is in flight are buffered (via
  // queueIfHydrating) so they don't race with the DB fetch and cause duplicates
  // or out-of-order messages on page reload.
  //
  // A 30-second AbortController timeout guards against a hanging server: if the
  // fetch takes longer than 30s, the session is still marked hydrated and the
  // buffered WS events are flushed so live-streaming is never permanently blocked.
  async function fetchMessages(sessionId: string): Promise<void> {
    if (!sessionId) return
    if (hydrated.has(sessionId)) return         // already loaded from DB
    if (preHydrationQueue.has(sessionId)) return // fetch already in flight

    // Begin buffering WS events for this session until the fetch completes.
    preHydrationQueue.set(sessionId, [])

    // 30-second timeout: if the server hangs, we still flush the queue so that
    // live WS streaming is not permanently blocked for this session.
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 30_000)
    try {
      const raw = await api.sessions.getMessages(sessionId, { signal: controller.signal })
      const msgs: ChatMessage[] = (Array.isArray(raw) ? raw : [])
        .filter((m) => {
          const r = m as Record<string, unknown>
          return (r.role === 'user' || r.role === 'assistant') && r.type !== 'cost'
        })
        .map((m) => {
          const r = m as Record<string, unknown>
          // Map persisted tool_calls so the "N tool calls · done" chip renders
          // on page reload, not just during live streaming.
          const rawToolCalls = r.tool_calls as Array<Record<string, unknown>> | undefined
          const toolCalls: ToolCallRecord[] | undefined = rawToolCalls?.length
            ? rawToolCalls.map(tc => ({
                id: (tc.id as string) ?? '',
                name: (tc.name as string) ?? '',
                args: (tc.args as Record<string, unknown>) ?? {},
                result: (tc.result as string | undefined) ?? undefined,
                done: true,
              }))
            : undefined
          return {
            id: r.id as string,
            role: r.role as 'user' | 'assistant',
            content: r.content as string,
            agent: (r.agent as string | undefined) || undefined,
            createdAt: (r.ts as string | undefined) || undefined,
            toolCalls,
          }
        })
      messagesBySession.value[sessionId] = msgs
    } catch {
      // Ignore — server may not have messages for this session, or the
      // 30-second timeout fired. Either way we fall through to flush the queue.
    } finally {
      clearTimeout(timeoutId)
    }

    // Mark session as hydrated regardless of success/timeout, then flush any
    // buffered WS events so live-streaming continues from the correct base state.
    hydrated.add(sessionId)
    const queue = preHydrationQueue.get(sessionId) ?? []
    preHydrationQueue.delete(sessionId)
    for (const fn of queue) fn()
    // Reset the overflow flag after flushing so the warning auto-clears once
    // the hydration backlog has been processed.
    hydrationQueueOverflowed.value = false
  }

  function formatSessionLabel(session: Session): string {
    if (session.title) return session.title
    const d = new Date(session.created_at)
    if (isNaN(d.getTime())) return session.id.slice(0, 8)
    return d.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    })
  }

  return {
    sessions,
    loading,
    fetchSessions,
    createSession,
    deleteSession,
    renameSession,
    getMessages,
    fetchMessages,
    queueIfHydrating,
    formatSessionLabel,
  }
}
