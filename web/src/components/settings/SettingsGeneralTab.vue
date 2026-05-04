<template>
  <div class="space-y-6">
    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">General</h3>
      <SettingsFieldRow label="Workspace Path" hint="Default working directory for new sessions">
        <input
          v-model="form.workspace_path"
          placeholder="~/projects or /absolute/path"
          class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors"
          @input="$emit('markDirty')"
        />
      </SettingsFieldRow>
      <SettingsFieldRow label="Max Turns" hint="Max agentic loop iterations (default 50)">
        <input
          v-model.number="form.max_turns"
          type="number"
          min="1"
          max="500"
          class="w-24 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors"
          @input="$emit('markDirty')"
        />
      </SettingsFieldRow>
      <SettingsFieldRow label="Bash Timeout" hint="Seconds before a shell command times out">
        <div class="flex items-center gap-2">
          <input
            v-model.number="form.bash_timeout_secs"
            type="number"
            min="5"
            max="3600"
            class="w-24 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors"
            @input="$emit('markDirty')"
          />
          <span class="text-xs text-huginn-muted">seconds</span>
        </div>
      </SettingsFieldRow>
      <SettingsFieldRow label="Context Limit" hint="Max context window in kilobytes">
        <div class="flex items-center gap-2">
          <input
            v-model.number="form.context_limit_kb"
            type="number"
            min="1"
            max="2048"
            class="w-24 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors"
            @input="$emit('markDirty')"
          />
          <span class="text-xs text-huginn-muted">KB</span>
        </div>
      </SettingsFieldRow>
      <SettingsFieldRow label="Diff Review Mode" hint="When to pause and show a diff for approval">
        <div class="relative">
          <select
            v-model="form.diff_review_mode"
            class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer"
            @change="$emit('markDirty')"
          >
            <option value="auto">Auto</option>
            <option value="always">Always</option>
            <option value="never">Never</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
      <SettingsFieldRow label="Compact Mode" hint="Auto-compact conversation history">
        <div class="relative">
          <select
            v-model="form.compact_mode"
            class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer"
            @change="$emit('markDirty')"
          >
            <option value="auto">Auto</option>
            <option value="always">Always</option>
            <option value="never">Never</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
    </section>
    <div class="border-t border-huginn-border" />
    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Diagnostics</h3>
      <SettingsFieldRow label="Log Level" hint="Changes take effect immediately. Use Debug to capture diagnostics.">
        <div class="relative">
          <select
            v-model="logLevel"
            class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer"
            :disabled="logLevelLoading"
            @change="applyLogLevel"
          >
            <option value="error">Error (least verbose)</option>
            <option value="warn">Warn (default)</option>
            <option value="info">Info</option>
            <option value="debug">Debug (most verbose)</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
    </section>
    <div class="border-t border-huginn-border" />
    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Behavior Flags</h3>
      <SettingsToggleRow
        :model-value="!!form.git_stage_on_write"
        label="Git stage on write"
        hint="Auto-stage files after each write"
        @update:model-value="setBoolean('git_stage_on_write', $event)"
      />
      <SettingsToggleRow
        :model-value="!!form.notepads_enabled"
        label="Notepads"
        hint="Enable persistent note-taking tools"
        @update:model-value="setBoolean('notepads_enabled', $event)"
      />
      <SettingsToggleRow
        :model-value="!!form.vision_enabled"
        label="Vision"
        hint="Enable image understanding in prompts"
        @update:model-value="setBoolean('vision_enabled', $event)"
      />
      <SettingsToggleRow
        :model-value="!!form.semantic_search"
        label="Semantic search"
        hint="Use embeddings for smarter file search"
        @update:model-value="setBoolean('semantic_search', $event)"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../composables/useApi'
import SettingsFieldRow from './SettingsFieldRow.vue'
import SettingsToggleRow from './SettingsToggleRow.vue'

const props = defineProps<{
  form: Record<string, unknown>
}>()

const emit = defineEmits<{
  (e: 'markDirty'): void
}>()

// ── Log Level state ────────────────────────────────────────────────────
const logLevel = ref('warn')
const logLevelLoading = ref(false)

onMounted(async () => {
  try {
    const res = await api.logLevel.get()
    logLevel.value = res.level.toLowerCase()
  } catch {
    // silently fall back to default
  }
})

async function applyLogLevel() {
  logLevelLoading.value = true
  try {
    await api.logLevel.set(logLevel.value)
  } catch {
    // silently ignore — the select already shows the intended value
  } finally {
    logLevelLoading.value = false
  }
}

// ── Helpers ────────────────────────────────────────────────────────────
function setBoolean(key: string, value: boolean) {
  props.form[key] = value
  emit('markDirty')
}
</script>
