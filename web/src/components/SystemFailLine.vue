<script setup lang="ts">
import { computed, ref } from 'vue'
import { failDiagnostic, failVisibleCopy } from '../utils/honesty'

const props = defineProps<{
  content?: string | null
  toolName?: string
  result?: string
}>()

const open = ref(false)

const copy = computed(() => failVisibleCopy(props.content, { toolName: props.toolName }))
const diagnostic = computed(() =>
  failDiagnostic(props.content, { toolName: props.toolName, result: props.result }),
)
</script>

<template>
  <div
    data-testid="system-fail-line"
    class="inline-flex items-start gap-1.5 px-2.5 py-1.5 rounded-lg text-xs
           border border-huginn-red/30 bg-huginn-red/8 text-huginn-red"
    :title="diagnostic"
    :aria-label="copy"
    :aria-description="diagnostic"
    tabindex="0"
  >
    <svg class="w-3.5 h-3.5 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
      <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
    </svg>
    <div class="min-w-0">
      <p data-testid="system-fail-copy">{{ copy }}</p>
      <button
        v-if="diagnostic"
        type="button"
        class="mt-0.5 text-[10px] text-huginn-red/70 underline-offset-2 hover:underline focus:outline-none focus-visible:underline"
        :aria-expanded="open"
        @click.stop="open = !open"
      >
        {{ open ? 'Hide details' : 'Details' }}
      </button>
      <p
        v-if="open && diagnostic"
        data-testid="system-fail-details"
        class="mt-1 text-[11px] text-huginn-muted break-words"
      >
        {{ diagnostic }}
      </p>
    </div>
  </div>
</template>
