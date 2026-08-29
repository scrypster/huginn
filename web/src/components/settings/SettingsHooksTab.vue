<template>
  <div class="space-y-6">
    <section class="space-y-2">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Hooks</h3>
      <p class="text-xs text-huginn-muted leading-relaxed">
        Hooks run a shell command you define before ("PreToolUse") or after ("PostToolUse") any tool call.
        A PreToolUse hook that exits non-zero blocks the tool and its output becomes the reason shown to the model.
        A PostToolUse hook only observes — it can never fail or change the tool call. <strong class="text-huginn-text font-medium">These run real shell commands on this machine with your permissions</strong> —
        only add one you wrote or reviewed yourself. Every run is recorded below in the audit log.
      </p>
    </section>

    <div v-if="loadError" class="px-4 py-2.5 rounded-xl border border-huginn-red/40 text-huginn-red bg-huginn-red/8 text-xs">
      {{ loadError }}
    </div>

    <section class="space-y-3">
      <div class="flex items-center justify-between">
        <h4 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Configured Hooks</h4>
        <button
          data-testid="hooks-reload"
          class="px-2.5 py-1 text-[11px] font-medium rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-colors"
          :disabled="reloading"
          @click="reload"
        >
          {{ reloading ? 'Reloading…' : 'Reload from disk' }}
        </button>
      </div>

      <div v-if="loading" class="py-4 text-center text-xs text-huginn-muted">Loading…</div>

      <div v-else-if="hooks.length === 0" class="py-4 text-center">
        <p class="text-huginn-muted text-xs">No hooks configured. Hooks are off until you add one.</p>
      </div>

      <div v-else class="space-y-2">
        <div
          v-for="h in hooks"
          :key="`${h.scope}:${h.id}`"
          data-testid="hook-row"
          class="px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="flex-1 min-w-0 space-y-1">
              <div class="flex items-center gap-2 flex-wrap">
                <p class="text-xs font-medium text-huginn-text font-mono">{{ h.id }}</p>
                <span class="px-1.5 py-0.5 rounded border border-huginn-border text-[10px] text-huginn-muted">{{ h.event }}</span>
                <span class="px-1.5 py-0.5 rounded border border-huginn-border text-[10px] text-huginn-muted">{{ h.scope }}</span>
                <span v-if="!h.enabled" class="px-1.5 py-0.5 rounded border border-huginn-yellow/40 text-[10px] text-huginn-yellow">disabled</span>
              </div>
              <p class="text-[11px] text-huginn-muted">
                tools: <span class="font-mono">{{ h.match.tools.join(', ') }}</span>
              </p>
              <p class="text-[11px] text-huginn-muted font-mono truncate">{{ h.action.command }}</p>
            </div>
            <div class="flex flex-col items-end gap-1.5 flex-shrink-0">
              <label class="flex items-center gap-1.5 text-[11px] text-huginn-muted cursor-pointer">
                <input
                  type="checkbox"
                  :checked="h.enabled"
                  data-testid="hook-toggle"
                  @change="toggleEnabled(h)"
                />
                enabled
              </label>
              <div class="flex gap-1.5">
                <button
                  class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-colors"
                  @click="startEdit(h)"
                >
                  Edit
                </button>
                <button
                  class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-blue/30 text-huginn-blue hover:bg-huginn-blue/10 transition-colors"
                  @click="testRun(h)"
                >
                  Test
                </button>
                <button
                  data-testid="hook-delete"
                  class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-colors"
                  @click="remove(h)"
                >
                  Delete
                </button>
              </div>
            </div>
          </div>
          <div v-if="testResults[hookKey(h)]" class="mt-2 pt-2 border-t border-huginn-border text-[11px] font-mono">
            <span :class="testResults[hookKey(h)]!.allowed ? 'text-huginn-green' : 'text-huginn-red'">
              {{ testResults[hookKey(h)]!.allowed ? 'allowed' : 'blocked' }} (exit {{ testResults[hookKey(h)]!.exit_code }})
            </span>
            <pre v-if="testResults[hookKey(h)]!.output" class="whitespace-pre-wrap text-huginn-muted mt-1">{{ testResults[hookKey(h)]!.output }}</pre>
          </div>
        </div>
      </div>
    </section>

    <div class="border-t border-huginn-border" />

    <section class="space-y-4">
      <div class="flex items-center justify-between">
        <h4 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">
          {{ editingID ? 'Edit Hook' : 'Add Hook' }}
        </h4>
        <button
          data-testid="hooks-copy-schema"
          class="px-2.5 py-1 text-[11px] font-medium rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-colors"
          @click="copySchema"
        >
          {{ copied ? 'Copied!' : 'Copy schema example' }}
        </button>
      </div>
      <p class="text-[11px] text-huginn-muted leading-relaxed">
        An agent with file-write access can author this same JSON directly into
        <code class="font-mono">.huginn/hooks.json</code> (workspace) or
        <code class="font-mono">~/.huginn/hooks.json</code> (global) — copy the schema above to hand it one.
      </p>

      <SettingsFieldRow label="ID" hint="Unique, stable identifier">
        <input v-model="draft.id" :disabled="!!editingID" placeholder="block-force-push" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono disabled:opacity-60" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Event" hint="When the hook runs">
        <div class="relative">
          <select v-model="draft.event" class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer">
            <option value="PreToolUse">PreToolUse (can block)</option>
            <option value="PostToolUse">PostToolUse (observe only)</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
      <SettingsFieldRow label="Tools" hint='Comma-separated tool names or globs — "*" for all'>
        <input v-model="toolsText" placeholder="bash, write_*" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Command" hint="Shell command; receives the tool call as JSON on stdin and via HUGINN_TOOL / HUGINN_TOOL_ARGS env vars">
        <textarea v-model="draft.action.command" rows="3" placeholder='[ "$HUGINN_TOOL" = bash ] &amp;&amp; exit 1 || exit 0' class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Timeout (seconds)" hint="Default 30, max 300">
        <input v-model.number="draft.action.timeout_secs" type="number" min="1" max="300" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Scope" hint="Where hooks.json is written">
        <div class="relative">
          <select v-model="draft.scope" class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer">
            <option value="workspace">workspace (.huginn/hooks.json)</option>
            <option value="global">global (~/.huginn/hooks.json)</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
      <label class="flex items-center gap-2 text-xs text-huginn-text cursor-pointer">
        <input v-model="draft.enabled" type="checkbox" />
        Enabled
      </label>

      <p v-if="formError" class="text-xs text-huginn-red">{{ formError }}</p>
      <div class="flex gap-2">
        <button
          data-testid="hooks-save"
          class="px-4 py-2 rounded-lg text-xs font-medium border border-huginn-green/30 text-huginn-green hover:bg-huginn-green/10 transition-all disabled:opacity-50"
          :disabled="saving"
          @click="save"
        >
          {{ saving ? 'Saving…' : editingID ? 'Save changes' : 'Add hook' }}
        </button>
        <button
          v-if="editingID"
          class="px-4 py-2 rounded-lg text-xs font-medium border border-huginn-border text-huginn-muted hover:bg-huginn-surface transition-all"
          @click="cancelEdit"
        >
          Cancel
        </button>
      </div>
    </section>

    <div class="border-t border-huginn-border" />

    <section class="space-y-3">
      <h4 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Recent Executions</h4>
      <div v-if="auditEntries.length === 0" class="text-xs text-huginn-muted py-2">No hooks have run yet.</div>
      <div v-else class="space-y-1.5 max-h-64 overflow-y-auto">
        <div
          v-for="(e, i) in auditEntries"
          :key="i"
          class="px-3 py-1.5 rounded-lg border border-huginn-border bg-huginn-surface/40 text-[11px] font-mono flex items-center gap-2"
        >
          <span :class="e.vetoed ? 'text-huginn-red' : 'text-huginn-muted'">{{ e.vetoed ? 'VETO' : 'ok' }}</span>
          <span class="text-huginn-text">{{ e.hook_id }}</span>
          <span class="text-huginn-muted">{{ e.event }} / {{ e.tool }}</span>
          <span class="text-huginn-muted ml-auto">exit {{ e.exit_code }}</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import SettingsFieldRow from './SettingsFieldRow.vue'
import { api } from '../../composables/useApi'
import type { HookEntry, HookAuditEntry, HookTestResult } from '../../composables/useApi'

const hooks = ref<HookEntry[]>([])
const auditEntries = ref<HookAuditEntry[]>([])
const loading = ref(true)
const loadError = ref('')
const reloading = ref(false)
const saving = ref(false)
const formError = ref('')
const copied = ref(false)
const editingID = ref<string | null>(null)
const editingScope = ref<'global' | 'workspace' | null>(null)
const testResults = ref<Record<string, HookTestResult>>({})

const SCHEMA_EXAMPLE = `{
  "hooks": [
    {
      "id": "block-force-push",
      "event": "PreToolUse",
      "match": { "tools": ["bash"] },
      "action": {
        "type": "command",
        "command": "case \\"$HUGINN_TOOL_ARGS\\" in *--force*) echo 'force push blocked' 1>&2; exit 1;; esac",
        "timeout_secs": 10
      },
      "enabled": true
    }
  ]
}`

function emptyDraft(): HookEntry {
  return {
    id: '',
    event: 'PreToolUse',
    match: { tools: [] },
    action: { type: 'command', command: '', timeout_secs: 30 },
    enabled: true,
    scope: 'workspace',
  }
}

const draft = ref<HookEntry>(emptyDraft())
const toolsText = computed({
  get: () => draft.value.match.tools.join(', '),
  set: (v: string) => {
    draft.value.match.tools = v.split(',').map(s => s.trim()).filter(Boolean)
  },
})

function hookKey(h: HookEntry) {
  return `${h.scope}:${h.id}`
}

async function loadHooks() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await api.hooks.list()
    hooks.value = res.hooks
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Failed to load hooks'
  } finally {
    loading.value = false
  }
  try {
    const auditRes = await api.hooks.audit(50)
    auditEntries.value = auditRes.entries
  } catch {
    // Audit is a nice-to-have; a failure here shouldn't block the hooks list.
  }
}

async function reload() {
  reloading.value = true
  loadError.value = ''
  try {
    await api.hooks.reload()
    await loadHooks()
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Reload failed'
  } finally {
    reloading.value = false
  }
}

function startEdit(h: HookEntry) {
  editingID.value = h.id
  editingScope.value = h.scope ?? 'workspace'
  draft.value = JSON.parse(JSON.stringify(h))
  formError.value = ''
}

function cancelEdit() {
  editingID.value = null
  editingScope.value = null
  draft.value = emptyDraft()
  formError.value = ''
}

async function save() {
  formError.value = ''
  if (!draft.value.id.trim()) { formError.value = 'ID is required'; return }
  if (draft.value.match.tools.length === 0) { formError.value = 'At least one tool (or "*") is required'; return }
  if (!draft.value.action.command.trim()) { formError.value = 'Command is required'; return }
  saving.value = true
  try {
    if (editingID.value) {
      await api.hooks.update(editingID.value, draft.value)
    } else {
      await api.hooks.create(draft.value)
    }
    cancelEdit()
    await loadHooks()
  } catch (e: unknown) {
    formError.value = e instanceof Error ? e.message : 'Save failed'
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(h: HookEntry) {
  const updated: HookEntry = { ...h, enabled: !h.enabled }
  try {
    await api.hooks.update(h.id, updated)
    await loadHooks()
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Failed to toggle hook'
  }
}

async function remove(h: HookEntry) {
  if (!window.confirm(`Delete hook "${h.id}"?`)) return
  try {
    await api.hooks.delete(h.id)
    await loadHooks()
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Failed to delete hook'
  }
}

async function testRun(h: HookEntry) {
  try {
    const firstTool = h.match.tools[0] ?? 'bash'
    const result = await api.hooks.test({ id: h.id, tool: firstTool === '*' ? 'bash' : firstTool, args: {} })
    testResults.value = { ...testResults.value, [hookKey(h)]: result }
  } catch (e: unknown) {
    testResults.value = {
      ...testResults.value,
      [hookKey(h)]: { allowed: false, exit_code: -1, output: '', error: e instanceof Error ? e.message : 'Test failed' },
    }
  }
}

async function copySchema() {
  try {
    await navigator.clipboard.writeText(SCHEMA_EXAMPLE)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    // Clipboard API can be unavailable (permissions, non-secure context) —
    // fail silently, the button just won't show "Copied!".
  }
}

onMounted(loadHooks)
</script>
