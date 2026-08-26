<template>
  <p
    :data-testid="testId"
    class="text-[10px] leading-snug"
    :class="boxed
      ? 'px-3 py-2 rounded-lg border border-huginn-yellow/40 bg-huginn-yellow/10 text-huginn-yellow'
      : 'text-huginn-yellow/90'"
  >
    {{ MODEL_TOOL_WARNING }}
  </p>
</template>

<script setup lang="ts">
import { MODEL_TOOL_WARNING } from '../views/agents/modelToolCapabilities'

withDefaults(defineProps<{
  testId?: string
  boxed?: boolean
}>(), {
  testId: 'model-tools-warning',
  boxed: false,
})
</script>
