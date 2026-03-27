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

  it('appends token content to the existing assistant message when session is in map', async () => {
    const { state } = setupSpace()
    state.sessionToSpaceMap.set(SESSION_ID, SPACE_ID)
    state.messages.push({
      id: 'msg-1',
      session_id: SESSION_ID,
      seq: 1,
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
