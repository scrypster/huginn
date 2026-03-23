/**
 * useApi tests
 *
 * Mocks globalThis.fetch to avoid real network calls and jsdom URL restrictions.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setToken, getToken, fetchToken, api } from '../useApi'

function ok(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('useApi — token management', () => {
  it('setToken/getToken roundtrip', () => {
    setToken('abc123')
    expect(getToken()).toBe('abc123')
  })

  it('fetchToken fetches from /api/v1/token and returns the token string', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      ok({ token: 'server-token-xyz' })
    )

    const tok = await fetchToken()
    expect(tok).toBe('server-token-xyz')
    expect(fetchSpy).toHaveBeenCalledWith('/api/v1/token')
    fetchSpy.mockRestore()
  })

  it('fetchToken throws when server returns non-ok status', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response('', { status: 500 })
    )

    await expect(fetchToken()).rejects.toThrow('Failed to fetch token: 500')
    fetchSpy.mockRestore()
  })
})

describe('useApi — apiFetch empty-token auto-init', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    setToken('')
  })

  it('auto-fetches token from /api/v1/token when token is empty', async () => {
    setToken('')
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      // First call: /api/v1/token (auto-init)
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'auto-token' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      // Second call: actual API request succeeds
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok', version: '1.0' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    await api.health()

    expect(fetchSpy).toHaveBeenCalledTimes(2)
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/token')
    expect(getToken()).toBe('auto-token')
    const [, opts] = fetchSpy.mock.calls[1]
    expect((opts?.headers as Record<string, string>)['Authorization']).toBe('Bearer auto-token')
  })

  it('proceeds with empty Bearer when auto-fetch fails (still recovers via 401 retry)', async () => {
    setToken('')
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      // Auto-init fetch throws
      .mockRejectedValueOnce(new Error('network error'))
      // Actual request 401s
      .mockResolvedValueOnce(new Response('', { status: 401 }))
      // 401 retry: fetchToken succeeds
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'recovered-token' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      // 401 retry: actual request succeeds
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok', version: '1.0' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    const result = await api.health()
    expect(result).toMatchObject({ status: 'ok' })
    expect(getToken()).toBe('recovered-token')
    expect(fetchSpy).toHaveBeenCalledTimes(4)
  })

  it('does NOT re-fetch token when token is already set', async () => {
    setToken('already-set')
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok', version: '1.0' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    await api.health()

    // Only 1 fetch — no /api/v1/token call
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/health')
  })
})

describe('useApi — apiFetch (via api.*)', () => {
  beforeEach(() => {
    setToken('test-token')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends Authorization Bearer header on every request', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ status: 'ok', version: '1.0' })
    )

    await api.health()

    const [, opts] = fetchSpy.mock.calls[0]
    const headers = opts?.headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer test-token')
  })

  it('throws on non-ok, non-401 responses', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('not found', { status: 404 })
    )

    await expect(api.health()).rejects.toThrow('404')
  })

  it('retries with fresh token on 401 and succeeds', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      // First call: 401 on the original request
      .mockResolvedValueOnce(new Response('', { status: 401 }))
      // Second call: fetchToken — returns fresh token
      .mockResolvedValueOnce(ok({ token: 'fresh-token' }))
      // Third call: retry with fresh token succeeds
      .mockResolvedValueOnce(ok({ status: 'ok', version: '1.1' }))

    const result = await api.health()

    expect(result).toEqual({ status: 'ok', version: '1.1' })
    expect(getToken()).toBe('fresh-token')
    // Three fetches: original → /api/v1/token → retry
    expect(fetchSpy).toHaveBeenCalledTimes(3)
  })

  it('throws when 401 and the retry also returns an error status', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response('', { status: 401 }))
      // fetchToken succeeds
      .mockResolvedValueOnce(ok({ token: 'fresh' }))
      // Retry returns 403 (not ok, not thrown by retry branch — falls through to outer throw)
      .mockResolvedValueOnce(new Response('forbidden', { status: 403 }))

    // The 401 retry branch only returns if retry.ok. If not, it falls through
    // to the outer `if (!res.ok)` which throws on the original 401 response.
    await expect(api.health()).rejects.toThrow('401')
  })

  it('api.sessions.list calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))

    await api.sessions.list()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/sessions')
  })

  it('api.sessions.create sends POST', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ session_id: 's1' })
    )

    await api.sessions.create()
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
  })

  it('api.sessions.rename sends PATCH with body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))

    await api.sessions.rename('sess-1', 'My Session')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/sessions/sess-1')
    expect(opts?.method).toBe('PATCH')
    expect(JSON.parse(opts?.body as string)).toEqual({ title: 'My Session' })
  })

  it('api.cloud.status calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ registered: true, connected: false })
    )

    const result = await api.cloud.status()
    expect(result.registered).toBe(true)
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/cloud/status')
  })

  it('api.config.update sends PUT with body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ saved: true, requires_restart: false })
    )

    const cfg = { theme: 'dark' }
    await api.config.update(cfg)
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('PUT')
    expect(JSON.parse(opts?.body as string)).toEqual(cfg)
  })

  it('api.agents.setActive sends PUT with name body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ active_agent: 'coder' })
    )

    await api.agents.setActive('coder')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/agents/active')
    expect(opts?.method).toBe('PUT')
    expect(JSON.parse(opts?.body as string)).toEqual({ name: 'coder' })
  })

  // 401 retry where fetchToken itself throws
  it('throws original 401 error when fetchToken throws during retry', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response('', { status: 401 }))
      .mockRejectedValueOnce(new Error('DNS failure'))
    await expect(api.health()).rejects.toThrow('401')
  })

  // sessions.create with spaceId
  it('api.sessions.create with spaceId includes space_id in body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ session_id: 's2' }))
    await api.sessions.create('space-abc')
    const [, opts] = fetchSpy.mock.calls[0]
    expect(JSON.parse(opts?.body as string)).toEqual({ space_id: 'space-abc' })
  })

  // agents.update
  it('api.agents.update sends PUT to /api/v1/agents/:name with data', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.agents.update('Coder', { model: 'claude-3' })
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/agents/Coder')
    expect(opts?.method).toBe('PUT')
    expect(JSON.parse(opts?.body as string)).toEqual({ model: 'claude-3' })
  })

  // connections
  it('api.connections.start sends POST with provider', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ auth_url: 'https://x.com' }))
    await api.connections.start('github')
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual({ provider: 'github' })
  })

  it('api.connections.delete sends DELETE to /api/v1/connections/:id', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ deleted: true }))
    await api.connections.delete('conn-1')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/connections/conn-1')
    expect(opts?.method).toBe('DELETE')
  })

  // spaces
  it('api.spaces.getDM URL-encodes agent name', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.spaces.getDM('My Agent')
    expect(fetchSpy.mock.calls[0][0]).toContain(encodeURIComponent('My Agent'))
  })

  it('api.spaces.createChannel sends POST with channel options', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    const chanOpts = { name: 'General', lead_agent: 'Coder', member_agents: ['GitAgent'] }
    await api.spaces.createChannel(chanOpts)
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toMatchObject(chanOpts)
  })

  it('api.spaces.markRead sends POST to /api/v1/spaces/:id/mark-read', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.spaces.markRead('space-1')
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/spaces/space-1/mark-read')
    expect(opts?.method).toBe('POST')
  })

  // muninn
  it('api.muninn.connect sends POST with credentials payload', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ ok: true }))
    const payload = { endpoint: 'http://x', username: 'u', password: 'p' }
    await api.muninn.connect(payload)
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual(payload)
  })

  it('api.muninn.createVault sends POST to /api/v1/muninn/vaults', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ vault_name: 'v1', token: 'tok' }))
    await api.muninn.createVault({ vault_name: 'v1', agent_label: 'Agent' })
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/muninn/vaults')
    expect(opts?.method).toBe('POST')
  })

  // credentials (representative pair)
  it('api.credentials.datadogTest POSTs to /api/v1/credentials/datadog/test', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ ok: true }))
    await api.credentials.datadogTest({ url: 'u', api_key: 'k', app_key: 'a' })
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/credentials/datadog/test')
  })

  it('api.credentials.datadogSave POSTs to /api/v1/credentials/datadog', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ id: 'c1', provider: 'datadog', account_label: 'prod' }))
    await api.credentials.datadogSave({ url: 'u', api_key: 'k', app_key: 'a' })
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/credentials/datadog')
  })

  // builtin
  it('api.builtin.activate sends POST with model name', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ activated: true, model: 'llama-3', requires_restart: false }))
    await api.builtin.activate('llama-3')
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual({ model: 'llama-3' })
  })

  // cloud connect/disconnect
  it('api.cloud.connect sends POST', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ status: 'connected' }))
    await api.cloud.connect()
    expect((fetchSpy.mock.calls[0][1] as RequestInit)?.method).toBe('POST')
  })

  it('api.cloud.disconnect sends DELETE', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ status: 'disconnected' }))
    await api.cloud.disconnect()
    expect((fetchSpy.mock.calls[0][1] as RequestInit)?.method).toBe('DELETE')
  })

  // models.pull
  it('api.models.pull sends POST with model name', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ status: 'pulling' }))
    await api.models.pull('llama-3')
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual({ name: 'llama-3' })
  })

  // system.githubSwitch
  it('api.system.githubSwitch sends POST with user', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ active: 'user2' }))
    await api.system.githubSwitch('user2')
    const [, opts] = fetchSpy.mock.calls[0]
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual({ user: 'user2' })
  })

  // ── additional singleton endpoints ──────────────────────────────────────────

  it('api.sessions.get calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.sessions.get('sess-x')
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/sessions/sess-x')
  })

  it('api.agents.list calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.agents.list()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/agents')
  })

  it('api.agents.get calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.agents.get('Coder')
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/agents/Coder')
  })

  it('api.agents.getActive calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ name: 'Coder' }))
    await api.agents.getActive()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/agents/active')
  })

  it('api.threads.list calls correct endpoint with sessionId', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.threads.list('sess-1')
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/sessions/sess-1/threads')
  })

  it('api.models.list calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.models.list()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/models')
  })

  it('api.models.available calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ models: [] }))
    await api.models.available()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/models/available')
  })

  it('api.config.get calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.config.get()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/config')
  })

  it('api.runtime.status calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ state: 'idle', session_id: '', machine_id: '' }))
    await api.runtime.status()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/runtime/status')
  })

  it('api.stats calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.stats()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/stats')
  })

  it('api.cost calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ session_total_usd: 0 }))
    await api.cost()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/cost')
  })

  it('api.logs uses default n=100', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ lines: [] }))
    await api.logs()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/logs?n=100')
  })

  it('api.logs passes explicit n', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ lines: [] }))
    await api.logs(50)
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/logs?n=50')
  })

  it('api.connections.list calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.connections.list()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/connections')
  })

  it('api.connections.providers calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.connections.providers()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/providers')
  })

  it('api.integrations.cliStatus calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.integrations.cliStatus()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/integrations/cli-status')
  })

  it('api.system.tools calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.system.tools()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/system/tools')
  })

  it('api.spaces.list calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.spaces.list()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/spaces')
  })

  it('api.spaces.get calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.spaces.get('sp-1')
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/spaces/sp-1')
  })

  it('api.spaces.updateSpace sends PATCH with body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({}))
    await api.spaces.updateSpace('sp-1', { name: 'General' })
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/spaces/sp-1')
    expect(opts?.method).toBe('PATCH')
    expect(JSON.parse(opts?.body as string)).toEqual({ name: 'General' })
  })

  it('api.spaces.sessions calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.spaces.sessions('sp-1')
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/space-sessions/sp-1')
  })

  it('api.muninn.status calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ connected: false }))
    await api.muninn.status()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/muninn/status')
  })

  it('api.muninn.test sends POST with payload', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ ok: true }))
    const payload = { endpoint: 'http://x', username: 'u', password: 'p' }
    await api.muninn.test(payload)
    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/muninn/test')
    expect(opts?.method).toBe('POST')
  })

  it('api.muninn.vaults calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ vaults: [] }))
    await api.muninn.vaults()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/muninn/vaults')
  })

  // ── credential providers (table-driven) ─────────────────────────────────────

  const credentialProviders: Array<{ key: string; slug: string }> = [
    { key: 'slackBot',    slug: 'slack_bot' },
    { key: 'jiraSA',      slug: 'jira_sa' },
    { key: 'linear',      slug: 'linear' },
    { key: 'gitlab',      slug: 'gitlab' },
    { key: 'discord',     slug: 'discord' },
    { key: 'vercel',      slug: 'vercel' },
    { key: 'stripe',      slug: 'stripe' },
    { key: 'pagerduty',   slug: 'pagerduty' },
    { key: 'newrelic',    slug: 'newrelic' },
    { key: 'elastic',     slug: 'elastic' },
    { key: 'grafana',     slug: 'grafana' },
    { key: 'crowdstrike', slug: 'crowdstrike' },
    { key: 'terraform',   slug: 'terraform' },
    { key: 'servicenow',  slug: 'servicenow' },
    { key: 'notion',      slug: 'notion' },
    { key: 'airtable',    slug: 'airtable' },
    { key: 'hubspot',     slug: 'hubspot' },
    { key: 'zendesk',     slug: 'zendesk' },
    { key: 'asana',       slug: 'asana' },
    { key: 'monday',      slug: 'monday' },
  ]

  credentialProviders.forEach(({ key, slug }) => {
    it(`api.credentials.${key}Test POSTs to /api/v1/credentials/${slug}/test`, async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ ok: true }))
      await (api.credentials as any)[`${key}Test`]({ key: 'val' })
      expect(spy.mock.calls[0]![0]).toBe(`/api/v1/credentials/${slug}/test`)
      expect((spy.mock.calls[0]![1] as RequestInit).method).toBe('POST')
    })

    it(`api.credentials.${key}Save POSTs to /api/v1/credentials/${slug}`, async () => {
      const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(ok({ id: 'c1', provider: slug, account_label: 'test' }))
      await (api.credentials as any)[`${key}Save`]({ key: 'val' })
      expect(spy.mock.calls[0]![0]).toBe(`/api/v1/credentials/${slug}`)
      expect((spy.mock.calls[0]![1] as RequestInit).method).toBe('POST')
    })
  })

  // ── builtin non-streaming ──────────────────────────────────────────────────

  it('api.builtin.status calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok({ installed: true, version: '1.0', binary_path: '', active_model: '', backend_type: '' }))
    await api.builtin.status()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/builtin/status')
  })

  it('api.builtin.catalog calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.builtin.catalog()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/builtin/catalog')
  })

  it('api.builtin.installedModels calls correct endpoint', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok([]))
    await api.builtin.installedModels()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/v1/builtin/models')
  })

  // ── streaming error paths ──────────────────────────────────────────────────

  it('downloadRuntimeStream calls onError when response is not ok', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 500 }))
    const onError = vi.fn()
    api.builtin.downloadRuntimeStream(() => {}, () => {}, onError)
    await vi.waitFor(() => expect(onError).toHaveBeenCalledWith('HTTP 500'))
  })

  it('pullModelStream calls onError when response is not ok', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 503 }))
    const onError = vi.fn()
    api.builtin.pullModelStream('llama-3', () => {}, () => {}, onError)
    await vi.waitFor(() => expect(onError).toHaveBeenCalledWith('HTTP 503'))
  })
})
