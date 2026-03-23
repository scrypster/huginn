// web/src/composables/useThreads.ts
import { ref } from 'vue'
import { api } from './useApi'
import type { Thread } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

// ── Reactive thread state (module-level singleton) ────────────────────────────
// Keyed by sessionId, value is map of threadId → LiveThread
const threadsBySession = ref<Record<string, Record<string, LiveThread>>>({})

// Non-null when the most recent loadThreads call failed (e.g. threadmgr not configured).
const threadsError = ref<string | null>(null)

// ── Reply-count cache (message_id → count) ────────────────────────────────────
// Updated by thread_reply_updated WS events. Consumed by ChatView to show
// live reply-count badges without polling the REST API.
export const replyCountsByMessageId = ref<Record<string, number>>({})

// LRU access order for session eviction (oldest first).
const sessionAccessOrder: string[] = []
const MAX_SESSIONS = 5

// ── Pending delegation previews (shown as approval banners in ChatView) ───────
export interface PendingPreview {
  sessionId: string
  threadId: string
  agentId: string
  task: string
  parentMessageId?: string
}
const pendingPreviews = ref<PendingPreview[]>([])

// ── LiveThread extends Thread with ephemeral UI state ─────────────────────────
export interface LiveThread extends Thread {
  streamingContent: string      // last 600 chars of streaming tokens (live typing indicator)
  toolCalls: LiveToolCall[]     // tool call history (most recent last)
  elapsedMs: number             // live elapsed milliseconds (updated by ticker)
  parentMessageId?: string      // chat message that triggered this thread
  _tickerHandle?: number        // setInterval handle for elapsed counter
}

export interface LiveToolCall {
  tool: string
  args?: Record<string, unknown>
  resultSummary?: string
  done: boolean
}

// ── Persisted thread shape (subset of LiveThread — no ephemeral fields) ───────
interface PersistedThread {
  id: string
  status: Thread['Status'] | 'stale'
  title?: string
  createdAt: string
  toolCalls: LiveToolCall[]
  sessionId: string
}

// ── Status helpers ────────────────────────────────────────────────────────────
export type ThreadStatus = Thread['Status']

export const TERMINAL_STATUSES = new Set<ThreadStatus>(['done', 'cancelled', 'error', 'completed', 'completed-with-timeout'] as ThreadStatus[])

export function isRunning(t: LiveThread): boolean {
  return !TERMINAL_STATUSES.has(t.Status)
}

// ── sessionStorage persistence ────────────────────────────────────────────────
const STORAGE_TTL_MS = 24 * 60 * 60 * 1000   // 24 hours
const STALE_STATUSES = new Set(['thinking', 'tooling', 'queued', 'resolving', 'blocked'])

function storageKey(sessionId: string): string {
  return `huginn:threads:${sessionId}`
}

interface StorageEntry {
  threads: PersistedThread[]
  savedAt: number
}

function persistThreads(sessionId: string): void {
  const map = threadsBySession.value[sessionId]
  if (!map) return
  const threads: PersistedThread[] = Object.values(map).map(t => ({
    id: t.ID,
    status: STALE_STATUSES.has(t.Status) ? 'stale' : t.Status,
    title: t.Task || undefined,
    createdAt: t.StartedAt,
    toolCalls: t.toolCalls.map(tc => ({ ...tc })),
    sessionId: t.SessionID,
  }))
  const entry: StorageEntry = { threads, savedAt: Date.now() }
  let serialized: string
  try {
    serialized = JSON.stringify(entry)
  } catch (err) {
    console.warn('[useThreads] Failed to serialize thread state:', err)
    return
  }
  try {
    sessionStorage.setItem(storageKey(sessionId), serialized)
  } catch (err) {
    // QuotaExceededError in private browsing / storage full — non-fatal
    console.warn('[useThreads] sessionStorage.setItem failed (quota or private browsing):', err)
  }
}

function restoreThreadsFromStorage(sessionId: string): PersistedThread[] {
  let raw: string | null = null
  try {
    raw = sessionStorage.getItem(storageKey(sessionId))
  } catch (err) {
    console.warn('[useThreads] sessionStorage.getItem failed (private browsing?):', err)
    return []
  }
  if (!raw) return []
  let entry: StorageEntry
  try {
    entry = JSON.parse(raw) as StorageEntry
  } catch (err) {
    console.warn('[useThreads] Failed to parse stored thread state — discarding:', err)
    try { sessionStorage.removeItem(storageKey(sessionId)) } catch { /* ignore */ }
    return []
  }
  try {
    if (Date.now() - entry.savedAt > STORAGE_TTL_MS) {
      sessionStorage.removeItem(storageKey(sessionId))
      return []
    }
    return entry.threads ?? []
  } catch (err) {
    console.warn('[useThreads] Error evaluating stored thread entry — discarding:', err)
    return []
  }
}

function purgeExpiredStorageEntries(): void {
  try {
    const toRemove: string[] = []
    let length = 0
    try { length = sessionStorage.length } catch { return }
    for (let i = 0; i < length; i++) {
      let key: string | null = null
      try { key = sessionStorage.key(i) } catch { continue }
      if (!key?.startsWith('huginn:threads:')) continue
      try {
        const raw = sessionStorage.getItem(key)
        if (!raw) { toRemove.push(key); continue }
        let entry: StorageEntry
        try {
          entry = JSON.parse(raw) as StorageEntry
        } catch {
          toRemove.push(key)
          continue
        }
        if (Date.now() - entry.savedAt > STORAGE_TTL_MS) toRemove.push(key)
      } catch {
        toRemove.push(key)
      }
    }
    for (const k of toRemove) {
      try { sessionStorage.removeItem(k) } catch { /* ignore */ }
    }
  } catch {
    // sessionStorage entirely unavailable (private browsing, etc.) — ignore
  }
}

// Debounced persist: avoids excessive writes during high-frequency streaming events.
const _persistTimers: Record<string, ReturnType<typeof setTimeout>> = {}
function debouncedPersist(sessionId: string, delayMs = 50): void {
  if (_persistTimers[sessionId]) clearTimeout(_persistTimers[sessionId])
  _persistTimers[sessionId] = setTimeout(() => {
    delete _persistTimers[sessionId]
    persistThreads(sessionId)
  }, delayMs)
}

// Purge old entries once per module load.
purgeExpiredStorageEntries()

// ── Composable factory ────────────────────────────────────────────────────────
export function useThreads() {

  function getSessionThreads(sessionId: string): LiveThread[] {
    return Object.values(threadsBySession.value[sessionId] ?? {})
  }

  function getActiveThreadCount(sessionId: string): number {
    return getSessionThreads(sessionId).filter(isRunning).length
  }

  function hasThreads(sessionId: string): boolean {
    return getSessionThreads(sessionId).length > 0
  }

  // ── Restore persisted thread state for a session (call before loadThreads) ──
  function restoreSession(sessionId: string): void {
    if (!sessionId) return
    const persisted = restoreThreadsFromStorage(sessionId)
    if (!persisted.length) return
    if (!threadsBySession.value[sessionId]) {
      threadsBySession.value[sessionId] = {}
    }
    for (const pt of persisted) {
      if (threadsBySession.value[sessionId][pt.id]) continue // already in memory
      threadsBySession.value[sessionId][pt.id] = {
        ID: pt.id,
        SessionID: pt.sessionId,
        AgentID: '',
        Task: pt.title || '',
        Status: pt.status as Thread['Status'],
        StartedAt: pt.createdAt,
        CompletedAt: '',
        TokensUsed: 0,
        TokenBudget: 0,
        streamingContent: '',
        toolCalls: pt.toolCalls,
        elapsedMs: 0,
      }
    }
  }

  // ── Seed from REST (call on session change) ───────────────────────────────
  async function loadThreads(sessionId: string): Promise<void> {
    if (!sessionId) return
    try {
      const threads = await api.threads.list(sessionId)
      if (!threadsBySession.value[sessionId]) {
        threadsBySession.value[sessionId] = {}
      }
      for (const t of threads) {
        const existing = threadsBySession.value[sessionId][t.ID]
        if (!existing) {
          // New from REST — seed with empty UI state
          threadsBySession.value[sessionId][t.ID] = {
            ...t,
            streamingContent: '',
            toolCalls: [],
            elapsedMs: 0,
          }
          // Start elapsed ticker if still running
          if (isRunning(threadsBySession.value[sessionId][t.ID]!)) {
            startTicker(sessionId, t.ID)
          }
        } else {
          // Thread already restored from storage — merge authoritative fields from REST.
          existing.Status = t.Status
          existing.AgentID = t.AgentID || existing.AgentID
          existing.Task = t.Task || existing.Task
          existing.CompletedAt = t.CompletedAt || existing.CompletedAt
          existing.TokensUsed = t.TokensUsed
          existing.TokenBudget = t.TokenBudget
          if (t.Summary) existing.Summary = t.Summary
          if (isRunning(existing)) startTicker(sessionId, t.ID)
        }
      }
      debouncedPersist(sessionId)
      threadsError.value = null
    } catch (e) {
      // threadmgr may not be configured on the server — log for observability
      console.warn('huginn: loadThreads failed', e)
      threadsError.value = 'Could not load threads'
    }
  }

  /**
   * refreshFromServer fetches current thread state from the REST API and
   * merges it into the in-memory map. Call this after a reconnect or reload
   * to resolve stale thread statuses.
   */
  async function refreshFromServer(sessionId: string): Promise<void> {
    await loadThreads(sessionId)
  }

  // ── Elapsed time ticker ───────────────────────────────────────────────────
  function startTicker(sessionId: string, threadId: string) {
    const t = threadsBySession.value[sessionId]?.[threadId]
    if (!t || t._tickerHandle) return
    const startMs = t.StartedAt ? new Date(t.StartedAt).getTime() : Date.now()
    t._tickerHandle = window.setInterval(() => {
      const thread = threadsBySession.value[sessionId]?.[threadId]
      if (!thread) return
      thread.elapsedMs = Date.now() - startMs
      if (!isRunning(thread)) stopTicker(sessionId, threadId)
    }, 500)
  }

  function stopTicker(sessionId: string, threadId: string) {
    const t = threadsBySession.value[sessionId]?.[threadId]
    if (!t?._tickerHandle) return
    clearInterval(t._tickerHandle)
    t._tickerHandle = undefined
  }

  // ── LRU session access tracking ───────────────────────────────────────────
  function touchSession(sessionId: string) {
    const idx = sessionAccessOrder.indexOf(sessionId)
    if (idx !== -1) sessionAccessOrder.splice(idx, 1)
    sessionAccessOrder.push(sessionId)
    // Evict oldest sessions beyond MAX_SESSIONS
    while (sessionAccessOrder.length > MAX_SESSIONS) {
      const evict = sessionAccessOrder.shift()!
      clearSession(evict)
    }
  }

  // ── Ensure a thread slot exists ───────────────────────────────────────────
  function ensureThread(sessionId: string, threadId: string): LiveThread {
    if (!threadsBySession.value[sessionId]) {
      threadsBySession.value[sessionId] = {}
    }
    touchSession(sessionId)
    if (!threadsBySession.value[sessionId][threadId]) {
      threadsBySession.value[sessionId][threadId] = {
        ID: threadId,
        SessionID: sessionId,
        AgentID: '',
        Task: '',
        Status: 'queued',
        StartedAt: new Date().toISOString(),
        CompletedAt: '',
        TokensUsed: 0,
        TokenBudget: 0,
        streamingContent: '',
        toolCalls: [],
        elapsedMs: 0,
      }
    }
    return threadsBySession.value[sessionId][threadId]
  }

  // ── Wire WS events (call once after ws is ready) ──────────────────────────
  function wireWS(ws: HuginnWS, sessionId: () => string) {

    ws.on('thread_started', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      t.Status = 'thinking'
      if (p.agent_id) t.AgentID = p.agent_id
      if (p.task) t.Task = p.task
      if (p.parent_message_id) t.parentMessageId = p.parent_message_id
      startTicker(sid, p.thread_id!)
      debouncedPersist(sid)
    })

    ws.on('thread_status', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      t.Status = p.status as ThreadStatus
      debouncedPersist(sid)
    })

    ws.on('thread_token', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      // Keep last 600 chars to avoid unbounded growth
      t.streamingContent = (t.streamingContent + p.token).slice(-600)
      // Streaming content is ephemeral — debounce persist for status only
      debouncedPersist(sid)
    })

    ws.on('thread_tool_call', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, unknown>
      const t = ensureThread(sid, p.thread_id as string)
      t.toolCalls.push({
        tool: p.tool as string,
        args: p.args as Record<string, unknown> | undefined,
        done: false,
      })
      debouncedPersist(sid)
    })

    ws.on('thread_tool_done', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      // Mark the last pending tool call matching this tool as done
      const tc = [...t.toolCalls].reverse().find(c => c.tool === p.tool && !c.done)
      if (tc) {
        tc.done = true
        tc.resultSummary = p.result_summary
      }
      debouncedPersist(sid)
    })

    ws.on('thread_done', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, unknown>
      const tid = p.thread_id as string
      const t = ensureThread(sid, tid)
      t.Status = (p.status as ThreadStatus) ?? 'done'
      t.elapsedMs = (p.elapsed_ms as number) ?? t.elapsedMs
      t.CompletedAt = new Date().toISOString()
      if (p.summary) {
        t.Summary = {
          Summary: p.summary as string,
          Status: p.status as string,
        }
      }
      t.streamingContent = ''
      stopTicker(sid, tid)
      debouncedPersist(sid)
    })

    ws.on('thread_help', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      t.Status = 'blocked'
      // Store help message in Summary so ThreadCard can display it
      t.Summary = { Summary: p.message!, Status: 'blocked' }
      stopTicker(sid, p.thread_id!)
      debouncedPersist(sid)
    })

    ws.on('thread_help_resolving', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      t.Status = 'resolving'
      debouncedPersist(sid)
    })

    ws.on('thread_help_resolved', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      const t = ensureThread(sid, p.thread_id!)
      t.Status = 'thinking'
      debouncedPersist(sid)
    })

    // Reply-count update: a thread reply was appended; update the badge cache.
    ws.on('thread_reply_updated', (msg: WSMessage) => {
      const p = msg.payload as { message_id?: string; reply_count?: number }
      if (p.message_id && typeof p.reply_count === 'number') {
        replyCountsByMessageId.value = {
          ...replyCountsByMessageId.value,
          [p.message_id]: p.reply_count,
        }
      }
    })

    // Delegation preview: server asks the user to approve/reject a delegation.
    // Store as a pending preview; ChatView renders an approval banner.
    ws.on('delegation_preview', (msg: WSMessage) => {
      const sid = msg.session_id ?? sessionId()
      if (!sid) return
      const p = msg.payload as Record<string, string>
      if (!p.thread_id || !p.agent_id) return
      // Avoid duplicates (idempotent on reconnect).
      const exists = pendingPreviews.value.some(
        pp => pp.threadId === p.thread_id && pp.sessionId === sid
      )
      if (!exists) {
        pendingPreviews.value.push({
          sessionId: sid,
          threadId: p.thread_id,
          agentId: p.agent_id,
          task: p.task ?? '',
          parentMessageId: p.parent_message_id,
        })
      }
    })
  }

  // ── Clear session threads (call on session destroy or LRU eviction) ─────────
  function clearSession(sessionId: string) {
    const threads = threadsBySession.value[sessionId]
    if (threads) {
      for (const tid of Object.keys(threads)) {
        stopTicker(sessionId, tid)
      }
      delete threadsBySession.value[sessionId]
    }
    const idx = sessionAccessOrder.indexOf(sessionId)
    if (idx !== -1) sessionAccessOrder.splice(idx, 1)
  }

  // ── Agent status derived from thread state ────────────────────────────────
  // Returns true if the named agent has at least one running (non-terminal) thread
  // across ALL sessions. Used by the sidebar to show per-agent activity indicators.
  function isAgentActive(agentName: string): boolean {
    const lower = agentName.toLowerCase()
    for (const threads of Object.values(threadsBySession.value)) {
      for (const t of Object.values(threads)) {
        if (t.AgentID.toLowerCase() === lower && isRunning(t)) return true
      }
    }
    return false
  }

  // ── Delegation preview ────────────────────────────────────────────────────
  function getSessionPreviews(sessionId: string): PendingPreview[] {
    return pendingPreviews.value.filter(p => p.sessionId === sessionId)
  }

  // ackPreview sends the user's approval/rejection to the server and removes
  // the pending preview from the local list. ws is passed explicitly because
  // the composable doesn't hold a WS reference itself.
  function ackPreview(ws: HuginnWS, preview: PendingPreview, approved: boolean): void {
    ws.send({
      type: 'delegation_preview_ack',
      session_id: preview.sessionId,
      payload: {
        thread_id: preview.threadId,
        session_id: preview.sessionId,
        approved,
      },
    })
    pendingPreviews.value = pendingPreviews.value.filter(
      p => !(p.threadId === preview.threadId && p.sessionId === preview.sessionId)
    )
  }

  return {
    getSessionThreads,
    getActiveThreadCount,
    hasThreads,
    loadThreads,
    restoreSession,
    refreshFromServer,
    wireWS,
    clearSession,
    getSessionPreviews,
    ackPreview,
    isAgentActive,
    threadsError,
  }
}
