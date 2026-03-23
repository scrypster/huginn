<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Instance URL</label>
      <input v-model="form.instanceUrl" data-testid="field-instance-url" class="field" placeholder="https://yourorg.service-now.com" :disabled="testing || saving" />
    </div>
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Username</label>
        <input v-model="form.username" data-testid="field-username" class="field" :disabled="testing || saving" />
      </div>
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Password</label>
        <input v-model="form.password" type="password" data-testid="field-password" class="field" placeholder="••••••••" :disabled="testing || saving" />
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
const form = reactive({ instanceUrl: '', username: '', password: '', label: '' })
defineExpose({
  getPayload: () => ({
    instance_url: form.instanceUrl,
    username:     form.username,
    password:     form.password,
    label:        form.label,
  }),
})
</script>
