<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Secret Key</label>
      <input
        v-model="form.apiKey"
        type="password"
        data-testid="field-api-key"
        class="field"
        placeholder="sk_live_… or sk_test_…"
        :disabled="testing || saving"
      />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input
        v-model="form.label"
        data-testid="field-label"
        class="field"
        placeholder="e.g. prod"
        :disabled="testing || saving"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineProps<{ testing: boolean; saving: boolean }>()

const form = reactive({ apiKey: '', label: '' })

defineExpose({ getPayload: () => ({ api_key: form.apiKey, label: form.label }) })
</script>
