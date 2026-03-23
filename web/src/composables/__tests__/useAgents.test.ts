import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

function ok(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const sampleAgents = [
  { name: 'Coder', color: '#58A6FF', icon: 'C', model: 'claude-3', is_default: true },
  { name: 'Planner', color: '#3FB950', icon: 'P', model: 'gpt-4', is_default: false },
]

// Reset module cache between tests so agents singleton is fresh
async function freshUseAgents() {
  vi.resetModules()
  const mod = await import('../useAgents')
  return mod.useAgents
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useAgents — fetchAgents', () => {
  it('populates agents ref with API response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents } = useAgents()
    await fetchAgents()

    expect(agents.value).toHaveLength(2)
    expect(agents.value[0]!.name).toBe('Coder')
    expect(agents.value[1]!.name).toBe('Planner')
  })

  it('sets loading=false after successful fetch', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { loading, fetchAgents } = useAgents()
    await fetchAgents()

    expect(loading.value).toBe(false)
  })

  it('sets loading=false even when fetch throws', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network error'))

    const useAgents = await freshUseAgents()
    const { loading, fetchAgents } = useAgents()
    await fetchAgents()

    expect(loading.value).toBe(false)
  })

  it('silently ignores fetch errors (agents stays empty)', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('server error'))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents } = useAgents()
    await fetchAgents()

    // useAgents catches errors silently
    expect(agents.value).toHaveLength(0)
  })

  it('calls correct endpoint /api/v1/agents', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))

    const useAgents = await freshUseAgents()
    const { fetchAgents } = useAgents()
    await fetchAgents()

    expect(spy.mock.calls[0]![0]).toBe('/api/v1/agents')
  })

  it('populates agents with all fields', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents } = useAgents()
    await fetchAgents()

    const coder = agents.value.find(a => a.name === 'Coder')
    expect(coder).toBeDefined()
    expect(coder!.color).toBe('#58A6FF')
    expect(coder!.model).toBe('claude-3')
    expect(coder!.is_default).toBe(true)
  })
})

describe('useAgents — updateAgent', () => {
  it('patches agent in local agents list by name', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, updateAgent } = useAgents()
    await fetchAgents()

    updateAgent('Coder', { model: 'claude-opus' })

    const coder = agents.value.find(a => a.name === 'Coder')
    expect(coder!.model).toBe('claude-opus')
  })

  it('adds agent to list when name not found (upsert for create/rename)', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, updateAgent } = useAgents()
    await fetchAgents()

    updateAgent('NewAgent', { name: 'NewAgent', model: 'new-model', color: '#ffffff', icon: 'N' })
    expect(agents.value.find(a => a.name === 'NewAgent')).toBeDefined()
    expect(agents.value).toHaveLength(3)
  })

  it('merges patch fields without overwriting others', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, updateAgent } = useAgents()
    await fetchAgents()

    updateAgent('Planner', { color: '#FF0000' })

    const planner = agents.value.find(a => a.name === 'Planner')
    expect(planner!.color).toBe('#FF0000')
    // Other fields should be preserved
    expect(planner!.model).toBe('gpt-4')
  })
})

describe('useAgents — removeAgent', () => {
  it('removes agent from local list by name', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, removeAgent } = useAgents()
    await fetchAgents()

    removeAgent('Coder')
    expect(agents.value.find(a => a.name === 'Coder')).toBeUndefined()
  })

  it('does not remove other agents', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, removeAgent } = useAgents()
    await fetchAgents()

    removeAgent('Coder')
    expect(agents.value.find(a => a.name === 'Planner')).toBeDefined()
  })

  it('does nothing when name not found', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(sampleAgents))

    const useAgents = await freshUseAgents()
    const { agents, fetchAgents, removeAgent } = useAgents()
    await fetchAgents()

    removeAgent('Ghost')
    expect(agents.value).toHaveLength(2)
  })
})
