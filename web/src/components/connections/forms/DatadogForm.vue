<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Datadog Site</label>
      <select v-model="form.site" data-testid="field-site" class="field" :disabled="testing || saving">
        <option v-for="s in DATADOG_SITES" :key="s.value" :value="s.value">{{ s.label }}</option>
      </select>
    </div>
    <div v-if="form.site === 'custom'">
      <label class="text-[10px] text-huginn-muted mb-1 block">Custom URL</label>
      <input
        v-model="form.customUrl"
        data-testid="field-custom-url"
        class="field"
        placeholder="https://api.custom.datadoghq.com"
        :disabled="testing || saving"
      />
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">API Key</label>
        <input
          v-model="form.apiKey"
          type="password"
          data-testid="field-api-key"
          class="field"
          placeholder="••••••••"
          :disabled="testing || saving"
        />
      </div>
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Application Key</label>
        <input
          v-model="form.appKey"
          type="password"
          data-testid="field-app-key"
          class="field"
          placeholder="••••••••"
          :disabled="testing || saving"
        />
      </div>
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input
        v-model="form.label"
        data-testid="field-label"
        class="field"
        placeholder="e.g. prod-us1"
        :disabled="testing || saving"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineProps<{ testing: boolean; saving: boolean }>()

const DATADOG_SITES = [
  { label: 'US1 (us1.datadoghq.com)',  value: 'https://api.datadoghq.com' },
  { label: 'US3 (us3.datadoghq.com)',  value: 'https://api.us3.datadoghq.com' },
  { label: 'US5 (us5.datadoghq.com)',  value: 'https://api.us5.datadoghq.com' },
  { label: 'EU1 (datadoghq.eu)',        value: 'https://api.datadoghq.eu' },
  { label: 'AP1 (ap1.datadoghq.com)',  value: 'https://api.ap1.datadoghq.com' },
  { label: 'FED (ddog-gov.com)',        value: 'https://api.ddog-gov.com' },
  { label: 'Custom URL…',              value: 'custom' },
]

const form = reactive({
  site:      'https://api.datadoghq.com',
  customUrl: '',
  apiKey:    '',
  appKey:    '',
  label:     '',
})

defineExpose({
  getPayload: () => ({
    url:     form.site === 'custom' ? form.customUrl : form.site,
    api_key: form.apiKey,
    app_key: form.appKey,
    label:   form.label,
  }),
})
</script>
