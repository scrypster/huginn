import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { wireSwarmWS, useSwarmStatus } from '../useSwarmStatus'
import type { HuginnWS, WSMessage } from '../useHuginnWS'

// ── helpers ───────────────────────────────────────────────────────────────────

function makeMockWS() {
  const handlers = new Map<string, ((msg: WSMessage) => void)[]>()

  const ws = {
    on: vi.fn((type: string, fn: (msg: WSMessage) => void) => {
      const list = handlers.get(type) ?? []
      list.push(fn)
      handlers.set(type, list)
    }),
    off: vi.fn((type: string, fn: (msg: WSMessage) => void) => {
      const list = handlers.get(type) ?? []
      handlers.set(type, list.filter(h => h !== fn))
    }),
    emit(type: string, payload?: Record<string, unknown>, sessionId?: string) {
      const msg: WSMessage = { type, payload, session_id: sessionId }
      const list = handlers.get(type) ?? []
      list.forEach(h => h(msg))
    },
  } as unknown as HuginnWS & { emit(type: string, payload?: Record<string, unknown>, sessionId?: string): void }

  return { ws, handlers }
}

beforeEach(() => {
  const { clearSwarm } = useSwarmStatus()
  clearSwarm()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── initial state ─────────────────────────────────────────────────────────────

describe('initial state', () => {
  it('swarmState is null before any events', () => {
    const { swarmState } = useSwarmStatus()
    expect(swarmState.value).toBeNull()
  })

  it('isSwarmActive is false when no swarm', () => {
    const { isSwarmActive } = useSwarmStatus()
    expect(isSwarmActive.value).toBe(false)
  })
})

// ── wireSwarmWS — swarm_start ─────────────────────────────────────────────────

describe('swarm_start event', () => {
  it('initialises swarmState with agent list', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')

    ws.emit('swarm_start', {
      agents: [
        { id: 'a1', name: 'Agent One' },
        { id: 'a2', name: 'Agent Two' },
      ],
    })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value).not.toBeNull()
    expect(swarmState.value!.sessionId).toBe('sess-1')
    expect(swarmState.value!.agents).toHaveLength(2)
    expect(swarmState.value!.agents[0].id).toBe('a1')
    expect(swarmState.value!.agents[0].status).toBe('waiting')
    expect(swarmState.value!.complete).toBe(false)
  })

  it('initialises all agents with waiting status and empty output', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')

    ws.emit('swarm_start', { agents: [{ id: 'x', name: 'X' }] })

    const { swarmState } = useSwarmStatus()
    const agent = swarmState.value!.agents[0]
    expect(agent.status).toBe('waiting')
    expect(agent.output).toBe('')
    expect(agent.success).toBeUndefined()
  })

  it('handles empty agents array', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')

    ws.emit('swarm_start', { agents: [] })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents).toHaveLength(0)
  })

  it('handles missing agents field in payload', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')

    ws.emit('swarm_start', {})

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents).toHaveLength(0)
  })

  it('sets isSwarmActive to true after swarm_start', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    const { isSwarmActive } = useSwarmStatus()
    expect(isSwarmActive.value).toBe(true)
  })
})

// ── wireSwarmWS — swarm_agent_status ─────────────────────────────────────────

describe('swarm_agent_status event', () => {
  it('updates agent status', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'running' })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].status).toBe('running')
  })

  it('sets success=true on done status', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'done', success: true })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].success).toBe(true)
  })

  it('sets success=false and error on error status', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'error', success: false, error: 'timeout' })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].status).toBe('error')
    expect(swarmState.value!.agents[0].success).toBe(false)
    expect(swarmState.value!.agents[0].error).toBe('timeout')
  })

  it('ignores unknown agent_id', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    // Should not throw
    ws.emit('swarm_agent_status', { agent_id: 'unknown', status: 'running' })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].status).toBe('waiting')
  })

  it('ignores events with wrong session_id', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-correct')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'running' }, 'sess-wrong')

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].status).toBe('waiting')
  })

  it('accepts events without session_id (unscoped broadcast)', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    // No session_id in the message → should not be filtered out
    ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'running' })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].status).toBe('running')
  })

  it('is a no-op when swarmState is null', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    // No swarm_start — swarmState is null

    expect(() => {
      ws.emit('swarm_agent_status', { agent_id: 'a1', status: 'running' })
    }).not.toThrow()
  })
})

// ── wireSwarmWS — swarm_agent_token ──────────────────────────────────────────

describe('swarm_agent_token event', () => {
  it('appends output to the correct agent', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }, { id: 'a2', name: 'A2' }] })

    ws.emit('swarm_agent_token', { agent_id: 'a1', content: 'hello ' })
    ws.emit('swarm_agent_token', { agent_id: 'a1', content: 'world' })
    ws.emit('swarm_agent_token', { agent_id: 'a2', content: 'other' })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].output).toBe('hello world')
    expect(swarmState.value!.agents[1].output).toBe('other')
  })

  it('caps output at 64KB, keeping the tail', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    // Send > 64KB
    const chunk = 'x'.repeat(32 * 1024)
    ws.emit('swarm_agent_token', { agent_id: 'a1', content: chunk })
    ws.emit('swarm_agent_token', { agent_id: 'a1', content: chunk })
    ws.emit('swarm_agent_token', { agent_id: 'a1', content: chunk }) // 96KB total

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].output.length).toBe(64 * 1024)
  })

  it('ignores events with wrong session_id', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_agent_token', { agent_id: 'a1', content: 'should be ignored' }, 'sess-other')

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.agents[0].output).toBe('')
  })

  it('ignores unknown agent_id', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    expect(() => {
      ws.emit('swarm_agent_token', { agent_id: 'unknown', content: 'data' })
    }).not.toThrow()
  })
})

// ── wireSwarmWS — swarm_complete ──────────────────────────────────────────────

describe('swarm_complete event', () => {
  it('marks swarm as complete', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    ws.emit('swarm_complete', { cancelled: false, dropped_events: 0 })

    const { swarmState, isSwarmActive } = useSwarmStatus()
    expect(swarmState.value!.complete).toBe(true)
    expect(isSwarmActive.value).toBe(false)
  })

  it('records cancelled=true when swarm was cancelled', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [] })

    ws.emit('swarm_complete', { cancelled: true, dropped_events: 0 })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.cancelled).toBe(true)
  })

  it('records dropped_events count', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [] })

    ws.emit('swarm_complete', { cancelled: false, dropped_events: 42 })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.droppedEvents).toBe(42)
  })

  it('ignores events with wrong session_id', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [] })

    ws.emit('swarm_complete', { cancelled: false, dropped_events: 0 }, 'sess-other')

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value!.complete).toBe(false)
  })

  it('is a no-op when swarmState is null', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')

    expect(() => {
      ws.emit('swarm_complete', { cancelled: false })
    }).not.toThrow()
  })
})

// ── useSwarmStatus — clearSwarm ───────────────────────────────────────────────

describe('clearSwarm', () => {
  it('resets swarmState to null', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    const { clearSwarm, swarmState } = useSwarmStatus()
    clearSwarm()

    expect(swarmState.value).toBeNull()
  })

  it('resets isSwarmActive to false', () => {
    const { ws } = makeMockWS()
    wireSwarmWS(ws, () => 'sess-1')
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    const { clearSwarm, isSwarmActive } = useSwarmStatus()
    clearSwarm()

    expect(isSwarmActive.value).toBe(false)
  })
})

// ── wireSwarmWS — unsubscribe ─────────────────────────────────────────────────

describe('unsubscribe', () => {
  it('calling unsubscribe stops future event processing', () => {
    const { ws } = makeMockWS()
    const unsub = wireSwarmWS(ws, () => 'sess-1')

    unsub()

    // Events after unsubscribe should not affect state
    ws.emit('swarm_start', { agents: [{ id: 'a1', name: 'A1' }] })

    const { swarmState } = useSwarmStatus()
    expect(swarmState.value).toBeNull()
  })

  it('calls ws.off for all registered event types', () => {
    const { ws } = makeMockWS()
    const unsub = wireSwarmWS(ws, () => 'sess-1')
    unsub()

    const mockWs = ws as unknown as { off: ReturnType<typeof vi.fn> }
    const offCalls = mockWs.off.mock.calls.map((c: unknown[]) => c[0])
    expect(offCalls).toContain('swarm_start')
    expect(offCalls).toContain('swarm_agent_status')
    expect(offCalls).toContain('swarm_agent_token')
    expect(offCalls).toContain('swarm_complete')
  })
})
