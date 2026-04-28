<template>
  <div class="flex items-center justify-between">
    <div>
      <p class="text-xs text-huginn-text">{{ label }}</p>
      <p v-if="hint" class="text-[11px] text-huginn-muted mt-0.5">{{ hint }}</p>
    </div>
    <button
      class="relative w-9 h-5 rounded-full transition-colors duration-200"
      :class="modelValue ? 'bg-huginn-blue' : 'bg-huginn-border'"
      @click="$emit('update:modelValue', !modelValue)"
    >
      <div
        class="absolute top-0.5 w-4 h-4 rounded-full bg-white shadow-sm transition-transform duration-200"
        :class="modelValue ? 'translate-x-4' : 'translate-x-0.5'"
      />
    </button>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  modelValue: boolean
  label: string
  hint?: string
}>()

defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()
</script>
