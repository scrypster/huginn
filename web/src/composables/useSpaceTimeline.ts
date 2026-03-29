import { reactive, toRefs } from 'vue'
import { api, type SpaceMessage } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

export type { SpaceMessage }

// Per-space timeline state. One reactive instance per space visit,
// kept in module-level cache so navigating back doesn't re-fetch.
interface TimelineState {
  messages: SpaceMessage[]
  cursor: string | null   // cursor for next scroll-up (null = no older messages)
  hasMore: boolean
  loadingInitial: boolean
  loadingMore: boolean
  error: string | null
  // Session routing: maps session_id → space_id for WS event dispatch.
  sessionToSpaceMap: Map<string, string>
  // The session to use when the user sends a new message.
  activeSessionId: string | null
}

// makeState returns a reactive object so mutations from wireSpaceTimelineWS
// are tracked by Vue's reactivity system without needing to go through refs.
function makeState(): TimelineState {
  return reactive({
    messages: [] as SpaceMessage[],
    cursor: null as string | null,
    hasMore: false,
    loadingInitial: false,
    loadingMore: false,
    error: null as string | null,
    sessionToSpaceMap: new Map<string, string>(),
    activeSessionId: null as string | null,
  })
}

// Module-level reactive state per space (retained across route changes).
const stateMap = new Map<string, TimelineState>()

function getState(spaceId: string): TimelineState {
  if (!stateMap.has(spaceId)) stateMap.set(spaceId, makeState())
  return stateMap.get(spaceId)!
}

// Deduplicate by message id using a Set for O(1) lookup.
function dedup(a: SpaceMessage[], b: SpaceMessage[]): SpaceMessage[] {
  const seen = new Set(a.map(m => m.id))
  return [...a, ...b.filter(m => !seen.has(m.id))]
}

// Global WS listener cleanup — replaced on each wireSpaceTimelineWS call.
let _wsCleanup: (() => void) | null = null

// wireSpaceTimelineWS registers WS listeners that append messages to the correct
// space timeline. Call once from App.vue after the WS connection is established.
// Returns an unsubscribe function.
export function wireSpaceTimelineWS(ws: HuginnWS): () => void {
  // Clean up any previous listeners.
  _wsCleanup?.()

  // loadingSessionIds tracks sessions that currently show a model-load status
  // placeholder ("Loading model, please wait..."). Local to this wire call so
  // it resets on reconnect and never leaks stale state into TimelineState.
  const loadingSessionIds = new Set<string>()

  const onStatus = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      // Find an existing streaming placeholder — a prior *persisted* assistant
      // message does NOT count (different id prefix).
      const streamPlaceholder = [...st.messages].reverse().find(
        (m: SpaceMessage) => m.session_id === sessionId && m.role === 'assistant' && m.id.startsWith('stream-'),
      )
      if (streamPlaceholder) {
        streamPlaceholder.content = msg.content ?? ''
      } else {
        st.messages.push({
          id: `stream-${sessionId}-${Date.now()}`,
          session_id: sessionId,
          seq: -1,
          ts: new Date().toISOString(),
          role: 'assistant',
          content: msg.content ?? '',
          agent: '',
        })
      }
      // Unconditional: any path that creates/updates a placeholder marks loading.
      loadingSessionIds.add(sessionId)
      break
    }
  }

  const onToken = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      if (msg.type === 'token' && msg.content) {
        // Find the active streaming placeholder for this session. Only stream- prefixed
        // messages qualify — persisted messages (replaced after "done") must never receive
        // new tokens, as that would append a second response to the first (multi-turn bug).
        const existing = [...st.messages].reverse().find(
          (m: SpaceMessage) => m.session_id === sessionId && m.role === 'assistant' && m.id.startsWith('stream-'),
        )
        if (existing) {
          if (loadingSessionIds.has(sessionId)) {
            // Replace the status placeholder content with the first real token.
            // cancelStatus() fires in Go before this message arrives, so the
            // status goroutine cannot fire after this point.
            existing.content = msg.content ?? ''
            loadingSessionIds.delete(sessionId)
          } else {
            existing.content += msg.content
          }
        } else {
          // Start a new streaming message placeholder.
          st.messages.push({
            id: `stream-${sessionId}-${Date.now()}`,
            session_id: sessionId,
            seq: -1,
            ts: new Date().toISOString(),
            role: 'assistant',
            content: msg.content ?? '',
            agent: ((msg as unknown as Record<string, unknown>).agent as string) ?? '',
          })
        }
      }
      break
    }
  }

  const onDone = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      // Update activeSessionId for the space that owns this session.
      st.activeSessionId = sessionId
      // Synchronously rename the stream- placeholder to done- before starting
      // the async fetch. This closes the race window where turn-2 tokens arrive
      // before the fetch resolves and onToken finds the old placeholder, causing
      // the second response to be appended to the first message bubble.
      const streamIdx = st.messages.findIndex(
        e => e.session_id === sessionId && e.id.startsWith('stream-')
      )
      if (streamIdx >= 0) {
        const placeholder = st.messages[streamIdx]
        if (placeholder) placeholder.id = placeholder.id.replace('stream-', 'done-')
      }
      // Refresh the last message from the server to get the stable DB id.
      // We fire-and-forget; if it fails the done- placeholder content is still visible.
      api.sessions.getMessages(sessionId, { limit: 5 }).then(fresh => {
        const freshMsgs = (Array.isArray(fresh) ? fresh : []) as SpaceMessage[]
        for (const m of freshMsgs) {
          if (!st.messages.some(e => e.id === m.id)) {
            // Only replace the done- placeholder with assistant messages — user
            // messages are already present as optimistic entries and swapping the
            // done- slot with a user message would corrupt the display order.
            if (m.role === 'assistant') {
              const doneIdx = st.messages.findIndex(
                e => e.session_id === sessionId && e.id.startsWith('done-')
              )
              if (doneIdx >= 0) {
                st.messages.splice(doneIdx, 1, m)
              } else {
                st.messages.push(m)
              }
            }
          }
        }
      }).catch(() => { /* non-fatal */ })
      break
    }
  }

  const onChat = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId || !msg.content) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      const raw = msg as unknown as Record<string, unknown>
      const newMsg: SpaceMessage = {
        id: (raw.id as string) || `ws-${Date.now()}`,
        session_id: sessionId,
        seq: (raw.seq as number) ?? -1,
        ts: (raw.ts as string) || new Date().toISOString(),
        role: (raw.role as 'user' | 'assistant') ?? 'user',
        content: msg.content ?? '',
        agent: (raw.agent as string) ?? '',
      }
      if (!st.messages.some(m => m.id === newMsg.id)) {
        st.messages.push(newMsg)
      }
      break
    }
  }

  const onToolResult = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      const p = msg.payload as Record<string, unknown> | undefined
      if (!p) break
      const streamMsg = [...st.messages].reverse().find(
        m => m.session_id === sessionId && m.role === 'assistant' && m.id.startsWith('stream-'),
      )
      if (streamMsg) {
        if (!streamMsg.toolCalls) streamMsg.toolCalls = []
        streamMsg.toolCalls.push({
          id: (p.id as string) ?? '',
          name: (p.tool as string) ?? '',
          args: (p.args as Record<string, unknown>) ?? {},
          result: (p.result as string) ?? '',
          done: true,
        })
      }
      break
    }
  }

  ws.on('status', onStatus)
  ws.on('token', onToken)
  ws.on('done', onDone)
  ws.on('chat', onChat)
  ws.on('tool_result', onToolResult)

  _wsCleanup = () => {
    ws.off('status', onStatus)
    ws.off('token', onToken)
    ws.off('done', onDone)
    ws.off('chat', onChat)
    ws.off('tool_result', onToolResult)
  }
  return _wsCleanup
}

export function useSpaceTimeline(spaceId: string) {
  const s = getState(spaceId)  // already reactive

  // Hydrate: fetch initial messages + sessions for this space.
  async function hydrate(force = false) {
    if (s.loadingInitial) return
    if (s.messages.length > 0 && !force) return // already loaded

    s.loadingInitial = true
    s.error = null

    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), 10_000)

    try {
      // Parallel fetch: messages + sessions (for routing map + activeSessionId).
      const [msgResult, sessions] = await Promise.all([
        api.spaces.messages(spaceId, undefined, 20),
        api.spaces.sessions(spaceId),
      ])

      // Replace messages in-place to preserve reactive array reference.
      s.messages.splice(0, s.messages.length, ...msgResult.messages)
      s.cursor = msgResult.next_cursor || null
      s.hasMore = !!msgResult.next_cursor

      // Populate sessionToSpaceMap and derive activeSessionId.
      s.sessionToSpaceMap.clear()
      const sessArr = Array.isArray(sessions) ? sessions : []
      for (const sess of sessArr as Array<{ id: string; updated_at: string }>) {
        s.sessionToSpaceMap.set(sess.id, spaceId)
      }
      if (sessArr.length > 0) {
        // Most recently updated session is the active one.
        const sorted = [...sessArr as Array<{ id: string; updated_at: string }>].sort(
          (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
        )
        s.activeSessionId = sorted[0]?.id ?? null
      }
    } catch (e) {
      if (e instanceof Error && e.name === 'AbortError') {
        s.error = 'Timeline load timed out. Please try again.'
      } else {
        s.error = 'Failed to load timeline.'
      }
    } finally {
      clearTimeout(timer)
      s.loadingInitial = false
    }
  }

  // loadMore fetches older messages and prepends them.
  // Returns the element id of the anchor (first message before load) for scroll restoration.
  async function loadMore(): Promise<string | null> {
    if (s.loadingMore || !s.hasMore || !s.cursor) return null

    s.loadingMore = true
    const anchorId = s.messages[0]?.id ?? null

    try {
      const result = await api.spaces.messages(spaceId, s.cursor, 20)
      const merged = dedup(result.messages, s.messages)
      s.messages.splice(0, s.messages.length, ...merged)
      s.cursor = result.next_cursor || null
      s.hasMore = !!result.next_cursor
    } catch {
      // Non-fatal: leave existing messages intact.
    } finally {
      s.loadingMore = false
    }
    return anchorId
  }

  function retryHydrate() {
    s.error = null
    hydrate(true)
  }

  // toRefs converts reactive object properties to individual Refs that stay
  // in sync with the reactive source — correct Vue 3 pattern for composables.
  return {
    ...toRefs(s),
    hydrate,
    loadMore,
    retryHydrate,
    getState: () => s,
  }
}

// getSpaceLastMessage returns a { text, relTime } snippet for the sidebar preview.
// Returns null if no messages are cached for this space yet.
export function getSpaceLastMessage(spaceId: string): { text: string; relTime: string } | null {
  const st = stateMap.get(spaceId)
  if (!st?.messages.length) return null
  const last = [...st.messages].reverse().find(m =>
    (m.role === 'user' || m.role === 'assistant') && !!m.content
  )
  if (!last) return null
  const raw = last.content.replace(/[#*`_[\]()>]/g, '').trim()
  const text = raw.length > 48 ? raw.slice(0, 48) + '…' : raw
  const prefix = last.role === 'user' ? 'You: ' : (last.agent ? `${last.agent}: ` : '')
  return { text: prefix + text, relTime: relativeTime(last.ts) }
}

function relativeTime(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const diffMs = Date.now() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'now'
  if (diffMin < 60) return `${diffMin}m`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h`
  return `${Math.floor(diffHr / 24)}d`
}

// clearSpaceTimeline removes cached state for a space (e.g. after archive).
export function clearSpaceTimeline(spaceId: string) {
  stateMap.delete(spaceId)
}

// getSessionSpaceId returns the space id that owns the given session, or null if
// the session is not tracked in any cached space timeline. Used by App.vue to
// suppress unread badges for sessions that belong to the currently-active space.
export function getSessionSpaceId(sessionId: string): string | null {
  for (const [spaceId, st] of stateMap.entries()) {
    if (st.sessionToSpaceMap.has(sessionId)) return spaceId
  }
  return null
}
