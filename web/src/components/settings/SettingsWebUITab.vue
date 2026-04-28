<template>
  <div class="space-y-6">
    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Web UI</h3>
      <SettingsToggleRow
        :model-value="!!form.web_ui_enabled"
        label="Enabled"
        hint="Start the web server with 'huginn serve'"
        @update:model-value="setBoolean('web_ui_enabled', $event)"
      />
      <SettingsFieldRow label="Port" hint="0 = dynamic (random available port)">
        <input
          v-model.number="form.web_ui_port"
          type="number"
          min="0"
          max="65535"
          placeholder="0"
          class="w-24 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors"
          @input="$emit('markDirty')"
        />
      </SettingsFieldRow>
      <SettingsFieldRow label="Bind address" hint="Loopback only recommended">
        <input
          v-model="form.web_ui_bind"
          class="w-40 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono"
          placeholder="127.0.0.1"
          @input="$emit('markDirty')"
        />
      </SettingsFieldRow>
      <SettingsToggleRow
        :model-value="!!form.web_ui_auto_open"
        label="Auto-open browser"
        hint="Open browser when server starts"
        @update:model-value="setBoolean('web_ui_auto_open', $event)"
      />
    </section>
    <div class="border-t border-huginn-border" />
    <section class="space-y-3">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Runtime Status</h3>
      <div class="space-y-2">
        <div
          v-for="(val, key) in runtimeStatus"
          :key="key"
          class="flex items-center gap-3 px-3 py-2 rounded-lg bg-huginn-surface/50 border border-huginn-border"
        >
          <span class="text-xs text-huginn-muted w-28 flex-shrink-0">{{ key }}</span>
          <span class="text-xs text-huginn-text font-mono truncate">{{ val }}</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import SettingsFieldRow from './SettingsFieldRow.vue'
import SettingsToggleRow from './SettingsToggleRow.vue'

const props = defineProps<{
  form: Record<string, unknown>
  runtimeStatus: Record<string, unknown>
}>()

const emit = defineEmits<{
  (e: 'markDirty'): void
}>()

function setBoolean(key: string, value: boolean) {
  props.form[key] = value
  emit('markDirty')
}
</script>
