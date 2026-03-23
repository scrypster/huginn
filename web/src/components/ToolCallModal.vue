<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="open && tc"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm"
        style="background: rgba(0,0,0,0.5);"
        @click.self="$emit('close')"
      >
        <div class="modal-panel w-full max-w-2xl max-h-[80vh] bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl flex flex-col overflow-hidden">

          <!-- Header -->
          <div class="flex items-center gap-3 px-5 py-3.5 border-b border-huginn-border flex-shrink-0">
            <span class="w-2 h-2 rounded-full bg-emerald-400 flex-shrink-0" />
            <svg class="w-3.5 h-3.5 text-huginn-muted flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
            </svg>
            <span class="text-sm font-semibold text-huginn-text flex-1 font-mono">{{ tc.name }}</span>
            <button
              @click="$emit('close')"
              class="text-huginn-muted hover:text-huginn-text transition-colors text-xl leading-none"
            >×</button>
          </div>

          <!-- Body -->
          <div class="overflow-y-auto p-5 space-y-5">
            <!-- Arguments -->
            <div v-if="tc.args && Object.keys(tc.args).length">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-wider mb-2">Arguments</p>
              <pre class="text-xs text-huginn-text bg-huginn-bg border border-huginn-border rounded-lg p-3.5 overflow-x-auto leading-relaxed">{{ prettyArgs }}</pre>
            </div>
            <div v-else>
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-wider mb-2">Arguments</p>
              <p class="text-xs text-huginn-muted italic">none</p>
            </div>

            <!-- Result -->
            <div v-if="tc.result">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-wider mb-2">Result</p>
              <pre class="text-xs text-huginn-text bg-huginn-bg border border-huginn-border rounded-lg p-3.5 overflow-x-auto whitespace-pre-wrap leading-relaxed">{{ prettyResult }}</pre>
            </div>
            <div v-else>
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-wider mb-2">Result</p>
              <p class="text-xs text-huginn-muted italic">no result captured</p>
            </div>
          </div>

        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ToolCallRecord } from '../composables/useSessions'

const props = defineProps<{
  open: boolean
  tc: ToolCallRecord | null
}>()

defineEmits<{ close: [] }>()

const prettyArgs = computed(() => {
  if (!props.tc?.args) return ''
  try { return JSON.stringify(props.tc.args, null, 2) } catch { return String(props.tc.args) }
})

const prettyResult = computed(() => {
  const raw = props.tc?.result
  if (!raw) return ''
  // Try to parse as JSON and pretty-print; fall back to raw string
  try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
})
</script>

<style scoped>
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-active {
  transition: opacity 200ms ease;
}
.modal-leave-active {
  transition: opacity 160ms ease;
}
.modal-enter-from .modal-panel {
  transform: translateY(16px) scale(0.97);
  opacity: 0;
}
.modal-leave-to .modal-panel {
  transform: translateY(8px) scale(0.98);
  opacity: 0;
}
.modal-enter-active .modal-panel {
  transition: transform 220ms ease-out, opacity 220ms ease-out;
}
.modal-leave-active .modal-panel {
  transition: transform 160ms ease-in, opacity 160ms ease-in;
}
</style>
