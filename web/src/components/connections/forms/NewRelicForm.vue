<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">API Key</label>
      <input v-model="form.apiKey" type="password" data-testid="field-api-key" class="field" placeholder="NRAK-…" :disabled="testing || saving" />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Account ID</label>
      <input v-model="form.accountId" data-testid="field-account-id" class="field" placeholder="1234567" :disabled="testing || saving" />
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
const form = reactive({ apiKey: '', accountId: '', label: '' })
defineExpose({ getPayload: () => ({ api_key: form.apiKey, account_id: form.accountId, label: form.label }) })
</script>
