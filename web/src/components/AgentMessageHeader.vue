<template>
  <div class="flex items-center gap-1.5 mb-1">
    <!-- Agent initial chip -->
    <span
      class="w-4 h-4 rounded text-[10px] font-bold flex items-center justify-center flex-shrink-0 select-none"
      :style="`background:${color}22;color:${color}`"
    >{{ initial }}</span>
    <!-- Agent name -->
    <span class="text-xs font-semibold" :style="`color:${color}`">{{ agentName }}</span>
    <!-- Timestamp -->
    <span class="text-[11px] text-huginn-muted/60">{{ formattedTime }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const PALETTE = ['#58A6FF', '#3FB950', '#FF7B72', '#D2A8FF', '#FFA657', '#79C0FF']

function agentColor(name: string): string {
  let h = 0
  for (const c of name) h = (Math.imul(31, h) + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]!
}

const props = defineProps<{
  agentName: string
  createdAt?: string
}>()

const color = computed(() => agentColor(props.agentName))

const initial = computed(() => (props.agentName?.[0] ?? '?').toUpperCase())

const formattedTime = computed(() => {
  if (!props.createdAt) return 'just now'
  const d = new Date(props.createdAt)
  if (isNaN(d.getTime())) return 'just now'
  const now = Date.now()
  const diffMs = now - d.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return 'just now'
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return d.toLocaleDateString()
})
</script>
