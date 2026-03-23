import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// ── helpers ──────────────────────────────────────────────────────────────────

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const sampleWorkstream = {
  id: 'ws-1',
  name: 'auth-rebuild',
  description: 'Rebuilding auth system',
  created_at: '2024-01-01T00:00:00Z',
}

// Reset module state between tests — useWorkstreams uses module-level refs
beforeEach(() => {
  vi.resetModules()
})

afterEach(() => {
  vi.restoreAllMocks()
})

async function createComposable() {
  // Re-import fresh to get a clean module state
  const { useWorkstreams } = await import('../useWorkstreams')
  return useWorkstreams()
}

// ── list ──────────────────────────────────────────────────────────────────────

describe('list', () => {
  it('populates workstreams on success (bare array)', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleWorkstream]))
    const { list, workstreams } = await createComposable()
    await list()
    expect(workstreams.value).toHaveLength(1)
    expect(workstreams.value[0].name).toBe('auth-rebuild')
  })

  it('populates workstreams on success (wrapped object)', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson({ workstreams: [sampleWorkstream] }),
    )
    const { list, workstreams } = await createComposable()
    await list()
    expect(workstreams.value).toHaveLength(1)
  })

  it('sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
    const { list, error } = await createComposable()
    await list()
    expect(error.value).toBeTruthy()
  })

  it('sets loading true during fetch and false after', async () => {
    let resolve!: (r: Response) => void
    vi.spyOn(globalThis, 'fetch').mockReturnValueOnce(new Promise<Response>(r => { resolve = r }))
    const { list, loading } = await createComposable()
    const p = list()
    expect(loading.value).toBe(true)
    resolve(okJson([]))
    await p
    expect(loading.value).toBe(false)
  })

  it('returns empty array when response has unexpected shape', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ data: 'unexpected' }))
    const { list, workstreams } = await createComposable()
    await list()
    expect(workstreams.value).toHaveLength(0)
  })
})

// ── create ────────────────────────────────────────────────────────────────────

describe('create', () => {
  it('adds workstream to list on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleWorkstream))
    const { create, workstreams } = await createComposable()
    const result = await create('auth-rebuild', 'desc')
    expect(result).not.toBeNull()
    expect(result?.id).toBe('ws-1')
    expect(workstreams.value.some(w => w.id === 'ws-1')).toBe(true)
  })

  it('returns null and sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('server error'))
    const { create, error } = await createComposable()
    const result = await create('bad-name')
    expect(result).toBeNull()
    expect(error.value).toBeTruthy()
  })

  it('sends POST with correct body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson(sampleWorkstream))
    const { create } = await createComposable()
    await create('my-project', 'a description')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/workstreams')
    expect((opts as RequestInit).method).toBe('POST')
    const body = JSON.parse((opts as RequestInit).body as string)
    expect(body.name).toBe('my-project')
    expect(body.description).toBe('a description')
  })
})

// ── remove ────────────────────────────────────────────────────────────────────

describe('remove', () => {
  it('removes workstream from list on success', async () => {
    // First populate
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson([sampleWorkstream]))
    const { list, remove, workstreams } = await createComposable()
    await list()
    expect(workstreams.value).toHaveLength(1)

    // Then delete
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    const ok = await remove('ws-1')
    expect(ok).toBe(true)
    expect(workstreams.value).toHaveLength(0)
  })

  it('returns false and sets error on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('forbidden'))
    const { remove, error } = await createComposable()
    const ok = await remove('ws-1')
    expect(ok).toBe(false)
    expect(error.value).toBeTruthy()
  })

  it('sends DELETE to correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({}))
    const { remove } = await createComposable()
    await remove('ws-42')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/workstreams/ws-42')
    expect((opts as RequestInit).method).toBe('DELETE')
  })
})

// ── tagSession ────────────────────────────────────────────────────────────────

describe('tagSession', () => {
  it('sends POST to workstreams/{id}/sessions', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    const { tagSession } = await createComposable()
    const ok = await tagSession('ws-1', 'sess-abc')
    expect(ok).toBe(true)
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/workstreams/ws-1/sessions')
    expect((opts as RequestInit).method).toBe('POST')
    const body = JSON.parse((opts as RequestInit).body as string)
    expect(body.session_id).toBe('sess-abc')
  })

  it('returns false on failure', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('oops'))
    const { tagSession } = await createComposable()
    const ok = await tagSession('ws-1', 'sess-abc')
    expect(ok).toBe(false)
  })
})

// ── parseProjectCreateCommand ─────────────────────────────────────────────────

describe('parseProjectCreateCommand', () => {
  it('parses /project create "name" with quotes', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('/project create "auth-rebuild"')).toBe('auth-rebuild')
  })

  it('parses /project create name without quotes', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('/project create my-project')).toBe('my-project')
  })

  it('parses /project create with extra spaces', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('  /project create "feature-x"  ')).toBe('feature-x')
  })

  it('returns null for non-matching input', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('hello world')).toBeNull()
    expect(parseProjectCreateCommand('/project list')).toBeNull()
    expect(parseProjectCreateCommand('')).toBeNull()
  })

  it('returns null for /project without subcommand', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('/project')).toBeNull()
  })

  it('parses multi-word project names in quotes', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('/project create "my cool project"')).toBe('my cool project')
  })

  it('parses multi-word project names without quotes', async () => {
    const { parseProjectCreateCommand } = await createComposable()
    expect(parseProjectCreateCommand('/project create my project')).toBe('my project')
  })
})
