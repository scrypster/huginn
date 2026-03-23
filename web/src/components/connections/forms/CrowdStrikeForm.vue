<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Base URL</label>
      <input v-model="form.baseUrl" data-testid="field-base-url" class="field" placeholder="https://api.crowdstrike.com" :disabled="testing || saving" />
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Client ID</label>
        <input v-model="form.clientId" data-testid="field-client-id" class="field" placeholder="••••••••" :disabled="testing || saving" />
      </div>
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Client Secret</label>
        <input v-model="form.clientSecret" type="password" data-testid="field-client-secret" class="field" placeholder="••••••••" :disabled="testing || saving" />
      </div>
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
const form = reactive({ baseUrl: 'https://api.crowdstrike.com', clientId: '', clientSecret: '', label: '' })
defineExpose({
  getPayload: () => ({
    base_url:      form.baseUrl,
    client_id:     form.clientId,
    client_secret: form.clientSecret,
    label:         form.label,
  }),
})
</script>
