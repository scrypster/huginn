import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setToken } from '../useApi'

vi.mock('../useThreads', () => ({
  useThreads: () => ({
    isAgentActive: vi.fn().mockReturnValue(false),
  }),
}))

vi.mock('../useSessions', () => ({
  useSessions: () => ({
    sessions: { value: [] },
  }),
}))

async function freshActivity() {
  vi.resetModules()
  vi.doMock('../useThreads', () => ({
    useThreads: () => ({
      isAgentActive: vi.fn().mockReturnValue(false),
    }),
  }))
  vi.doMock('../useSessions', () => ({
    useSessions: () => ({
      sessions: { value: [] },
    }),
  }))
  const apiMod = await import('../useApi')
  apiMod.setToken('test-token')
  const companies = await import('../useCompanies')
  companies.useCompanies().clearCompanies()
  const { companies: list } = companies.useCompanies()
  list.value = [
    { id: 'huginn', name: 'Huginn', icon: 'H', color: '#58a6ff', members: ['Winston', 'Steve', 'Reggie'] },
    { id: 'lab', name: 'Lab', icon: 'L', color: '#3fb950', members: ['Winston', 'Sam'] },
  ]
  const mod = await import('../useAgentActivity')
  const activity = mod.useAgentActivity()
  activity.clearActivity()
  return activity
}

beforeEach(() => {
  localStorage.clear()
  setToken('test-token')
})

afterEach(() => {
  setToken('')
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('useAgentActivity', () => {
  it('pulses the typing agent and not others', async () => {
    const { isAgentPulsing, noteTyping, clearActivity } = await freshActivity()
    expect(isAgentPulsing('Reggie', 'huginn')).toBe(false)
    expect(isAgentPulsing('Steve', 'huginn')).toBe(false)

    noteTyping('Reggie', true)
    expect(isAgentPulsing('Reggie', 'huginn')).toBe(true)
    expect(isAgentPulsing('Steve', 'huginn')).toBe(false)
    expect(isAgentPulsing('Winston', 'huginn')).toBe(false)

    noteTyping('Reggie', false)
    expect(isAgentPulsing('Reggie', 'huginn')).toBe(false)
    clearActivity()
  })

  it('does not pulse Huginn-only Steve on Lab', async () => {
    const { isAgentPulsing, noteTyping } = await freshActivity()
    noteTyping('Steve', true)
    expect(isAgentPulsing('Steve', 'huginn')).toBe(true)
    expect(isAgentPulsing('Steve', 'lab')).toBe(false)
    expect(isAgentPulsing('Steve', null)).toBe(true)
  })

  it('wires space_reply_typing for that agent only', async () => {
    const { isAgentPulsing, wireActivityWS } = await freshActivity()
    const handlers: Record<string, (msg: { payload?: Record<string, unknown> }) => void> = {}
    const ws = {
      on: (type: string, fn: (msg: { payload?: Record<string, unknown> }) => void) => { handlers[type] = fn },
      off: vi.fn(),
    }
    wireActivityWS(ws as any)
    handlers['space_reply_typing']?.({ payload: { agent: 'Reggie', space_id: 'ch-1' } })
    expect(isAgentPulsing('Reggie', 'huginn')).toBe(true)
    expect(isAgentPulsing('Sam', 'lab')).toBe(false)
    handlers['space_reply_typing_done']?.({ payload: { agent: 'Reggie' } })
    expect(isAgentPulsing('Reggie', 'huginn')).toBe(false)
  })

  it('pulses hallway thinking without parent_id (ws.chat / desk DM)', async () => {
    const { isAgentPulsing, wireActivityWS } = await freshActivity()
    const handlers: Record<string, (msg: { payload?: Record<string, unknown> }) => void> = {}
    const ws = {
      on: (type: string, fn: (msg: { payload?: Record<string, unknown> }) => void) => { handlers[type] = fn },
      off: vi.fn(),
    }
    wireActivityWS(ws as any)
    handlers['space_reply_typing']?.({ payload: { agent: 'Winston', space_id: 'huginn-ch', session_id: 'sess-1' } })
    expect(isAgentPulsing('Winston', 'huginn')).toBe(true)
    expect(isAgentPulsing('Steve', 'huginn')).toBe(false)
    handlers['space_reply_typing_done']?.({ payload: { agent: 'Winston', session_id: 'sess-1' } })
    expect(isAgentPulsing('Winston', 'huginn')).toBe(false)
  })
})
