<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Base URL</label>
      <input
        v-model="form.baseUrl"
        data-testid="field-base-url"
        class="field"
        placeholder="https://gitlab.com"
        :disabled="testing || saving"
      />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Personal Access Token</label>
      <input
        v-model="form.token"
        type="password"
        data-testid="field-token"
        class="field"
        placeholder="glpat-…"
        :disabled="testing || saving"
      />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input
        v-model="form.label"
        data-testid="field-label"
        class="field"
        placeholder="e.g. self-hosted"
        :disabled="testing || saving"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineProps<{ testing: boolean; saving: boolean }>()

const form = reactive({ baseUrl: 'https://gitlab.com', token: '', label: '' })

defineExpose({
  getPayload: () => ({ base_url: form.baseUrl, token: form.token, label: form.label }),
})
</script>
