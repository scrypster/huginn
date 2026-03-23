<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Private App Token</label>
      <input v-model="form.token" type="password" data-testid="field-token" class="field" placeholder="••••••••" :disabled="testing || saving" />
      <p class="text-[10px] text-huginn-muted mt-1">Create a Private App in HubSpot Settings → Integrations → Private Apps</p>
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
const form = reactive({ token: '', label: '' })
defineExpose({ getPayload: () => ({ token: form.token, label: form.label }) })
</script>
