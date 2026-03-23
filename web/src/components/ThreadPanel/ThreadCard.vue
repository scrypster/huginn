<template>
  <div
    class="rounded-xl overflow-hidden transition-all duration-300"
    :class="thread.Status === 'blocked'
      ? 'border-huginn-yellow/40'
      : isExpanded ? 'border-huginn-border' : 'border-transparent'"
    style="border-width:1px;background:rgba(30,37,48,0.7)"
  >
    <!-- ── Card Header ─────────────────────────────────────────── -->
    <button
      class="w-full flex items-center gap-2.5 px-3 py-2.5 text-left transition-colors duration-100 hover:bg-white/[0.03]"
      @click="isExpanded = !isExpanded"
    >
      <!-- Agent avatar -->
      <div
        class="w-6 h-6 rounded-md flex items-center justify-center text-[11px] font-bold flex-shrink-0 transition-all duration-300"
        :style="avatarStyle"
      >{{ agentInitial }}</div>

      <!-- Task text + status -->
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-1.5">
          <span class="text-xs font-medium text-huginn-text truncate">{{ thread.AgentID || 'Agent' }}</span>
          <!-- Status badge -->
          <span
            class="text-[10px] px-1.5 py-0.5 rounded-full font-medium flex-shrink-0"
            :class="statusBadgeClass"
          >{{ statusLabel }}</span>
        </div>
        <p class="text-[11px] text-huginn-muted truncate mt-0.5 leading-snug">{{ thread.Task || 'Running task…' }}</p>
      </div>

      <!-- Right side: elapsed + expand chevron -->
      <div class="flex items-center gap-2 flex-shrink-0">
        <span class="text-[11px] font-mono" :class="isRunning ? 'text-huginn-blue' : 'text-huginn-muted/60'">
          {{ elapsedLabel }}
        </span>
        <!-- Thinking pulse (only when running) -->
        <div v-if="isRunning" class="flex gap-0.5">
          <span class="w-1 h-1 rounded-full animate-bounce"
            :style="`background:${statusColor};animation-delay:0ms`" />
          <span class="w-1 h-1 rounded-full animate-bounce"
            :style="`background:${statusColor};animation-delay:120ms`" />
          <span class="w-1 h-1 rounded-full animate-bounce"
            :style="`background:${statusColor};animation-delay:240ms`" />
        </div>
        <!-- Status icon (when done) -->
        <div v-else>
          <svg v-if="thread.Status === 'done'" class="w-3.5 h-3.5 text-huginn-green" viewBox="0 0 24 24"
            fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <polyline points="20 6 9 17 4 12" />
          </svg>
          <svg v-else-if="thread.Status === 'error'" class="w-3.5 h-3.5 text-huginn-red" viewBox="0 0 24 24"
            fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          <svg v-else-if="thread.Status === 'cancelled'" class="w-3.5 h-3.5 text-huginn-muted/50" viewBox="0 0 24 24"
            fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" />
          </svg>
          <svg v-else-if="thread.Status === 'blocked'" class="w-3.5 h-3.5 text-huginn-yellow" viewBox="0 0 24 24"
            fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
        </div>
        <!-- Expand chevron -->
        <svg class="w-3 h-3 text-huginn-muted/40 transition-transform duration-200"
          :class="isExpanded ? 'rotate-180' : ''"
          viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </div>
    </button>

    <!-- ── Expanded body ────────────────────────────────────────── -->
    <div v-if="isExpanded"
      class="border-t px-3 pb-3 pt-2 space-y-2.5"
      style="border-color:rgba(48,54,61,0.5)"
    >
      <!-- Streaming tokens (visible while running) -->
      <div v-if="thread.streamingContent && isRunning"
        class="rounded-lg px-2.5 py-2 text-[11px] font-mono leading-relaxed text-huginn-muted overflow-hidden"
        style="background:rgba(22,27,34,0.6);max-height:80px;overflow-y:auto"
      >{{ thread.streamingContent }}</div>

      <!-- Tool call history: collapsed "N tool calls" chip, click to expand -->
      <div v-if="thread.toolCalls.length" class="space-y-1">
        <button
          @click="toolsExpanded = !toolsExpanded"
          class="flex items-center gap-1.5 w-full text-left py-0.5 transition-colors"
          :class="toolsExpanded ? 'text-huginn-muted/80' : 'text-huginn-muted/50 hover:text-huginn-muted/80'"
        >
          <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
            :style="hasRunningTools ? 'color:rgba(210,153,34,0.7)' : 'color:rgba(88,166,255,0.5)'">
            <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
          </svg>
          <span class="text-[11px]">{{ thread.toolCalls.length }} tool call{{ thread.toolCalls.length !== 1 ? 's' : '' }}</span>
          <svg class="w-3 h-3 ml-auto flex-shrink-0 transition-transform duration-150"
            :class="toolsExpanded ? 'rotate-180' : ''"
            viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </button>
        <!-- Expanded: individual tool rows -->
        <div v-if="toolsExpanded" class="space-y-1 pl-4 border-l-2" style="border-color:rgba(255,255,255,0.06)">
          <div v-for="(tc, i) in thread.toolCalls" :key="i"
            class="flex items-center gap-2 text-[11px] px-2 py-1 rounded-lg"
            :style="tc.done
              ? 'background:rgba(63,185,80,0.06);border:1px solid rgba(63,185,80,0.12)'
              : 'background:rgba(210,153,34,0.06);border:1px solid rgba(210,153,34,0.15)'"
          >
            <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
              :style="tc.done ? 'color:rgba(63,185,80,0.7)' : 'color:rgba(210,153,34,0.7)'">
              <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
            </svg>
            <span class="font-mono font-medium flex-1 truncate" :style="tc.done ? 'color:#3fb950' : 'color:#d29922'">{{ tc.tool }}</span>
            <span v-if="tc.done" class="text-huginn-muted/50 text-[10px]">done</span>
            <span v-else class="text-[10px]" style="color:rgba(210,153,34,0.7)">running</span>
          </div>
        </div>
      </div>

      <!-- Summary / completion output -->
      <div v-if="thread.Summary?.Summary && !isRunning" class="space-y-1.5">
        <p class="text-[10px] uppercase tracking-wider text-huginn-muted/50 font-medium">Summary</p>
        <p class="text-[11px] text-huginn-muted leading-relaxed">{{ parseSummary(thread.Summary.Summary) }}</p>

        <!-- Files modified -->
        <div v-if="thread.Summary.FilesModified?.length" class="flex flex-wrap gap-1 mt-1">
          <span v-for="f in thread.Summary.FilesModified" :key="f"
            class="text-[10px] font-mono px-1.5 py-0.5 rounded"
            style="background:rgba(88,166,255,0.1);color:rgba(88,166,255,0.8)">
            {{ f.split('/').pop() }}
          </span>
        </div>

        <!-- Key decisions -->
        <ul v-if="thread.Summary.KeyDecisions?.length" class="space-y-0.5 mt-1">
          <li v-for="d in thread.Summary.KeyDecisions" :key="d"
            class="text-[11px] text-huginn-muted/70 flex gap-1.5">
            <span class="text-huginn-muted/30 flex-shrink-0">·</span>
            <span>{{ d }}</span>
          </li>
        </ul>
      </div>

      <!-- Blocked: input form -->
      <div v-if="thread.Status === 'blocked'" class="space-y-1.5">
        <p class="text-[10px] uppercase tracking-wider font-medium" style="color:rgba(210,153,34,0.8)">Waiting for input</p>
        <div class="flex gap-1.5">
          <input
            v-model="injectInput"
            class="flex-1 text-xs px-2.5 py-1.5 rounded-lg outline-none border text-huginn-text placeholder-huginn-muted/40"
            style="background:rgba(22,27,34,0.8);border-color:rgba(210,153,34,0.3)"
            placeholder="Type a response…"
            @keydown.enter="submitInject"
          />
          <button
            @click="submitInject"
            class="px-2.5 py-1.5 rounded-lg text-xs font-medium transition-all duration-150 active:scale-95"
            :class="injectInput.trim() ? '' : 'animate-pulse'"
            style="background:rgba(210,153,34,0.2);color:rgba(210,153,34,0.9);border:1px solid rgba(210,153,34,0.3)"
          >Send</button>
        </div>
      </div>

      <!-- Token count + Cancel (only when running) -->
      <div v-if="isRunning" class="flex items-center justify-between pt-0.5">
        <span class="text-[10px] text-huginn-muted/40 font-mono">
          {{ thread.TokensUsed.toLocaleString() }} tokens
          {{ thread.TokenBudget > 0 ? `/ ${thread.TokenBudget.toLocaleString()}` : '' }}
        </span>
        <button
          @click.stop="$emit('cancel', thread.ID)"
          class="text-[10px] px-2 py-1 rounded-lg transition-all duration-150 hover:bg-huginn-red/10 hover:text-huginn-red active:scale-95"
          style="color:rgba(248,81,73,0.5);border:1px solid rgba(248,81,73,0.15)"
        >Cancel</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { LiveThread } from '../../composables/useThreads'
import { TERMINAL_STATUSES } from '../../composables/useThreads'

const props = defineProps<{
  thread: LiveThread
  agentColor?: string
  agentIcon?: string
}>()

const emit = defineEmits<{
  (e: 'cancel', threadId: string): void
  (e: 'inject', threadId: string, content: string): void
}>()

const isExpanded = ref(true)
const toolsExpanded = ref(false)
const injectInput = ref('')

// ── Computed ────────────────────────────────────────────────────────────────
const isRunning = computed(() => !TERMINAL_STATUSES.has(props.thread.Status))
const hasRunningTools = computed(() => props.thread.toolCalls.some(tc => !tc.done))

const agentInitial = computed(() =>
  props.agentIcon || (props.thread.AgentID?.[0]?.toUpperCase() ?? '?')
)

const statusColor = computed(() => {
  switch (props.thread.Status) {
    case 'thinking':  return 'rgba(88,166,255,0.9)'
    case 'tooling':   return 'rgba(210,153,34,0.9)'
    case 'done':      return 'rgba(63,185,80,0.9)'
    case 'blocked':   return 'rgba(210,153,34,0.9)'
    case 'resolving': return 'rgba(88,166,255,0.9)'
    case 'error':     return 'rgba(248,81,73,0.9)'
    default:          return 'rgba(139,148,158,0.5)'
  }
})

const avatarStyle = computed(() => {
  const color = props.agentColor ?? statusColor.value
  return `background:${color}22;color:${color};box-shadow:0 0 0 1px ${color}33`
})

const statusLabel = computed(() => {
  switch (props.thread.Status) {
    case 'queued':                return 'queued'
    case 'thinking':              return 'thinking'
    case 'tooling':               return 'using tools'
    case 'done':                  return 'done'
    case 'completed':             return 'done'
    case 'completed-with-timeout': return 'done'
    case 'blocked':               return 'needs input'
    case 'resolving':             return 'consulting Mark…'
    case 'cancelled':             return 'cancelled'
    case 'error':                 return 'error'
    default:                      return props.thread.Status
  }
})

const statusBadgeClass = computed(() => {
  switch (props.thread.Status) {
    case 'thinking':               return 'bg-huginn-blue/10 text-huginn-blue'
    case 'tooling':                return 'bg-huginn-yellow/10 text-huginn-yellow'
    case 'done':                   return 'bg-huginn-green/10 text-huginn-green'
    case 'completed':              return 'bg-huginn-green/10 text-huginn-green'
    case 'completed-with-timeout': return 'bg-huginn-green/10 text-huginn-green'
    case 'blocked':                return 'bg-huginn-yellow/10 text-huginn-yellow'
    case 'resolving':              return 'bg-huginn-blue/10 text-huginn-blue'
    case 'error':                  return 'bg-huginn-red/10 text-huginn-red'
    default:                       return 'bg-huginn-muted/10 text-huginn-muted/60'
  }
})

const elapsedLabel = computed(() => {
  const ms = props.thread.elapsedMs
  if (ms < 1000) return `${ms}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${s % 60}s`
})

// ── Helpers ──────────────────────────────────────────────────────────────────
// Strip tool-call JSON that leaks into thread summaries when LLM outputs
// { "name": "request_help", "arguments": { "message": "..." } } as text.
function parseSummary(summary: string): string {
  try {
    const parsed = JSON.parse(summary)
    if (typeof parsed === 'object' && parsed !== null) {
      if (parsed.arguments?.message) return parsed.arguments.message as string
      if (parsed.message) return parsed.message as string
    }
  } catch { /* not JSON */ }
  return summary
}

// ── Actions ──────────────────────────────────────────────────────────────────
function submitInject() {
  const content = injectInput.value.trim()
  if (!content) return
  emit('inject', props.thread.ID, content)
  injectInput.value = ''
}
</script>
