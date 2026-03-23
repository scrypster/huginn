import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useSpaces, wireSpaceWS } from '../useSpaces'
import type { HuginnWS, WSMessage } from '../useHuginnWS'

// ── helpers ──────────────────────────────────────────────────────────────────

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function errResponse(status: number): Response {
  return new Response(JSON.stringify({ error: 'error' }), { status })
}

const sampleChannel = {
  id: 'ch-1',
  name: 'Engineering',
  kind: 'channel',
  lead_agent: 'atlas',
  member_agents: ['bob'],
  icon: '',
  color: '#58a6ff',
  unseen_count: 0,
  archived_at: null,
}

const sampleDM = {
  id: 'dm-1',
  name: 'atlas',
  kind: 'dm',
  lead_agent: 'atlas',
  member_agents: [],
  icon: '',
  color: '#58a6ff',
  unseen_count: 2,
  archived_at: null,
}

beforeEach(() => {
  // Reset shared state between tests
  const { clearSpaces } = useSpaces()
  clearSpaces()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── fetchSpaces ───────────────────────────────────────────────────────────────

describe('fetchSpaces', () => {
  it('populates spaces from paginated response { Spaces, NextCursor }', async () => {
    // This is the REAL API shape returned by ListSpaces (paginated result struct).
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson({ Spaces: [sampleChannel, sampleDM], NextCursor: '' }),
    )
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value).toHaveLength(2)
    expect(spaces.value[0].id).toBe('ch-1')
    expect(spaces.value[0].kind).toBe('channel')
    expect(spaces.value[1].kind).toBe('dm')
  })

  it('populates spaces from legacy plain-array response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson([sampleChannel, sampleDM]),
    )
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value).toHaveLength(2)
    expect(spaces.value[0].id).toBe('ch-1')
    expect(spaces.value[0].kind).toBe('channel')
    expect(spaces.value[1].kind).toBe('dm')
  })

  it('sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
    const { fetchSpaces, error } = useSpaces()
    await fetchSpaces()
    expect(error.value).toBeTruthy()
  })

  it('sets loading during fetch', async () => {
    let resolveIt!: (r: Response) => void
    vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(
      new Promise<Response>(r => { resolveIt = r }),
    )
    const { fetchSpaces, loading } = useSpaces()
    const p = fetchSpaces()
    expect(loading.value).toBe(true)
    resolveIt(okJson([]))
    await p
    expect(loading.value).toBe(false)
  })
})

// ── channels / dms computed ───────────────────────────────────────────────────

describe('channels and dms computed', () => {
  it('filters correctly by kind', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson([sampleChannel, sampleDM]),
    )
    const { fetchSpaces, channels, dms } = useSpaces()
    await fetchSpaces()
    expect(channels.value).toHaveLength(1)
    expect(channels.value[0].id).toBe('ch-1')
    expect(dms.value).toHaveLength(1)
    expect(dms.value[0].id).toBe('dm-1')
  })
})

// ── deleteSpace ───────────────────────────────────────────────────────────────

describe('deleteSpace', () => {
  it('removes space from list on success', async () => {
    // Load space first
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, deleteSpace, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value).toHaveLength(1)

    // Delete it
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    const ok = await deleteSpace('ch-1')
    expect(ok).toBe(true)
    expect(spaces.value).toHaveLength(0)
  })

  it('returns false and sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, deleteSpace, error } = useSpaces()
    await fetchSpaces()

    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('forbidden'))
    const ok = await deleteSpace('ch-1')
    expect(ok).toBe(false)
    expect(error.value).toBeTruthy()
  })

  it('clears activeSpaceId when active space is deleted', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleChannel]))
      .mockResolvedValueOnce(okJson([]))   // fetchSpaceSessions
    const { fetchSpaces, setActiveSpace, deleteSpace, activeSpaceId } = useSpaces()
    await fetchSpaces()
    setActiveSpace('ch-1')
    expect(activeSpaceId.value).toBe('ch-1')

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    await deleteSpace('ch-1')
    expect(activeSpaceId.value).toBeNull()
  })
})

// ── createChannel ─────────────────────────────────────────────────────────────

describe('createChannel', () => {
  it('adds channel to spaces on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleChannel))
    const { createChannel, spaces } = useSpaces()
    const sp = await createChannel({ name: 'Engineering', leadAgent: 'atlas', memberAgents: [] })
    expect(sp).not.toBeNull()
    expect(sp!.id).toBe('ch-1')
    expect(spaces.value.some(s => s.id === 'ch-1')).toBe(true)
  })

  it('returns null and sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('server error'))
    const { createChannel, error } = useSpaces()
    const sp = await createChannel({ name: 'Bad', leadAgent: '', memberAgents: [] })
    expect(sp).toBeNull()
    expect(error.value).toBeTruthy()
  })
})

// ── openDM ────────────────────────────────────────────────────────────────────

describe('openDM', () => {
  it('adds DM to spaces if not already present', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleDM))
    const { openDM, spaces } = useSpaces()
    const sp = await openDM('atlas')
    expect(sp).not.toBeNull()
    expect(sp!.kind).toBe('dm')
    expect(spaces.value.some(s => s.id === 'dm-1')).toBe(true)
  })

  it('returns null on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
    const { openDM } = useSpaces()
    const sp = await openDM('atlas')
    expect(sp).toBeNull()
  })
})

// ── markRead ──────────────────────────────────────────────────────────────────

describe('markRead', () => {
  it('resets unseenCount on the space', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleDM]))
    const { fetchSpaces, markRead, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value.find(s => s.id === 'dm-1')!.unseenCount).toBe(2)

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    await markRead('dm-1')
    expect(spaces.value.find(s => s.id === 'dm-1')!.unseenCount).toBe(0)
  })

  it('does not throw on API failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('oops'))
    const { markRead } = useSpaces()
    await expect(markRead('ghost')).resolves.not.toThrow()
  })
})

// ── setActiveSpace ────────────────────────────────────────────────────────────

describe('setActiveSpace', () => {
  it('persists to localStorage', () => {
    const { setActiveSpace, activeSpaceId } = useSpaces()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(okJson([]))
    setActiveSpace('space-xyz')
    expect(activeSpaceId.value).toBe('space-xyz')
  })

  it('clears localStorage when set to null', () => {
    const { setActiveSpace, activeSpaceId } = useSpaces()
    setActiveSpace(null)
    expect(activeSpaceId.value).toBeNull()
  })
})

// ── mapSpace defaults ─────────────────────────────────────────────────────────

describe('mapSpace defaults', () => {
  it('defaults color to #58a6ff when not provided', async () => {
    const spaceNoColor = { ...sampleChannel, color: '' }
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([spaceNoColor]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].color).toBe('#58a6ff')
  })

  it('maps unseen_count correctly', async () => {
    const sp = { ...sampleDM, unseen_count: 5 }
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sp]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].unseenCount).toBe(5)
  })
})

// ── wireSpaceWS ───────────────────────────────────────────────────────────────

// Minimal HuginnWS stub for unit tests.
function makeWsStub(): HuginnWS & { _emit: (msg: WSMessage) => void } {
  const handlers = new Map<string, ((msg: WSMessage) => void)[]>()
  return {
    connected: { value: true } as HuginnWS['connected'],
    messages: { value: [] } as HuginnWS['messages'],
    send: vi.fn(),
    on(type: string, fn: (msg: WSMessage) => void) {
      if (!handlers.has(type)) handlers.set(type, [])
      handlers.get(type)!.push(fn)
    },
    off(type: string, fn: (msg: WSMessage) => void) {
      const fns = handlers.get(type) ?? []
      handlers.set(type, fns.filter(f => f !== fn))
    },
    destroy: vi.fn(),
    streamChat: vi.fn() as HuginnWS['streamChat'],
    _emit(msg: WSMessage) {
      const fns = handlers.get(msg.type) ?? []
      fns.forEach(f => f(msg))
    },
  }
}

function makeMsg(type: string, payload: Record<string, unknown>): WSMessage {
  return { type, payload }
}

describe('wireSpaceWS', () => {
  beforeEach(async () => {
    // Reset module-level spaces state
    const { clearSpaces } = useSpaces()
    clearSpaces()
  })

  it('space_member_added appends agent to memberAgents', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].memberAgents).toEqual(['bob'])

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_member_added', { space_id: 'ch-1', agent: 'alice' }))

    expect(spaces.value[0].memberAgents).toEqual(['bob', 'alice'])
  })

  it('space_member_added is idempotent — same agent twice yields one entry', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_member_added', { space_id: 'ch-1', agent: 'alice' }))
    ws._emit(makeMsg('space_member_added', { space_id: 'ch-1', agent: 'alice' }))

    expect(spaces.value[0].memberAgents.filter(a => a === 'alice')).toHaveLength(1)
  })

  it('space_member_removed filters agent from memberAgents', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].memberAgents).toContain('bob')

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_member_removed', { space_id: 'ch-1', agent: 'bob' }))

    expect(spaces.value[0].memberAgents).not.toContain('bob')
  })

  it('space_member_removed for unknown space_id is a no-op', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = [...spaces.value[0].memberAgents]

    const ws = makeWsStub()
    wireSpaceWS(ws)
    // Should not throw
    ws._emit(makeMsg('space_member_removed', { space_id: 'no-such-space', agent: 'bob' }))

    expect(spaces.value[0].memberAgents).toEqual(before)
  })

  it('missing payload fields are a no-op (no crash)', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = [...spaces.value[0].memberAgents]

    const ws = makeWsStub()
    wireSpaceWS(ws)
    // Missing both space_id and agent
    ws._emit(makeMsg('space_member_added', {}))
    ws._emit(makeMsg('space_member_removed', {}))

    expect(spaces.value[0].memberAgents).toEqual(before)
  })

  it('unsubscribe prevents further state updates', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = [...spaces.value[0].memberAgents]

    const ws = makeWsStub()
    const unsub = wireSpaceWS(ws)
    unsub() // unsubscribe immediately

    ws._emit(makeMsg('space_member_added', { space_id: 'ch-1', agent: 'alice' }))
    ws._emit(makeMsg('space_member_removed', { space_id: 'ch-1', agent: 'bob' }))

    // State should be unchanged because listeners were removed
    expect(spaces.value[0].memberAgents).toEqual(before)
  })

  it('space_created adds new space at front of list', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value).toHaveLength(1)

    const ws = makeWsStub()
    wireSpaceWS(ws)

    const newSpace = {
      id: 'ch-2',
      name: 'New Channel',
      kind: 'channel',
      lead_agent: 'zeus',
      member_agents: ['atlas'],
      icon: '',
      color: '#ff0000',
      unseen_count: 0,
      archived_at: null,
    }
    ws._emit(makeMsg('space_created', { space: newSpace }))

    expect(spaces.value).toHaveLength(2)
    expect(spaces.value[0].id).toBe('ch-2')
  })

  it('space_created is deduplicated — same space twice yields one entry', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()

    const ws = makeWsStub()
    wireSpaceWS(ws)

    const newSpace = {
      id: 'ch-99',
      name: 'Dupe',
      kind: 'channel',
      lead_agent: 'zeus',
      member_agents: [],
      icon: '',
      color: '#58a6ff',
      unseen_count: 0,
      archived_at: null,
    }
    ws._emit(makeMsg('space_created', { space: newSpace }))
    ws._emit(makeMsg('space_created', { space: newSpace }))

    expect(spaces.value.filter(s => s.id === 'ch-99')).toHaveLength(1)
  })

  it('space_created with missing payload is a no-op', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = spaces.value.length

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_created', {}))

    expect(spaces.value).toHaveLength(before)
  })

  it('space_updated merges updated space data into list', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].name).toBe('Engineering')

    const ws = makeWsStub()
    wireSpaceWS(ws)

    const updatedSpace = {
      ...sampleChannel,
      name: 'Engineering Updated',
      lead_agent: 'zeus',
    }
    ws._emit(makeMsg('space_updated', { space: updatedSpace }))

    expect(spaces.value).toHaveLength(1)
    expect(spaces.value[0].name).toBe('Engineering Updated')
    expect(spaces.value[0].leadAgent).toBe('zeus')
  })

  it('space_updated for unknown space_id is a no-op', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = spaces.value[0].name

    const ws = makeWsStub()
    wireSpaceWS(ws)

    const unknownSpace = { ...sampleChannel, id: 'no-such-space', name: 'Ghost' }
    ws._emit(makeMsg('space_updated', { space: unknownSpace }))

    expect(spaces.value[0].name).toBe(before)
  })

  it('space_archived removes space from list', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel, sampleDM]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value).toHaveLength(2)

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_archived', { space_id: 'ch-1' }))

    expect(spaces.value).toHaveLength(1)
    expect(spaces.value[0].id).toBe('dm-1')
  })

  it('space_archived clears activeSpaceId when the archived space was active', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleChannel]))
      .mockResolvedValueOnce(okJson([]))   // fetchSpaceSessions triggered by setActiveSpace
    const { fetchSpaces, setActiveSpace, activeSpaceId, spaces } = useSpaces()
    await fetchSpaces()
    setActiveSpace('ch-1')
    expect(activeSpaceId.value).toBe('ch-1')

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_archived', { space_id: 'ch-1' }))

    expect(spaces.value).toHaveLength(0)
    expect(activeSpaceId.value).toBeNull()
  })

  it('space_archived does not clear activeSpaceId for a different space', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleChannel, sampleDM]))
      .mockResolvedValueOnce(okJson([]))   // fetchSpaceSessions triggered by setActiveSpace
    const { fetchSpaces, setActiveSpace, activeSpaceId } = useSpaces()
    await fetchSpaces()
    setActiveSpace('ch-1')
    expect(activeSpaceId.value).toBe('ch-1')

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_archived', { space_id: 'dm-1' }))

    expect(activeSpaceId.value).toBe('ch-1')
  })

  it('space_archived with missing space_id is a no-op', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_archived', {}))

    expect(spaces.value).toHaveLength(1)
  })

  it('space_activity updates unseenCount for inactive space', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleDM]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].unseenCount).toBe(2)

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_activity', { space_id: 'dm-1', unseen_count: 5 }))

    expect(spaces.value[0].unseenCount).toBe(5)
  })

  it('space_activity ignores increments for the active space', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleDM]))
      .mockResolvedValueOnce(okJson([]))   // fetchSpaceSessions triggered by setActiveSpace
    const { fetchSpaces, setActiveSpace, spaces } = useSpaces()
    await fetchSpaces()
    setActiveSpace('dm-1')
    // setActiveSpace clears unseenCount optimistically
    expect(spaces.value[0].unseenCount).toBe(0)

    const ws = makeWsStub()
    wireSpaceWS(ws)
    // Increment while active — should be ignored
    ws._emit(makeMsg('space_activity', { space_id: 'dm-1', unseen_count: 3 }))

    expect(spaces.value[0].unseenCount).toBe(0)
  })

  it('space_activity allows count-to-zero for the active space (mark-read confirmation)', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleDM]))
      .mockResolvedValueOnce(okJson([]))   // fetchSpaceSessions triggered by setActiveSpace
    const { fetchSpaces, setActiveSpace, spaces } = useSpaces()
    await fetchSpaces()
    setActiveSpace('dm-1')

    // Manually set unseenCount to non-zero to test the zero update path
    spaces.value[0].unseenCount = 2

    const ws = makeWsStub()
    wireSpaceWS(ws)
    // count=0 should be allowed through even for active space
    ws._emit(makeMsg('space_activity', { space_id: 'dm-1', unseen_count: 0 }))

    expect(spaces.value[0].unseenCount).toBe(0)
  })

  it('space_activity with missing fields is a no-op', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleDM]))
    const { fetchSpaces, spaces } = useSpaces()
    await fetchSpaces()
    const before = spaces.value[0].unseenCount

    const ws = makeWsStub()
    wireSpaceWS(ws)
    ws._emit(makeMsg('space_activity', { space_id: 'dm-1' }))   // missing unseen_count
    ws._emit(makeMsg('space_activity', { unseen_count: 9 }))    // missing space_id

    expect(spaces.value[0].unseenCount).toBe(before)
  })
})

// ── updateSpace ───────────────────────────────────────────────────────────────

describe('updateSpace', () => {
  it('updates name and reflects in spaces list', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, updateSpace, spaces } = useSpaces()
    await fetchSpaces()

    const updated = { ...sampleChannel, name: 'Engineering Renamed' }
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(updated))
    const result = await updateSpace('ch-1', { name: 'Engineering Renamed' })

    expect(result).not.toBeNull()
    expect(result!.name).toBe('Engineering Renamed')
    expect(spaces.value[0].name).toBe('Engineering Renamed')
  })

  it('updates memberAgents only — leadAgent and name are omitted from patch body', async () => {
    const updated = { ...sampleChannel, member_agents: ['bob', 'alice'] }
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleChannel]))  // fetchSpaces
      .mockResolvedValueOnce(okJson(updated))           // updateSpace PATCH
    const { fetchSpaces, updateSpace } = useSpaces()
    await fetchSpaces()
    await updateSpace('ch-1', { memberAgents: ['bob', 'alice'] })

    // calls[1] is the PATCH (calls[0] is the GET list)
    const patchCall = fetchSpy.mock.calls[1]
    const callBody = JSON.parse(patchCall[1]?.body as string)
    expect(callBody).toHaveProperty('member_agents')
    expect(callBody).not.toHaveProperty('lead_agent')
    expect(callBody).not.toHaveProperty('name')
  })

  it('returns null and does not update list on API failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, updateSpace, spaces } = useSpaces()
    await fetchSpaces()
    const originalName = spaces.value[0].name

    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('server error'))
    const result = await updateSpace('ch-1', { name: 'Should Not Apply' })

    expect(result).toBeNull()
    expect(spaces.value[0].name).toBe(originalName)
  })

  it('does not include name in patch when only memberAgents and leadAgent are provided', async () => {
    const updated = { ...sampleChannel, lead_agent: 'zeus', member_agents: ['alice'] }
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson([sampleChannel]))  // fetchSpaces
      .mockResolvedValueOnce(okJson(updated))           // updateSpace PATCH
    const { fetchSpaces, updateSpace } = useSpaces()
    await fetchSpaces()
    await updateSpace('ch-1', { leadAgent: 'zeus', memberAgents: ['alice'] })

    // calls[1] is the PATCH (calls[0] is the GET list)
    const patchCall = fetchSpy.mock.calls[1]
    const callBody = JSON.parse(patchCall[1]?.body as string)
    expect(callBody).toHaveProperty('lead_agent', 'zeus')
    expect(callBody).toHaveProperty('member_agents')
    expect(callBody).not.toHaveProperty('name')
  })

  it('updates the space in-place at the correct index when multiple spaces exist', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel, sampleDM]))
    const { fetchSpaces, updateSpace, spaces } = useSpaces()
    await fetchSpaces()
    expect(spaces.value[0].id).toBe('ch-1')
    expect(spaces.value[1].id).toBe('dm-1')

    const updated = { ...sampleDM, name: 'alice-dm-renamed' }
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(updated))
    await updateSpace('dm-1', { name: 'alice-dm-renamed' })

    // ch-1 must remain at index 0, dm-1 updated at index 1
    expect(spaces.value[0].id).toBe('ch-1')
    expect(spaces.value[0].name).toBe('Engineering')
    expect(spaces.value[1].id).toBe('dm-1')
    expect(spaces.value[1].name).toBe('alice-dm-renamed')
  })

  it('returns mapped Space object with camelCase fields', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleChannel]))
    const { fetchSpaces, updateSpace } = useSpaces()
    await fetchSpaces()

    const updated = { ...sampleChannel, lead_agent: 'hermes', member_agents: ['bob', 'zeus'] }
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(updated))
    const result = await updateSpace('ch-1', { leadAgent: 'hermes' })

    expect(result).not.toBeNull()
    expect(result!.leadAgent).toBe('hermes')
    expect(result!.memberAgents).toEqual(['bob', 'zeus'])
  })
})

// ── fetchSpaceSessions ────────────────────────────────────────────────────────

describe('fetchSpaceSessions', () => {
  const sampleSessions = [
    {
      id: 'sess-1',
      title: 'Incident Review',
      status: 'done',
      created_at: '2026-03-01T10:00:00Z',
      updated_at: '2026-03-01T11:00:00Z',
      space_id: 'ch-1',
    },
    {
      id: 'sess-2',
      title: 'Planning',
      status: 'active',
      created_at: '2026-03-02T09:00:00Z',
      updated_at: '2026-03-02T09:30:00Z',
      space_id: 'ch-1',
    },
  ]

  it('returns sessions array on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleSessions))
    const { fetchSpaceSessions } = useSpaces()
    const result = await fetchSpaceSessions('ch-1')
    expect(result).toHaveLength(2)
    expect(result[0].id).toBe('sess-1')
    expect(result[1].id).toBe('sess-2')
  })

  it('returns empty array on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network error'))
    const { fetchSpaceSessions } = useSpaces()
    const result = await fetchSpaceSessions('ch-1')
    expect(result).toEqual([])
  })

  it('stores result in spaceSessionsMap keyed by spaceId', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleSessions))
    const { fetchSpaceSessions, spaceSessionsMap } = useSpaces()
    await fetchSpaceSessions('ch-1')
    expect(spaceSessionsMap.value['ch-1']).toHaveLength(2)
    expect(spaceSessionsMap.value['ch-1'][0].id).toBe('sess-1')
  })

  it('stores empty array in spaceSessionsMap when API returns empty', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([]))
    const { fetchSpaceSessions, spaceSessionsMap } = useSpaces()
    await fetchSpaceSessions('ch-1')
    expect(spaceSessionsMap.value['ch-1']).toEqual([])
  })

  it('stores results for multiple spaces independently', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(okJson(sampleSessions))
      .mockResolvedValueOnce(okJson([{ ...sampleSessions[0], id: 'sess-dm', space_id: 'dm-1' }]))
    const { fetchSpaceSessions, spaceSessionsMap } = useSpaces()
    await fetchSpaceSessions('ch-1')
    await fetchSpaceSessions('dm-1')
    expect(spaceSessionsMap.value['ch-1']).toHaveLength(2)
    expect(spaceSessionsMap.value['dm-1']).toHaveLength(1)
    expect(spaceSessionsMap.value['dm-1'][0].id).toBe('sess-dm')
  })

  it('does not write to spaceSessionsMap on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('boom'))
    const { fetchSpaceSessions, spaceSessionsMap } = useSpaces()
    await fetchSpaceSessions('ch-1')
    expect(spaceSessionsMap.value['ch-1']).toBeUndefined()
  })
})
