import { reactive, toRefs } from 'vue'
import { api, type SpaceMessage } from './useApi'
import { plaintextPreview } from '../utils/honesty'
import type { HuginnWS, WSMessage } from './useHuginnWS'
import { visibleAssistantContent } from '../utils/visibleAssistantContent'
import { sortMessagesChronological } from './useMessageEnrichment'

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

// Visit-independent last-message cache + session→space index. Prefetch writes
// here so unvisited DMs show a preview and so getSessionSpaceId can map
// sessions that were never hydrated. Must stay out of stateMap — putting even
// one message there would make hydrate() skip the real timeline.
const lastMessageCache = reactive<Record<string, { text: string; relTime: string }>>({})
const sessionSpaceIndex = new Map<string, string>()
const sidebarPrefetched = new Set<string>()
const sidebarPrefetchInflight = new Map<string, Promise<void>>()

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

  // pendingToolResults buffers tool call results that arrive before the
  // streaming placeholder exists (the prefetch pattern: tools run before
  // any tokens are emitted). Flushed when onToken creates the placeholder.
  const pendingToolResults = new Map<string, { id: string; name: string; args: Record<string, unknown>; result: string }[]>()

  const wireAgent = (msg: WSMessage): string => {
    const raw = msg as unknown as Record<string, unknown>
    return typeof raw.agent === 'string' ? raw.agent.trim() : ''
  }

  const sessionMsgs = (st: TimelineState, sessionId: string): SpaceMessage[] =>
    st.messages.filter(m => m.session_id === sessionId)

  const shouldMintStream = (st: TimelineState, sessionId: string, incoming: string): boolean => {
    const text = incoming.trim()
    if (!text) return false
    const msgs = sessionMsgs(st, sessionId)
    const last = msgs[msgs.length - 1]
    if (last?.role === 'user') return true
    if (last?.role === 'assistant' && !last.id.startsWith('stream-') && (last.content || '').includes(text)) {
      return false
    }
    return !msgs.some(m =>
      m.role === 'assistant' && !m.id.startsWith('stream-') && (m.content || '').trim() === text,
    )
  }

  const onStatus = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    const statusText = (msg.content ?? '').trim()
    // Status is never teammate speech. Loading-model and resume "thinking"
    // must not mint hallway bubbles (SNAP-0).
    if (/^loading model/i.test(statusText) || /^thinking$/i.test(statusText)) {
      loadingSessionIds.add(sessionId)
      return
    }
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      const streamPlaceholder = [...st.messages].reverse().find(
        (m: SpaceMessage) => m.session_id === sessionId && m.role === 'assistant' && m.id.startsWith('stream-'),
      )
      if (streamPlaceholder) {
        const named = wireAgent(msg)
        if (named && !streamPlaceholder.agent) streamPlaceholder.agent = named
      }
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
          const named = wireAgent(msg)
          if (named && !existing.agent) existing.agent = named
          if (loadingSessionIds.has(sessionId)) {
            // Replace the status placeholder content with the first real token.
            // cancelStatus() fires in Go before this message arrives, so the
            // status goroutine cannot fire after this point.
            existing.content = visibleAssistantContent(msg.content ?? '')
            loadingSessionIds.delete(sessionId)
          } else {
            existing.content = visibleAssistantContent(existing.content + msg.content)
          }
        } else {
          // Resume/replay of an already-persisted hire ("They're here.")
          // must not mint a second hallway bubble — including when the last
          // row is a different persist (SNAP-0 / 0.5 hard-refresh).
          const incoming = (msg.content ?? '').trim()
          if (!shouldMintStream(st, sessionId, incoming)) {
            break
          }
          const pending = pendingToolResults.get(sessionId) ?? []
          pendingToolResults.delete(sessionId)
          st.messages.push({
            id: `stream-${sessionId}-${Date.now()}`,
            session_id: sessionId,
            seq: -1,
            ts: new Date().toISOString(),
            role: 'assistant',
            content: visibleAssistantContent(msg.content ?? ''),
            agent: wireAgent(msg),
            toolCalls: pending.length > 0 ? pending.map(p => ({ ...p, done: true })) : undefined,
          })
        }
      }
      break
    }
  }

  const onDone = (msg: WSMessage): void => {
    const sessionId = msg.session_id
    if (!sessionId) return
    // Ignore backend StreamDone forwarded as "done" without run_id. ChatView
    // already drops those; stamping stream- → done- here forks leftover
    // tokens (PONG then nameless ONG) into a second timeline row.
    if (!msg.run_id) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      // Update activeSessionId for the space that owns this session.
      st.activeSessionId = sessionId

      const streamIdx = st.messages.findIndex(
        e => e.session_id === sessionId && e.id.startsWith('stream-')
      )

      // Stamp the server-assigned message ID onto the placeholder immediately
      // (synchronously) so that thread_started events — which carry
      // parent_message_id matching the server ID — can find this message
      // before the async fetch resolves. This also closes the race window
      // where turn-2 tokens could append to the old stream- placeholder.
      const serverMsgId = (msg.payload as Record<string, string> | undefined)?.message_id
      if (streamIdx >= 0) {
        const placeholder = st.messages[streamIdx]
        if (placeholder) {
          if (serverMsgId) {
            placeholder.id = serverMsgId
          } else {
            placeholder.id = placeholder.id.replace('stream-', 'done-')
          }
        }
      }

      // Refresh the last message from the server to get full stable data.
      // We fire-and-forget; if it fails the placeholder content is still visible.
      api.sessions.getMessages(sessionId, { limit: 5 }).then(fresh => {
        const freshMsgs = (Array.isArray(fresh) ? fresh : []) as SpaceMessage[]
        for (const m of freshMsgs) {
          // Check if this message already exists in the timeline (by server ID).
          const existingIdx = st.messages.findIndex(e => e.id === m.id)
          if (existingIdx >= 0) {
            // Already in timeline (stamped by server ID above). Replace with
            // full server data but preserve any thread data attached by WS handlers.
            const existing = st.messages[existingIdx]!
            if ((existing as any).delegatedThreads) {
              ;(m as any).delegatedThreads = (existing as any).delegatedThreads
            }
            st.messages.splice(existingIdx, 1, m)
          } else if (!st.messages.some(e => e.id === m.id)) {
            // New message — check for done- placeholder to replace.
            if (m.role === 'assistant') {
              const doneIdx = st.messages.findIndex(
                e => e.session_id === sessionId && e.id.startsWith('done-')
              )
              if (doneIdx >= 0) {
                const old = st.messages[doneIdx]!
                if ((old as any).delegatedThreads) {
                  ;(m as any).delegatedThreads = (old as any).delegatedThreads
                }
                st.messages.splice(doneIdx, 1, m)
              } else if (!m.parent_id) {
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
        ts: (raw.ts as string) || (raw.created_at as string) || new Date().toISOString(),
        created_at: (raw.created_at as string) || (raw.ts as string) || new Date().toISOString(),
        role: (raw.role as 'user' | 'assistant') ?? 'user',
        content: msg.content ?? '',
        agent: (raw.agent as string) ?? '',
      }
      if (newMsg.parent_id) break
      if (!st.messages.some(m => m.id === newMsg.id)) {
        st.messages.push(newMsg)
      }
      break
    }
  }

  const onToolCall = (msg: WSMessage): void => {
    // tool_call fires when the model starts a tool invocation. We register it
    // so that prefetch tool calls (before any tokens) are tracked and their
    // results (from onToolResult) can be buffered and flushed onto the
    // streaming placeholder once the first token creates it.
    // The handler itself is intentionally minimal — the result is what matters
    // for display, handled in onToolResult below.
    const sessionId = msg.session_id
    if (!sessionId) return
    for (const [, st] of stateMap.entries()) {
      if (!st.sessionToSpaceMap.has(sessionId)) continue
      // Ensure the pending buffer exists for this session.
      if (!pendingToolResults.has(sessionId)) pendingToolResults.set(sessionId, [])
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
      const record = {
        id: (p.id as string) ?? '',
        name: (p.tool as string) ?? '',
        args: (p.args as Record<string, unknown>) ?? {},
        result: (p.result as string) ?? '',
      }
      // Accept both stream- (result arrived before done) and done- (result
      // arrived after onDone renamed the placeholder — the late-result race).
      const streamMsg = [...st.messages].reverse().find(
        m => m.session_id === sessionId && m.role === 'assistant' &&
          (m.id.startsWith('stream-') || m.id.startsWith('done-')),
      )
      if (streamMsg) {
        if (!streamMsg.toolCalls) streamMsg.toolCalls = []
        streamMsg.toolCalls.push({ ...record, done: true })
      } else {
        // No streaming placeholder yet — buffer for when the first token arrives.
        const buf = pendingToolResults.get(sessionId) ?? []
        buf.push(record)
        pendingToolResults.set(sessionId, buf)
      }
      break
    }
  }

  const onSpaceReply = (msg: WSMessage): void => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const spaceId = typeof p.space_id === 'string' ? p.space_id : ''
    const parentId = typeof p.parent_id === 'string' ? p.parent_id : ''
    if (!spaceId || !parentId) return
    const st = stateMap.get(spaceId)
    if (!st) return
    const root = st.messages.find(m => m.id === parentId)
    if (!root) return
    if (typeof p.reply_count === 'number') root.reply_count = p.reply_count
    if (typeof p.last_preview === 'string') root.last_preview = p.last_preview
  }

  ws.on('status', onStatus)
  ws.on('token', onToken)
  ws.on('done', onDone)
  ws.on('chat', onChat)
  ws.on('tool_call', onToolCall)
  ws.on('tool_result', onToolResult)
  ws.on('space_reply', onSpaceReply)

  _wsCleanup = () => {
    ws.off('status', onStatus)
    ws.off('token', onToken)
    ws.off('done', onDone)
    ws.off('chat', onChat)
    ws.off('tool_call', onToolCall)
    ws.off('tool_result', onToolResult)
    ws.off('space_reply', onSpaceReply)
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
        api.spaces.messages(spaceId, undefined, 20, { signal: controller.signal }),
        api.spaces.sessions(spaceId, { signal: controller.signal }),
      ])

      // Replace messages in-place to preserve reactive array reference.
      const roots = sortMessagesChronological(msgResult.messages.filter(m => !m.parent_id))
      s.messages.splice(0, s.messages.length, ...roots)
      s.cursor = msgResult.next_cursor || null
      s.hasMore = !!msgResult.next_cursor

      // Populate sessionToSpaceMap and derive activeSessionId.
      s.sessionToSpaceMap.clear()
      const sessArr = Array.isArray(sessions) ? sessions : []
      for (const sess of sessArr as Array<{ id: string; updated_at: string }>) {
        s.sessionToSpaceMap.set(sess.id, spaceId)
        sessionSpaceIndex.set(sess.id, spaceId)
      }
      sidebarPrefetched.add(spaceId)
      rememberLastMessage(spaceId, s.messages)
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
      const merged = sortMessagesChronological(dedup(result.messages, s.messages))
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

export { plaintextPreview } from '../utils/honesty'

function snippetFromMessages(messages: SpaceMessage[] | undefined): { text: string; relTime: string } | null {
  if (!messages?.length) return null
  const ordered = sortMessagesChronological(
    messages.filter(m => (m.role === 'user' || m.role === 'assistant') && !!m.content),
  )
  for (let i = ordered.length - 1; i >= 0; i--) {
    const last = ordered[i]
    if (!last) continue
    if (/^\s*loading model\b/i.test(last.content)) continue
    const text = plaintextPreview(last.content)
    if (!text || /^\s*loading model\b/i.test(text)) continue
    const prefix = last.role === 'user' ? 'You: ' : (last.agent ? `${last.agent}: ` : '')
    return { text: prefix + text, relTime: relativeTime(last.ts) }
  }
  return null
}

function rememberSpaceSessions(spaceId: string, sessions: Array<{ id: string }>): void {
  for (const sess of sessions) {
    if (sess.id) sessionSpaceIndex.set(sess.id, spaceId)
  }
}

function rememberLastMessage(spaceId: string, messages: SpaceMessage[]): void {
  const snippet = snippetFromMessages(messages)
  if (snippet) lastMessageCache[spaceId] = snippet
}

// prefetchSpaceSidebar fills session→space maps and last-message snippets for
// spaces the user has not opened yet. Concurrent calls share the in-flight
// promise so a second caller waits instead of skipping an unfinished index.
export async function prefetchSpaceSidebar(spaceIds: string[]): Promise<void> {
  await Promise.all(spaceIds.map((spaceId) => {
    if (sidebarPrefetched.has(spaceId)) return Promise.resolve()
    const inflight = sidebarPrefetchInflight.get(spaceId)
    if (inflight) return inflight
    const work = (async () => {
      try {
        const [sessions, msgResult] = await Promise.all([
          api.spaces.sessions(spaceId),
          api.spaces.messages(spaceId, undefined, 5),
        ])
        const sessArr = Array.isArray(sessions) ? sessions : []
        rememberSpaceSessions(spaceId, sessArr as Array<{ id: string }>)
        rememberLastMessage(spaceId, msgResult?.messages ?? [])
        sidebarPrefetched.add(spaceId)
      } catch {
        // Allow a later retry if this space's listing failed.
      } finally {
        sidebarPrefetchInflight.delete(spaceId)
      }
    })()
    sidebarPrefetchInflight.set(spaceId, work)
    return work
  }))
}

// getSpaceLastMessage returns a { text, relTime } snippet for the sidebar preview.
// Prefers the live timeline (visited), then the prefetch cache (unvisited).
export function getSpaceLastMessage(spaceId: string): { text: string; relTime: string } | null {
  const fromTimeline = snippetFromMessages(stateMap.get(spaceId)?.messages)
  if (fromTimeline) return fromTimeline
  return lastMessageCache[spaceId] ?? null
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
  delete lastMessageCache[spaceId]
  sidebarPrefetched.delete(spaceId)
  for (const [sid, sidSpace] of sessionSpaceIndex) {
    if (sidSpace === spaceId) sessionSpaceIndex.delete(sid)
  }
}

// getSessionSpaceId returns the space id that owns the given session, or null if
// the session is not in a visited timeline and has not been indexed via
// api.spaces.sessions. Uncached (never-indexed) IDs must not be treated as
// orphans — they may belong to an unvisited space.
export function getSessionSpaceId(sessionId: string): string | null {
  for (const [spaceId, st] of stateMap.entries()) {
    if (st.sessionToSpaceMap.has(sessionId)) return spaceId
  }
  return sessionSpaceIndex.get(sessionId) ?? null
}

// True once every listed space has been indexed via prefetch or a visit.
// Orphan pruning must wait for this — otherwise unvisited space sessions
// look unmapped and get dropped as orphans.
export function spaceSessionsIndexed(spaceIds: string[]): boolean {
  return spaceIds.length > 0 && spaceIds.every(id => sidebarPrefetched.has(id))
}

// listCachedSpaceMessages returns messages already hydrated into space timelines.
// Used by Cmd+K global search so channel/DM text is findable without a new backend.
export function listCachedSpaceMessages(): Array<{ spaceId: string; messages: SpaceMessage[] }> {
  const out: Array<{ spaceId: string; messages: SpaceMessage[] }> = []
  for (const [spaceId, st] of stateMap.entries()) {
    if (st.messages.length) out.push({ spaceId, messages: st.messages })
  }
  return out
}

// getSpaceTimelineState returns the cached (or freshly created) timeline for a
// space. ChatView uses this to write session-scoped WS events onto the owner
// space's messages even when a different space is currently open.
export function getSpaceTimelineState(spaceId: string): TimelineState {
  return getState(spaceId)
}
