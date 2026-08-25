import { ref } from 'vue'
import { api, getToken } from './useApi'

// hydrationQueueOverflowed is set to true when any session's pre-hydration WS
// event queue exceeds MAX_HYDRATION_QUEUE_SIZE. Components can watch this ref
// to show a user-visible warning (e.g. an amber toast) and then reset it to
// false once the warning has been acknowledged or auto-dismissed.
export const hydrationQueueOverflowed = ref(false)
const hydrationQueueOverflowBySession = ref<Record<string, true>>({})

function setHydrationOverflow(sessionId: string, overflowed: boolean): void {
  if (!sessionId) return
  if (overflowed) {
    hydrationQueueOverflowBySession.value = {
      ...hydrationQueueOverflowBySession.value,
      [sessionId]: true,
    }
  } else if (hydrationQueueOverflowBySession.value[sessionId]) {
    const next = { ...hydrationQueueOverflowBySession.value }
    delete next[sessionId]
    hydrationQueueOverflowBySession.value = next
  }
  hydrationQueueOverflowed.value = Object.keys(hydrationQueueOverflowBySession.value).length > 0
}

export interface Session {
  id: string
  agent_id: string
  agent?: string // primary agent name from session manifest
  state: string
  created_at: string
  updated_at: string
  title?: string
  external_kind?: string
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
  task?: string           // task description delegated to this agent (from thread_started payload)
  done?: boolean
  replyCount?: number     // actual thread reply count from DB (for badge label)
  inlineSummary?: string  // thread completion summary shown inline (Slack-style thread preview)
}

export interface ThreadReply {
  id: string
  agent: string
  content: string
}

export interface PermissionDenial {
  threadId: string
  agentId: string
  tool: string
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
  permissionDenials?: PermissionDenial[]
  threadSummary?: boolean       // true for synthetic completion cards injected on thread_done
  threadSummaryThreadId?: string // thread ID this completion card belongs to (dedup guard)
  // Space-mode fields present on messages fetched from container history
  session_id?: string
  seq?: number
  ts?: string
}

// Module-level shared state (singleton across all component instances)
const sessions = ref<Session[]>([])
const loading = ref(false)
const fetchSessionsError = ref<string | null>(null)
const messagesBySession = ref<Record<string, ChatMessage[]>>({})
const fetchErrorBySession = ref<Record<string, string | null>>({})
// agentThinking: true from message send until first token/status/done/error
const agentThinkingBySession = ref<Record<string, boolean>>({})
// lastSeenMessageId: set when agent starts streaming to mark the last user message as "seen"
const lastSeenMessageIdBySession = ref<Record<string, string | null>>({})

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
const hydrationRequestTokenBySession = new Map<string, symbol>()

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
      // Signal UI components to show a user-visible warning for this session.
      setHydrationOverflow(sessionId, true)
    }
    q.push(handler)
    return true
  }
  return false
}

export function useSessions() {
  async function fetchSessions() {
    loading.value = true
    fetchSessionsError.value = null
    try {
      const data = await api.sessions.list() as unknown
      if (Array.isArray(data)) {
        sessions.value = data as Session[]
      } else {
        sessions.value = (data as { sessions?: Session[] }).sessions ?? []
      }
    } catch (err: unknown) {
      fetchSessionsError.value = err instanceof Error ? err.message : 'Failed to load sessions'
    } finally {
      loading.value = false
    }
  }

  async function createSession(spaceId?: string): Promise<Session> {
    const data = await api.sessions.create(spaceId) as unknown as { session_id?: string; id?: string }
    const id = data.id ?? data.session_id
    if (!id) {
      throw new Error('Server did not return a session ID')
    }
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
    let deleted = false
    try {
      const resp = await fetch(`/api/v1/sessions/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${getToken()}` },
      })
      deleted = resp.ok
    } catch {
      deleted = false
    }
    if (!deleted) return
    sessions.value = sessions.value.filter(s => s.id !== id)
    delete messagesBySession.value[id]
    delete agentThinkingBySession.value[id]
    delete lastSeenMessageIdBySession.value[id]
    delete fetchErrorBySession.value[id]
    // Clean up hydration state so the session can be re-fetched if re-created.
    hydrated.delete(id)
    preHydrationQueue.delete(id) // discard any buffered handlers — session is gone
    hydrationRequestTokenBySession.delete(id)
    setHydrationOverflow(id, false)
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
    const hydrationToken = Symbol(sessionId)
    hydrationRequestTokenBySession.set(sessionId, hydrationToken)
    preHydrationQueue.set(sessionId, [])
    // Reset any prior fetch error so stale errors don't persist on re-fetch.
    clearFetchError(sessionId)

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
            threadSummary: (r.type === 'thread_event' && r.tool_name === 'thread_done') || undefined,
            threadSummaryThreadId: (r.type === 'thread_event' && typeof r.tool_call_id === 'string')
              ? r.tool_call_id as string
              : undefined,
          }
        })
      messagesBySession.value[sessionId] = msgs
    } catch (err: unknown) {
      // AbortError is from our own 30s timeout — not a real error, silently continue.
      // Any other error (network down, 401, 5xx) is surfaced so the UI can show it.
      if (err instanceof Error && err.name !== 'AbortError') {
        fetchErrorBySession.value = {
          ...fetchErrorBySession.value,
          [sessionId]: err.message || 'Failed to load messages',
        }
        // Inject a synthetic message so the user sees history failed to load
        // rather than silently seeing an empty history before live WS events.
        messagesBySession.value[sessionId] = [{
          id: `hydration-error-${sessionId}`,
          role: 'assistant' as const,
          content: '⚠️ Message history could not be loaded. Showing live events only.',
        }]
      }
    } finally {
      clearTimeout(timeoutId)
    }

    // Mark session as hydrated regardless of success/timeout, then flush any
    // buffered WS events so live-streaming continues from the correct base state.
    const currentHydrationToken = hydrationRequestTokenBySession.get(sessionId)
    if (currentHydrationToken !== hydrationToken) {
      // Session was deleted (or superseded by a newer hydration attempt) while
      // this fetch was in flight. Do not re-mark it hydrated or flush stale
      // buffered handlers.
      return
    }
    hydrationRequestTokenBySession.delete(sessionId)
    hydrated.add(sessionId)
    const queue = preHydrationQueue.get(sessionId) ?? []
    preHydrationQueue.delete(sessionId)
    for (const fn of queue) fn()
    // Reset the overflow flag after flushing so the warning auto-clears once
    // the hydration backlog has been processed.
    setHydrationOverflow(sessionId, false)
  }

  // refetchMessages forces a fresh history load for a session that may have
  // missed WS events — e.g. when a reconnect "resume" came back with gap=true
  // because the server's replay buffer could not cover the disconnect window.
  // Reuses the hydrate-then-subscribe machinery of fetchMessages: WS events
  // arriving during the refetch are buffered and flushed afterwards.
  async function refetchMessages(sessionId: string): Promise<void> {
    if (!sessionId) return
    if (preHydrationQueue.has(sessionId)) return // fetch already in flight
    hydrated.delete(sessionId)
    await fetchMessages(sessionId)
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

  function getAgentThinking(sessionId: string): boolean {
    return agentThinkingBySession.value[sessionId] ?? false
  }

  function setAgentThinking(sessionId: string, value: boolean) {
    agentThinkingBySession.value[sessionId] = value
  }

  function getLastSeenMessageId(sessionId: string): string | null {
    return lastSeenMessageIdBySession.value[sessionId] ?? null
  }

  function setLastSeenMessageId(sessionId: string, id: string | null) {
    lastSeenMessageIdBySession.value[sessionId] = id
  }

  function getFetchError(sessionId: string): string | null {
    return fetchErrorBySession.value[sessionId] ?? null
  }

  function clearFetchError(sessionId: string): void {
    if (fetchErrorBySession.value[sessionId] !== undefined) {
      const next = { ...fetchErrorBySession.value }
      delete next[sessionId]
      fetchErrorBySession.value = next
    }
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
    refetchMessages,
    queueIfHydrating,
    formatSessionLabel,
    getAgentThinking,
    setAgentThinking,
    getLastSeenMessageId,
    setLastSeenMessageId,
    getFetchError,
    clearFetchError,
    fetchSessionsError,
  }
}
