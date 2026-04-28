<template>
  <div class="flex items-center gap-1">
    <!-- Copy -->
    <button
      data-testid="msg-copy"
      @click="handleCopy"
      class="text-[10px] px-2 py-0.5 rounded transition-colors"
      style="background:rgba(255,255,255,0.06);color:#8b949e"
      :title="copied ? 'Copied!' : 'Copy'"
    >{{ copied ? 'Copied!' : 'Copy' }}</button>

    <!-- Retry (user messages only) -->
    <button
      v-if="msg.role === 'user'"
      data-testid="msg-retry"
      @click="$emit('retry', msg.content)"
      class="text-[10px] px-2 py-0.5 rounded transition-colors"
      style="background:rgba(255,255,255,0.06);color:#8b949e"
    >Retry</button>

    <!-- Save to Memory (assistant messages, vault configured) -->
    <template v-if="msg.role === 'assistant' && agentVaultName">
      <!-- Agent already saved — show indicator, no button -->
      <span
        v-if="agentAlreadySaved"
        class="text-[10px] px-2 py-0.5 rounded"
        style="color:#3fb950"
      >Saved ✓</span>
      <!-- User can save -->
      <button
        v-else
        data-testid="msg-save-memory"
        @click="handleSaveMemory"
        class="text-[10px] px-2 py-0.5 rounded transition-colors"
        style="background:rgba(255,255,255,0.06);color:#8b949e"
        :disabled="saving"
      >{{ saveLabel }}</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChatMessage } from '../composables/useSessions'

const MEMORY_TOOLS = ['muninn_remember', 'muninn_decide', 'muninn_evolve']

const props = defineProps<{
  msg: ChatMessage
  agentVaultName: string
}>()

const emit = defineEmits<{
  (e: 'retry', content: string): void
  (e: 'save-memory', payload: { vault: string; content: string }): void
}>()

const copied = ref(false)
const saving = ref(false)
const saved = ref(false)

const saveLabel = computed(() => {
  if (saving.value) return 'Saving…'
  if (saved.value) return 'Saved ✓'
  return 'Save to Memory'
})

// True when the agent already called a memory tool during this turn.
const agentAlreadySaved = computed(() =>
  props.msg.toolCalls?.some(tc => MEMORY_TOOLS.includes(tc.name)) ?? false
)

async function handleCopy() {
  try {
    await navigator.clipboard.writeText(props.msg.content)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard API not available */ }
}

function handleSaveMemory() {
  emit('save-memory', { vault: props.agentVaultName, content: props.msg.content })
}
</script>
