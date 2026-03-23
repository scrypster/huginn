<template>
  <div class="flex flex-col gap-3">
    <div class="grid grid-cols-2 gap-3">
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Endpoint</label>
        <input
          v-model="form.endpoint"
          data-testid="field-endpoint"
          class="field"
          placeholder="http://localhost:8475"
          :disabled="testing || saving"
        />
      </div>
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Username</label>
        <input
          v-model="form.username"
          data-testid="field-username"
          class="field"
          :disabled="testing || saving"
        />
      </div>
    </div>
    <div>
      <label class="text-[10px] text-huginn-muted mb-1 block">Password</label>
      <input
        v-model="form.password"
        type="password"
        data-testid="field-password"
        class="field"
        :disabled="testing || saving"
      />
      <p class="text-[10px] text-huginn-muted/60 mt-1">Default password is <code class="font-mono text-huginn-muted">password</code></p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'

defineProps<{ testing: boolean; saving: boolean }>()

const form = reactive({
  endpoint: 'http://localhost:8475',
  username: 'root',
  password: 'password',
})

defineExpose({
  getPayload: () => ({
    endpoint: form.endpoint,
    username: form.username,
    password: form.password,
  }),
})
</script>
