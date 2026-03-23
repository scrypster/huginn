<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Subdomain</label>
      <input v-model="form.subdomain" data-testid="field-subdomain" class="field" placeholder="yourcompany" :disabled="testing || saving" />
      <p class="text-[10px] text-huginn-muted mt-1">The part before .zendesk.com in your URL</p>
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Email</label>
      <input v-model="form.email" type="email" data-testid="field-email" class="field" placeholder="you@example.com" :disabled="testing || saving" />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">API Token</label>
      <input v-model="form.token" type="password" data-testid="field-token" class="field" placeholder="••••••••" :disabled="testing || saving" />
      <p class="text-[10px] text-huginn-muted mt-1">Find your token at Admin Center → Apps and Integrations → APIs → Zendesk API</p>
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input v-model="form.label" data-testid="field-label" class="field" placeholder="e.g. support" :disabled="testing || saving" />
    </div>
  </div>
</template>
<script setup lang="ts">
import { reactive } from 'vue'
defineProps<{ testing: boolean; saving: boolean }>()
const form = reactive({ subdomain: '', email: '', token: '', label: '' })
defineExpose({ getPayload: () => ({ subdomain: form.subdomain, email: form.email, token: form.token, label: form.label }) })
</script>
