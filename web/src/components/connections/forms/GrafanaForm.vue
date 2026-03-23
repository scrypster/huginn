<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Base URL</label>
      <input v-model="form.baseUrl" data-testid="field-base-url" class="field" placeholder="https://yourorg.grafana.net" :disabled="testing || saving" />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">API Key</label>
      <input v-model="form.apiKey" type="password" data-testid="field-api-key" class="field" placeholder="••••••••" :disabled="testing || saving" />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input v-model="form.label" data-testid="field-label" class="field" placeholder="e.g. prod" :disabled="testing || saving" />
    </div>
  </div>
</template>
<script setup lang="ts">
import { reactive } from 'vue'
defineProps<{ testing: boolean; saving: boolean }>()
const form = reactive({ baseUrl: '', apiKey: '', label: '' })
defineExpose({ getPayload: () => ({ url: form.baseUrl, token: form.apiKey, label: form.label }) })
</script>
