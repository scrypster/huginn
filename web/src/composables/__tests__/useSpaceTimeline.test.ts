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
} from '../useSpaceTimeline'

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

    ws.emit('done', { type: 'done', session_id: SESSION_ID })
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

    ws.emit('done', { type: 'done', session_id: SESSION_ID })
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
    ws.emit('done', { type: 'done', session_id: SESSION_ID })
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

    ws.emit('done', { type: 'done', session_id: SESSION_ID })
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
    ws.emit('done', { type: 'done', session_id: SESSION_ID }) // renames stream- → done-
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

  it('re-calling wireSpaceTimelineWS cleans up tool_result listener', async () => {
    const ws = createMockWs()

    wireSpaceTimelineWS(ws as any)
    const countAfterFirst = ws.handlerCount('tool_result')

    wireSpaceTimelineWS(ws as any)
    const countAfterSecond = ws.handlerCount('tool_result')

    expect(countAfterSecond).toBe(countAfterFirst)
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

  it('returns the correct space when multiple spaces are tracked', () => {
    useSpaceTimeline(SPACE_ID).getState().sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    useSpaceTimeline(SPACE_B).getState().sessionToSpaceMap.set(SESSION_B, SPACE_B)

    expect(getSessionSpaceId(SESSION_ID)).toBe(SPACE_ID)
    expect(getSessionSpaceId(SESSION_B)).toBe(SPACE_B)
  })
})
