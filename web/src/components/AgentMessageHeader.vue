<template>
  <div class="flex items-center gap-1.5 mb-1">
    <!-- Agent initial chip -->
    <span
      class="w-4 h-4 rounded text-[10px] font-bold flex items-center justify-center flex-shrink-0 select-none"
      :style="`background:${color}22;color:${color}`"
    >{{ initial }}</span>
    <!-- Agent name — title shows description on hover when available -->
    <span
      class="text-xs font-semibold"
      :style="`color:${color}`"
      :title="agentDescription || undefined"
    >{{ agentName }}</span>
    <!-- Timestamp — title shows absolute clock time on hover -->
    <span class="text-[11px] text-huginn-muted/60" :title="absoluteTime || undefined">{{ formattedTime }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatClockTime, formatRelativeTime } from '../utils/relativeTime'

const PALETTE = ['#58A6FF', '#3FB950', '#FF7B72', '#D2A8FF', '#FFA657', '#79C0FF']

function agentColor(name: string): string {
  let h = 0
  for (const c of name) h = (Math.imul(31, h) + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]!
}

const props = defineProps<{
  agentName: string
  createdAt?: string
  agentDescription?: string
}>()

const color = computed(() => agentColor(props.agentName))

const initial = computed(() => (props.agentName?.[0] ?? '?').toUpperCase())

const formattedTime = computed(() => formatRelativeTime(props.createdAt) || 'just now')
const absoluteTime = computed(() => formatClockTime(props.createdAt))
</script>
