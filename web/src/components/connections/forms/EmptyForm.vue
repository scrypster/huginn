<template>
  <div class="text-xs text-huginn-muted py-2">Form coming soon.</div>
</template>

<script setup lang="ts">
defineProps<{ testing: boolean; saving: boolean }>()
defineExpose({ getPayload: () => ({} as Record<string, string>) })
</script>
