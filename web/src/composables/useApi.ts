import { ref } from 'vue'

export interface FinishSummary {
  Summary: string
  FilesModified?: string[]
  KeyDecisions?: string[]
  Artifacts?: string[]
  Status: string
}

export interface Thread {
  ID: string
  SessionID: string
  AgentID: string
  Task: string
  Status: 'queued' | 'thinking' | 'tooling' | 'done' | 'completed' | 'completed-with-timeout' | 'blocked' | 'cancelled' | 'error' | 'resolving'
  StartedAt: string
  CompletedAt: string
  Summary?: FinishSummary
  TokensUsed: number
  TokenBudget: number
}

export interface Connection {
  id: string
  provider: string
  account_label: string
  account_id: string
  scopes: string[]
  created_at: string
  expires_at: string
  metadata?: Record<string, string>
}

export interface ToolbeltEntry {
  connection_id: string
  provider: string
  profile?: string
  approval_gate: boolean
}

export interface Agent {
  name: string
  model: string
  system_prompt: string
  color: string
  icon: string
  memory_type?: string
  memory_enabled?: boolean
  context_notes_enabled?: boolean
  vault_name?: string
  memory_mode?: string
  vault_description?: string
  toolbelt?: ToolbeltEntry[]
  local_tools?: string[]   // tool names; ["*"] = all builtins; undefined/[] = none
  skills?: unknown[]
  is_default?: boolean
  [key: string]: unknown
}

export interface Provider {
  name: string
  display_name: string
  icon: string
  description: string
  scopes: string[]
  multi_account: boolean
  configured: boolean
}

export interface BuiltinStatus {
  installed: boolean
  version: string
  binary_path: string
  active_model: string
  backend_type: string
}

export interface BuiltinCatalogEntry {
  name: string
  description: string
  provider: string
  provider_url: string
  host: string
  host_url: string
  filename: string
  size_bytes: number
  min_ram_gb: number
  recommended_ram_gb: number
  context_length: number
  tags: string[]
  source: string
  installed: boolean
}

export interface ProviderModel {
  id: string
  name: string
  description?: string
  context_length?: number
  pricing_prompt?: number      // USD per million tokens
  pricing_completion?: number  // USD per million tokens
  provider?: string            // sub-provider (OpenRouter only)
  created_at?: string
  tags?: string[]
}

export interface BuiltinInstalledModel {
  name: string
  filename: string
  path: string
  size_bytes: number
  installed_at: string
}

export interface SpaceMessage {
  id: string
  session_id: string
  seq: number
  ts: string
  role: 'user' | 'assistant'
  content: string
  agent: string
}

export interface SystemToolStatus {
  name: string
  installed: boolean
  authed: boolean
  identity: string
  profiles: string[]
  error?: string
}

export interface CLITool {
  name: string
  display_name: string
  icon: string
  icon_color: string
  description: string
  installed: boolean
  version?: string
  authenticated: boolean
  account?: string
  auth_hint?: string
  install_commands: {
    mac?: string
    linux?: string
    windows?: string
  }
}

const token = ref('')

export function setToken(t: string) {
  token.value = t
}

export function getToken(): string {
  return token.value
}


export async function fetchToken(): Promise<string> {
  // Always fetch fresh from server — token is stable on disk but localStorage
  // becomes stale after server restarts (especially with dynamic ports).
  const res = await fetch('/api/v1/token')
  if (!res.ok) throw new Error(`Failed to fetch token: ${res.status}`)
  const data = await res.json()
  return data.token
}

export async function apiFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  // Auto-fetch token on first use if App.vue hasn't initialized it yet.
  // Vue 3 fires child onMounted() before parent onMounted(), so views that
  // make API calls on mount can race ahead of initApp()/setToken().
  if (!token.value) {
    try {
      const tok = await fetchToken()
      setToken(tok)
    } catch { /* proceed; 401 retry below will recover */ }
  }

  const res = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token.value}`,
      ...(opts.headers as Record<string, string> || {}),
    },
  })
  if (res.status === 401) {
    // Token is stale — refetch and retry once
    try {
      const fresh = await fetchToken()
      setToken(fresh)
      const retry = await fetch(path, {
        ...opts,
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${fresh}`,
          ...(opts.headers as Record<string, string> || {}),
        },
      })
      if (retry.ok) return retry.json()
    } catch { /* fall through */ }
  }
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    let message = `${res.status}`
    try {
      const parsed = JSON.parse(body)
      if (typeof parsed?.error === 'string' && parsed.error) message = parsed.error
      else message = `${res.status} ${body}`
    } catch { message = `${res.status} ${body}` }
    throw new Error(message)
  }
  try {
    return await res.json()
  } catch {
    throw new Error(`Server returned non-JSON response (${res.status} ${res.url})`)
  }
}

export const api = {
  health: () => apiFetch<{ status: string; version: string; satellite_connected: boolean; backend_status: string }>('/api/v1/health'),

  sessions: {
    list: () => apiFetch<Array<Record<string, unknown>>>('/api/v1/sessions'),
    create: (spaceId?: string) => apiFetch<{ session_id: string }>('/api/v1/sessions', {
      method: 'POST',
      body: spaceId ? JSON.stringify({ space_id: spaceId }) : undefined,
    }),
    get: (id: string) => apiFetch<Record<string, unknown>>(`/api/v1/sessions/${id}`),
    rename: (id: string, title: string) =>
      apiFetch(`/api/v1/sessions/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ title }),
      }),
    getMessages: (id: string, options?: { limit?: number; signal?: AbortSignal }) =>
      apiFetch<unknown[]>(`/api/v1/sessions/${id}/messages?limit=${options?.limit ?? 50}`, {
        signal: options?.signal,
      }),
    search: (q: string, signal?: AbortSignal) =>
      apiFetch<Array<Record<string, unknown>>>(`/api/v1/sessions/search?q=${encodeURIComponent(q)}`, { signal }),
  },

  agents: {
    list: () => apiFetch<Agent[]>('/api/v1/agents'),
    get: (name: string) => apiFetch<Agent>(`/api/v1/agents/${name}`),
    update: (name: string, data: unknown) =>
      apiFetch(`/api/v1/agents/${name}`, { method: 'PUT', body: JSON.stringify(data) }),
    testVault: (agentName: string, vaultName?: string) =>
      apiFetch<{ status: string; vault: string; tools_count?: number; warning?: string }>(`/api/v1/agents/${encodeURIComponent(agentName)}/vault/test`, {
        method: 'POST',
        body: JSON.stringify({ vault_name: vaultName ?? '' }),
      }),
  },

  threads: {
    list: (sessionId: string) =>
      apiFetch<Thread[]>(`/api/v1/sessions/${sessionId}/threads`),
  },

  models: {
    list: () => apiFetch<Record<string, string>>('/api/v1/models'),
    available: () => apiFetch<{ models?: unknown[]; error?: string }>('/api/v1/models/available'),
    pull: (name: string) =>
      apiFetch<{ status: string }>('/api/v1/models/pull', {
        method: 'POST',
        body: JSON.stringify({ name }),
      }),
    delete: (name: string) =>
      apiFetch<{ deleted: boolean }>(`/api/v1/models/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      }),
  },

  config: {
    get: () => apiFetch<Record<string, unknown>>('/api/v1/config'),
    update: (cfg: unknown) =>
      apiFetch<{ saved: boolean; requires_restart: boolean }>('/api/v1/config', {
        method: 'PUT',
        body: JSON.stringify(cfg),
      }),
  },

  runtime: {
    status: () => apiFetch<{ state: string; session_id: string; machine_id: string }>('/api/v1/runtime/status'),
  },

  stats: () => apiFetch<Record<string, number>>('/api/v1/stats'),

  statsHistory: (since?: number) => {
    const q = since != null ? `?since=${since}` : ''
    return apiFetch<{
      stats: Array<{ ts: number; key: string; kind: string; value: number }>
      cost: Array<{ ts: number; session_id: string; cost_usd: number; prompt_tokens: number; completion_tokens: number }>
    }>(`/api/v1/stats/history${q}`)
  },

  cost: () => apiFetch<{ session_total_usd: number }>('/api/v1/cost'),

  logs: (n = 100) => apiFetch<{ lines: string[] }>(`/api/v1/logs?n=${n}`),

  connections: {
    list: () => apiFetch<Connection[]>('/api/v1/connections'),
    providers: () => apiFetch<Provider[]>('/api/v1/providers'),
    // catalog returns the credential provider catalog as a generic array;
    // useCredentialCatalog.ts owns the typed CredentialCatalogEntry interface.
    catalog: () => apiFetch<Record<string, unknown>[]>('/api/v1/connections/catalog'),
    start: (provider: string) =>
      apiFetch<{ auth_url: string }>('/api/v1/connections/start', {
        method: 'POST',
        body: JSON.stringify({ provider }),
      }),
    delete: (id: string) =>
      apiFetch<{ deleted: boolean }>(`/api/v1/connections/${id}`, {
        method: 'DELETE',
      }),
    setDefault: (id: string) =>
      apiFetch<{ ok: boolean }>(`/api/v1/connections/${id}/default`, {
        method: 'PUT',
      }),
  },

  integrations: {
    cliStatus: () => apiFetch<CLITool[]>('/api/v1/integrations/cli-status'),
  },

  system: {
    tools: () => apiFetch<SystemToolStatus[]>('/api/v1/system/tools'),
    githubSwitch: (user: string) => apiFetch<{ active: string }>('/api/v1/system/github/switch', {
      method: 'POST',
      body: JSON.stringify({ user }),
    }),
  },

  cloud: {
    status: () => apiFetch<{ registered: boolean; connected: boolean; machine_id?: string; cloud_url?: string }>('/api/v1/cloud/status'),
    connect: () => apiFetch<{ status: string }>('/api/v1/cloud/connect', { method: 'POST' }),
    disconnect: () => apiFetch<{ status: string }>('/api/v1/cloud/connect', { method: 'DELETE' }),
  },

  spaces: {
    list: () => apiFetch<unknown[]>('/api/v1/spaces'),
    get: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}`),
    getDM: (agentName: string) => apiFetch<unknown>(`/api/v1/spaces/dm/${encodeURIComponent(agentName)}`),
    createChannel: (opts: { name: string; lead_agent: string; member_agents: string[]; icon?: string; color?: string }) =>
      apiFetch<unknown>('/api/v1/spaces', { method: 'POST', body: JSON.stringify(opts) }),
    updateSpace: (id: string, patch: Record<string, unknown>) =>
      apiFetch<unknown>(`/api/v1/spaces/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
    markRead: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}/mark-read`, { method: 'POST' }),
    sessions: (id: string) => apiFetch<unknown[]>(`/api/v1/space-sessions/${id}`),
    deleteSpace: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}`, { method: 'DELETE' }),
    // Returns chronological messages across all sessions in a space.
    // Use `before` (cursor from a prior response) to load older messages.
    messages: (spaceId: string, before?: string, limit = 20) => {
      const params = new URLSearchParams({ limit: String(limit) })
      if (before) params.set('before', before)
      return apiFetch<{ messages: SpaceMessage[]; next_cursor: string }>(`/api/v1/space-messages/${spaceId}?${params}`)
    },
  },

  muninn: {
    status: () => apiFetch<{ connected: boolean; endpoint?: string; username?: string }>('/api/v1/muninn/status'),
    test: (payload: { endpoint: string; username: string; password: string }) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/muninn/test', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    connect: (payload: { endpoint: string; username: string; password: string }) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/muninn/connect', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    vaults: () => apiFetch<{ vaults: string[] }>('/api/v1/muninn/vaults'),
    createVault: (payload: { vault_name: string; agent_label: string }) =>
      apiFetch<{ vault_name: string; token: string }>('/api/v1/muninn/vaults', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
  },

  credentials: {
    datadogTest: (payload: { url: string; api_key: string; app_key: string }) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/datadog/test', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    datadogSave: (payload: { url: string; api_key: string; app_key: string; label?: string }) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/datadog', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    splunkTest: (payload: { url: string; token: string }) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/splunk/test', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    splunkSave: (payload: { url: string; token: string; label?: string }) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/splunk', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),

    // Slack Bot
    slackBotTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/slack_bot/test', { method: 'POST', body: JSON.stringify(payload) }),
    slackBotSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/slack_bot', { method: 'POST', body: JSON.stringify(payload) }),

    // Jira Service Account
    jiraSATest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/jira_sa/test', { method: 'POST', body: JSON.stringify(payload) }),
    jiraSASave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/jira_sa', { method: 'POST', body: JSON.stringify(payload) }),

    // Linear
    linearTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/linear/test', { method: 'POST', body: JSON.stringify(payload) }),
    linearSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/linear', { method: 'POST', body: JSON.stringify(payload) }),

    // GitLab
    gitlabTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/gitlab/test', { method: 'POST', body: JSON.stringify(payload) }),
    gitlabSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/gitlab', { method: 'POST', body: JSON.stringify(payload) }),

    // Discord
    discordTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/discord/test', { method: 'POST', body: JSON.stringify(payload) }),
    discordSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/discord', { method: 'POST', body: JSON.stringify(payload) }),

    // Vercel
    vercelTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/vercel/test', { method: 'POST', body: JSON.stringify(payload) }),
    vercelSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/vercel', { method: 'POST', body: JSON.stringify(payload) }),

    // Stripe
    stripeTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/stripe/test', { method: 'POST', body: JSON.stringify(payload) }),
    stripeSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/stripe', { method: 'POST', body: JSON.stringify(payload) }),

    // PagerDuty
    pagerdutyTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/pagerduty/test', { method: 'POST', body: JSON.stringify(payload) }),
    pagerdutySave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/pagerduty', { method: 'POST', body: JSON.stringify(payload) }),

    // New Relic
    newrelicTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/newrelic/test', { method: 'POST', body: JSON.stringify(payload) }),
    newrelicSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/newrelic', { method: 'POST', body: JSON.stringify(payload) }),

    // Elastic
    elasticTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/elastic/test', { method: 'POST', body: JSON.stringify(payload) }),
    elasticSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/elastic', { method: 'POST', body: JSON.stringify(payload) }),

    // Grafana
    grafanaTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/grafana/test', { method: 'POST', body: JSON.stringify(payload) }),
    grafanaSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/grafana', { method: 'POST', body: JSON.stringify(payload) }),

    // CrowdStrike
    crowdstrikeTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/crowdstrike/test', { method: 'POST', body: JSON.stringify(payload) }),
    crowdstrikeSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/crowdstrike', { method: 'POST', body: JSON.stringify(payload) }),

    // Terraform Cloud
    terraformTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/terraform/test', { method: 'POST', body: JSON.stringify(payload) }),
    terraformSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/terraform', { method: 'POST', body: JSON.stringify(payload) }),

    // ServiceNow
    servicenowTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/servicenow/test', { method: 'POST', body: JSON.stringify(payload) }),
    servicenowSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/servicenow', { method: 'POST', body: JSON.stringify(payload) }),

    // Notion
    notionTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/notion/test', { method: 'POST', body: JSON.stringify(payload) }),
    notionSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/notion', { method: 'POST', body: JSON.stringify(payload) }),

    // Airtable
    airtableTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/airtable/test', { method: 'POST', body: JSON.stringify(payload) }),
    airtableSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/airtable', { method: 'POST', body: JSON.stringify(payload) }),

    // HubSpot
    hubspotTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/hubspot/test', { method: 'POST', body: JSON.stringify(payload) }),
    hubspotSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/hubspot', { method: 'POST', body: JSON.stringify(payload) }),

    // Zendesk
    zendeskTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/zendesk/test', { method: 'POST', body: JSON.stringify(payload) }),
    zendeskSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/zendesk', { method: 'POST', body: JSON.stringify(payload) }),

    // Asana
    asanaTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/asana/test', { method: 'POST', body: JSON.stringify(payload) }),
    asanaSave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/asana', { method: 'POST', body: JSON.stringify(payload) }),

    // Monday
    mondayTest: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/credentials/monday/test', { method: 'POST', body: JSON.stringify(payload) }),
    mondaySave: (payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>('/api/v1/credentials/monday', { method: 'POST', body: JSON.stringify(payload) }),

    // Generic catalog-driven endpoints — used by GenericCredentialForm for all
    // catalog providers (datadog, splunk, pagerduty, …, monday).
    testGeneric: (provider: string, payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>(`/api/v1/credentials/${provider}/test`, { method: 'POST', body: JSON.stringify(payload) }),
    saveGeneric: (provider: string, payload: Record<string, string>) =>
      apiFetch<{ id: string; provider: string; account_label: string }>(`/api/v1/credentials/${provider}`, { method: 'POST', body: JSON.stringify(payload) }),
  },

  providers: {
    models: (provider: string) =>
      apiFetch<ProviderModel[]>(`/api/v1/providers/${encodeURIComponent(provider)}/models`),
  },

  builtin: {
    status: () => apiFetch<BuiltinStatus>('/api/v1/builtin/status'),

    catalog: (refresh = false) => apiFetch<BuiltinCatalogEntry[]>(`/api/v1/builtin/catalog${refresh ? '?refresh=1' : ''}`),

    installedModels: () => apiFetch<BuiltinInstalledModel[]>('/api/v1/builtin/models'),

    activate: (model: string) =>
      apiFetch<{ activated: boolean; model: string; requires_restart: boolean }>('/api/v1/builtin/activate', {
        method: 'POST',
        body: JSON.stringify({ model }),
      }),

    delete: (name: string) =>
      apiFetch<{ deleted: boolean }>(`/api/v1/builtin/models/${encodeURIComponent(name)}`, {
        method: 'DELETE',
      }),

    downloadRuntimeStream(
      onEvent: (e: { downloaded: number; total: number }) => void,
      onDone: () => void,
      onError: (msg: string) => void,
    ): AbortController {
      const ctrl = new AbortController()
      ;(async () => {
        try {
          const res = await fetch('/api/v1/builtin/download', {
            method: 'POST',
            signal: ctrl.signal,
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${token.value}`,
            },
            body: JSON.stringify({}),
          })
          if (!res.ok || !res.body) {
            onError(`HTTP ${res.status}`)
            return
          }
          const reader = res.body.getReader()
          const decoder = new TextDecoder()
          let buf = ''
          while (true) {
            const { done, value } = await reader.read()
            if (done) break
            buf += decoder.decode(value, { stream: true })
            const parts = buf.split(/\r?\n\r?\n/)
            buf = parts.pop() ?? ''
            for (const part of parts) {
              const line = part.trim()
              if (!line.startsWith('data: ')) continue
              try {
                const msg = JSON.parse(line.slice(6))
                if (msg.type === 'progress') onEvent({ downloaded: msg.downloaded, total: msg.total })
                else if (msg.type === 'done') onDone()
                else if (msg.type === 'error') onError(msg.content ?? 'Unknown error')
              } catch { /* ignore malformed lines */ }
            }
          }
        } catch (e) {
          if ((e as Error).name !== 'AbortError') onError((e as Error).message ?? 'Stream error')
        }
      })()
      return ctrl
    },

    pullModelStream(
      name: string,
      onEvent: (e: { downloaded: number; total: number; speed: number }) => void,
      onDone: (name: string) => void,
      onError: (msg: string) => void,
    ): AbortController {
      const ctrl = new AbortController()
      ;(async () => {
        try {
          const res = await fetch('/api/v1/builtin/models/pull', {
            method: 'POST',
            signal: ctrl.signal,
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${token.value}`,
            },
            body: JSON.stringify({ name }),
          })
          if (!res.ok || !res.body) {
            onError(`HTTP ${res.status}`)
            return
          }
          const reader = res.body.getReader()
          const decoder = new TextDecoder()
          let buf = ''
          while (true) {
            const { done, value } = await reader.read()
            if (done) break
            buf += decoder.decode(value, { stream: true })
            const parts = buf.split(/\r?\n\r?\n/)
            buf = parts.pop() ?? ''
            for (const part of parts) {
              const line = part.trim()
              if (!line.startsWith('data: ')) continue
              try {
                const msg = JSON.parse(line.slice(6))
                if (msg.type === 'progress') onEvent({ downloaded: msg.downloaded, total: msg.total, speed: msg.speed })
                else if (msg.type === 'done') onDone(msg.name ?? name)
                else if (msg.type === 'error') onError(msg.content ?? 'Unknown error')
              } catch { /* ignore malformed lines */ }
            }
          }
        } catch (e) {
          if ((e as Error).name !== 'AbortError') onError((e as Error).message ?? 'Stream error')
        }
      })()
      return ctrl
    },
  },
}
