import { ref } from 'vue'

export interface WSMessage {
  type: string
  session_id?: string
  content?: string
  payload?: Record<string, unknown>
  run_id?: string
  seq?: number
  // epoch is the server-startup random identifier. When it changes the
  // frontend knows the server restarted and resets its ordering buffer.
  epoch?: number
}

// Per-session ordering buffer state.
// epoch: the server-startup epoch of the last delivered message.
//        When a message arrives with a different epoch we know the server
//        restarted and must reset lastSeq + buffer before delivering.
// lastSeq: the highest contiguous sequence number delivered to handlers.
// buffer: messages received out-of-order waiting to be delivered.
interface SessionSeqState {
  epoch: number | null
  lastSeq: number
  buffer: Map<number, WSMessage>
}

const SEQ_BUFFER_MAX = 20
const ORDERING_DROP_WARN_COOLDOWN_MS = 10_000
const SEQ_GAP_RESYNC_MS = 3_000

export type ConnectionState = 'connecting' | 'connected' | 'disconnected' | 'reconnecting'

const BACKOFF_INITIAL_MS = 1_000
const BACKOFF_MAX_MS = 30_000
const BACKOFF_MULTIPLIER = 2
const BACKOFF_JITTER = 0.2

// After this many consecutive failures the WS enters "degraded" mode:
// a flat 60 s retry instead of giving up. The UI can show a banner.
const MAX_RECONNECT_ATTEMPTS = 10
const DEGRADED_RETRY_MS = 60_000

// App-layer heartbeat: browser cannot send RFC 6455 protocol-level pings —
// only servers may. We send {type:"ping"} every 45 s to detect application-
// layer hangs (e.g. stalled goroutines, write queue backed up) that survive
// at the TCP level. The server responds with {type:"pong"}. If no pong arrives
// within 10 s the connection is closed (code 4000) and a reconnect is triggered.
// Network-layer liveness is handled separately by the server's 30 s protocol ping.
const HEARTBEAT_INTERVAL_MS = 45_000
const HEARTBEAT_PONG_WAIT_MS = 10_000

// Close codes that signal a deliberate, permanent server-side termination.
// Do NOT reconnect on these — the server or operator acted intentionally.
//   1000 = normal closure (session end, logout)
//   1001 = going away (server shutdown)
//   4001 = auth_expired (server rejected the token; reconnect would loop)
const PERMANENT_CLOSE_CODES = new Set([1000, 1001, 4001])

// Maximum number of outgoing messages to queue while the socket is down.
// When full, the oldest entry is dropped (backpressure: prefer fresh messages).
const OUTBOX_MAX = 100

// Maximum number of sessions to send a "resume" for after a reconnect.
// The active session always goes first; other sessions this tab has seen
// seq-stamped traffic for follow, capped to stay well under the server's
// inbound rate limit (30 msgs / 10 s).
const RESUME_MAX_SESSIONS = 8

function jitteredDelay(base: number): number {
  const jitter = 1 + (Math.random() * 2 - 1) * BACKOFF_JITTER
  return Math.min(base * jitter, BACKOFF_MAX_MS)
}

export function useHuginnWS(token: string) {
  const connected = ref(false)
  const connectionState = ref<ConnectionState>('connecting')
  const messages = ref<WSMessage[]>([])
  const lastError = ref<string | null>(null)
  const isDegraded = ref(false)
  const pendingCount = ref(0)

  let ws: WebSocket | null = null
  let intentionallyClosed = false
  const reconnectAttempts = ref(0)
  let currentBackoffMs = BACKOFF_INITIAL_MS
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let countdownInterval: ReturnType<typeof setInterval> | null = null
  const nextRetryAt = ref<number | null>(null)
  const secondsUntilRetry = ref<number>(0)
  const handlers = new Map<string, ((msg: WSMessage) => void)[]>()

  // Tracks the active session so we can re-subscribe on reconnect.
  let activeSessionId: string | null = null

  // Per-session ordering buffer: keyed by session_id.
  // Only messages that carry a seq field are buffered; messages without seq
  // (e.g. global broadcasts) pass straight through for backward compatibility.
  const seqStates = new Map<string, SessionSeqState>()
  const orderingDropWarnAt = new Map<string, number>()
  const seqGapRecoveryTimers = new Map<string, ReturnType<typeof setTimeout>>()

  // Outgoing message queue: drained on reconnect, bounded by OUTBOX_MAX.
  const outbox: WSMessage[] = []

  // Set when the socket drops unexpectedly so the next successful open sends
  // "resume" messages to recover session events missed while disconnected.
  let resumePending = false

  // ── Heartbeat ───────────────────────────────────────────────────────────────
  let heartbeatInterval: ReturnType<typeof setInterval> | null = null
  let heartbeatPongTimeout: ReturnType<typeof setTimeout> | null = null

  function startHeartbeat() {
    stopHeartbeat()
    heartbeatInterval = setInterval(() => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping' }))
        heartbeatPongTimeout = setTimeout(() => {
          console.warn('[WS] App-layer heartbeat pong timeout — closing for reconnect')
          ws?.close(4000, 'heartbeat_timeout')
        }, HEARTBEAT_PONG_WAIT_MS)
      }
    }, HEARTBEAT_INTERVAL_MS)
  }

  function stopHeartbeat() {
    if (heartbeatInterval !== null) {
      clearInterval(heartbeatInterval)
      heartbeatInterval = null
    }
    if (heartbeatPongTimeout !== null) {
      clearTimeout(heartbeatPongTimeout)
      heartbeatPongTimeout = null
    }
  }

  // ── Outbox ──────────────────────────────────────────────────────────────────
  function drainOutbox() {
    while (outbox.length > 0 && ws?.readyState === WebSocket.OPEN) {
      const msg = outbox.shift()!
      ws.send(JSON.stringify(msg))
    }
    pendingCount.value = outbox.length
  }

  // ── Countdown helpers ────────────────────────────────────────────────────────
  function startCountdown(delayMs: number) {
    if (countdownInterval !== null) clearInterval(countdownInterval)
    secondsUntilRetry.value = Math.ceil(delayMs / 1000)
    countdownInterval = setInterval(() => {
      if (nextRetryAt.value === null) {
        clearInterval(countdownInterval!)
        countdownInterval = null
        secondsUntilRetry.value = 0
        return
      }
      const remaining = Math.ceil((nextRetryAt.value - Date.now()) / 1000)
      secondsUntilRetry.value = Math.max(0, remaining)
    }, 1_000)
  }

  function stopCountdown() {
    if (countdownInterval !== null) {
      clearInterval(countdownInterval)
      countdownInterval = null
    }
    nextRetryAt.value = null
    secondsUntilRetry.value = 0
  }

  // ── Message delivery ─────────────────────────────────────────────────────────
  function dispatchMsg(msg: WSMessage) {
    messages.value.push(msg)
    const fns = handlers.get(msg.type) ?? []
    fns.forEach(fn => fn(msg))
  }

  function dispatchOrderingWarning(sessionId: string, content: string) {
    const now = Date.now()
    const lastWarnAt = orderingDropWarnAt.get(sessionId) ?? 0
    if (now - lastWarnAt < ORDERING_DROP_WARN_COOLDOWN_MS) return
    orderingDropWarnAt.set(sessionId, now)
    dispatchMsg({
      type: 'warning',
      session_id: sessionId,
      content,
    })
  }

  function clearGapRecoveryTimer(sessionId: string) {
    const timer = seqGapRecoveryTimers.get(sessionId)
    if (timer != null) {
      clearTimeout(timer)
      seqGapRecoveryTimers.delete(sessionId)
    }
  }

  function clearAllGapRecoveryTimers() {
    for (const timer of seqGapRecoveryTimers.values()) clearTimeout(timer)
    seqGapRecoveryTimers.clear()
  }

  function flushBufferedInOrder(state: SessionSeqState) {
    let next = state.lastSeq + 1
    while (state.buffer.has(next)) {
      const buffered = state.buffer.get(next)!
      state.buffer.delete(next)
      dispatchMsg(buffered)
      state.lastSeq = next
      next++
    }
  }

  function scheduleGapRecovery(sessionId: string) {
    if (seqGapRecoveryTimers.has(sessionId)) return
    const timer = setTimeout(() => {
      seqGapRecoveryTimers.delete(sessionId)
      const liveState = seqStates.get(sessionId)
      if (!liveState || liveState.buffer.size === 0) return
      const minBufferedSeq = Math.min(...liveState.buffer.keys())
      if (minBufferedSeq <= liveState.lastSeq+1) return
      // Missing sequence never arrived. Skip the gap and continue delivering
      // buffered messages to keep the timeline live.
      liveState.lastSeq = minBufferedSeq - 1
      dispatchOrderingWarning(
        sessionId,
        'Realtime ordering gap persisted; some delayed events were skipped to recover live updates. Refresh if this session looks stale.',
      )
      flushBufferedInOrder(liveState)
      if (liveState.buffer.size > 0) {
        scheduleGapRecovery(sessionId)
      }
    }, SEQ_GAP_RESYNC_MS)
    seqGapRecoveryTimers.set(sessionId, timer)
  }

  function deliverOrBuffer(msg: WSMessage) {
    // App-layer pong: reset heartbeat timeout and do not dispatch to handlers.
    if (msg.type === 'pong') {
      if (heartbeatPongTimeout !== null) {
        clearTimeout(heartbeatPongTimeout)
        heartbeatPongTimeout = null
      }
      return
    }

    const sessionId = msg.session_id
    const seq = msg.seq

    // No seq field or no session ID → pass through unordered (backward compat).
    if (seq == null || seq === 0 || sessionId == null) {
      dispatchMsg(msg)
      return
    }

    // Fetch or create the per-session state.
    let state = seqStates.get(sessionId)
    if (state == null) {
      state = { epoch: null, lastSeq: 0, buffer: new Map() }
      seqStates.set(sessionId, state)
    }

    // Epoch change → server restarted. Reset ordering state and deliver
    // the message immediately as the first message of the new stream.
    const msgEpoch = typeof msg.epoch === 'number' ? msg.epoch : null
    if (msgEpoch !== null && msgEpoch !== state.epoch) {
      console.info(
        `[WS] Server epoch changed for session ${sessionId}: ` +
        `${state.epoch ?? 'unset'} → ${msgEpoch}. Resetting sequence buffer.`,
      )
      state.epoch = msgEpoch
      state.lastSeq = seq
      state.buffer.clear()
      clearGapRecoveryTimer(sessionId)
      dispatchMsg(msg)
      return
    }

    if (seq === state.lastSeq + 1) {
      dispatchMsg(msg)
      state.lastSeq = seq
      flushBufferedInOrder(state)
      if (state.buffer.size === 0) clearGapRecoveryTimer(sessionId)
    } else if (seq > state.lastSeq + 1) {
      if (state.buffer.size >= SEQ_BUFFER_MAX) {
        const oldest = Math.min(...state.buffer.keys())
        state.buffer.delete(oldest)
        dispatchOrderingWarning(
          sessionId,
          'Realtime updates were delayed; some out-of-order events were dropped. Refresh if this session looks stale.',
        )
      }
      state.buffer.set(seq, msg)
      scheduleGapRecovery(sessionId)
    }
    // seq <= lastSeq → duplicate or replay; silently drop.
  }

  // ── Core connection ──────────────────────────────────────────────────────────
  function connect() {
    if (intentionallyClosed) return
    connectionState.value = reconnectAttempts.value === 0 ? 'connecting' : 'reconnecting'
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${protocol}//${location.host}/ws?token=${token}`)

    ws.onopen = () => {
      connected.value = true
      connectionState.value = 'connected'
      isDegraded.value = false
      lastError.value = null
      reconnectAttempts.value = 0
      currentBackoffMs = BACKOFF_INITIAL_MS
      stopCountdown()
      startHeartbeat()
      drainOutbox()
      // Re-subscribe to the active session if we had one before disconnect.
      if (activeSessionId) {
        sendHello(activeSessionId)
      }
      // After a drop, ask the server to replay session events we missed while
      // disconnected. Replayed messages carry their original seq and flow
      // through the ordering buffer, which drops duplicates (seq <= lastSeq).
      // A resume_ok with gap=true means the server's replay buffer could not
      // cover everything — the app should re-fetch history via REST (handled
      // by whoever registered an on('resume_ok') handler).
      if (resumePending) {
        resumePending = false
        sendResume()
      }
    }

    ws.onclose = (event) => {
      connected.value = false
      stopHeartbeat()
      clearAllGapRecoveryTimers()
      resumePending = true
      if (!intentionallyClosed) {
        if (PERMANENT_CLOSE_CODES.has(event.code)) {
          connectionState.value = 'disconnected'
          lastError.value = event.reason || `Connection terminated (code ${event.code})`
          return
        }
        lastError.value = event.reason || `Connection closed (code ${event.code})`
        scheduleReconnect()
      } else {
        connectionState.value = 'disconnected'
      }
    }

    ws.onerror = () => {
      if (!lastError.value) {
        lastError.value = 'WebSocket error'
      }
    }

    ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        deliverOrBuffer(msg)
      } catch {
        // ignore malformed messages
      }
    }
  }

  function scheduleReconnect() {
    if (intentionallyClosed) return
    reconnectAttempts.value++
    connectionState.value = 'reconnecting'

    let delay: number
    if (reconnectAttempts.value > MAX_RECONNECT_ATTEMPTS) {
      // Degraded mode: flat 60 s retry. Never give up — operators can fix things.
      isDegraded.value = true
      delay = DEGRADED_RETRY_MS
    } else {
      delay = jitteredDelay(currentBackoffMs)
      currentBackoffMs = Math.min(currentBackoffMs * BACKOFF_MULTIPLIER, BACKOFF_MAX_MS)
    }

    nextRetryAt.value = Date.now() + delay
    startCountdown(delay)
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      stopCountdown()
      connect()
    }, delay)
  }

  /**
   * reconnectNow cancels any pending backoff timer and immediately attempts to
   * reconnect. Clears degraded state so the attempt budget resets.
   * Does nothing if already connected or intentionally closed.
   */
  function reconnectNow() {
    if (intentionallyClosed || connectionState.value === 'connected') return
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopCountdown()
    // Reset attempt counter and degraded state for a fresh attempt budget.
    reconnectAttempts.value = Math.max(0, reconnectAttempts.value - 1)
    currentBackoffMs = BACKOFF_INITIAL_MS
    isDegraded.value = false
    connect()
  }

  // ── Page Visibility API ──────────────────────────────────────────────────────
  // When the tab becomes visible or the window gains focus after being hidden,
  // reconnect immediately if we are not already connected. This handles the
  // common pattern of a laptop waking from sleep or a user switching back to
  // the tab — the socket is often silently dead by that point.
  function onVisibilityChange() {
    if (document.visibilityState === 'visible' && !intentionallyClosed && !connected.value) {
      reconnectNow()
    }
  }

  function onWindowFocus() {
    if (!intentionallyClosed && !connected.value) {
      reconnectNow()
    }
  }

  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('focus', onWindowFocus)

  // ── Public API ───────────────────────────────────────────────────────────────
  /** Register which session we're currently subscribed to (used for re-hello on reconnect). */
  function setActiveSession(sessionId: string | null) {
    activeSessionId = sessionId
  }

  function sendHello(sessionId: string) {
    send({ type: 'hello', session_id: sessionId })
  }

  /**
   * sendResume asks the server to replay session events missed while the
   * socket was down. The active session is resumed first, then any other
   * sessions this tab has seen seq-stamped traffic for (capped at
   * RESUME_MAX_SESSIONS). last_seq is the highest contiguous seq delivered to
   * handlers; epoch lets the server detect a restart and signal a gap.
   */
  function sendResume() {
    const targets: string[] = []
    if (activeSessionId) targets.push(activeSessionId)
    for (const id of seqStates.keys()) {
      if (!targets.includes(id)) targets.push(id)
    }
    for (const id of targets.slice(0, RESUME_MAX_SESSIONS)) {
      const state = seqStates.get(id)
      send({
        type: 'resume',
        session_id: id,
        payload: {
          last_seq: state?.lastSeq ?? 0,
          epoch: state?.epoch ?? 0,
        },
      })
    }
  }

  function send(msg: WSMessage) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg))
    } else {
      // Queue for delivery on reconnect; drop oldest if outbox is full.
      if (outbox.length >= OUTBOX_MAX) outbox.shift()
      outbox.push(msg)
      pendingCount.value = outbox.length
    }
  }

  function on(type: string, fn: (msg: WSMessage) => void) {
    if (!handlers.has(type)) handlers.set(type, [])
    handlers.get(type)!.push(fn)
  }

  function off(type: string, fn: (msg: WSMessage) => void) {
    const fns = handlers.get(type) ?? []
    handlers.set(type, fns.filter(f => f !== fn))
  }

  function destroy() {
    intentionallyClosed = true
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('focus', onWindowFocus)
    stopHeartbeat()
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopCountdown()
    clearAllGapRecoveryTimers()
    ws?.close()
  }

  /**
   * streamChat sends a message to the SSE chat-stream endpoint and calls
   * onToken for each streamed token until the server emits a "done" event.
   */
  async function streamChat(
    sessionID: string,
    content: string,
    onToken: (token: string) => void,
  ): Promise<void> {
    const response = await fetch(`/api/v1/sessions/${sessionID}/chat/stream`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
      body: JSON.stringify({ content }),
    })

    if (!response.ok || !response.body) {
      throw new Error(`Stream failed: ${response.status}`)
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split(/\r?\n/)
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          try {
            const event = JSON.parse(line.slice(6))
            if (event.type === 'token') onToken(event.content as string)
            if (event.type === 'done') return
            if (event.type === 'error') throw new Error(event.content as string)
          } catch (e) {
            if (e instanceof Error && e.message !== '') throw e
            // skip other malformed lines
          }
        }
      }
    }
  }

  connect()

  return {
    connected,
    connectionState,
    lastError,
    isDegraded,
    pendingCount,
    reconnectAttempts,
    maxReconnectAttempts: MAX_RECONNECT_ATTEMPTS,
    messages,
    nextRetryAt,
    secondsUntilRetry,
    send,
    on,
    off,
    destroy,
    streamChat,
    reconnectNow,
    setActiveSession,
    sendHello,
    sendResume,
  }
}

export type HuginnWS = ReturnType<typeof useHuginnWS>
