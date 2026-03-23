/**
 * useConfig — shared config state with change-detection polling.
 *
 * Polls GET /api/v1/config every 30 s and emits a `changed` event
 * when the server-side config differs from the local snapshot.
 */
import { ref, onUnmounted } from 'vue'
import { api } from './useApi'

export interface MCPServer {
  name: string
  transport: string
  command?: string
  args?: string[]
  url?: string
  env?: Record<string, string>
}

export interface HuginnConfig {
  reasoner_model: string
  ollama_base_url: string
  backend: {
    type: string
    endpoint: string
    provider: string
    api_key: string
  }
  theme: string
  context_limit_kb: number
  git_stage_on_write: boolean
  workspace_path: string
  max_turns: number
  tools_enabled: boolean
  allowed_tools: string[]
  disallowed_tools: string[]
  bash_timeout_secs: number
  diff_review_mode: string
  notepads_enabled: boolean
  compact_mode: string
  compact_trigger: number
  vision_enabled: boolean
  brave_api_key: string
  web_ui: { enabled: boolean; port: number; auto_open: boolean; bind: string }
  integrations: {
    google: { client_id: string; client_secret: string }
    github: { client_id: string; client_secret: string }
    slack: { client_id: string; client_secret: string }
    jira: { client_id: string; client_secret: string }
    bitbucket: { client_id: string; client_secret: string }
  }
  cloud: { url: string }
  mcp_servers?: MCPServer[]
  version: number
}

const config = ref<HuginnConfig | null>(null)
const loading = ref(false)
const externallyChanged = ref(false) // true when server config differs from our snapshot
let snapshot = ''
let pollTimer: ReturnType<typeof setInterval> | null = null
let refCount = 0

async function loadConfig(): Promise<HuginnConfig> {
  const data = await api.config.get() as unknown as HuginnConfig
  config.value = data
  snapshot = JSON.stringify(data)
  return data
}

async function saveConfig(cfg: HuginnConfig) {
  const result = await api.config.update(cfg) as { saved: boolean; requires_restart: boolean }
  config.value = cfg
  snapshot = JSON.stringify(cfg)
  externallyChanged.value = false
  return result
}

async function pollForChanges() {
  try {
    const data = await api.config.get() as unknown as HuginnConfig
    const current = JSON.stringify(data)
    if (snapshot && current !== snapshot) {
      config.value = data
      snapshot = current
      externallyChanged.value = true
    }
  } catch { /* ignore poll errors */ }
}

export function useConfig() {
  refCount++

  if (refCount === 1 && !pollTimer) {
    pollTimer = setInterval(pollForChanges, 30_000)
  }

  onUnmounted(() => {
    refCount--
    if (refCount === 0 && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  })

  return { config, loading, externallyChanged, loadConfig, saveConfig }
}
