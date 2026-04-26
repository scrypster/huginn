<template>
  <div class="flex h-full bg-huginn-bg">

    <!-- ── Left sidebar nav ──────────────────────────────────────────── -->
    <div class="w-48 flex-shrink-0 flex flex-col border-r border-huginn-border"
      style="background:rgba(22,27,34,0.6)">
      <!-- Sidebar header -->
      <div class="flex items-center px-4 h-11 border-b border-huginn-border flex-shrink-0">
        <span class="text-xs font-semibold text-huginn-muted uppercase tracking-widest">Settings</span>
      </div>
      <!-- Nav items -->
      <nav class="flex-1 overflow-y-auto py-2">
        <button v-for="t in tabs" :key="t.id" @click="activeTab = t.id"
          class="w-full flex items-center gap-2.5 px-4 py-2 text-sm transition-colors duration-100 text-left"
          :class="activeTab === t.id
            ? 'text-huginn-text bg-huginn-surface/60'
            : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface/30'">
          <component :is="t.icon" class="w-4 h-4 flex-shrink-0" />
          {{ t.label }}
        </button>
      </nav>
      <!-- Unsaved indicator at bottom of sidebar -->
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

    <!-- ── Main content ──────────────────────────────────────────────── -->
    <div class="flex-1 flex flex-col min-w-0">

      <!-- Config changed banner -->
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

          <!-- Save feedback -->
          <div v-if="saveMsg" class="px-4 py-2.5 rounded-xl border text-xs"
            :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
            {{ saveMsg }}
          </div>

          <!-- ── General ───────────────────────────────────────────── -->
          <div v-if="activeTab === 'general'" class="space-y-6">
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">General</h3>
              <FieldRow label="Workspace Path" hint="Default working directory for new sessions">
                <input v-model="form.workspace_path" @input="dirty = true" placeholder="~/projects or /absolute/path"
                  class="field-input" />
              </FieldRow>
              <FieldRow label="Max Turns" hint="Max agentic loop iterations (default 50)">
                <input v-model.number="form.max_turns" @input="dirty = true" type="number" min="1" max="500"
                  class="field-input w-24" />
              </FieldRow>
              <FieldRow label="Bash Timeout" hint="Seconds before a shell command times out">
                <div class="flex items-center gap-2">
                  <input v-model.number="form.bash_timeout_secs" @input="dirty = true" type="number" min="5" max="3600"
                    class="field-input w-24" />
                  <span class="text-xs text-huginn-muted">seconds</span>
                </div>
              </FieldRow>
              <FieldRow label="Context Limit" hint="Max context window in kilobytes">
                <div class="flex items-center gap-2">
                  <input v-model.number="form.context_limit_kb" @input="dirty = true" type="number" min="1" max="2048"
                    class="field-input w-24" />
                  <span class="text-xs text-huginn-muted">KB</span>
                </div>
              </FieldRow>
              <FieldRow label="Diff Review Mode" hint="When to pause and show a diff for approval">
                <div class="relative">
                  <select v-model="form.diff_review_mode" @change="dirty = true" class="field-select">
                    <option value="auto">Auto</option>
                    <option value="always">Always</option>
                    <option value="never">Never</option>
                  </select>
                  <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
                </div>
              </FieldRow>
              <FieldRow label="Compact Mode" hint="Auto-compact conversation history">
                <div class="relative">
                  <select v-model="form.compact_mode" @change="dirty = true" class="field-select">
                    <option value="auto">Auto</option>
                    <option value="always">Always</option>
                    <option value="never">Never</option>
                  </select>
                  <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
                </div>
              </FieldRow>
            </section>
            <div class="border-t border-huginn-border" />
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Behavior Flags</h3>
              <ToggleRow :modelValue="!!form.git_stage_on_write" @update:modelValue="v => { form.git_stage_on_write = v; dirty = true }"
                label="Git stage on write" hint="Auto-stage files after each write" />
              <ToggleRow :modelValue="!!form.notepads_enabled" @update:modelValue="v => { form.notepads_enabled = v; dirty = true }"
                label="Notepads" hint="Enable persistent note-taking tools" />
              <ToggleRow :modelValue="!!form.vision_enabled" @update:modelValue="v => { form.vision_enabled = v; dirty = true }"
                label="Vision" hint="Enable image understanding in prompts" />
              <ToggleRow :modelValue="!!form.semantic_search" @update:modelValue="v => { form.semantic_search = v; dirty = true }"
                label="Semantic search" hint="Use embeddings for smarter file search" />
            </section>
          </div>

          <!-- ── Tools ────────────────────────────────────────────── -->
          <div v-if="activeTab === 'tools'" class="space-y-6">
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Tool Access</h3>
              <ToggleRow :modelValue="!!form.tools_enabled" @update:modelValue="v => { form.tools_enabled = v; dirty = true }"
                label="Tools enabled" hint="Allow huginn to use tools (file read/write, bash, etc.)" />
            </section>
            <div class="border-t border-huginn-border" />
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Allowed Tools</h3>
              <p class="text-xs text-huginn-muted">Whitelist — empty means all tools allowed. One tool name per line.</p>
              <textarea v-model="allowedToolsText" @input="dirty = true; syncToolsFromText()"
                placeholder="read_file&#10;write_file&#10;bash"
                rows="6"
                class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y"
              />
            </section>
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Disallowed Tools</h3>
              <p class="text-xs text-huginn-muted">Blacklist — tools that are always blocked.</p>
              <textarea v-model="disallowedToolsText" @input="dirty = true; syncToolsFromText()"
                placeholder="bash&#10;web_search"
                rows="4"
                class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y"
              />
            </section>
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Web Search</h3>
              <FieldRow label="Brave API Key" hint="Required for web_search tool">
                <div class="relative">
                  <input v-model="form.brave_api_key" @input="dirty = true"
                    :type="showBraveKey ? 'text' : 'password'"
                    placeholder="BSA..."
                    class="field-input pr-10 font-mono" />
                  <button @click="showBraveKey = !showBraveKey"
                    class="absolute right-2.5 top-1/2 -translate-y-1/2 text-huginn-muted hover:text-huginn-text transition-colors">
                    <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path v-if="!showBraveKey" d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle v-if="!showBraveKey" cx="12" cy="12" r="3" />
                      <path v-if="showBraveKey" d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24" />
                      <line v-if="showBraveKey" x1="1" y1="1" x2="23" y2="23" />
                    </svg>
                  </button>
                </div>
              </FieldRow>
            </section>
          </div>

          <!-- ── Web UI ─────────────────────────────────────────── -->
          <div v-if="activeTab === 'webui'" class="space-y-6">
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Web UI</h3>
              <ToggleRow :modelValue="!!form.web_ui_enabled" @update:modelValue="v => { form.web_ui_enabled = v; dirty = true }"
                label="Enabled" hint="Start the web server with 'huginn serve'" />
              <FieldRow label="Port" hint="0 = dynamic (random available port)">
                <input v-model.number="form.web_ui_port" @input="dirty = true" type="number" min="0" max="65535"
                  class="field-input w-24" placeholder="0" />
              </FieldRow>
              <FieldRow label="Bind address" hint="Loopback only recommended">
                <input v-model="form.web_ui_bind" @input="dirty = true"
                  class="field-input w-40 font-mono" placeholder="127.0.0.1" />
              </FieldRow>
              <ToggleRow :modelValue="!!form.web_ui_auto_open" @update:modelValue="v => { form.web_ui_auto_open = v; dirty = true }"
                label="Auto-open browser" hint="Open browser when server starts" />
            </section>
            <div class="border-t border-huginn-border" />
            <section class="space-y-3">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Runtime Status</h3>
              <div class="space-y-2">
                <div v-for="(val, key) in runtimeStatus" :key="key"
                  class="flex items-center gap-3 px-3 py-2 rounded-lg bg-huginn-surface/50 border border-huginn-border">
                  <span class="text-xs text-huginn-muted w-28 flex-shrink-0">{{ key }}</span>
                  <span class="text-xs text-huginn-text font-mono truncate">{{ val }}</span>
                </div>
              </div>
            </section>
          </div>

          <!-- ── Integrations ──────────────────────────────────── -->
          <div v-if="activeTab === 'integrations'" class="space-y-6">
            <p class="text-xs text-huginn-muted">OAuth credentials for external service integrations. Leave blank to disable.</p>
            <section v-for="p in integrationProviders" :key="p.key" class="space-y-3">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">{{ p.label }}</h3>
              <div class="grid grid-cols-2 gap-3">
                <FieldRow label="Client ID" compact>
                  <input v-model="form[`${p.key}_client_id` as keyof typeof form]" @input="dirty = true"
                    :placeholder="p.key + '-client-id'" class="field-input font-mono text-xs" />
                </FieldRow>
                <FieldRow label="Client Secret" compact>
                  <input v-model="form[`${p.key}_client_secret` as keyof typeof form]" @input="dirty = true"
                    type="password" placeholder="••••••••" class="field-input font-mono text-xs" />
                </FieldRow>
              </div>
            </section>
          </div>

          <!-- ── MCP Servers ────────────────────────────────────── -->
          <div v-if="activeTab === 'mcp'" class="space-y-6">
            <p class="text-xs text-huginn-muted">Model Context Protocol servers provide external tools and data to your agents.</p>

            <!-- Existing servers -->
            <section v-if="mcpServers.length > 0" class="space-y-3">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Configured Servers</h3>
              <div class="space-y-2">
                <div v-for="(srv, idx) in mcpServers" :key="srv.name"
                  class="px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50">
                  <div class="flex items-start justify-between gap-3">
                    <div class="flex-1 min-w-0 space-y-0.5">
                      <p class="text-xs font-medium text-huginn-text font-mono">{{ srv.name }}</p>
                      <p class="text-[11px] text-huginn-muted">
                        <span class="px-1.5 py-0.5 rounded border border-huginn-border text-[10px]">{{ srv.transport }}</span>
                        <span class="ml-2 font-mono truncate">{{ srv.command || srv.url || '' }}</span>
                      </p>
                      <div v-if="srv.env && Object.keys(srv.env).length > 0" class="text-[10px] text-huginn-muted/70 font-mono space-y-0.5 mt-1">
                        <div v-for="(val, key) in srv.env" :key="key">{{ key }}={{ val }}</div>
                      </div>
                    </div>
                    <button @click="removeMcpServer(idx)"
                      class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-colors flex-shrink-0">
                      Remove
                    </button>
                  </div>
                </div>
              </div>
            </section>

            <div v-if="mcpServers.length === 0" class="py-4 text-center">
              <p class="text-huginn-muted text-xs">No MCP servers configured.</p>
            </div>

            <div class="border-t border-huginn-border" />

            <!-- Add server form -->
            <section class="space-y-4">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Add Server</h3>
              <FieldRow label="Name" hint="Unique identifier">
                <input v-model="newMcp.name" placeholder="my-mcp-server" class="field-input font-mono text-xs" />
              </FieldRow>
              <FieldRow label="Transport" hint="Connection method">
                <div class="relative">
                  <select v-model="newMcp.transport" class="field-select">
                    <option value="stdio">stdio (subprocess)</option>
                    <option value="sse">sse (HTTP Server-Sent Events)</option>
                    <option value="http">http (streamable HTTP)</option>
                  </select>
                  <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
                </div>
              </FieldRow>
              <FieldRow v-if="newMcp.transport === 'stdio'" label="Command" hint="Executable path">
                <input v-model="newMcp.command" placeholder="/usr/local/bin/mcp-server" class="field-input font-mono text-xs" />
              </FieldRow>
              <FieldRow v-if="newMcp.transport === 'stdio'" label="Args" hint="One arg per line">
                <textarea v-model="newMcp.argsText" rows="3" placeholder="--port&#10;8080"
                  class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y" />
              </FieldRow>
              <FieldRow v-if="newMcp.transport !== 'stdio'" label="URL" hint="Server URL (https://)">
                <input v-model="newMcp.url" placeholder="https://my-mcp-server.example.com" class="field-input font-mono text-xs" />
              </FieldRow>
              <FieldRow label="Environment variables" hint="KEY=VALUE pairs, one per line. Secret values are redacted in display.">
                <textarea v-model="newMcp.envText" rows="4" placeholder="MY_API_TOKEN=sk-...&#10;BASE_URL=https://api.example.com"
                  class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y" />
              </FieldRow>
              <p v-if="mcpAddError" class="text-xs text-huginn-red">{{ mcpAddError }}</p>
              <button @click="addMcpServer"
                class="px-4 py-2 rounded-lg text-xs font-medium border border-huginn-green/30 text-huginn-green hover:bg-huginn-green/10 transition-all">
                Add server
              </button>
            </section>
          </div>

          <!-- ── About ──────────────────────────────────────────────── -->
          <div v-if="activeTab === 'about'" class="space-y-6" data-testid="settings-about-panel">
            <p class="text-xs text-huginn-muted">Build information for the running Huginn instance. Useful for confirming an upgrade has taken effect.</p>

            <section class="space-y-3">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Application</h3>
              <div class="rounded-xl border border-huginn-border bg-huginn-surface/40 divide-y divide-huginn-border">
                <div class="flex items-center justify-between px-4 py-3">
                  <span class="text-xs text-huginn-muted">Name</span>
                  <span class="text-xs text-huginn-text">Huginn</span>
                </div>
                <div class="flex items-center justify-between px-4 py-3">
                  <span class="text-xs text-huginn-muted">Version</span>
                  <span class="text-xs font-mono text-huginn-text" data-testid="settings-version-value">{{ versionLabel }}</span>
                </div>
              </div>
            </section>
          </div>

        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, defineComponent, h } from 'vue'
import { api } from '../composables/useApi'
import { useConfig, type MCPServer } from '../composables/useConfig'
import { useVersion } from '../composables/useVersion'

const { config, loading, externallyChanged, loadConfig, saveConfig } = useConfig()
// useVersion returns the build version (cached app-wide). The composable
// also fires a fetch on App.vue mount, so by the time Settings renders the
// label is usually already populated; calling loadVersion again here is a
// safe no-op if the user navigates straight to Settings before App.vue
// finishes its onMounted hook.
const { versionLabel, loadVersion } = useVersion()

// ── Sub-components ───────────────────────────────────────────────────
const FieldRow = defineComponent({
  props: { label: String, hint: String, compact: Boolean },
  setup(props, { slots }) {
    return () => h('div', { class: props.compact ? 'space-y-1' : 'space-y-1.5' }, [
      h('div', { class: 'flex items-center justify-between' }, [
        h('label', { class: 'text-xs text-huginn-muted' }, props.label),
        props.hint ? h('span', { class: 'text-[11px] text-huginn-muted/60 max-w-xs text-right' }, props.hint) : null,
      ]),
      slots.default?.(),
    ])
  },
})

const ToggleRow = defineComponent({
  props: { modelValue: Boolean, label: String, hint: String },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    return () => h('div', { class: 'flex items-center justify-between' }, [
      h('div', [
        h('p', { class: 'text-xs text-huginn-text' }, props.label),
        props.hint ? h('p', { class: 'text-[11px] text-huginn-muted mt-0.5' }, props.hint) : null,
      ]),
      h('button', {
        onClick: () => { emit('update:modelValue', !props.modelValue); emit('change') },
        class: `relative w-9 h-5 rounded-full transition-colors duration-200 ${props.modelValue ? 'bg-huginn-blue' : 'bg-huginn-border'}`,
      }, [
        h('div', {
          class: `absolute top-0.5 w-4 h-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${props.modelValue ? 'translate-x-4' : 'translate-x-0.5'}`,
        }),
      ]),
    ])
  },
})

// ── State ─────────────────────────────────────────────────────────────
const activeTab = ref('general')
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

// Simple inline SVG icon components for the sidebar
const IconGeneral = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('circle', { cx: '12', cy: '12', r: '3' }),
  h('path', { d: 'M19.07 4.93a10 10 0 010 14.14M4.93 4.93a10 10 0 000 14.14' }),
]) })
const IconTools = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('path', { d: 'M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z' }),
]) })
const IconWebUI = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('rect', { x: '3', y: '3', width: '18', height: '18', rx: '2' }),
  h('path', { d: 'M3 9h18' }),
  h('path', { d: 'M9 21V9' }),
]) })
const IconIntegrations = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('path', { d: 'M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71' }),
  h('path', { d: 'M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71' }),
]) })
const IconMCP = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('rect', { x: '2', y: '3', width: '20', height: '14', rx: '2' }),
  h('path', { d: 'M8 21h8M12 17v4' }),
  h('path', { d: 'M7 8h3m4 0h3' }),
]) })
const IconAbout = defineComponent({ render: () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round' }, [
  h('circle', { cx: '12', cy: '12', r: '10' }),
  h('path', { d: 'M12 16v-4' }),
  h('path', { d: 'M12 8h.01' }),
]) })

const tabs = [
  { id: 'general', label: 'General', icon: IconGeneral },
  { id: 'tools', label: 'Tools', icon: IconTools },
  { id: 'webui', label: 'Web UI', icon: IconWebUI },
  { id: 'integrations', label: 'Integrations', icon: IconIntegrations },
  { id: 'mcp', label: 'MCP Servers', icon: IconMCP },
  { id: 'about', label: 'About', icon: IconAbout },
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

function syncToolsFromText() {
  form.value.allowed_tools = allowedToolsText.value.split('\n').map(s => s.trim()).filter(Boolean)
  form.value.disallowed_tools = disallowedToolsText.value.split('\n').map(s => s.trim()).filter(Boolean)
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
  ])
  populateForm(cfg as unknown as Record<string, unknown>)
})
</script>

<style scoped>
.field-input {
  @apply w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors;
}
.field-select {
  @apply w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer;
}
</style>
