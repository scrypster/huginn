<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Splunk URL</label>
      <input
        v-model="form.url"
        data-testid="field-url"
        class="field"
        placeholder="https://your-org.splunkcloud.com"
        :disabled="testing || saving"
      />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Bearer Token</label>
      <input
        v-model="form.token"
        type="password"
        data-testid="field-token"
        class="field"
        placeholder="••••••••"
        :disabled="testing || saving"
      />
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Label (optional)</label>
      <input
        v-model="form.label"
        data-testid="field-splunk-label"
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

const form = reactive({ url: '', token: '', label: '' })

defineExpose({
  getPayload: () => ({ url: form.url, token: form.token, label: form.label }),
})
</script>
