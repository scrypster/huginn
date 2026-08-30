<template>
  <div class="flex h-full bg-huginn-bg">
    <div class="w-48 flex-shrink-0 flex flex-col border-r border-huginn-border"
      style="background:rgba(22,27,34,0.6)">
      <div class="flex items-center px-4 h-11 border-b border-huginn-border flex-shrink-0">
        <span class="text-xs font-semibold text-huginn-muted uppercase tracking-widest">Settings</span>
      </div>
      <nav class="flex-1 overflow-y-auto py-2">
        <button v-for="t in tabs" :key="t.id" @click="activeTab = t.id"
          class="w-full flex items-center gap-2.5 px-4 py-2 text-sm transition-colors duration-100 text-left"
          :class="activeTab === t.id
            ? 'text-huginn-text bg-huginn-surface/60'
            : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface/30'">
          <SettingsTabIcon :icon="t.icon" />
          {{ t.label }}
        </button>
      </nav>
      <div v-if="dirty" class="px-4 py-3 border-t border-huginn-border flex-shrink-0">
        <div class="text-[11px] text-huginn-yellow mb-2">Unsaved changes</div>
        <div class="flex gap-1.5">
          <button @click="discard" class="flex-1 px-2 py-1.5 text-[11px] text-huginn-muted border border-huginn-border rounded-md hover:bg-huginn-surface transition-all">Discard</button>
          <button @click="save" :disabled="saving"
            class="flex-1 px-2 py-1.5 text-[11px] font-medium text-white rounded-md transition-all disabled:opacity-50"
            style="background:rgba(88,166,255,0.9)">{{ saving ? '…' : 'Save' }}</button>
        </div>
      </div>
    </div>

    <div class="flex-1 flex flex-col min-w-0">
      <div v-if="externallyChanged" class="mx-4 mt-3 flex-shrink-0">
        <div class="flex items-center gap-3 px-4 py-2.5 rounded-xl border border-huginn-yellow/40 text-huginn-yellow text-xs"
          style="background:rgba(210,153,34,0.07)">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          Config updated externally — showing latest values.
          <button @click="externallyChanged = false" class="ml-auto text-huginn-muted hover:text-huginn-text">×</button>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center flex-1">
        <div class="w-5 h-5 border-2 border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
      </div>

      <div v-else class="flex-1 overflow-y-auto">
        <div class="max-w-2xl mx-auto px-6 py-6 space-y-6">
          <div v-if="saveMsg" class="px-4 py-2.5 rounded-xl border text-xs"
            :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
            {{ saveMsg }}
          </div>

          <SettingsGeneralTab
            v-if="activeTab === 'general'"
            :form="form"
            @mark-dirty="markDirty"
          />

          <SettingsToolsTab
            v-if="activeTab === 'tools'"
            :form="form"
            :allowed-tools-text="allowedToolsText"
            :disallowed-tools-text="disallowedToolsText"
            :show-brave-key="showBraveKey"
            @update:allowed-tools-text="allowedToolsText = $event"
            @update:disallowed-tools-text="disallowedToolsText = $event"
            @update:show-brave-key="showBraveKey = $event"
            @mark-dirty="markDirty"
            @sync-tools="syncToolsFromText"
          />

          <SettingsWebUITab
            v-if="activeTab === 'webui'"
            :form="form"
            :runtime-status="runtimeStatus"
            @mark-dirty="markDirty"
          />

          <SettingsIntegrationsTab
            v-if="activeTab === 'integrations'"
            :form="form"
            :integration-providers="integrationProviders"
            @mark-dirty="markDirty"
          />

          <SettingsHooksTab
            v-if="activeTab === 'hooks'"
          />

          <SettingsCheckpointsTab
            v-if="activeTab === 'checkpoints'"
          />

          <SettingsMcpTab
            v-if="activeTab === 'mcp'"
            :mcp-servers="mcpServers"
            :new-mcp="newMcp"
            :mcp-add-error="mcpAddError"
            :browser-enabled="browserEnabled"
            :browser-status="browserStatus"
            @add-mcp-server="addMcpServer"
            @remove-mcp-server="removeMcpServer"
            @toggle-browser="toggleBrowser"
          />

          <SettingsNotificationsTab
            v-if="activeTab === 'notifications'"
            :notif="notif"
          />

          <SettingsAboutTab
            v-if="activeTab === 'about'"
            :version-label="versionLabel"
          />

        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, type MCPServerStatus } from '../composables/useApi'
import { useConfig, type MCPServer } from '../composables/useConfig'
import { useVersion } from '../composables/useVersion'
import { useBrowserNotifications } from '../composables/useBrowserNotifications'
import SettingsTabIcon from '../components/settings/SettingsTabIcon.vue'
import SettingsGeneralTab from '../components/settings/SettingsGeneralTab.vue'
import SettingsToolsTab from '../components/settings/SettingsToolsTab.vue'
import SettingsWebUITab from '../components/settings/SettingsWebUITab.vue'
import SettingsIntegrationsTab from '../components/settings/SettingsIntegrationsTab.vue'
import SettingsHooksTab from '../components/settings/SettingsHooksTab.vue'
import SettingsCheckpointsTab from '../components/settings/SettingsCheckpointsTab.vue'
import SettingsMcpTab from '../components/settings/SettingsMcpTab.vue'
import SettingsNotificationsTab from '../components/settings/SettingsNotificationsTab.vue'
import SettingsAboutTab from '../components/settings/SettingsAboutTab.vue'

const { config, loading, externallyChanged, loadConfig, saveConfig } = useConfig()
const notif = useBrowserNotifications()
// useVersion returns the build version (cached app-wide). The composable
// also fires a fetch on App.vue mount, so by the time Settings renders the
// label is usually already populated; calling loadVersion again here is a
// safe no-op if the user navigates straight to Settings before App.vue
// finishes its onMounted hook.
const { versionLabel, loadVersion } = useVersion()

// ── State ─────────────────────────────────────────────────────────────
type SettingsTabID = 'general' | 'tools' | 'webui' | 'integrations' | 'hooks' | 'checkpoints' | 'mcp' | 'notifications' | 'about'
type SettingsTabIconName = SettingsTabID
type SettingsTab = { id: SettingsTabID; label: string; icon: SettingsTabIconName }

const activeTab = ref<SettingsTabID>('general')
const dirty = ref(false)
const saving = ref(false)
const saveMsg = ref('')
const saveError = ref(false)
const showBraveKey = ref(false)
const runtimeStatus = ref<Record<string, unknown>>({})
const allowedToolsText = ref('')
const disallowedToolsText = ref('')

type FormKey = string
const form = ref<Record<FormKey, unknown>>({
  workspace_path: '', max_turns: 50, bash_timeout_secs: 120, context_limit_kb: 200,
  diff_review_mode: 'auto', compact_mode: 'auto', git_stage_on_write: false,
  notepads_enabled: false, vision_enabled: false, semantic_search: false,
  tools_enabled: true, brave_api_key: '',
  web_ui_enabled: true, web_ui_port: 0, web_ui_bind: '127.0.0.1', web_ui_auto_open: true,
  google_client_id: '', google_client_secret: '',
  github_client_id: '', github_client_secret: '',
  slack_client_id: '', slack_client_secret: '',
  jira_client_id: '', jira_client_secret: '',
  bitbucket_client_id: '', bitbucket_client_secret: '',
})
let originalForm = ''

const tabs: SettingsTab[] = [
  { id: 'general', label: 'General', icon: 'general' },
  { id: 'tools', label: 'Tools', icon: 'tools' },
  { id: 'webui', label: 'Web UI', icon: 'webui' },
  { id: 'integrations', label: 'Integrations', icon: 'integrations' },
  { id: 'hooks', label: 'Hooks', icon: 'hooks' },
  { id: 'checkpoints', label: 'Checkpoints', icon: 'checkpoints' },
  { id: 'mcp', label: 'MCP Servers', icon: 'mcp' },
  { id: 'notifications', label: 'Notifications', icon: 'notifications' },
  { id: 'about', label: 'About', icon: 'about' },
]

const integrationProviders = [
  { key: 'google', label: 'Google' },
  { key: 'github', label: 'GitHub' },
  { key: 'slack', label: 'Slack' },
  { key: 'jira', label: 'Jira' },
  { key: 'bitbucket', label: 'Bitbucket' },
]

// ── MCP Servers state ─────────────────────────────────────────────────
const mcpServers = ref<MCPServer[]>([])
const mcpAddError = ref('')
const newMcp = ref({ name: '', transport: 'stdio', command: '', argsText: '', url: '', envText: '' })

function addMcpServer() {
  mcpAddError.value = ''
  const name = newMcp.value.name.trim()
  if (!name) { mcpAddError.value = 'Name is required'; return }
  if (mcpServers.value.some(s => s.name === name)) { mcpAddError.value = 'Server name already exists'; return }

  const env: Record<string, string> = {}
  for (const line of newMcp.value.envText.split('\n')) {
    const l = line.trim()
    if (!l) continue
    const eq = l.indexOf('=')
    if (eq < 1) continue
    env[l.slice(0, eq)] = l.slice(eq + 1)
  }
  const srv: MCPServer = { name, transport: newMcp.value.transport }
  if (newMcp.value.transport === 'stdio') {
    srv.command = newMcp.value.command.trim()
    const args = newMcp.value.argsText.split('\n').map(s => s.trim()).filter(Boolean)
    if (args.length) srv.args = args
  } else {
    srv.url = newMcp.value.url.trim()
  }
  if (Object.keys(env).length > 0) srv.env = env
  mcpServers.value = [...mcpServers.value, srv]
  dirty.value = true
  newMcp.value = { name: '', transport: 'stdio', command: '', argsText: '', url: '', envText: '' }
}

function removeMcpServer(idx: number) {
  if (!window.confirm('Remove this MCP server?')) return
  mcpServers.value = mcpServers.value.filter((_, i) => i !== idx)
  dirty.value = true
}

// ── Browser (Playwright) toggle ─────────────────────────────────────
// A purpose-built on/off switch over the generic MCP server list above: it
// manages a single well-known entry (name: "playwright") instead of asking
// the user to fill in the generic Add Server form with the right command.
const BROWSER_SERVER_NAME = 'playwright'
// Matches the pinned install path from the server's own InstallHint
// (internal/mcp/manager.go knownInstallHints) — npx @latest blows the MCP
// 10s initialize deadline, so the server must be pinned, not resolved live.
const BROWSER_DEFAULT_COMMAND = '~/.huginn/mcp-bin/node_modules/.bin/playwright-mcp'

const browserEnabled = computed(() => mcpServers.value.some(s => s.name === BROWSER_SERVER_NAME))
const browserStatuses = ref<MCPServerStatus[]>([])
const browserStatus = computed<MCPServerStatus | undefined>(() =>
  browserStatuses.value.find(s => s.name === BROWSER_SERVER_NAME)
)

async function refreshMcpStatus() {
  try {
    const { servers } = await api.mcp.status()
    browserStatuses.value = servers
  } catch {
    // Best-effort — the toggle still works from the config alone; a failed
    // status fetch just leaves browserStatus undefined ("Checking status…").
  }
}

function toggleBrowser(enabled: boolean) {
  if (enabled) {
    if (browserEnabled.value) return
    mcpServers.value = [...mcpServers.value, {
      name: BROWSER_SERVER_NAME,
      transport: 'stdio',
      command: BROWSER_DEFAULT_COMMAND,
    }]
  } else {
    mcpServers.value = mcpServers.value.filter(s => s.name !== BROWSER_SERVER_NAME)
  }
  dirty.value = true
}

function syncToolsFromText() {
  form.value.allowed_tools = allowedToolsText.value.split('\n').map(s => s.trim()).filter(Boolean)
  form.value.disallowed_tools = disallowedToolsText.value.split('\n').map(s => s.trim()).filter(Boolean)
}

function markDirty() {
  dirty.value = true
}

function populateForm(cfg: Record<string, unknown>) {
  form.value.workspace_path = cfg.workspace_path ?? ''
  form.value.max_turns = cfg.max_turns ?? 50
  form.value.bash_timeout_secs = cfg.bash_timeout_secs ?? 120
  form.value.context_limit_kb = cfg.context_limit_kb ?? 200
  form.value.diff_review_mode = cfg.diff_review_mode ?? 'auto'
  form.value.compact_mode = cfg.compact_mode ?? 'auto'
  form.value.git_stage_on_write = cfg.git_stage_on_write ?? false
  form.value.notepads_enabled = cfg.notepads_enabled ?? false
  form.value.vision_enabled = cfg.vision_enabled ?? false
  form.value.semantic_search = cfg.semantic_search ?? false
  form.value.tools_enabled = cfg.tools_enabled ?? true
  form.value.brave_api_key = cfg.brave_api_key ?? ''
  const wu = (cfg.web_ui as Record<string, unknown>) ?? {}
  form.value.web_ui_enabled = wu.enabled ?? true
  form.value.web_ui_port = wu.port ?? 0
  form.value.web_ui_bind = wu.bind ?? '127.0.0.1'
  form.value.web_ui_auto_open = wu.auto_open ?? true
  const integ = (cfg.integrations as Record<string, Record<string, string>>) ?? {}
  for (const p of integrationProviders) {
    form.value[`${p.key}_client_id`] = integ[p.key]?.client_id ?? ''
    form.value[`${p.key}_client_secret`] = integ[p.key]?.client_secret ?? ''
  }
  allowedToolsText.value = ((cfg.allowed_tools as string[]) ?? []).join('\n')
  disallowedToolsText.value = ((cfg.disallowed_tools as string[]) ?? []).join('\n')
  mcpServers.value = (cfg.mcp_servers as MCPServer[]) ?? []
  originalForm = JSON.stringify(form.value)
  dirty.value = false
}

async function save() {
  saving.value = true
  saveMsg.value = ''
  saveError.value = false
  try {
    if (!config.value) throw new Error('Config not loaded')
    syncToolsFromText()
    const updated = {
      ...config.value,
      workspace_path: form.value.workspace_path as string,
      max_turns: form.value.max_turns as number,
      bash_timeout_secs: form.value.bash_timeout_secs as number,
      context_limit_kb: form.value.context_limit_kb as number,
      diff_review_mode: form.value.diff_review_mode as string,
      compact_mode: form.value.compact_mode as string,
      git_stage_on_write: form.value.git_stage_on_write as boolean,
      notepads_enabled: form.value.notepads_enabled as boolean,
      vision_enabled: form.value.vision_enabled as boolean,
      semantic_search: form.value.semantic_search as boolean,
      tools_enabled: form.value.tools_enabled as boolean,
      brave_api_key: form.value.brave_api_key as string,
      allowed_tools: form.value.allowed_tools as string[],
      disallowed_tools: form.value.disallowed_tools as string[],
      web_ui: {
        enabled: form.value.web_ui_enabled as boolean,
        port: form.value.web_ui_port as number,
        bind: form.value.web_ui_bind as string,
        auto_open: form.value.web_ui_auto_open as boolean,
      },
      integrations: {
        google:    { client_id: form.value.google_client_id as string,    client_secret: form.value.google_client_secret as string },
        github:    { client_id: form.value.github_client_id as string,    client_secret: form.value.github_client_secret as string },
        slack:     { client_id: form.value.slack_client_id as string,     client_secret: form.value.slack_client_secret as string },
        jira:      { client_id: form.value.jira_client_id as string,      client_secret: form.value.jira_client_secret as string },
        bitbucket: { client_id: form.value.bitbucket_client_id as string, client_secret: form.value.bitbucket_client_secret as string },
      },
      mcp_servers: mcpServers.value,
    }
    const result = await saveConfig(updated)
    originalForm = JSON.stringify(form.value)
    dirty.value = false
    saveMsg.value = result.requires_restart ? 'Saved — restart required for some changes' : 'Settings saved'
    setTimeout(() => { saveMsg.value = '' }, 4000)
  } catch (e: unknown) {
    saveMsg.value = e instanceof Error ? e.message : 'Save failed'
    saveError.value = true
  } finally {
    saving.value = false
  }
}

function discard() {
  Object.assign(form.value, JSON.parse(originalForm))
  dirty.value = false
}

onMounted(async () => {
  const [cfg] = await Promise.all([
    loadConfig(),
    api.runtime.status().then(s => { runtimeStatus.value = s as unknown as Record<string, unknown> }).catch(() => {}),
    // Idempotent: useVersion caches across the app, so this is a no-op
    // when the user enters Settings after App.vue has already loaded.
    loadVersion(),
    refreshMcpStatus(),
  ])
  populateForm(cfg as unknown as Record<string, unknown>)
})
</script>
