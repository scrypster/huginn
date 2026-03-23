<template>
  <!-- Sliding panel (fixed width, slides in/out) -->
  <div
    class="flex flex-col h-full border-l border-huginn-border flex-shrink-0 transition-all duration-300 ease-in-out overflow-hidden"
    :style="panelStyle"
    style="background:rgba(18,23,30,0.98);backdrop-filter:blur(12px)"
  >
    <!-- ── Panel Header ─────────────────────────────────────────── -->
    <div class="flex items-center gap-2 px-3 h-11 border-b border-huginn-border flex-shrink-0"
      style="background:rgba(22,27,34,0.6)">
      <!-- Icon -->
      <div class="w-4 h-4 flex items-center justify-center flex-shrink-0">
        <svg class="w-3.5 h-3.5 text-huginn-blue" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/>
          <circle cx="9" cy="7" r="4"/>
          <path d="M23 21v-2a4 4 0 00-3-3.87"/>
          <path d="M16 3.13a4 4 0 010 7.75"/>
        </svg>
      </div>

      <span class="text-xs font-semibold text-huginn-text tracking-wide flex-1">Threads</span>

      <!-- Active count badge -->
      <div v-if="activeCount > 0"
        class="text-[10px] font-bold px-1.5 py-0.5 rounded-full tabular-nums"
        style="background:rgba(88,166,255,0.15);color:rgba(88,166,255,0.9);box-shadow:0 0 8px rgba(88,166,255,0.15)">
        {{ activeCount }}
      </div>

      <!-- Collapse button -->
      <button
        @click="$emit('collapse')"
        class="w-6 h-6 rounded-lg flex items-center justify-center text-huginn-muted/40 hover:text-huginn-muted hover:bg-huginn-surface transition-all duration-100 ml-1"
        title="Collapse thread panel"
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <polyline points="13 17 18 12 13 7"/>
          <polyline points="6 17 11 12 6 7"/>
        </svg>
      </button>
    </div>

    <!-- ── Thread list ─────────────────────────────────────────── -->
    <div class="flex-1 overflow-y-auto px-2 py-2 space-y-2">
      <!-- Empty state -->
      <div v-if="threads.length === 0"
        class="flex flex-col items-center justify-center h-32 gap-2 opacity-40">
        <svg class="w-8 h-8 text-huginn-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
          <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/>
          <circle cx="9" cy="7" r="4"/>
          <path d="M23 21v-2a4 4 0 00-3-3.87"/>
          <path d="M16 3.13a4 4 0 010 7.75"/>
        </svg>
        <p class="text-xs text-huginn-muted">No active threads</p>
      </div>

      <!-- Thread cards (running first, then terminal) -->
      <TransitionGroup name="thread-list" tag="div" class="space-y-2">
        <ThreadCard
          v-for="thread in sortedThreads"
          :key="thread.ID"
          :thread="thread"
          :agent-color="agentColors[thread.AgentID]"
          :agent-icon="agentIcons[thread.AgentID]"
          @cancel="$emit('cancel', $event)"
          @inject="(threadId, content) => $emit('inject', threadId, content)"
        />
      </TransitionGroup>
    </div>

    <!-- ── Footer: total cost / token summary ─────────────────── -->
    <div v-if="threads.length > 0"
      class="flex items-center gap-3 px-3 py-2 border-t border-huginn-border flex-shrink-0"
      style="background:rgba(22,27,34,0.4)">
      <span class="text-[10px] text-huginn-muted/40 font-mono">
        {{ totalTokens.toLocaleString() }} total tokens
      </span>
      <span v-if="doneCount > 0" class="text-[10px] text-huginn-green/50 ml-auto">
        {{ doneCount }}/{{ threads.length }} done
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ThreadCard from './ThreadCard.vue'
import type { LiveThread } from '../../composables/useThreads'
import { TERMINAL_STATUSES } from '../../composables/useThreads'

const props = defineProps<{
  threads: LiveThread[]
  agentColors: Record<string, string>   // agentName → color hex
  agentIcons: Record<string, string>    // agentName → icon char
  visible: boolean
}>()

defineEmits<{
  (e: 'collapse'): void
  (e: 'cancel', threadId: string): void
  (e: 'inject', threadId: string, content: string): void
}>()

// ── Computed ────────────────────────────────────────────────────────────────
const panelStyle = computed(() => ({
  width: props.visible ? '360px' : '0px',
  minWidth: props.visible ? '360px' : '0px',
}))

const activeCount = computed(() =>
  props.threads.filter(t => !TERMINAL_STATUSES.has(t.Status)).length
)

const doneCount = computed(() =>
  props.threads.filter(t => t.Status === 'done').length
)

const totalTokens = computed(() =>
  props.threads.reduce((sum, t) => sum + (t.TokensUsed ?? 0), 0)
)

// Sort: running threads first (queued/thinking/tooling/blocked), then terminal
const sortedThreads = computed(() => {
  return [...props.threads].sort((a, b) => {
    const aRunning = !TERMINAL_STATUSES.has(a.Status) ? 0 : 1
    const bRunning = !TERMINAL_STATUSES.has(b.Status) ? 0 : 1
    return aRunning - bRunning
  })
})
</script>

<style scoped>
/* Thread list enter/leave transitions */
.thread-list-enter-active {
  transition: all 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.thread-list-leave-active {
  transition: all 0.2s ease-in;
}
.thread-list-enter-from {
  opacity: 0;
  transform: translateX(20px) scale(0.97);
}
.thread-list-leave-to {
  opacity: 0;
  transform: translateX(20px);
}
.thread-list-move {
  transition: transform 0.3s ease;
}
</style>
