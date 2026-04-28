<template>
  <div class="space-y-6">
    <p class="text-xs text-huginn-muted">OAuth credentials for external service integrations. Leave blank to disable.</p>
    <section v-for="p in integrationProviders" :key="p.key" class="space-y-3">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">{{ p.label }}</h3>
      <div class="grid grid-cols-2 gap-3">
        <SettingsFieldRow label="Client ID" compact>
          <input
            v-model="form[`${p.key}_client_id`]"
            :placeholder="p.key + '-client-id'"
            class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono"
            @input="$emit('markDirty')"
          />
        </SettingsFieldRow>
        <SettingsFieldRow label="Client Secret" compact>
          <input
            v-model="form[`${p.key}_client_secret`]"
            type="password"
            placeholder="••••••••"
            class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono"
            @input="$emit('markDirty')"
          />
        </SettingsFieldRow>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import SettingsFieldRow from './SettingsFieldRow.vue'

defineProps<{
  form: Record<string, unknown>
  integrationProviders: Array<{ key: string; label: string }>
}>()

defineEmits<{
  (e: 'markDirty'): void
}>()
</script>
