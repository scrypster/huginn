<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="open && content"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm"
        style="background: rgba(0,0,0,0.5);"
        @click.self="$emit('close')"
      >
        <div class="modal-panel w-full max-w-2xl max-h-[80vh] bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl flex flex-col overflow-hidden">

          <!-- Header -->
          <div class="flex items-center gap-3 px-5 py-3.5 border-b border-huginn-border flex-shrink-0">
            <svg class="w-3.5 h-3.5 text-huginn-muted flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <ellipse cx="12" cy="5" rx="9" ry="3" />
              <path d="M3 5v6c0 1.66 4.03 3 9 3s9-1.34 9-3V5" />
              <path d="M3 11v6c0 1.66 4.03 3 9 3s9-1.34 9-3v-6" />
            </svg>
            <span class="text-sm font-semibold text-huginn-text flex-1">where we left off</span>
            <span class="text-[10px] text-huginn-muted/60">injected into system prompt</span>
            <button
              @click="$emit('close')"
              class="text-huginn-muted hover:text-huginn-text transition-colors text-xl leading-none ml-2"
            >×</button>
          </div>

          <!-- Body -->
          <div class="overflow-y-auto p-5">
            <p v-if="isHistoryOnly" class="text-xs text-huginn-muted leading-relaxed">
              Memory context was pre-loaded for this session before the first message was sent.
              The injected content is not stored — send a new message to see what's loaded fresh.
            </p>
            <pre v-else class="text-xs text-huginn-text bg-huginn-bg border border-huginn-border rounded-lg p-3.5 overflow-x-auto whitespace-pre-wrap leading-relaxed">{{ formattedContent }}</pre>
          </div>

        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  open: boolean
  content: string | null
}>()

defineEmits<{ close: [] }>()

const isHistoryOnly = computed(() => props.content === '__history__')

// Pretty-print any JSON objects/arrays found in the content.
const formattedContent = computed(() => {
  if (!props.content || isHistoryOnly.value) return ''
  return props.content.replace(/(\{[\s\S]*?\}|\[[\s\S]*?\])/g, (match) => {
    try {
      return JSON.stringify(JSON.parse(match), null, 2)
    } catch {
      return match
    }
  })
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
