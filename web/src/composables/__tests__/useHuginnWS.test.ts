import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Minimal WebSocket mock
class MockWebSocket {
  static OPEN = 1
  static CONNECTING = 0
  static CLOSING = 2
  static CLOSED = 3

  readyState = MockWebSocket.CONNECTING
  url: string
  onopen: (() => void) | null = null
  onclose: ((ev: { code: number; reason: string; wasClean: boolean }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  sentMessages: string[] = []
  closed = false

  constructor(url: string) {
    this.url = url
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
    expect(MockWebSocket.latest().url).toBe('ws://localhost:3000/ws?token=mytoken')
    expect(ws.connected.value).toBe(false)
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

  it('ignores malformed JSON in incoming messages', async () => {
    const ws = await createWS()
    MockWebSocket.latest().simulateOpen()
    // Simulate a raw bad message
    MockWebSocket.latest().onmessage?.({ data: 'not-json{{' })
    expect(ws.messages.value).toHaveLength(0)
    ws.destroy()
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
