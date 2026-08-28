import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'

// ── API mock ─────────────────────────────────────────────────────────
const mockGetMessages = vi.fn()

vi.mock('../useApi', () => ({
  api: {
    sessions: {
      getMessages: (...args: unknown[]) => mockGetMessages(...args),
    },
    spaces: {
      messages: vi.fn().mockResolvedValue({ messages: [], next_cursor: null }),
      sessions: vi.fn().mockResolvedValue([]),
    },
  },
}))

import {
  wireSpaceTimelineWS,
  useSpaceTimeline,
  clearSpaceTimeline,
  getSessionSpaceId,
  getSpaceLastMessage,
  plaintextPreview,
  prefetchSpaceSidebar,
  spaceSessionsIndexed,
  listCachedSpaceMessages,
  getSpaceTimelineState,
} from '../useSpaceTimeline'
import { api } from '../useApi'

// ── Mock WS factory ───────────────────────────────────────────────────
function createMockWs() {
  const handlers = new Map<string, ((msg: any) => void)[]>()

  return {
    on: vi.fn((type: string, fn: (msg: any) => void) => {
      if (!handlers.has(type)) handlers.set(type, [])
      handlers.get(type)!.push(fn)
    }),
    off: vi.fn((type: string, fn: (msg: any) => void) => {
      const fns = handlers.get(type) ?? []
      handlers.set(type, fns.filter(f => f !== fn))
    }),
    emit(type: string, msg: any) {
      const fns = handlers.get(type) ?? []
      fns.forEach(fn => fn(msg))
    },
    handlerCount(type: string) {
      return (handlers.get(type) ?? []).length
    },
  }
}

// ── Helpers ───────────────────────────────────────────────────────────
const SPACE_ID = 'space-test-1'
const SESSION_ID = 'session-test-1'
const RUN_ID = 'run-1'

function emitDone(ws: ReturnType<typeof createMockWs>, extra: Record<string, unknown> = {}) {
  ws.emit('done', { type: 'done', session_id: SESSION_ID, run_id: RUN_ID, ...extra })
}

function setupSpace() {
  const tl = useSpaceTimeline(SPACE_ID)
  const state = tl.getState()
  return { tl, state }
}

// ── Tests ─────────────────────────────────────────────────────────────
describe('wireSpaceTimelineWS', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearSpaceTimeline(SPACE_ID)
  })

  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })

  // ── Token routing ─────────────────────────────────────────────────

  it('appends token content to an active stream- placeholder when session is in map', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    // Use a stream- id (active placeholder) — tokens must append here.
    state.messages.push({
      id: `stream-${SESSION_ID}-12345`,
      session_id: SESSION_ID,
      seq: -1,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: 'Hello ',
      agent: 'bot',
    })

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'world' })
    await nextTick()

    expect(state.messages[0].content).toBe('Hello world')
    expect(state.messages).toHaveLength(1)
  })

  it('tokens for second turn create new streaming placeholder when only persisted messages exist', async () => {
    // After the first response's "done" event, the stream- placeholder is replaced by a
    // real persisted message (real DB id, no stream- prefix). When the second response
    // starts streaming, tokens must create a NEW placeholder instead of appending to the
    // first response's persisted message (the multi-turn message-appending bug).
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    // Simulate a persisted assistant message (first turn complete, placeholder replaced)
    state.messages.push({
      id: 'db-msg-persisted-1',
      session_id: SESSION_ID,
      seq: 1,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: 'First response complete',
      agent: 'bot',
    })

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Second turn starts — first token arrives
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Second' })
    await nextTick()

    // Must create a new stream- placeholder, NOT append to the persisted message
    expect(state.messages).toHaveLength(2)
    expect(state.messages[0].content).toBe('First response complete') // unchanged
    expect(state.messages[1].id).toMatch(/^stream-/)
    expect(state.messages[1].content).toBe('Second')
  })

  it('creates a streaming placeholder when no prior assistant message exists in the session', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    // Only a user message — no assistant message yet
    state.messages.push({
      id: 'u-1',
      session_id: SESSION_ID,
      seq: 1,
      ts: new Date().toISOString(),
      role: 'user',
      content: 'Hi',
      agent: '',
    })

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Hey there' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('Hey there')
    expect(assistantMsgs[0].id).toMatch(/^stream-/)
    expect(assistantMsgs[0].session_id).toBe(SESSION_ID)
  })

  it('silently ignores token when session is NOT in sessionToSpaceMap', async () => {
    const { state } = setupSpace()
    // Deliberately do NOT add session to map — this is the bug scenario
    state.messages.push({
      id: 'u-1',
      session_id: SESSION_ID,
      seq: 1,
      ts: new Date().toISOString(),
      role: 'user',
      content: 'Hello',
      agent: '',
    })

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'response text' })
    await nextTick()

    // No assistant message should have been created
    expect(state.messages.filter(m => m.role === 'assistant')).toHaveLength(0)
    // Message count unchanged
    expect(state.messages).toHaveLength(1)
  })

  it('accumulates tokens across multiple events on the same streaming placeholder', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'one ' })
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'two ' })
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'three' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('one two three')
  })

  it('strips leading tool-call JSON from streamed assistant content', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: '{"name": "bash", "arguments": {"command": "echo PONG"}}' })
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'PONG' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('PONG')
    expect(assistantMsgs[0].content).not.toContain('{"name"')
  })

  it('streamed leftover P then ONG stays one PONG bubble (first char not dropped or forked)', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'P' })
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'ONG' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('PONG')
  })

  it('plain streamed PONG stays PONG', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    for (const ch of 'PONG') {
      ws.emit('token', { type: 'token', session_id: SESSION_ID, content: ch })
    }
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('PONG')
  })

  it('done without run_id does not close the stream (no orphan ONG after PONG)', async () => {
    // Live PR 134 regression: parseSSE StreamDone is forwarded as "done"
    // without run_id. Stamping stream- → done- made the leftover suffix
    // "ONG" a second nameless bubble; sidebar preview became ONG.
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'P' })
    await nextTick()
    ws.emit('done', { type: 'done', session_id: SESSION_ID }) // no run_id
    await nextTick()
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'ONG' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(1)
    expect(assistantMsgs[0].content).toBe('PONG')
    expect(assistantMsgs[0].id).toMatch(/^stream-/)
    expect(getSpaceLastMessage(SPACE_ID)?.text).toBe('PONG')
  })

  it('ignores token events with no session_id', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', content: 'orphaned token' }) // no session_id
    await nextTick()

    expect(state.messages).toHaveLength(0)
  })

  // ── Done handler ──────────────────────────────────────────────────

  it('done: updates activeSessionId and triggers getMessages refresh', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    mockGetMessages.mockResolvedValue([
      {
        id: 'persisted-1',
        session_id: SESSION_ID,
        seq: 1,
        ts: new Date().toISOString(),
        role: 'assistant',
        content: 'Final answer',
        agent: 'bot',
      },
    ])

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    emitDone(ws)
    await nextTick()
    await new Promise(r => setTimeout(r, 0)) // flush microtasks for the getMessages promise

    expect(state.activeSessionId).toBe(SESSION_ID)
    expect(mockGetMessages).toHaveBeenCalledWith(SESSION_ID, { limit: 5 })
  })

  it('done: is silently ignored when session is NOT in map', async () => {
    const { state } = setupSpace()
    // Deliberately do NOT add session to map
    state.activeSessionId = null

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    emitDone(ws)
    await nextTick()

    expect(state.activeSessionId).toBeNull()
    expect(mockGetMessages).not.toHaveBeenCalled()
  })

  it('done: synchronously renames stream- placeholder before async fetch so next turn tokens create a new bubble', async () => {
    // Regression: when done fires, the stream- placeholder stays until the async fetch
    // resolves. If turn-2 tokens arrive during that window, onToken finds the old
    // placeholder and appends instead of creating a new bubble.
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    // Never-resolving promise simulates slow/pending fetch (the race window).
    mockGetMessages.mockReturnValue(new Promise(() => {}))

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Turn 1: emit tokens then done
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'first' })
    await nextTick()
    emitDone(ws)
    await nextTick()

    // Turn 2: tokens arrive while fetch is still pending
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'second' })
    await nextTick()

    const assistantMsgs = state.messages.filter(m => m.role === 'assistant')
    // Must have TWO separate assistant messages, not one concatenated message
    expect(assistantMsgs).toHaveLength(2)
    expect(assistantMsgs[0].content).toBe('first')
    expect(assistantMsgs[1].content).toBe('second')
    expect(assistantMsgs[1].id).toMatch(/^stream-/)
  })

  it('done: replaces streaming placeholder with the persisted message after refresh', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    // Add a streaming placeholder (id starts with "stream-")
    state.messages.push({
      id: `stream-${SESSION_ID}-12345`,
      session_id: SESSION_ID,
      seq: -1,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: 'streamed content',
      agent: 'bot',
    })
    mockGetMessages.mockResolvedValue([
      {
        id: 'db-msg-1',
        session_id: SESSION_ID,
        seq: 5,
        ts: new Date().toISOString(),
        role: 'assistant',
        content: 'streamed content',
        agent: 'bot',
      },
    ])

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    emitDone(ws)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    // Streaming placeholder should be replaced by the DB message
    expect(state.messages).toHaveLength(1)
    expect(state.messages[0].id).toBe('db-msg-1')
    expect(state.messages[0].seq).toBe(5)
  })

  // ── Listener lifecycle ────────────────────────────────────────────

  it('re-calling wireSpaceTimelineWS removes prior listeners before registering new ones', async () => {
    const ws = createMockWs()

    wireSpaceTimelineWS(ws as any)
    const countAfterFirst = ws.handlerCount('token')

    wireSpaceTimelineWS(ws as any)
    const countAfterSecond = ws.handlerCount('token')

    // Should not accumulate; count stays the same after re-wire
    expect(countAfterSecond).toBe(countAfterFirst)
  })

  // ── Tool result handling ──────────────────────────────────────────

  it('tool_result: attaches tool call result to the active streaming message', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Start streaming — creates a stream- placeholder
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Searching...' })
    await nextTick()

    ws.emit('tool_result', {
      type: 'tool_result',
      session_id: SESSION_ID,
      payload: { id: 'tc-1', tool: 'search', args: { q: 'test' }, result: 'found 5 items' },
    })
    await nextTick()

    const streaming = state.messages.find(m => m.id.startsWith('stream-'))
    expect(streaming).toBeDefined()
    expect(streaming!.toolCalls).toHaveLength(1)
    expect(streaming!.toolCalls![0]).toMatchObject({
      id: 'tc-1',
      name: 'search',
      args: { q: 'test' },
      result: 'found 5 items',
      done: true,
    })
  })

  it('tool_result: ignored when session is NOT in sessionToSpaceMap', async () => {
    const { state } = setupSpace()
    // Deliberately do NOT add session to map

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('tool_result', {
      type: 'tool_result',
      session_id: SESSION_ID,
      payload: { id: 'tc-1', tool: 'search', args: {}, result: 'data' },
    })
    await nextTick()

    expect(state.messages).toHaveLength(0)
  })

  it('tool_result: multiple tool results accumulate on the same streaming message', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Working...' })
    await nextTick()

    ws.emit('tool_result', { type: 'tool_result', session_id: SESSION_ID, payload: { id: 'tc-1', tool: 'read_file', args: {}, result: 'file content' } })
    ws.emit('tool_result', { type: 'tool_result', session_id: SESSION_ID, payload: { id: 'tc-2', tool: 'write_file', args: {}, result: 'ok' } })
    await nextTick()

    const streaming = state.messages.find(m => m.id.startsWith('stream-'))
    expect(streaming!.toolCalls).toHaveLength(2)
    expect(streaming!.toolCalls![0].name).toBe('read_file')
    expect(streaming!.toolCalls![1].name).toBe('write_file')
  })

  it('tool_result: attaches to done- placeholder when it arrives after done renames the stream-', async () => {
    // Race: done fires and renames stream- → done- before tool_result arrives.
    // The result must still be attached (not silently dropped).
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    mockGetMessages.mockReturnValue(new Promise(() => {})) // never resolves

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Using tool...' })
    await nextTick()
    emitDone(ws) // renames stream- → done-
    await nextTick()

    // tool_result arrives after done renamed the placeholder
    ws.emit('tool_result', {
      type: 'tool_result',
      session_id: SESSION_ID,
      payload: { id: 'tc-late', tool: 'search', args: {}, result: 'late result' },
    })
    await nextTick()

    const doneMsg = state.messages.find(m => m.id.startsWith('done-'))
    expect(doneMsg).toBeDefined()
    expect(doneMsg!.toolCalls).toHaveLength(1)
    expect(doneMsg!.toolCalls![0]).toMatchObject({ id: 'tc-late', result: 'late result' })
  })

  // ── tool_call event (live indicator) ──────────────────────────────
  //
  // Regression: wireSpaceTimelineWS only registered 'tool_result' but never
  // 'tool_call', so the streaming message never got tool call data when tools
  // run as prefetch (before any tokens arrive — the normal MuninnDB pattern).
  // Results were silently dropped because no stream- placeholder existed yet.

  it('tool_call + token: tool result is visible on the streaming message', async () => {
    // Simulates the muninn_recall prefetch pattern:
    // tool_call fires → tool_result fires → first token arrives
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Step 1: tool_call arrives (before any token)
    ws.emit('tool_call', {
      type: 'tool_call',
      session_id: SESSION_ID,
      payload: { id: 'tc-prefetch', tool: 'muninn_recall', args: { context: 'user history' } },
    })
    await nextTick()

    // Step 2: tool_result arrives (before any token)
    ws.emit('tool_result', {
      type: 'tool_result',
      session_id: SESSION_ID,
      payload: { id: 'tc-prefetch', tool: 'muninn_recall', args: { context: 'user history' }, result: 'You talked about X before.' },
    })
    await nextTick()

    // Step 3: first token arrives — creates streaming placeholder
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Based on your history...' })
    await nextTick()

    const streaming = state.messages.find(m => m.id.startsWith('stream-'))
    expect(streaming).toBeDefined()
    expect(streaming!.toolCalls).toBeDefined()
    expect(streaming!.toolCalls!.length).toBeGreaterThanOrEqual(1)
    const tc = streaming!.toolCalls!.find(t => t.id === 'tc-prefetch')
    expect(tc).toBeDefined()
    expect(tc!.name).toBe('muninn_recall')
    expect(tc!.result).toBe('You talked about X before.')
    expect(tc!.done).toBe(true)
  })

  it('tool_call is registered as a WS listener', () => {
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    expect(ws.handlerCount('tool_call')).toBeGreaterThan(0)
  })

  it('re-calling wireSpaceTimelineWS cleans up tool_call listener', async () => {
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    const countAfterFirst = ws.handlerCount('tool_call')
    wireSpaceTimelineWS(ws as any)
    const countAfterSecond = ws.handlerCount('tool_call')
    expect(countAfterSecond).toBe(countAfterFirst)
  })

  it('re-calling wireSpaceTimelineWS cleans up tool_result listener', async () => {
    const ws = createMockWs()

    wireSpaceTimelineWS(ws as any)
    const countAfterFirst = ws.handlerCount('tool_result')

    wireSpaceTimelineWS(ws as any)
    const countAfterSecond = ws.handlerCount('tool_result')

    expect(countAfterSecond).toBe(countAfterFirst)
  })

  // ── done: server message ID stamping ─────────────────────────────

  it('done: stamps server message_id onto placeholder synchronously so thread_started can find it', async () => {
    // Regression: thread_started fires right after done with parent_message_id
    // matching the server ID. The placeholder must have the server ID before
    // the async fetch resolves, otherwise thread_started can't find the message.
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    // Never-resolving fetch so we can inspect state before fetch resolves
    mockGetMessages.mockReturnValue(new Promise(() => {}))

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Stream a token then fire done with server message ID in payload
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'response' })
    await nextTick()

    emitDone(ws, { payload: { message_id: 'server-msg-001' } })
    await nextTick()

    // Placeholder must now have the server ID (not stream- or done- prefix)
    expect(state.messages[0].id).toBe('server-msg-001')
    expect(state.messages[0].content).toBe('response')
  })

  it('done: falls back to done- prefix when payload has no message_id', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    mockGetMessages.mockReturnValue(new Promise(() => {}))

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'hello' })
    await nextTick()
    emitDone(ws) // run_id present, no message_id payload
    await nextTick()

    expect(state.messages[0].id).toMatch(/^done-/)
  })

  it('done: preserves delegatedThreads when async fetch replaces message', async () => {
    // Regression: thread_started attaches delegatedThreads to the placeholder.
    // When the async fetch resolves and replaces it with a fresh server message,
    // delegatedThreads must be carried over.
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Stream and done with server ID
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'delegating...' })
    await nextTick()

    mockGetMessages.mockResolvedValue([
      {
        id: 'server-msg-002',
        session_id: SESSION_ID,
        seq: 1,
        ts: new Date().toISOString(),
        role: 'assistant',
        content: 'delegating...',
        agent: 'Tom',
      },
    ])

    emitDone(ws, { payload: { message_id: 'server-msg-002' } })
    await nextTick()

    // Simulate thread_started attaching delegatedThreads BEFORE fetch resolves
    const msg = state.messages.find((m: any) => m.id === 'server-msg-002')
    expect(msg).toBeDefined()
    ;(msg as any).delegatedThreads = [{ threadId: 't-1', agentId: 'Sam', msgId: 'server-msg-002', replyCount: 0 }]

    // Let the async fetch resolve and replace
    await new Promise(r => setTimeout(r, 0))

    // delegatedThreads must survive the replacement
    const replaced = state.messages.find((m: any) => m.id === 'server-msg-002')
    expect(replaced).toBeDefined()
    expect((replaced as any).delegatedThreads).toHaveLength(1)
    expect((replaced as any).delegatedThreads[0].threadId).toBe('t-1')
  })

  it('done: next-turn tokens still create new bubble after server ID stamping', async () => {
    // Verify the stream- prefix protection still works when we use server IDs
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    mockGetMessages.mockReturnValue(new Promise(() => {}))

    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)

    // Turn 1
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'first' })
    await nextTick()
    emitDone(ws, { payload: { message_id: 'server-msg-003' } })
    await nextTick()

    // Turn 2 tokens must NOT append to the server-ID message
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'second' })
    await nextTick()

    const assistantMsgs = state.messages.filter((m: any) => m.role === 'assistant')
    expect(assistantMsgs).toHaveLength(2)
    expect(assistantMsgs[0].content).toBe('first')
    expect(assistantMsgs[0].id).toBe('server-msg-003')
    expect(assistantMsgs[1].content).toBe('second')
    expect(assistantMsgs[1].id).toMatch(/^stream-/)
  })

  // ── clearSpaceTimeline ────────────────────────────────────────────

  it('clearSpaceTimeline removes cached state so next hydrate starts fresh', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    state.messages.push({
      id: 'm1', session_id: SESSION_ID, seq: 1, ts: '', role: 'user', content: 'hi', agent: '',
    })

    clearSpaceTimeline(SPACE_ID)

    // After clear, getting state again returns a fresh empty instance
    const freshState = useSpaceTimeline(SPACE_ID).getState()
    expect(freshState.messages).toHaveLength(0)
    expect(freshState.sessionToSpaceMap.size).toBe(0)
  })
})

describe('getSpaceLastMessage', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })

  it('sidebar preview is PONG with agent prefix, not orphan ONG', () => {
    const { state } = setupSpace()
    state.messages.push({
      id: 'a1',
      session_id: SESSION_ID,
      seq: 1,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: 'PONG',
      agent: 'Steve',
    })
    expect(getSpaceLastMessage(SPACE_ID)).toMatchObject({ text: 'Steve: PONG' })
  })
})

// ── getSessionSpaceId ─────────────────────────────────────────────────────────
// Used by App.vue to suppress unread badges for sessions that belong to the
// currently-active space (the user is already viewing that chat).

const SPACE_B = 'space-b'
const SESSION_B = 'session-b'

describe('getSessionSpaceId', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })

  it('returns the space id when a session is in that space timeline', () => {
    const tl = useSpaceTimeline(SPACE_ID)
    tl.getState().sessionToSpaceMap.set(SESSION_ID, SPACE_ID)

    expect(getSessionSpaceId(SESSION_ID)).toBe(SPACE_ID)
  })

  it('returns null when the session is not in any timeline', () => {
    expect(getSessionSpaceId('unknown-session')).toBeNull()
  })

  it('sidebar preview humanizes fail tokens and never shows TOOL_FAIL', () => {
    const tl = useSpaceTimeline(SPACE_ID)
    tl.getState().messages.push({
      id: 'm-fail',
      session_id: SESSION_ID,
      seq: 1,
      ts: new Date().toISOString(),
      role: 'assistant',
      agent: 'Steve',
      content: 'TOOL_FAIL: The json_tool is not available',
    } as any)

    const preview = getSpaceLastMessage(SPACE_ID)
    expect(preview).not.toBeNull()
    expect(preview!.text).toContain("Couldn't do that")
    expect(preview!.text).toContain('Steve:')
    expect(preview!.text).not.toContain('TOOL_FAIL')
    expect(preview!.text).not.toContain('wait_for_threads')
  })

  it('returns the correct space when multiple spaces are tracked', () => {
    useSpaceTimeline(SPACE_ID).getState().sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    useSpaceTimeline(SPACE_B).getState().sessionToSpaceMap.set(SESSION_B, SPACE_B)

    expect(getSessionSpaceId(SESSION_ID)).toBe(SPACE_ID)
    expect(getSessionSpaceId(SESSION_B)).toBe(SPACE_B)
  })

  it('maps via prefetch index without visiting the timeline', async () => {
    vi.mocked(api.spaces.sessions).mockResolvedValueOnce([
      { id: SESSION_B, title: 'Steve', status: 'idle', created_at: '', updated_at: '', space_id: SPACE_B },
    ])
    vi.mocked(api.spaces.messages).mockResolvedValueOnce({
      messages: [{
        id: 'm-last', session_id: SESSION_B, seq: 1, ts: new Date().toISOString(),
        role: 'assistant', content: 'TOOL_FAIL: missing key', agent: 'Steve',
      }],
      next_cursor: '',
    })

    await prefetchSpaceSidebar([SPACE_B])

    expect(getSessionSpaceId(SESSION_B)).toBe(SPACE_B)
    expect(spaceSessionsIndexed([SPACE_B])).toBe(true)
    // Prefetch must not seed the visit cache — hydrate still has to load history.
    expect(useSpaceTimeline(SPACE_B).getState().messages).toHaveLength(0)
    const snippet = getSpaceLastMessage(SPACE_B)
    expect(snippet).not.toBeNull()
    expect(snippet!.text).toContain("Couldn't do that")
    expect(snippet!.text).not.toContain('TOOL_FAIL')
    expect(snippet!.text).not.toContain('wait_for_threads')
  })
})

describe('plaintextPreview', () => {
  it('keeps TOOL_FAIL underscores', () => {
    expect(plaintextPreview('check TOOL_FAIL in the log')).toBe('check TOOL_FAIL in the log')
  })

  it('strips markdown wrappers without eating underscores', () => {
    expect(plaintextPreview('**bold** and `code` and # heading')).toBe('bold and code and heading')
  })

  it('truncates long snippets', () => {
    const long = 'a'.repeat(60)
    expect(plaintextPreview(long)).toBe('a'.repeat(48) + '…')
  })
})

describe('getSpaceLastMessage', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })

  it('returns null when the space has never been opened or prefetched', () => {
    expect(getSpaceLastMessage(SPACE_ID)).toBeNull()
  })

  it('uses the live timeline when the space has been visited', () => {
    const { state } = setupSpace()
    state.messages.push({
      id: 'm1', session_id: SESSION_ID, seq: 1, ts: new Date().toISOString(),
      role: 'assistant', content: 'hello from **Steve**', agent: 'Steve',
    })
    const snippet = getSpaceLastMessage(SPACE_ID)
    expect(snippet?.text).toBe('Steve: hello from Steve')
  })
})

describe('listCachedSpaceMessages', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })

  it('returns only spaces that have cached messages', () => {
    useSpaceTimeline(SPACE_ID).getState().messages.push({
      id: 'm1', session_id: SESSION_ID, seq: 1, ts: '', role: 'user', content: 'hi', agent: '',
    })
    useSpaceTimeline(SPACE_B).getState()

    const listed = listCachedSpaceMessages()
    expect(listed).toHaveLength(1)
    expect(listed[0].spaceId).toBe(SPACE_ID)
    expect(listed[0].messages).toHaveLength(1)
  })
})

describe('getSpaceTimelineState', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
    clearSpaceTimeline(SPACE_B)
  })

  it('returns the same cached state as useSpaceTimeline for that space', () => {
    const tl = useSpaceTimeline(SPACE_ID)
    tl.getState().sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    tl.getState().messages.push({
      id: 'm1', session_id: SESSION_ID, seq: 1, ts: '', role: 'user', content: 'hi', agent: '',
    })

    const viaLookup = getSpaceTimelineState(SPACE_ID)
    expect(viaLookup).toBe(tl.getState())
    expect(viaLookup.messages).toHaveLength(1)
    expect(getSpaceTimelineState(SPACE_B)).not.toBe(viaLookup)
  })
})

describe('stale turn / loading model', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearSpaceTimeline(SPACE_ID)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })

  it('does not paint Loading model as hallway speech', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    ws.emit('status', { type: 'status', session_id: SESSION_ID, content: 'Loading model, please wait...' })
    await nextTick()
    expect(state.messages.filter(m => /loading model/i.test(m.content))).toHaveLength(0)
  })

  it('does not mint a thinking status bubble', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    ws.emit('status', { type: 'status', session_id: SESSION_ID, content: 'thinking', agent: 'Winston' })
    await nextTick()
    expect(state.messages.filter(m => m.role === 'assistant')).toHaveLength(0)
  })

  it('does not replay an older persist when the last row is a different assistant', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    state.messages.push({
      id: 'old-hire', session_id: SESSION_ID, seq: 2, ts: new Date().toISOString(),
      role: 'assistant', content: "They're here.", agent: 'Winston',
    })
    state.messages.push({
      id: 'later', session_id: SESSION_ID, seq: 5, ts: new Date().toISOString(),
      role: 'assistant', content: 'There are 7 people in this channel.', agent: 'Winston',
    })
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: "They're here.", agent: 'Winston' })
    await nextTick()
    expect(state.messages.filter(m => m.id.startsWith('stream-'))).toHaveLength(0)
    expect(state.messages.filter(m => m.content.includes("They're here."))).toHaveLength(1)
  })

  it('stamps Winston on a new stream bubble from the WS agent field', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    state.messages.push({
      id: 'u-new', session_id: SESSION_ID, seq: 6, ts: new Date().toISOString(),
      role: 'user', content: '@Winston ping', agent: '',
    })
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: 'Pong.', agent: 'Winston' })
    await nextTick()
    const stream = state.messages.filter(m => m.id.startsWith('stream-'))
    expect(stream).toHaveLength(1)
    expect(stream[0].agent).toBe('Winston')
  })

  it('does not emit a ghost They\'re here. for an already-persisted hire', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    state.messages.push({
      id: 'persisted-hire',
      session_id: SESSION_ID,
      seq: 4,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: "They're here.",
      agent: 'Winston',
    })
    const ws = createMockWs()
    wireSpaceTimelineWS(ws as any)
    ws.emit('token', { type: 'token', session_id: SESSION_ID, content: "They're here." })
    await nextTick()
    const ghosts = state.messages.filter(m => m.id.startsWith('stream-') && m.content.includes("They're here."))
    expect(ghosts).toHaveLength(0)
    expect(state.messages.filter(m => m.content.includes("They're here."))).toHaveLength(1)
  })
})

describe('rail preview never Loading model', () => {
  beforeEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })
  afterEach(() => {
    clearSpaceTimeline(SPACE_ID)
  })

  it('walks back past Loading model status for getSpaceLastMessage', () => {
    const { state } = setupSpace()
    state.messages.push({
      id: 'pong', session_id: SESSION_ID, seq: 1, ts: new Date().toISOString(),
      role: 'assistant', content: 'Pong.', agent: 'Winston',
    })
    state.messages.push({
      id: 'load', session_id: SESSION_ID, seq: 2, ts: new Date().toISOString(),
      role: 'assistant', content: 'Loading model, pleas…', agent: 'Winston',
    })
    const snippet = getSpaceLastMessage(SPACE_ID)
    expect(snippet?.text ?? '').not.toMatch(/loading model/i)
    expect(snippet?.text).toContain('Pong.')
  })
})
