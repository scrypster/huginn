<template>
  <div :class="compact ? 'space-y-1' : 'space-y-1.5'">
    <div class="flex items-center justify-between">
      <label class="text-xs text-huginn-muted">{{ label }}</label>
      <span v-if="hint" class="text-[11px] text-huginn-muted/60 max-w-xs text-right">{{ hint }}</span>
    </div>
    <slot />
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label: string
  hint?: string
  compact?: boolean
}>()
</script>
