import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Minimal WebSocket mock
class MockWebSocket {
  static OPEN = 1
  static CONNECTING = 0
  static CLOSING = 2
  static CLOSED = 3

  readyState = MockWebSocket.CONNECTING
  url: string
  protocols: string | string[] | undefined
  onopen: (() => void) | null = null
  onclose: ((ev: { code: number; reason: string; wasClean: boolean }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  sentMessages: string[] = []
  closed = false

  constructor(url: string, protocols?: string | string[]) {
    this.url = url
    this.protocols = protocols
    MockWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sentMessages.push(data)
  }

  close() {
    this.closed = true
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.({ code: 1000, reason: '', wasClean: true })
  }

  // Test helpers
  simulateOpen() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }

  simulateClose(code = 1006) {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.({ code, reason: '', wasClean: false })
  }

  static instances: MockWebSocket[] = []
  static reset() { MockWebSocket.instances = [] }
  static latest(): MockWebSocket { return MockWebSocket.instances[MockWebSocket.instances.length - 1] }
}

describe('useHuginnWS', () => {
  beforeEach(() => {
    MockWebSocket.reset()
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', MockWebSocket)
    // Provide minimal location globals for jsdom
    Object.defineProperty(globalThis, 'location', {
      value: { protocol: 'http:', host: 'localhost:3000' },
      writable: true,
      configurable: true,
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  async function createWS(token = 'tok') {
    const { useHuginnWS } = await import('../useHuginnWS')
    return useHuginnWS(token)
  }

  it('connects immediately on creation and uses ws: protocol for http', async () => {
    const ws = await createWS('mytoken')
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.latest().url).toBe('ws://localhost:3000/ws')
    expect(ws.connected.value).toBe(false)
    ws.destroy()
  })

  it('authenticates via the Sec-WebSocket-Protocol subprotocol, never the URL', async () => {
    const ws = await createWS('super-secret-token')
    const sock = MockWebSocket.latest()
    // The token must never appear in the URL — a failed connection attempt
    // logs the URL verbatim to the browser console/network panel, which
    // would leak the token. It must be carried in the subprotocol instead.
    expect(sock.url).not.toContain('super-secret-token')
    expect(sock.url).not.toContain('token=')
    expect(sock.protocols).toBe('huginn-token.super-secret-token')
    ws.destroy()
  })

  it('sets connected=true when WebSocket opens', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()
    expect(ws.connected.value).toBe(true)
    ws.destroy()
  })

  it('sets connected=false on close', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()
    expect(ws.connected.value).toBe(true)
    // destroy first so reconnect doesn't fire
    ws.destroy()
    MockWebSocket.latest().simulateClose()
    expect(ws.connected.value).toBe(false)
  })

  it('reconnects after 2 s when closed without destroy()', async () => {
    await createWS()
    const first = MockWebSocket.latest()
    first.simulateOpen()

    // Close without destroy — should schedule reconnect
    first.simulateClose()
    expect(MockWebSocket.instances).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(2000)
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('does NOT reconnect after destroy()', async () => {
    const ws = await createWS()
    ws.destroy()
    MockWebSocket.latest().simulateClose()
    await vi.advanceTimersByTimeAsync(5000)
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('pushes received messages into messages.value', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()
    MockWebSocket.latest().simulateMessage({ type: 'ping', content: 'hi' })
    expect(ws.messages.value).toHaveLength(1)
    expect(ws.messages.value[0]).toEqual({ type: 'ping', content: 'hi' })
    ws.destroy()
  })

  it('dispatches messages to registered handlers by type', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const handler = vi.fn()
    ws.on('task_update', handler)
    MockWebSocket.latest().simulateMessage({ type: 'task_update', payload: { id: 1 } })

    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith({ type: 'task_update', payload: { id: 1 } })
    ws.destroy()
  })

  it('does not call handler after off() removes it', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const handler = vi.fn()
    ws.on('ping', handler)
    ws.off('ping', handler)
    MockWebSocket.latest().simulateMessage({ type: 'ping' })

    expect(handler).not.toHaveBeenCalled()
    ws.destroy()
  })

  it('send() writes JSON to the WebSocket when open', async () => {
    const ws = await createWS()
    const mock = MockWebSocket.latest()
    mock.simulateOpen()
    mock.readyState = MockWebSocket.OPEN

    ws.send({ type: 'hello', content: 'world' })
    expect(mock.sentMessages).toHaveLength(1)
    expect(JSON.parse(mock.sentMessages[0])).toEqual({ type: 'hello', content: 'world' })
    ws.destroy()
  })

  it('send() is a no-op when socket is not OPEN', async () => {
    const ws = await createWS()
    // Socket starts in CONNECTING state
    ws.send({ type: 'hello' })
    expect(MockWebSocket.latest().sentMessages).toHaveLength(0)
    ws.destroy()
  })

  it('intercepts pong heartbeat messages without dispatching', async () => {
    const ws = await createWS()
    const socket = MockWebSocket.latest()
    socket.simulateOpen()

    await vi.advanceTimersByTimeAsync(45_000)
    expect(socket.sentMessages.length).toBeGreaterThan(0)
    expect(JSON.parse(socket.sentMessages[0] ?? '{}')).toEqual({ type: 'ping' })

    socket.simulateMessage({ type: 'pong' })
    expect(ws.messages.value).toHaveLength(0)

    await vi.advanceTimersByTimeAsync(10_100)
    expect(socket.closed).toBe(false)
    ws.destroy()
  })

  it('buffers out-of-order sequenced session messages and delivers in order', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const seen: number[] = []
    ws.on('thread_done', (msg) => { seen.push(msg.seq ?? -1) })

    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-1',
      epoch: 123,
      seq: 1,
      payload: { thread_id: 't-1' },
    })
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-1',
      epoch: 123,
      seq: 3,
      payload: { thread_id: 't-1' },
    })
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-1',
      epoch: 123,
      seq: 2,
      payload: { thread_id: 't-1' },
    })

    expect(seen).toEqual([1, 2, 3])
    ws.destroy()
  })

  it('drops duplicate sequenced messages for a session', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const handler = vi.fn()
    ws.on('thread_done', handler)

    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-dup',
      epoch: 123,
      seq: 1,
      payload: { thread_id: 't-1' },
    })
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-dup',
      epoch: 123,
      seq: 1,
      payload: { thread_id: 't-1' },
    })

    expect(handler).toHaveBeenCalledTimes(1)
    ws.destroy()
  })

  it('resets per-session sequence state when epoch changes', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const seen: Array<{ epoch?: number; seq?: number }> = []
    ws.on('thread_done', (msg) => { seen.push({ epoch: msg.epoch, seq: msg.seq }) })

    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-epoch',
      epoch: 100,
      seq: 5,
      payload: { thread_id: 't-1' },
    })
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-epoch',
      epoch: 200,
      seq: 1,
      payload: { thread_id: 't-2' },
    })

    expect(seen).toEqual([
      { epoch: 100, seq: 5 },
      { epoch: 200, seq: 1 },
    ])
    ws.destroy()
  })

  it('treats epoch=0 as a valid epoch value', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const seen: Array<{ epoch?: number; seq?: number }> = []
    ws.on('thread_done', (msg) => { seen.push({ epoch: msg.epoch, seq: msg.seq }) })

    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-zero-epoch',
      epoch: 0,
      seq: 1,
      payload: { thread_id: 't-1' },
    })
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-zero-epoch',
      epoch: 0,
      seq: 2,
      payload: { thread_id: 't-2' },
    })

    expect(seen).toEqual([
      { epoch: 0, seq: 1 },
      { epoch: 0, seq: 2 },
    ])
    ws.destroy()
  })

  it('emits warning when out-of-order buffer overflows', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()

    const warningHandler = vi.fn()
    ws.on('warning', warningHandler)

    // Prime contiguous sequence at 1, then flood out-of-order events that all
    // depend on missing seq=2 so they accumulate in the gap buffer.
    MockWebSocket.latest().simulateMessage({
      type: 'thread_done',
      session_id: 'sess-overflow',
      epoch: 321,
      seq: 1,
      payload: { thread_id: 't-1' },
    })
    for (let seq = 3; seq <= 24; seq++) {
      MockWebSocket.latest().simulateMessage({
        type: 'thread_done',
        session_id: 'sess-overflow',
        epoch: 321,
        seq,
        payload: { thread_id: `t-${seq}` },
      })
    }

    expect(warningHandler).toHaveBeenCalled()
    expect(warningHandler.mock.calls[0]?.[0]?.content).toContain('out-of-order events were dropped')
    ws.destroy()
  })

  it('recovers from a persistent sequence gap after timeout', async () => {
    vi.useFakeTimers()
    try {
      const ws = await createWS()
      MockWebSocket.latest().simulateOpen()

      const seen: number[] = []
      const warningHandler = vi.fn()
      ws.on('thread_done', (msg) => { seen.push(msg.seq ?? -1) })
      ws.on('warning', warningHandler)

      MockWebSocket.latest().simulateMessage({
        type: 'thread_done',
        session_id: 'sess-gap',
        epoch: 444,
        seq: 1,
        payload: { thread_id: 't-1' },
      })
      MockWebSocket.latest().simulateMessage({
        type: 'thread_done',
        session_id: 'sess-gap',
        epoch: 444,
        seq: 3,
        payload: { thread_id: 't-3' },
      })
      // No seq=2 arrives; timer-based recovery should eventually flush seq=3.
      await vi.advanceTimersByTimeAsync(3_100)

      expect(seen).toEqual([1, 3])
      expect(warningHandler).toHaveBeenCalled()
      expect(warningHandler.mock.calls.some(
        call => String(call[0]?.content ?? '').includes('ordering gap persisted'),
      )).toBe(true)
      ws.destroy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('ignores malformed JSON in incoming messages', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()
    // Simulate a raw bad message
    MockWebSocket.latest().onmessage?.({ data: 'not-json{{' })
    expect(ws.messages.value).toHaveLength(0)
    ws.destroy()
  })

  describe('resume after reconnect', () => {
    function sentResumes(sock: MockWebSocket) {
      return sock.sentMessages
        .map(m => JSON.parse(m))
        .filter(m => m.type === 'resume')
    }

    it('does not send resume on the initial connection', async () => {
      const ws = await createWS()
      MockWebSocket.latest().simulateOpen()
      expect(sentResumes(MockWebSocket.latest())).toHaveLength(0)
      ws.destroy()
    })

    it('sends resume with the last delivered seq and epoch after a drop', async () => {
      const ws = await createWS()
      const first = MockWebSocket.latest()
      first.simulateOpen()

      // Deliver seq-stamped traffic so the session is tracked.
      first.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 1 })
      first.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 2 })
      first.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 3 })

      // Unclean drop → reconnect.
      first.simulateClose(1006)
      await vi.advanceTimersByTimeAsync(2_000)
      const second = MockWebSocket.latest()
      expect(second).not.toBe(first)
      second.simulateOpen()

      const resumes = sentResumes(second)
      expect(resumes).toHaveLength(1)
      expect(resumes[0].session_id).toBe('s1')
      expect(resumes[0].payload).toEqual({ last_seq: 3, epoch: 7 })
      ws.destroy()
    })

    it('resumes the active session first', async () => {
      const ws = await createWS()
      const first = MockWebSocket.latest()
      first.simulateOpen()
      ws.setActiveSession('sess-active')
      first.simulateMessage({ type: 'token', session_id: 'sess-other', epoch: 7, seq: 1 })

      first.simulateClose(1006)
      await vi.advanceTimersByTimeAsync(2_000)
      const second = MockWebSocket.latest()
      second.simulateOpen()

      const resumes = sentResumes(second)
      expect(resumes.map(r => r.session_id)).toEqual(['sess-active', 'sess-other'])
      // No seq state tracked yet for the active session → last_seq 0.
      expect(resumes[0].payload.last_seq).toBe(0)
      expect(resumes[1].payload.last_seq).toBe(1)
      ws.destroy()
    })

    it('drops replayed messages already delivered (seq <= lastSeq)', async () => {
      const ws = await createWS()
      const handler = vi.fn()
      ws.on('token', handler)
      const sock = MockWebSocket.latest()
      sock.simulateOpen()

      sock.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 1 })
      sock.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 2 })
      // Server replays seq 1-3 after a resume; only seq 3 is new.
      sock.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 1 })
      sock.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 2 })
      sock.simulateMessage({ type: 'token', session_id: 's1', epoch: 7, seq: 3 })

      expect(handler).toHaveBeenCalledTimes(3)
      expect(handler.mock.calls.map(c => c[0].seq)).toEqual([1, 2, 3])
      ws.destroy()
    })

    it('passes resume_ok through to handlers unordered', async () => {
      const ws = await createWS()
      const handler = vi.fn()
      ws.on('resume_ok', handler)
      const sock = MockWebSocket.latest()
      sock.simulateOpen()

      sock.simulateMessage({
        type: 'resume_ok',
        session_id: 's1',
        payload: { seq: 9, gap: true, replayed: 0 },
      })
      expect(handler).toHaveBeenCalledTimes(1)
      expect(handler.mock.calls[0][0].payload.gap).toBe(true)
      ws.destroy()
    })
  })

  describe('streamChat', () => {
    function makeSSEStream(lines: string[]): ReadableStream<Uint8Array> {
      const encoder = new TextEncoder()
      return new ReadableStream({
        start(controller) {
          for (const line of lines) {
            controller.enqueue(encoder.encode(line + '\n'))
          }
          controller.close()
        },
      })
    }

    it('calls onToken for each token event', async () => {
      const tokens: string[] = []
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        body: makeSSEStream([
          'data: {"type":"token","content":"Hello"}',
          'data: {"type":"token","content":" world"}',
          'data: {"type":"done"}',
        ]),
      }))
      const ws = await createWS('tok')
      await ws.streamChat('sess-1', 'hi', (tok) => tokens.push(tok))
      expect(tokens).toEqual(['Hello', ' world'])
      ws.destroy()
    })

    it('resolves when done event is received', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        body: makeSSEStream([
          'data: {"type":"done"}',
        ]),
      }))
      const ws = await createWS('tok')
      await expect(ws.streamChat('sess-1', 'hi', () => {})).resolves.toBeUndefined()
      ws.destroy()
    })

    it('throws when response is not ok', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      }))
      const ws = await createWS('tok')
      await expect(ws.streamChat('sess-1', 'hi', () => {})).rejects.toThrow('Stream failed: 500')
      ws.destroy()
    })

    it('throws on error event from stream', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        body: makeSSEStream([
          'data: {"type":"error","content":"model failed"}',
        ]),
      }))
      const ws = await createWS('tok')
      await expect(ws.streamChat('sess-1', 'hi', () => {})).rejects.toThrow('model failed')
      ws.destroy()
    })

    it('skips malformed lines and non-data lines', async () => {
      const tokens: string[] = []
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
        ok: true,
        body: makeSSEStream([
          'not-valid-sse',
          ': comment',
          'data: {"type":"token","content":"ok"}',
          'data: {"type":"done"}',
        ]),
      }))
      const ws = await createWS('tok')
      await ws.streamChat('sess-1', 'hi', (tok) => tokens.push(tok))
      expect(tokens).toEqual(['ok'])
      ws.destroy()
    })
  })
})
