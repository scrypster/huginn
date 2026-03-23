<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Instance URL</label>
      <input
        v-model="form.instanceUrl"
        data-testid="field-instance-url"
        class="field"
        placeholder="https://yourorg.atlassian.net"
        :disabled="testing || saving"
      />
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Email</label>
        <input
          v-model="form.email"
          data-testid="field-email"
          class="field"
          placeholder="you@example.com"
          :disabled="testing || saving"
        />
      </div>
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">API Token</label>
        <input
          v-model="form.token"
          type="password"
          data-testid="field-token"
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
        placeholder="e.g. prod"
        :disabled="testing || saving"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineProps<{ testing: boolean; saving: boolean }>()

const form = reactive({ instanceUrl: '', email: '', token: '', label: '' })

defineExpose({
  getPayload: () => ({
    instance_url: form.instanceUrl,
    email: form.email,
    token: form.token,
    label: form.label,
  }),
})
</script>
