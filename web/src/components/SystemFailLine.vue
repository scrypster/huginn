<script setup lang="ts">
import { computed } from 'vue'
import { failDiagnostic, failVisibleCopy } from '../utils/honesty'

const props = defineProps<{
  content?: string | null
  toolName?: string
  teammate?: string
  result?: string
}>()

const copy = computed(() =>
  failVisibleCopy(props.content, { toolName: props.toolName, teammate: props.teammate }),
)
const diagnostic = computed(() =>
  failDiagnostic(props.content, { toolName: props.toolName, result: props.result }),
)
</script>

<template>
  <p
    data-testid="system-fail-line"
    class="text-sm text-huginn-text/80 leading-relaxed italic"
    :title="diagnostic"
    :aria-label="copy"
    :aria-description="diagnostic"
    tabindex="0"
  >
    <span data-testid="system-fail-copy">{{ copy }}</span>
  </p>
</template>
