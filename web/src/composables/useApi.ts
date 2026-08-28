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
  // IsSpecialist + ModelID: set only for a one-off spawn_specialist thread —
  // the agent is an ephemeral overlay entry (agents.AgentRegistry), never on
  // the roster. ThreadCard renders a "temporary" pill + model id from these.
  IsSpecialist?: boolean
  ModelID?: string
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

export interface AgentCapabilityConnection {
  connection_id: string
  provider: string
  account_label?: string
  account_id?: string
}

export interface AgentCapabilityProvider {
  provider: string
  display_name?: string
  category?: string
  type?: string
  multi_account: boolean
}

export interface AgentCapabilityMatrix {
  connections: AgentCapabilityConnection[]
  providers: AgentCapabilityProvider[]
}

export interface AgentToolbeltDecision {
  entry: ToolbeltEntry
  allowed: boolean
  reason_code?: string
  reason?: string
  resolved_provider?: string
}

export interface AgentCapabilityValidation {
  valid: boolean
  decisions: AgentToolbeltDecision[]
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
  description?: string          // one-line description shown in member panels and tooltips
  toolbelt?: ToolbeltEntry[]
  local_tools?: string[]   // tool names; ["*"] = all builtins; undefined/[] = none
  approved_tools?: string[] // tool names pre-approved to skip the permission prompt
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
  created_at?: string
  role: 'user' | 'assistant'
  content: string
  agent: string
  // Set on persisted follow-up messages (lead-agent synthesis replies).
  // Used by the frontend to force header rendering even when the previous
  // message is from the same agent.
  parent_message_id?: string
  // Slack-style space reply parent. Empty/absent = channel/DM root.
  parent_id?: string
  // Slack-style reply count for the "N replies" chip.
  reply_count?: number
  last_preview?: string
  new_since?: number
  // Populated from WS tool_result events during streaming, or from the server on load.
  // done is absent when loaded from the server (treat absent as true — all persisted calls are complete).
  toolCalls?: { id: string; name: string; args: Record<string, unknown>; result?: string; done?: boolean; diff?: FileDiff }[]
}

// FileDiff is the before/after unified diff attached to a write_file/edit_file
// tool result (see tools.BuildFileDiff on the Go side). Mirrors the "diff" key
// written into ToolResult.Metadata and persisted on PersistedToolCall.Diff.
export interface FileDiff {
  path: string
  unified: string
  added: number
  removed: number
  truncated: boolean
  is_new: boolean
  is_delete: boolean
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

/**
 * ensureToken resolves once a token is available, fetching one first if
 * App.vue's initApp() hasn't set it yet (or a caller polls independently of
 * the main boot sequence — e.g. delivery-queue badge). Callers that build
 * their own request instead of going through apiFetch should await this
 * before their first request so it doesn't fire pre-auth and log a spurious
 * 401 (the failure is swallowed here; the caller's own request will surface
 * it if the token still can't be fetched).
 */
export async function ensureToken(): Promise<string> {
  if (!token.value) {
    try {
      setToken(await fetchToken())
    } catch { /* leave empty; caller's request will 401 */ }
  }
  return token.value
}

export async function apiFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  // Auto-fetch token on first use if App.vue hasn't initialized it yet.
  // Vue 3 fires child onMounted() before parent onMounted(), so views that
  // make API calls on mount can race ahead of initApp()/setToken().
  // Guarded by `if` (not an unconditional `await ensureToken()`) so the
  // common case — token already set — falls straight through to fetch()
  // in the same microtask tick. Callers such as useVersion.ts rely on that
  // synchronous-until-the-real-fetch behavior to dedupe concurrent calls
  // into a single in-flight request.
  if (!token.value) await ensureToken()

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
  health: () => apiFetch<{ status: string; version: string; stale?: boolean; satellite_connected: boolean; backend_status: string }>('/api/v1/health'),
  restart: () => apiFetch<{ status: string }>('/api/v1/restart', { method: 'POST' }),

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
    capabilityMatrix: () =>
      apiFetch<AgentCapabilityMatrix>('/api/v1/agents/capability-matrix'),
    validateCapabilityMatrix: (toolbelt: ToolbeltEntry[]) =>
      apiFetch<AgentCapabilityValidation>('/api/v1/agents/capability-matrix/validate', {
        method: 'POST',
        body: JSON.stringify({ toolbelt }),
      }),
    testVault: (agentName: string, vaultName?: string) =>
      apiFetch<{ status: string; vault: string; tools_count?: number; warning?: string }>(`/api/v1/agents/${encodeURIComponent(agentName)}/vault/test`, {
        method: 'POST',
        body: JSON.stringify({ vault_name: vaultName ?? '' }),
      }),
  },

  threads: {
    list: (sessionId: string) =>
      apiFetch<Thread[]>(`/api/v1/sessions/${sessionId}/threads`),
    get: (sessionId: string, threadId: string) =>
      apiFetch<{ ID: string; parentMessageId?: string; AgentID?: string }>(
        `/api/v1/sessions/${sessionId}/threads/${threadId}`
      ),
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

  stats: () => apiFetch<Record<string, number | null>>('/api/v1/stats'),

  statsHistory: (since?: number) => {
    const q = since != null ? `?since=${since}` : ''
    return apiFetch<{
      stats: Array<{ ts: number; key: string; kind: string; value: number }>
      cost: Array<{ ts: number; session_id: string; cost_usd: number; prompt_tokens: number; completion_tokens: number }>
    }>(`/api/v1/stats/history${q}`)
  },

  cost: () => apiFetch<{
    session_total_usd: number
    prompt_tokens_total?: number
    completion_tokens_total?: number
    is_local?: boolean
  }>('/api/v1/cost'),

  logs: (n = 100) => apiFetch<{ lines: string[] }>(`/api/v1/logs?n=${n}`),

  logLevel: {
    get: () => apiFetch<{ level: string }>('/api/v1/log-level'),
    set: (level: string) => apiFetch<{ level: string }>('/api/v1/log-level', {
      method: 'PUT',
      body: JSON.stringify({ level }),
    }),
  },

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

  companies: {
    // GET /api/v1/companies — fail-soft at the composable. Missing API → empty list = desk only.
    list: () => apiFetch<unknown>('/api/v1/companies'),
    create: (body: { name: string; vault?: string; members?: string[]; icon?: string; color?: string }) =>
      apiFetch<unknown>('/api/v1/companies', { method: 'POST', body: JSON.stringify(body) }),
    get: (id: string) => apiFetch<unknown>(`/api/v1/companies/${encodeURIComponent(id)}`),
    update: (id: string, patch: { lead?: string; name?: string; vault?: string; icon?: string; color?: string }) =>
      apiFetch<unknown>(`/api/v1/companies/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch) }),
    seat: (id: string, agent: string) =>
      apiFetch<unknown>(`/api/v1/companies/${encodeURIComponent(id)}/members`, {
        method: 'POST',
        body: JSON.stringify({ agent }),
      }),
    unseat: (id: string, agent: string) =>
      apiFetch<unknown>(`/api/v1/companies/${encodeURIComponent(id)}/members/${encodeURIComponent(agent)}`, {
        method: 'DELETE',
      }),
    remove: (id: string) =>
      apiFetch<unknown>(`/api/v1/companies/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  },

  spaces: {
    list: (opts?: { company_id?: string }) => {
      const params = new URLSearchParams()
      if (opts?.company_id) params.set('company_id', opts.company_id)
      const q = params.toString()
      return apiFetch<unknown[]>(q ? `/api/v1/spaces?${q}` : '/api/v1/spaces')
    },
    get: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}`),
    getDM: (agentName: string, opts?: { company_id?: string }) => {
      const params = new URLSearchParams()
      if (opts?.company_id) params.set('company_id', opts.company_id)
      const q = params.toString()
      return apiFetch<unknown>(`/api/v1/spaces/dm/${encodeURIComponent(agentName)}${q ? `?${q}` : ''}`)
    },
    createChannel: (opts: { name: string; lead_agent: string; member_agents: string[]; icon?: string; color?: string; company_id?: string; kind?: string }) =>
      apiFetch<unknown>('/api/v1/spaces', { method: 'POST', body: JSON.stringify(opts) }),
    updateSpace: (id: string, patch: Record<string, unknown>) =>
      apiFetch<unknown>(`/api/v1/spaces/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
    markRead: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}/mark-read`, { method: 'POST' }),
    sessions: (id: string, opts?: { signal?: AbortSignal }) =>
      apiFetch<unknown[]>(`/api/v1/space-sessions/${id}`, { signal: opts?.signal }),
    deleteSpace: (id: string) => apiFetch<unknown>(`/api/v1/spaces/${id}`, { method: 'DELETE' }),
    // Returns chronological messages across all sessions in a space.
    // Use `before` (cursor from a prior response) to load older messages.
    messages: (spaceId: string, before?: string, limit = 20, opts?: { signal?: AbortSignal }) => {
      const params = new URLSearchParams({ limit: String(limit) })
      if (before) params.set('before', before)
      return apiFetch<{ messages: SpaceMessage[]; next_cursor: string }>(
        `/api/v1/space-messages/${spaceId}?${params}`,
        { signal: opts?.signal },
      )
    },
    replies: (spaceId: string, parentId: string, opts?: { signal?: AbortSignal }) => {
      const params = new URLSearchParams({ parent_id: parentId })
      return apiFetch<{ messages: SpaceMessage[]; participant?: boolean; unseen?: number }>(
        `/api/v1/space-messages/${spaceId}/replies?${params}`,
        { signal: opts?.signal },
      )
    },
    postMessage: (spaceId: string, body: { content: string; parent_id?: string }) =>
      apiFetch<SpaceMessage>(`/api/v1/space-messages/${spaceId}`, {
        method: 'POST',
        body: JSON.stringify(body),
      }),
    markThreadRead: (spaceId: string, parentId: string) =>
      apiFetch<{ ok: boolean; unseen: number }>(`/api/v1/space-messages/${spaceId}/thread-read`, {
        method: 'POST',
        body: JSON.stringify({ parent_id: parentId }),
      }),
  },

  muninn: {
    status: () => apiFetch<{ connected: boolean; detected?: boolean; installed?: boolean; running?: boolean; endpoint?: string; username?: string }>('/api/v1/muninn/status'),
    test: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/muninn/test', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    connect: (payload: Record<string, string>) =>
      apiFetch<{ ok: boolean; error?: string }>('/api/v1/muninn/connect', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
    connectLocal: () =>
      apiFetch<{ ok: boolean; connected: boolean; installed?: boolean; running?: boolean; detected?: boolean; endpoint?: string; vaults?: string[] }>('/api/v1/muninn/connect-local', {
        method: 'POST',
      }),
    vaults: () => apiFetch<{ vaults: string[] }>('/api/v1/muninn/vaults'),
    remember: (vault: string, content: string) =>
      apiFetch<{ id?: string }>('/api/v1/muninn/tool', {
        method: 'POST',
        body: JSON.stringify({
          vault,
          tool: 'muninn_remember',
          args: { concept: content.trim().slice(0, 60), content },
        }),
      }),
    createVault: (payload: { vault_name: string; agent_label: string }) =>
      apiFetch<{ vault_name: string; token: string }>('/api/v1/muninn/vaults', {
        method: 'POST',
        body: JSON.stringify(payload),
      }),
  },

  credentials: {
    // Generic catalog-driven endpoints — used by CredentialModal for all credential/database providers.
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
