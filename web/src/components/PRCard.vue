<template>
  <div v-if="pr" class="mt-2 rounded-xl border border-huginn-border overflow-hidden">
    <div class="flex items-center gap-2 px-3 py-1.5 bg-huginn-surface/40">
      <svg class="w-3.5 h-3.5 text-huginn-muted flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="18" cy="18" r="3" />
        <circle cx="6" cy="6" r="3" />
        <path d="M13 6h3a2 2 0 012 2v7" />
        <line x1="6" y1="9" x2="6" y2="21" />
      </svg>
      <a
        :href="pr.url"
        target="_blank"
        rel="noopener noreferrer"
        class="text-xs text-huginn-text truncate flex-1 hover:underline"
        :title="pr.url"
      >
        <span class="font-mono text-huginn-muted">#{{ pr.number }}</span>
        <span v-if="pr.title"> {{ pr.title }}</span>
      </a>
      <span v-if="pr.branch" class="text-[11px] font-mono text-huginn-muted flex-shrink-0" :title="pr.branch">
        {{ pr.branch }}
      </span>
      <span
        v-if="pr.checks"
        class="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded-full flex-shrink-0"
        :class="checksPillClass"
      >
        {{ checksLabel }}
      </span>
      <a
        :href="pr.url"
        target="_blank"
        rel="noopener noreferrer"
        class="text-huginn-muted hover:text-huginn-text flex-shrink-0"
        title="Open in browser"
        aria-label="Open pull request in browser"
      >
        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6" />
          <polyline points="15 3 21 3 21 9" />
          <line x1="10" y1="14" x2="21" y2="3" />
        </svg>
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ToolCallRecord } from '../composables/useSessions'
import { extractPRInfo } from '../utils/prInfo'

const props = defineProps<{
  toolCalls?: ToolCallRecord[]
}>()

const pr = computed(() => extractPRInfo(props.toolCalls))

const checksLabel = computed(() => {
  switch (pr.value?.checks) {
    case 'pass':
      return 'checks passing'
    case 'fail':
      return 'checks failing'
    case 'pending':
      return 'checks pending'
    default:
      return ''
  }
})

const checksPillClass = computed(() => {
  switch (pr.value?.checks) {
    case 'pass':
      return 'bg-huginn-green/15 text-huginn-green'
    case 'fail':
      return 'bg-huginn-red/15 text-huginn-red'
    case 'pending':
      return 'bg-huginn-muted/15 text-huginn-muted'
    default:
      return ''
  }
})
</script>
