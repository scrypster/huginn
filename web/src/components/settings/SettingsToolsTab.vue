<template>
  <div class="space-y-6">
    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Tool Access</h3>
      <SettingsToggleRow
        :model-value="!!form.tools_enabled"
        label="Tools enabled"
        hint="Allow huginn to use tools (file read/write, bash, etc.)"
        @update:model-value="setBoolean('tools_enabled', $event)"
      />
    </section>

    <div class="border-t border-huginn-border" />

    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Allowed Tools</h3>
      <p class="text-xs text-huginn-muted">Whitelist — empty means all tools allowed. One tool name per line.</p>
      <textarea
        :value="allowedToolsText"
        placeholder="read_file&#10;write_file&#10;bash"
        rows="6"
        class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y"
        @input="updateAllowed(($event.target as HTMLTextAreaElement).value)"
      />
    </section>

    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Disallowed Tools</h3>
      <p class="text-xs text-huginn-muted">Blacklist — tools that are always blocked.</p>
      <textarea
        :value="disallowedToolsText"
        placeholder="bash&#10;web_search"
        rows="4"
        class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y"
        @input="updateDisallowed(($event.target as HTMLTextAreaElement).value)"
      />
    </section>

    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Web Search</h3>
      <SettingsFieldRow label="Brave API Key" hint="Required for web_search tool">
        <div class="relative">
          <input
            v-model="form.brave_api_key"
            :type="showBraveKey ? 'text' : 'password'"
            placeholder="BSA..."
            class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-10 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono"
            @input="$emit('markDirty')"
          />
          <button
            class="absolute right-2.5 top-1/2 -translate-y-1/2 text-huginn-muted hover:text-huginn-text transition-colors"
            @click="$emit('update:showBraveKey', !showBraveKey)"
          >
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path v-if="!showBraveKey" d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle v-if="!showBraveKey" cx="12" cy="12" r="3" />
              <path v-if="showBraveKey" d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24" />
              <line v-if="showBraveKey" x1="1" y1="1" x2="23" y2="23" />
            </svg>
          </button>
        </div>
      </SettingsFieldRow>
    </section>
  </div>
</template>

<script setup lang="ts">
import SettingsFieldRow from './SettingsFieldRow.vue'
import SettingsToggleRow from './SettingsToggleRow.vue'

const props = defineProps<{
  form: Record<string, unknown>
  allowedToolsText: string
  disallowedToolsText: string
  showBraveKey: boolean
}>()

const emit = defineEmits<{
  (e: 'markDirty'): void
  (e: 'syncTools'): void
  (e: 'update:allowedToolsText', value: string): void
  (e: 'update:disallowedToolsText', value: string): void
  (e: 'update:showBraveKey', value: boolean): void
}>()

function setBoolean(key: string, value: boolean) {
  props.form[key] = value
  emit('markDirty')
}

function updateAllowed(value: string) {
  emit('update:allowedToolsText', value)
  emit('markDirty')
  emit('syncTools')
}

function updateDisallowed(value: string) {
  emit('update:disallowedToolsText', value)
  emit('markDirty')
  emit('syncTools')
}
</script>
