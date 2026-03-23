import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Helper to build a mock Response
function ok(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function err(status: number, body = ''): Response {
  return new Response(body, { status })
}

const sampleMessages = [
  { id: 'm1', role: 'user', content: 'Hello', agent: '', seq: 1, created_at: '2024-01-01T00:00:00Z' },
  { id: 'm2', role: 'assistant', content: 'Hi there', agent: 'Tom', seq: 2, created_at: '2024-01-01T00:00:01Z' },
]

async function freshUseThreadDetail() {
  vi.resetModules()
  // Stub useApi getToken so it can be imported without real server
  vi.mock('../useApi', () => ({
    getToken: () => 'test-token',
    api: {},
  }))
  const mod = await import('../useThreadDetail')
  return mod.useThreadDetail
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.resetModules()
})

describe('useThreadDetail — open', () => {
  it('fetches messages from /api/v1/messages/{id}/thread', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/v1/messages/msg-123/thread',
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: 'Bearer test-token' }) })
    )
  })

  it('sets isOpen=true after open()', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.isOpen.value).toBe(true)
  })

  it('stores threadMessageId after open()', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-abc')

    expect(td.threadMessageId.value).toBe('msg-abc')
  })

  it('populates messages from array response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.messages.value).toHaveLength(2)
    expect(td.messages.value[0]!.id).toBe('m1')
    expect(td.messages.value[1]!.agent).toBe('Tom')
  })

  it('populates messages from { messages: [...] } response shape', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ messages: sampleMessages }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.messages.value).toHaveLength(2)
  })

  it('returns empty array for unexpected response shape', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ data: 'weird' }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.messages.value).toHaveLength(0)
  })

  it('sets loading=false after successful fetch', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.loading.value).toBe(false)
  })

  it('sets loading=false and error on fetch failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network down'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.loading.value).toBe(false)
    expect(td.error.value).toContain('network down')
  })

  it('sets error on non-ok HTTP response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(err(404, 'not found'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')

    expect(td.error.value).toContain('404')
  })

  it('clears previous messages before loading new thread', async () => {
    const secondMessages = [{ id: 'x1', role: 'assistant', content: 'New', agent: 'Bot', seq: 1, created_at: '' }]
    vi.spyOn(globalThis, 'fetch')
      // open('msg-1'): thread fetch + artifact fetch (no agent so skipped, but just in case)
      .mockResolvedValueOnce(ok(sampleMessages))   // msg-1 thread messages
      .mockResolvedValueOnce(ok([]))               // msg-1 artifact fetch (returns empty)
      // open('msg-2'): thread fetch + artifact fetch
      .mockResolvedValueOnce(ok(secondMessages))   // msg-2 thread messages
      .mockResolvedValueOnce(ok([]))               // msg-2 artifact fetch

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-1')
    expect(td.messages.value).toHaveLength(2)

    await td.open('msg-2')
    expect(td.messages.value).toHaveLength(1)
    expect(td.messages.value[0]!.id).toBe('x1')
  })

  it('URL-encodes the messageId in the fetch path', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('id with spaces')

    const url = fetchSpy.mock.calls[0]![0] as string
    expect(url).toBe('/api/v1/messages/id%20with%20spaces/thread')
  })
})

describe('useThreadDetail — close', () => {
  it('sets isOpen=false', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')
    td.close()

    expect(td.isOpen.value).toBe(false)
  })

  it('clears threadMessageId', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')
    td.close()

    expect(td.threadMessageId.value).toBeNull()
  })

  it('clears messages', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-123')
    td.close()

    expect(td.messages.value).toHaveLength(0)
  })

  it('clears error', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('oops'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('msg-bad')
    expect(td.error.value).toBeTruthy()

    td.close()
    expect(td.error.value).toBeNull()
  })
})

describe('useThreadDetail — initial state', () => {
  it('starts with isOpen=false', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.isOpen.value).toBe(false)
  })

  it('starts with empty messages', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.messages.value).toHaveLength(0)
  })

  it('starts with null threadMessageId', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.threadMessageId.value).toBeNull()
  })

  it('starts with null error', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.error.value).toBeNull()
  })

  it('starts with loading=false', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.loading.value).toBe(false)
  })

  it('starts with artifact=null', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.artifact.value).toBeNull()
  })

  it('starts with delegationChain=[]', async () => {
    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    expect(td.delegationChain.value).toEqual([])
  })
})

// ── artifact loading ──────────────────────────────────────────────────────────

const sampleArtifact = {
  id: 'art-1',
  kind: 'document',
  title: 'My Doc',
  content: 'doc content',
  agent_name: 'Tom',
  status: 'draft',
  triggering_message_id: 'm2',
}

describe('useThreadDetail — artifact loading', () => {
  it('fetches artifacts from /api/v1/agents/{name}/artifacts', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))          // thread messages
      .mockResolvedValueOnce(ok([sampleArtifact]))        // artifact fetch

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    // Second fetch should be artifact endpoint
    const secondUrl = fetchSpy.mock.calls[1]![0] as string
    expect(secondUrl).toContain('/api/v1/agents/Tom/artifacts')
  })

  it('sets artifact when triggering_message_id matches messageId', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    expect(td.artifact.value).not.toBeNull()
    expect(td.artifact.value!.id).toBe('art-1')
  })

  it('artifact is null when no triggering_message_id matches', async () => {
    const differentArtifact = { ...sampleArtifact, triggering_message_id: 'other-id' }
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([differentArtifact]))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    expect(td.artifact.value).toBeNull()
  })

  it('artifact remains null when artifact fetch fails', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockRejectedValueOnce(new Error('artifact api down'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    expect(td.artifact.value).toBeNull()
  })

  it('derives agentName from first assistant message when agentName param is empty', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))   // Tom is the assistant agent
      .mockResolvedValueOnce(ok([]))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', '')

    const secondUrl = fetchSpy.mock.calls[1]![0] as string
    expect(secondUrl).toContain('/api/v1/agents/Tom/artifacts')
  })

  it('skips artifact fetch when no agentName is available', async () => {
    const noAgentMessages = sampleMessages.map(m => ({ ...m, agent: '' }))
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(noAgentMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m1', '')

    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('handles artifacts in { artifacts: [...] } response shape', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok({ artifacts: [sampleArtifact] }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    expect(td.artifact.value).not.toBeNull()
    expect(td.artifact.value!.id).toBe('art-1')
  })

  it('close() clears artifact', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    expect(td.artifact.value).not.toBeNull()

    td.close()
    expect(td.artifact.value).toBeNull()
  })
})

// ── handleAcceptArtifact ──────────────────────────────────────────────────────

describe('useThreadDetail — handleAcceptArtifact', () => {
  it('calls PATCH /api/v1/artifacts/{id}/status with status=accepted', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))   // PATCH

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    await td.handleAcceptArtifact('art-1')

    const patchCall = fetchSpy.mock.calls[2]!
    expect(patchCall[0]).toContain('/api/v1/artifacts/art-1/status')
    const opts = patchCall[1] as RequestInit
    expect(opts.method).toBe('PATCH')
    const body = JSON.parse(opts.body as string)
    expect(body.status).toBe('accepted')
  })

  it('updates artifact status locally to accepted', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    expect(td.artifact.value!.status).toBe('draft')

    await td.handleAcceptArtifact('art-1')
    expect(td.artifact.value!.status).toBe('accepted')
  })

  it('does not throw if PATCH request fails', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockRejectedValueOnce(new Error('patch failed'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    await expect(td.handleAcceptArtifact('art-1')).resolves.not.toThrow()
  })
})

// ── handleRejectArtifact ──────────────────────────────────────────────────────

describe('useThreadDetail — handleRejectArtifact', () => {
  it('calls PATCH /api/v1/artifacts/{id}/status with status=rejected', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    await td.handleRejectArtifact('art-1')

    const patchCall = fetchSpy.mock.calls[2]!
    expect(patchCall[0]).toContain('/api/v1/artifacts/art-1/status')
    const body = JSON.parse((patchCall[1] as RequestInit).body as string)
    expect(body.status).toBe('rejected')
  })

  it('updates artifact status locally to rejected', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    await td.handleRejectArtifact('art-1')
    expect(td.artifact.value!.status).toBe('rejected')
  })

  it('includes rejection reason in request body when provided', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    await td.handleRejectArtifact('art-1', 'Too risky')

    const patchCall = fetchSpy.mock.calls[2]!
    const body = JSON.parse((patchCall[1] as RequestInit).body as string)
    expect(body.reason).toBe('Too risky')
  })

  it('stores rejection_reason on the local artifact', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockResolvedValueOnce(ok({ ok: true }))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    await td.handleRejectArtifact('art-1', 'Out of scope')
    expect(td.artifact.value!.rejection_reason).toBe('Out of scope')
  })

  it('does not throw if PATCH request fails', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockRejectedValueOnce(new Error('network'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')

    await expect(td.handleRejectArtifact('art-1')).resolves.not.toThrow()
  })
})

// ── wireThreadDetailWS ────────────────────────────────────────────────────────

// Helper: fresh module that also returns wireThreadDetailWS (module-level export)
async function freshWithWire() {
  vi.resetModules()
  vi.mock('../useApi', () => ({ getToken: () => 'test-token', api: {} }))
  const mod = await import('../useThreadDetail')
  return { useThreadDetail: mod.useThreadDetail, wireThreadDetailWS: mod.wireThreadDetailWS }
}

// Build a simple mock WS with on/off tracking
function mockWS() {
  const registrations: { type: string; fn: (msg: unknown) => void }[] = []
  const removals: { type: string; fn: (msg: unknown) => void }[] = []
  return {
    on(type: string, fn: (msg: unknown) => void) { registrations.push({ type, fn }) },
    off(type: string, fn: (msg: unknown) => void) { removals.push({ type, fn }) },
    emit(type: string, payload: unknown) {
      registrations
        .filter(r => r.type === type)
        .forEach(r => r.fn({ payload }))
    },
    registrations,
    removals,
  }
}

describe('useThreadDetail — wireThreadDetailWS', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('thread_started appends agent to delegationChain when panel is open', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))
    const td = useThreadDetail()
    await td.open('msg-ww1')

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_started', { thread_id: 'thr-1', agent_id: 'builder' })

    expect(td.delegationChain.value).toContain('builder')
  })

  it('thread_started does not add duplicate agents to delegationChain', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleMessages))
    const td = useThreadDetail()
    await td.open('msg-ww2')

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_started', { thread_id: 'thr-1', agent_id: 'builder' })
    ws.emit('thread_started', { thread_id: 'thr-2', agent_id: 'builder' })

    expect(td.delegationChain.value.filter(a => a === 'builder')).toHaveLength(1)
  })

  it('thread_started does not modify delegationChain when panel is closed', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Do NOT call td.open() — panel stays closed
    const td = useThreadDetail()

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_started', { thread_id: 'thr-1', agent_id: 'builder' })

    expect(td.delegationChain.value).toHaveLength(0)
  })

  it('thread_done triggers a debounced refetch when panel is open', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Use empty messages so open() makes exactly 1 fetch (no agents → no artifact fetch)
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-ww3')

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_done', { thread_id: 'thr-done' })

    // Before debounce settles, fetch should not have been called again
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    // Advance past debounce and flush async
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    expect(fetchSpy).toHaveBeenCalledTimes(2)
  })

  it('thread_status "done" triggers a refetch', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Use empty messages so open() makes exactly 1 fetch (no agents → no artifact fetch)
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-ww4')

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_status', { thread_id: 'thr-st', status: 'done' })
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    expect(fetchSpy).toHaveBeenCalledTimes(2)
  })

  it('thread_status "thinking" does NOT trigger a refetch', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Use empty messages so open() makes exactly 1 fetch (no assistant agent → no artifact fetch)
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-ww5')

    const callsAfterOpen = fetchSpy.mock.calls.length

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_status', { thread_id: 'thr-thinking', status: 'thinking' })
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    // No additional fetch triggered by non-terminal status
    expect(fetchSpy).toHaveBeenCalledTimes(callsAfterOpen)
  })

  it('refetch is skipped when panel is closed before debounce fires', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Use empty messages so open() makes exactly 1 fetch (no agent → no artifact fetch)
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-ww6')

    const callsAfterOpen = fetchSpy.mock.calls.length

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_done', { thread_id: 'thr-closed' })
    td.close() // close before debounce fires

    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    // Refetch is guarded by isOpen — no additional fetch should have fired
    expect(fetchSpy).toHaveBeenCalledTimes(callsAfterOpen)
  })

  it('unsubscribe function calls ws.off for all subscribed event types', async () => {
    const { useThreadDetail: _td, wireThreadDetailWS } = await freshWithWire()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))

    const ws = mockWS()
    const unsubscribe = wireThreadDetailWS(ws as any)
    unsubscribe()

    const removedTypes = ws.removals.map(r => r.type)
    expect(removedTypes).toContain('thread_started')
    expect(removedTypes).toContain('thread_done')
    expect(removedTypes).toContain('thread_status')
    // 5 subscriptions: thread_started, thread_token, thread_done, thread_status, thread_tool_call
    expect(removedTypes).toContain('thread_token')
    expect(removedTypes).toContain('thread_tool_call')
    expect(ws.removals).toHaveLength(5)
  })
})

// ── Debounced refetch ─────────────────────────────────────────────────────────

describe('useThreadDetail — debounced refetch', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('emitting thread_done three times rapidly results in only one refetch', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-debounce1')

    const callsAfterOpen = fetchSpy.mock.calls.length

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    // Fire three thread_done events without advancing timers
    ws.emit('thread_done', { thread_id: 'thr-d1' })
    ws.emit('thread_done', { thread_id: 'thr-d1' })
    ws.emit('thread_done', { thread_id: 'thr-d1' })

    // Debounce hasn't fired yet
    expect(fetchSpy).toHaveBeenCalledTimes(callsAfterOpen)

    // Advance past debounce window
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    // Only one additional fetch should have been made
    expect(fetchSpy).toHaveBeenCalledTimes(callsAfterOpen + 1)
  })

  it('refetch after thread_done uses the current threadMessageId value', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    const td = useThreadDetail()
    await td.open('msg-current-id')

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_done', { thread_id: 'thr-id1' })
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    // The refetch URL should use 'msg-current-id'
    const lastCall = fetchSpy.mock.calls[fetchSpy.mock.calls.length - 1]!
    expect(lastCall[0] as string).toContain('msg-current-id')
  })
})

// ── Concurrent open() calls ───────────────────────────────────────────────────

describe('useThreadDetail — concurrent open() calls', () => {
  it('second open() call clears messages from first and replaces with second result', async () => {
    // Use plain user-only messages so no artifact fetches are triggered
    const firstMessages = [
      { id: 'first-1', role: 'user', content: 'First message', agent: '', seq: 1, created_at: '' },
    ]
    const secondMessages = [
      { id: 'second-1', role: 'user', content: 'Second message', agent: '', seq: 1, created_at: '' },
      { id: 'second-2', role: 'user', content: 'Second follow-up', agent: '', seq: 2, created_at: '' },
    ]

    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(firstMessages))   // open('msg1') thread fetch (no assistant → no artifact)
      .mockResolvedValueOnce(ok(secondMessages))  // open('msg2') thread fetch (no assistant → no artifact)

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()

    await td.open('msg1')
    expect(td.messages.value).toHaveLength(1)
    expect(td.threadMessageId.value).toBe('msg1')

    await td.open('msg2')
    expect(td.messages.value).toHaveLength(2)
    expect(td.messages.value[0]!.id).toBe('second-1')
    expect(td.threadMessageId.value).toBe('msg2')
  })

  it('second open() resets error from first failed open()', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockRejectedValueOnce(new Error('first failed'))
      .mockResolvedValueOnce(ok(sampleMessages))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()

    await td.open('msg-fail')
    expect(td.error.value).toBeTruthy()

    await td.open('msg-ok')
    expect(td.error.value).toBeNull()
    expect(td.messages.value).toHaveLength(2)
  })
})

// ── Error recovery ────────────────────────────────────────────────────────────

describe('useThreadDetail — error recovery', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('when refetch triggered by thread_done fails, existing messages are preserved', async () => {
    const { useThreadDetail, wireThreadDetailWS } = await freshWithWire()
    // Use user-only messages so no artifact fetch is triggered (avoids counting issues)
    const userOnlyMessages = [
      { id: 'u1', role: 'user', content: 'Hello', agent: '', seq: 1, created_at: '' },
      { id: 'u2', role: 'user', content: 'World', agent: '', seq: 2, created_at: '' },
    ]
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(userOnlyMessages))        // initial open (no artifact fetch → only 1 call)
      .mockRejectedValueOnce(new Error('refetch failed')) // debounced refetch fails

    const td = useThreadDetail()
    await td.open('msg-recovery1')
    expect(td.messages.value).toHaveLength(2)

    const callsAfterOpen = fetchSpy.mock.calls.length

    const ws = mockWS()
    wireThreadDetailWS(ws as any)

    ws.emit('thread_done', { thread_id: 'thr-rec1' })
    vi.advanceTimersByTime(300)
    await vi.runAllTimersAsync()

    // Existing messages should still be there despite refetch failure
    expect(td.messages.value).toHaveLength(2)
    expect(fetchSpy).toHaveBeenCalledTimes(callsAfterOpen + 1)
  })

  it('when handleAcceptArtifact fails, artifact state is not cleared', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockRejectedValueOnce(new Error('patch failed'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    expect(td.artifact.value).not.toBeNull()
    const originalArtifact = td.artifact.value

    await td.handleAcceptArtifact('art-1')

    // Artifact should still be present (not cleared on failure)
    expect(td.artifact.value).not.toBeNull()
    expect(td.artifact.value!.id).toBe(originalArtifact!.id)
  })

  it('when handleRejectArtifact fails, artifact state is not cleared', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(sampleMessages))
      .mockResolvedValueOnce(ok([sampleArtifact]))
      .mockRejectedValueOnce(new Error('reject patch failed'))

    const useThreadDetail = await freshUseThreadDetail()
    const td = useThreadDetail()
    await td.open('m2', 'Tom')
    expect(td.artifact.value).not.toBeNull()

    await td.handleRejectArtifact('art-1', 'not needed')

    expect(td.artifact.value).not.toBeNull()
    expect(td.artifact.value!.id).toBe('art-1')
  })
})
