import type { Page } from '@playwright/test'

// Fixtures inlined to avoid resolveJsonModule requirement.

const workflowsFixture = [
  {
    id: 'wf-1',
    name: 'Daily Report',
    enabled: true,
    schedule: '0 9 * * 1-5',
    version: 1,
    steps: [
      {
        name: 'Gather Data',
        agent: 'Coder',
        prompt: 'Collect daily metrics',
        position: 0,
        on_failure: 'stop',
        model_override: 'claude-haiku-4',
        when: '',
        sub_workflow: '',
      },
      {
        name: 'Send Summary',
        agent: 'Coder',
        prompt: 'Summarize and report',
        position: 1,
        on_failure: 'continue',
        inputs: [{ from_step: 'Gather Data', as: 'data' }],
      },
    ],
    retry: { max_retries: 2, delay: '10s' },
    chain: { next: '', on_success: true, on_failure: false },
    notification: { on_success: true, on_failure: true, severity: 'info', deliver_to: [] },
  },
  {
    id: 'wf-2',
    name: 'Downstream',
    enabled: true,
    schedule: '',
    version: 1,
    steps: [],
    notification: { on_success: false, on_failure: false, severity: 'info', deliver_to: [] },
  },
]

const notificationsFixture = [
  {
    id: 'notif-1',
    workflow_id: 'wf-1',
    workflow_name: 'Daily Report',
    run_id: 'run-1',
    summary: 'Daily Report completed successfully',
    detail: '**Run ID:** run-1\n**Status:** complete\n\n**Steps:**\n✅ Gather Data\n✅ Send Summary',
    status: 'pending',
    proposed_actions: [],
    created_at: '2026-03-13T09:00:00Z',
  },
]

const spacesFixture = [
  {
    id: 'space-general', name: 'General', kind: 'channel',
    lead_agent: 'Coder', member_agents: ['Coder', 'GitAgent'],
    icon: 'G', color: '#58a6ff', unseen_count: 0,
    created_at: '2026-03-01T00:00:00Z', updated_at: '2026-03-01T00:00:00Z',
  },
  {
    id: 'space-eng', name: 'Engineering', kind: 'channel',
    lead_agent: 'GitAgent', member_agents: ['GitAgent'],
    icon: 'E', color: '#3fb950', unseen_count: 3,
    created_at: '2026-03-01T00:00:00Z', updated_at: '2026-03-01T00:00:00Z',
  },
  {
    id: 'dm-alice', name: 'Alice', kind: 'dm',
    lead_agent: 'Coder', member_agents: ['Coder'],
    icon: 'A', color: '#f78166', unseen_count: 1,
    created_at: '2026-03-01T00:00:00Z', updated_at: '2026-03-01T00:00:00Z',
  },
]
// Field names match the Go API response and what AgentsView / ChatView expect:
//   - `model` (not model_id) — matched by AgentForm.model and Agent.model
//   - no `slot` field — not part of any frontend interface
const agentsFixture = [
  {
    name: 'Coder',
    model: 'claude-sonnet-4-6',
    icon: 'C',
    color: '#58a6ff',
    is_default: true,
    system_prompt: '',
    memory_enabled: false,
    vault_name: '',
    toolbelt: [],
  },
  {
    name: 'GitAgent',
    model: 'claude-sonnet-4-6',
    icon: 'G',
    color: '#3fb950',
    is_default: false,
    system_prompt: '',
    memory_enabled: false,
    vault_name: '',
    toolbelt: [
      { connection_id: 'conn-gh-1', provider: 'github_cli', profile: '', approval_gate: false },
    ],
  },
]

// Matches the Connection interface: id, provider, account_label, account_id, scopes, created_at, expires_at
const connectionsFixture = [
  {
    id: 'conn-gh-1',
    provider: 'github',
    account_label: 'test-user',
    account_id: '12345',
    scopes: [],
    created_at: '2026-03-10T00:00:00Z',
    expires_at: '',
  },
]

// Minimal catalog fixture — enough to drive CategoryNav categories and card grid.
// Mirrors the shape of catalog.Entry from the Go server.
const catalogFixture = [
  {
    id: 'slack', name: 'Slack', description: 'Slack messaging', category: 'communication',
    icon: 'SL', icon_color: '#4a154b', type: 'oauth', default_label: 'Slack',
    multi_account: false, fields: [], validation: { available: false },
  },
  {
    id: 'discord', name: 'Discord', description: 'Discord bot', category: 'communication',
    icon: 'DC', icon_color: '#5865f2', type: 'credentials', default_label: 'Discord',
    multi_account: false,
    fields: [{ key: 'bot_token', label: 'Bot Token', type: 'password', required: true, stored_in: 'creds', placeholder: 'Bot token' }],
    validation: { available: true, description: 'Verifies bot token.' },
  },
  {
    id: 'github', name: 'GitHub', description: 'GitHub OAuth', category: 'dev_tools',
    icon: 'GH', icon_color: '#24292f', type: 'oauth', default_label: 'GitHub',
    multi_account: false, fields: [], validation: { available: false },
  },
  {
    id: 'datadog', name: 'Datadog', description: 'Datadog monitoring', category: 'observability',
    icon: 'DD', icon_color: '#632ca6', type: 'credentials', default_label: 'Datadog',
    multi_account: false,
    fields: [{ key: 'api_key', label: 'API Key', type: 'password', required: true, stored_in: 'creds', placeholder: 'API key' }],
    validation: { available: true, description: 'Calls /api/v1/validate.' },
  },
  {
    id: 'aws', name: 'AWS', description: 'Amazon Web Services', category: 'cloud',
    icon: 'AW', icon_color: '#ff9900', type: 'system', default_label: 'AWS',
    multi_account: false, fields: [], validation: { available: false },
  },
  {
    id: 'postgres', name: 'PostgreSQL', description: 'PostgreSQL database', category: 'databases',
    icon: 'PG', icon_color: '#336791', type: 'credentials', default_label: 'PostgreSQL',
    multi_account: false,
    fields: [{ key: 'url', label: 'Connection URL', type: 'url', required: true, stored_in: 'creds', placeholder: 'postgres://...' }],
    validation: { available: true, description: 'Connects to database.' },
  },
  {
    id: 'notion', name: 'Notion', description: 'Notion workspace', category: 'productivity',
    icon: 'NO', icon_color: '#000000', type: 'credentials', default_label: 'Notion',
    multi_account: false,
    fields: [{ key: 'api_key', label: 'API Key', type: 'password', required: true, stored_in: 'creds', placeholder: 'secret_...' }],
    validation: { available: true, description: 'Calls /v1/users/me.' },
  },
  {
    id: 'github_cli', name: 'GitHub CLI', description: 'GitHub CLI system tool', category: 'system',
    icon: 'GC', icon_color: '#24292f', type: 'system', default_label: 'GitHub CLI',
    multi_account: false, fields: [], validation: { available: false },
  },
]

// Matches SystemToolStatus interface: name, installed, authed, identity, profiles, error
const systemToolsFixture = [
  { name: 'github_cli', installed: true, authed: true, identity: 'test-user', profiles: [], error: '' },
  { name: 'bash', installed: true, authed: true, identity: '', profiles: [], error: '' },
]

/**
 * Overrides a route to return a specific HTTP error status.
 * Must be called AFTER setupApiMocks() — LIFO ordering ensures this handler wins.
 *
 * @param page      Playwright Page
 * @param urlGlob   URL glob pattern matching the API endpoint(s) to intercept
 * @param status    HTTP status code, e.g. 500
 * @param body      Optional error response body (defaults to { error: 'simulated error' })
 * @param options   Optional { method } to restrict to a specific HTTP verb
 */
export async function setupRouteError(
  page: Page,
  urlGlob: string,
  status: number,
  body: Record<string, unknown> = { error: 'simulated error' },
  options?: { method?: string },
) {
  await page.route(urlGlob, route => {
    if (options?.method && route.request().method() !== options.method) {
      return route.continue()
    }
    return route.fulfill({ status, json: body })
  })
}

/**
 * Returns a Playwright route handler that responds with an error for a specific
 * HTTP method and falls through (continue) for all other methods.
 *
 * Useful when registering handlers inline without the async helper overhead.
 *
 * @param method  HTTP verb to intercept, e.g. 'POST'
 * @param status  HTTP error status, e.g. 422
 * @param body    Optional error body
 */
export function createMethodErrorHandler(
  method: string,
  status: number,
  body: Record<string, unknown> = { error: 'simulated error' },
) {
  return (route: Parameters<Parameters<Page['route']>[1]>[0]) => {
    if (route.request().method() === method) {
      return route.fulfill({ status, json: body })
    }
    return route.continue()
  }
}

/**
 * Returns a stateful route handler that succeeds on the first N calls then
 * returns an error thereafter (or vice-versa using the `failFirst` flag).
 *
 * Useful for testing retry / recovery flows.
 *
 * @param successBody   JSON body to return on success
 * @param errorStatus   HTTP status for the error response
 * @param errorBody     JSON body for the error response
 * @param failFirst     When true, the first call fails and subsequent calls succeed
 * @param options       Optional { method } to restrict to a specific HTTP verb
 */
export function createStatefulHandler(
  successBody: Record<string, unknown>,
  errorStatus: number,
  errorBody: Record<string, unknown> = { error: 'simulated error' },
  failFirst = true,
  options?: { method?: string },
) {
  let called = false
  return (route: Parameters<Parameters<Page['route']>[1]>[0]) => {
    if (options?.method && route.request().method() !== options.method) {
      return route.continue()
    }
    const firstCall = !called
    called = true
    const shouldError = failFirst ? firstCall : !firstCall
    if (shouldError) {
      return route.fulfill({ status: errorStatus, json: errorBody })
    }
    return route.fulfill({ json: successBody })
  }
}

/**
 * Sets up all standard API mocks needed for the app to initialize.
 * Call at the start of every test before page.goto().
 *
 * IMPORTANT: Playwright matches routes in LIFO order (last registered = highest priority).
 * Register the catch-all FIRST, then specific routes so the specific handlers win.
 */
export async function setupApiMocks(page: Page) {
  // 0. Catch-all registered FIRST so it has lowest priority (LIFO: last-in wins).
  // Specific routes below are registered later and therefore take precedence.
  await page.route('**/api/v1/**', route => {
    console.warn('[mock-api] unmocked:', route.request().url())
    return route.fulfill({ status: 200, json: {} })
  })

  // 1. Auth token — always the first call the app makes
  await page.route('**/api/v1/token', route =>
    route.fulfill({ json: { token: 'test-e2e-token' } })
  )

  // 2. Stats
  await page.route('**/api/v1/stats', route =>
    route.fulfill({ json: {} })
  )

  // 3. Cost
  await page.route('**/api/v1/cost', route =>
    route.fulfill({ json: { session_total_usd: 0 } })
  )

  // 4. Health
  await page.route('**/api/v1/health', route =>
    route.fulfill({ json: { status: 'ok', version: 'test' } })
  )

  // 5. Cloud status
  await page.route('**/api/v1/cloud/status', route =>
    route.fulfill({ json: { registered: false, connected: false } })
  )

  // 6. Runtime status
  await page.route('**/api/v1/runtime/status', route =>
    route.fulfill({ json: { state: 'idle', session_id: '', machine_id: 'test-machine-id' } })
  )

  // 7. Config
  await page.route('**/api/v1/config', route =>
    route.fulfill({ json: { theme: 'dark' } })
  )

  // 8. Sessions list and create
  await page.route('**/api/v1/sessions', route => {
    const method = route.request().method()
    if (method === 'GET') {
      return route.fulfill({ json: [] })
    }
    if (method === 'POST') {
      return route.fulfill({ status: 201, json: { session_id: 'test-session-1' } })
    }
    return route.continue()
  })

  // 9. Muninn endpoints
  await page.route('**/api/v1/muninn/status', route =>
    route.fulfill({ json: { connected: false } })
  )
  await page.route('**/api/v1/muninn/vaults', route =>
    route.fulfill({ json: { vaults: [] } })
  )

  // 10. Models available (more specific — registered before models list)
  await page.route('**/api/v1/models/available', route =>
    route.fulfill({ json: { models: [] } })
  )

  // 11. Models list
  await page.route('**/api/v1/models', route =>
    route.fulfill({ json: {} })
  )

  // 12. System tools
  await page.route('**/api/v1/system/tools', route =>
    route.fulfill({ json: systemToolsFixture })
  )

  // 13. Connections list
  await page.route('**/api/v1/connections', route =>
    route.fulfill({ json: connectionsFixture })
  )

  // 13a. Connections catalog — registered AFTER /connections so it wins (LIFO)
  await page.route('**/api/v1/connections/catalog', route =>
    route.fulfill({ json: catalogFixture })
  )

  // 14. Individual agent by name (GET / PUT) — registered before agents list
  await page.route('**/api/v1/agents/**', route => {
    const method = route.request().method()
    if (method === 'GET') {
      const url = route.request().url()
      const agentName = url.split('/agents/')[1]?.split('?')[0]
      const ag = agentsFixture.find(a => a.name === agentName) ?? agentsFixture[0]
      return route.fulfill({ json: ag })
    }
    if (method === 'PUT' || method === 'PATCH') {
      return route.fulfill({ json: agentsFixture[0] })
    }
    if (method === 'DELETE') {
      return route.fulfill({ json: { deleted: true } })
    }
    return route.continue()
  })

  // 15. Active agent — registered after agents/** so it takes higher priority (LIFO)
  await page.route('**/api/v1/agents/active', route => {
    const method = route.request().method()
    if (method === 'GET') {
      return route.fulfill({ json: { name: 'Coder' } })
    }
    if (method === 'PUT') {
      return route.fulfill({ json: { active_agent: 'Coder' } })
    }
    return route.continue()
  })

  // 16. Agents list (GET) and create (POST) — registered last = highest priority for /agents
  await page.route('**/api/v1/agents', route => {
    const method = route.request().method()
    if (method === 'GET') {
      return route.fulfill({ json: agentsFixture })
    }
    if (method === 'POST') {
      return route.fulfill({ status: 201, json: agentsFixture[0] })
    }
    return route.continue()
  })

  // 17. Workflow runs (more specific — registered before workflows/**)
  await page.route('**/api/v1/workflows/**/runs', route =>
    route.fulfill({ json: [] })
  )

  // 18. Individual workflow (GET/PUT/DELETE/POST run) — registered before workflows list
  await page.route('**/api/v1/workflows/**', route => {
    const method = route.request().method()
    if (method === 'GET') return route.fulfill({ json: workflowsFixture[0] })
    if (method === 'PUT') return route.fulfill({ json: workflowsFixture[0] })
    if (method === 'DELETE') return route.fulfill({ status: 204, body: '' })
    if (method === 'POST') return route.fulfill({ json: {} }) // for /run
    return route.continue()
  })

  // 19. Workflows list — registered last = highest priority for /workflows
  await page.route('**/api/v1/workflows', route => {
    const method = route.request().method()
    if (method === 'GET') return route.fulfill({ json: workflowsFixture })
    if (method === 'POST') return route.fulfill({ json: { ...workflowsFixture[0], id: 'new-wf-1', name: 'New Workflow' } })
    return route.continue()
  })

  // 20. Notifications — individual before list
  await page.route('**/api/v1/notifications/**', route => route.fulfill({ json: {} }))
  await page.route('**/api/v1/notifications', route =>
    route.fulfill({ json: notificationsFixture })
  )

  // 21aa. Space messages timeline — registered before spaces/** wildcard
  await page.route('**/api/v1/space-messages/**', route =>
    route.fulfill({ json: { messages: [], next_cursor: '' } })
  )

  // 22. Inbox summary (new endpoint for unread badge)
  await page.route('**/api/v1/inbox/summary', route =>
    route.fulfill({ json: { unread: 0, items: [] } })
  )

  // 23. Skills list
  await page.route('**/api/v1/skills', route =>
    route.fulfill({ json: [] })
  )

  // 24. Session messages (GET /api/v1/sessions/*/messages*)
  await page.route('**/api/v1/sessions/*/messages*', route =>
    route.fulfill({ json: [] })
  )

  // 25. Container threads (GET /api/v1/containers/*/threads)
  await page.route('**/api/v1/containers/*/threads', route =>
    route.fulfill({ json: [] })
  )

  // 21a. Space sessions (more specific — registered before spaces/**)
  await page.route('**/api/v1/spaces/*/sessions', route =>
    route.fulfill({ json: [] })
  )

  // 21b. Space mark-read (more specific — registered before spaces/**)
  await page.route('**/api/v1/spaces/*/mark-read', route =>
    route.fulfill({ json: {} })
  )

  // 21c. Individual space (GET/PATCH/DELETE) — registered before spaces list
  await page.route('**/api/v1/spaces/**', route => {
    const method = route.request().method()
    if (method === 'GET') {
      const url = route.request().url()
      const spaceId = url.split('/spaces/')[1]?.split('?')[0]
      const sp = spacesFixture.find(s => s.id === spaceId) ?? spacesFixture[0]
      return route.fulfill({ json: sp })
    }
    if (method === 'PATCH' || method === 'PUT') {
      return route.fulfill({ json: spacesFixture[0] })
    }
    if (method === 'DELETE') {
      return route.fulfill({ status: 204, body: '' })
    }
    return route.continue()
  })

  // 21d. Spaces list (GET) and create (POST) — registered last = highest priority
  // GET returns the real paginated shape: { Spaces: [...], NextCursor: "" }
  await page.route('**/api/v1/spaces', route => {
    const method = route.request().method()
    if (method === 'GET') {
      return route.fulfill({ json: { Spaces: spacesFixture, NextCursor: '' } })
    }
    if (method === 'POST') {
      return route.fulfill({
        status: 201,
        json: {
          id: 'space-new', name: 'New Channel', kind: 'channel',
          lead_agent: 'Coder', member_agents: [],
          icon: 'N', color: '#58a6ff', unseen_count: 0,
          created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
        },
      })
    }
    return route.continue()
  })
}
