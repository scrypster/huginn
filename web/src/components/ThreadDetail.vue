<template>
  <!-- Slide-in drawer from right, 400px wide -->
  <div
    class="flex flex-col h-full border-l border-huginn-border flex-shrink-0 transition-all duration-300 ease-in-out overflow-hidden"
    :style="panelStyle"
    style="background:rgba(18,23,30,0.98);backdrop-filter:blur(12px)"
  >
    <!-- ── Header ──────────────────────────────────────────────── -->
    <div class="flex items-center gap-2 px-3 h-11 border-b border-huginn-border flex-shrink-0"
      style="background:rgba(22,27,34,0.6)">
      <!-- Back arrow + title -->
      <button
        @click="$emit('close')"
        class="w-6 h-6 rounded-lg flex items-center justify-center text-huginn-muted/60 hover:text-huginn-text hover:bg-huginn-surface transition-all duration-100 flex-shrink-0"
        title="Close thread"
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <polyline points="15 18 9 12 15 6" />
        </svg>
      </button>
      <span class="text-xs font-semibold text-huginn-text tracking-wide flex-1 truncate">
        Thread
      </span>
      <!-- Close X button -->
      <button
        @click="$emit('close')"
        class="w-6 h-6 rounded-lg flex items-center justify-center text-huginn-muted/40 hover:text-huginn-muted hover:bg-huginn-surface transition-all duration-100 ml-1"
        title="Close"
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <!-- ── Body ────────────────────────────────────────────────── -->
    <div class="flex flex-col flex-1 min-h-0">

    <div class="flex-1 overflow-y-auto py-4 px-4 space-y-4">
      <!-- Loading state -->
      <div v-if="loading" class="flex items-center justify-center h-16 gap-2">
        <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:0ms" />
        <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:150ms" />
        <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:300ms" />
      </div>

      <!-- Error state -->
      <div v-else-if="error"
        class="px-3 py-3 rounded-xl border text-xs text-huginn-red"
        style="background:rgba(255,123,114,0.07);border-color:rgba(255,123,114,0.25)">
        {{ error }}
      </div>

      <!-- Empty state -->
      <div v-else-if="messages.length === 0"
        class="flex flex-col items-center justify-center h-32 gap-2 opacity-40">
        <svg class="w-8 h-8 text-huginn-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
          <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
        </svg>
        <p class="text-xs text-huginn-muted">No messages in this thread</p>
      </div>

      <!-- Message list (grouped so consecutive tool calls collapse into one row) -->
      <template v-for="(item, idx) in groupedMessages" :key="item.type === 'message' ? item.msg.id : item.key">

        <!-- Tool call group: collapsed "N tool calls" with expandable details -->
        <div v-if="item.type === 'toolgroup'">
          <!-- Collapsed summary row -->
          <button
            @click="toggleGroup(item.key)"
            class="flex items-center gap-1.5 py-1 text-huginn-muted/50 hover:text-huginn-muted/80 transition-colors w-full text-left"
          >
            <svg class="w-3 h-3 flex-shrink-0 text-huginn-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
            </svg>
            <span class="text-[11px]">
              {{ item.calls.length }} tool call{{ item.calls.length !== 1 ? 's' : '' }}
            </span>
            <svg class="w-3 h-3 ml-auto flex-shrink-0 transition-transform" :class="{'rotate-180': expandedGroups[item.key]}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>

          <!-- Expanded detail: each tool call + its result -->
          <div v-if="expandedGroups[item.key]" class="mt-1 space-y-1 pl-4 border-l-2" style="border-color:rgba(255,255,255,0.08)">
            <template v-for="call in item.calls" :key="call.id">
              <!-- Tool call row -->
              <div class="flex items-center gap-2 px-2 py-1.5 rounded-lg border border-huginn-border bg-huginn-surface/30 text-xs">
                <svg class="w-3 h-3 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
                </svg>
                <span class="text-huginn-text font-medium">{{ extractToolName(call.content) }}</span>
              </div>
              <!-- Matching tool result (if any) -->
              <template v-if="item.results.find(r => r.tool_name === call.tool_name)">
                <!-- Use v-for single-element array to bind parseConsultResult once per call card,
                     avoiding 7 redundant re-evaluations of the same expression. -->
                <template v-for="cr in [call.tool_name === 'consult_agent' ? parseConsultResult(item.results.find(r => r.tool_name === call.tool_name)?.content ?? '') : null]" :key="call.tool_name + '-cr'">
                  <!-- Consultation card: special rendering for consult_agent results -->
                  <div v-if="cr" class="rounded-lg border overflow-hidden"
                    :style="`border-color:${agentColor(cr.agentName)}33`">
                    <!-- Consultation header -->
                    <div class="flex items-center gap-1.5 px-2 py-1.5"
                      :style="`background:${agentColor(cr.agentName)}10`">
                      <div class="w-3.5 h-3.5 rounded text-[8px] font-bold flex items-center justify-center flex-shrink-0"
                        :style="`background:${agentColor(cr.agentName)}22;color:${agentColor(cr.agentName)}`">
                        {{ cr.agentName[0]?.toUpperCase() }}
                      </div>
                      <span class="text-[11px] font-medium"
                        :style="`color:${agentColor(cr.agentName)}`">
                        {{ cr.agentName }}
                      </span>
                      <span class="text-[10px] text-huginn-muted/50 ml-auto">consulted</span>
                    </div>
                    <!-- Consultation answer -->
                    <div class="px-2 py-1.5 bg-huginn-surface/20">
                      <div class="md-content text-[11px] text-huginn-muted leading-relaxed break-words"
                        v-html="renderMarkdown(cr.answer)" />
                    </div>
                  </div>
                  <!-- Generic tool result -->
                  <div v-else class="px-2 py-1.5 rounded-lg border border-huginn-border bg-huginn-surface/20">
                    <pre class="text-[11px] text-huginn-muted overflow-x-auto max-h-24 leading-relaxed whitespace-pre-wrap break-words">{{ item.results.find(r => r.tool_name === call.tool_name)?.content }}</pre>
                  </div>
                </template>
              </template>
            </template>
          </div>
        </div>

        <!-- Regular messages (non-tool) -->
        <template v-else-if="item.type === 'message'">
          <!-- Delegation divider: shown when agent changes between adjacent messages -->
          <div v-if="idx > 0 && item.msg.agent && groupedMessages[idx - 1]?.type === 'message' && (groupedMessages[idx - 1] as any).msg?.agent && item.msg.agent !== (groupedMessages[idx - 1] as any).msg?.agent"
            class="delegation-divider flex items-center gap-2 py-1">
            <div class="flex-1 border-t border-dashed" style="border-color:rgba(255,255,255,0.15)" />
            <span class="text-[10px] text-huginn-muted/60 whitespace-nowrap">
              handed off to {{ item.msg.agent }}
            </span>
            <div class="flex-1 border-t border-dashed" style="border-color:rgba(255,255,255,0.15)" />
          </div>

          <!-- User message -->
          <div v-if="item.msg.role === 'user'" class="flex justify-end">
            <div class="md-content max-w-[85%] px-3 py-2.5 rounded-2xl rounded-tr-sm text-sm text-huginn-text leading-relaxed break-words"
              style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.22)"
              v-html="renderMarkdown(item.msg.content)" />
          </div>

          <!-- Assistant message -->
          <div v-else class="flex gap-2.5">
            <!-- Agent avatar -->
            <div class="w-6 h-6 rounded-md flex items-center justify-center flex-shrink-0 mt-0.5 select-none"
              :style="`background:${agentColor(item.msg.agent)}22;border:1px solid ${agentColor(item.msg.agent)}33`">
              <span class="text-[10px] font-bold" :style="`color:${agentColor(item.msg.agent)}`">
                {{ (item.msg.agent?.[0] ?? 'A').toUpperCase() }}
              </span>
            </div>
            <div class="flex-1 min-w-0 pt-0.5">
              <!-- Agent name + time -->
              <div class="flex items-center gap-1.5 mb-0.5">
                <span class="text-xs font-semibold" :style="`color:${agentColor(item.msg.agent)}`">
                  {{ item.msg.agent || 'Agent' }}
                </span>
                <span class="text-[11px] text-huginn-muted/50">{{ formatTime(item.msg.created_at) }}</span>
              </div>
              <!-- Message content -->
              <div v-if="item.msg.content" class="md-content text-sm text-huginn-text leading-relaxed break-words"
                v-html="renderMarkdown(item.msg.content)" />
              <!-- Streaming cursor -->
              <span v-if="(item.msg as any).streaming" class="inline-block w-1.5 h-3.5 bg-huginn-muted/60 rounded-sm animate-pulse ml-0.5 align-middle" />
            </div>
          </div>
        </template>

      </template>

      <!-- Artifact card at bottom of thread -->
      <ArtifactCard
        v-if="artifact"
        :artifact="artifact"
        @accept="$emit('accept-artifact', $event)"
        @reject="$emit('reject-artifact', $event)"
      />

      <!-- Observation deck -->
      <ObservationDeck
        v-if="messages.length > 0"
        :messages="messages"
        :agent-name="primaryAgent"
      />
    </div>

    <!-- ── Injection input ──────────────────────────────────────── -->
    <div v-if="threadId" class="px-3 pb-3 flex-shrink-0 border-t border-huginn-border"
      :style="threadStatus === 'blocked' ? 'background:rgba(210,153,34,0.06)' : ''">
      <div class="flex items-center gap-2 pt-2.5">
        <!-- Hint: highlighted when blocked -->
        <span v-if="threadStatus === 'blocked'"
          class="text-[10px] text-huginn-yellow font-semibold uppercase tracking-wide flex-shrink-0">
          Help requested
        </span>
        <span v-else class="text-[10px] text-huginn-muted/50 uppercase tracking-wide flex-shrink-0">
          Inject
        </span>
        <input
          v-model="injectInput"
          type="text"
          placeholder="Send a message to this thread..."
          class="flex-1 min-w-0 bg-huginn-surface/50 border border-huginn-border rounded-lg px-2.5 py-1.5 text-xs text-huginn-text placeholder-huginn-muted/40 outline-none focus:border-huginn-blue/40 transition-colors"
          :disabled="injectState === 'sending'"
          @keydown.enter="handleInject"
        />
        <button
          @click="handleInject"
          :disabled="!injectInput.trim() || injectState === 'sending'"
          class="px-2.5 py-1.5 rounded-lg text-[11px] font-medium transition-all duration-150 disabled:opacity-40 flex-shrink-0"
          :class="{
            'text-huginn-green border border-huginn-green/30 hover:bg-huginn-green/15': injectState === 'idle' || injectState === 'sent',
            'text-huginn-muted border border-huginn-border': injectState === 'sending',
            'text-huginn-red border border-huginn-red/30': injectState === 'failed',
          }"
        >
          <span v-if="injectState === 'sending'">···</span>
          <span v-else-if="injectState === 'sent'">✓</span>
          <span v-else-if="injectState === 'failed'">retry</span>
          <span v-else>Send</span>
        </button>
      </div>
      <p v-if="injectState === 'failed'"
        class="text-[10px] text-huginn-red mt-1 pl-14">
        Failed to deliver — thread buffer full. Try again.
      </p>
    </div>
  </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { ThreadMessage, ThreadArtifact } from '../composables/useThreadDetail'
import ArtifactCard from './ArtifactCard.vue'
import ObservationDeck from './ObservationDeck.vue'

// ── Tool call grouping ────────────────────────────────────────────────
// Consecutive tool_call / tool_result rows are collapsed into a single group.
// Each group shows "N tool calls" — clicking expands inline to show details.
type MsgItem = { type: 'message'; msg: ThreadMessage }
type ToolGroup = {
  type: 'toolgroup'
  key: string
  calls: ThreadMessage[]
  results: ThreadMessage[]
}
type GroupedItem = MsgItem | ToolGroup

const PALETTE = ['#58A6FF', '#3FB950', '#FF7B72', '#D2A8FF', '#FFA657', '#79C0FF']

function agentColor(name: string): string {
  if (!name) return PALETTE[0]!
  let h = 0
  for (const c of name) h = (Math.imul(31, h) + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]!
}

function renderMarkdown(content: string): string {
  if (!content) return ''
  return DOMPurify.sanitize(marked.parse(content) as string)
}

function formatTime(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const diffMs = Date.now() - d.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return 'just now'
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return d.toLocaleDateString()
}

function extractToolName(content: string): string {
  try {
    const parsed = JSON.parse(content)
    return parsed.name ?? parsed.tool ?? content
  } catch {
    return content
  }
}

// parseConsultResult extracts the consulted agent name and answer from a
// consult_agent tool result string formatted as "[AgentName's response]\nAnswer...".
// Returns null if the content doesn't match the consultation format.
function parseConsultResult(content: string): { agentName: string; answer: string } | null {
  if (!content) return null
  const match = content.match(/^\[([^\]]+)'s response\]\n?([\s\S]*)$/)
  if (!match) return null
  return { agentName: match[1]!, answer: match[2]!.trim() }
}

const props = defineProps<{
  visible: boolean
  messages: ThreadMessage[]
  loading: boolean
  error: string | null
  artifact?: ThreadArtifact | null
  threadStatus?: string     // current status of the live thread (for injection UX)
  threadId?: string         // live thread ID for injection
}>()

// ── Tool call grouping ────────────────────────────────────────────────
// Groups consecutive tool_call / tool_result messages so they render as a
// single collapsible "N tool calls" summary instead of individual rows.
const expandedGroups = ref<Record<string, boolean>>({})

function toggleGroup(key: string) {
  expandedGroups.value = { ...expandedGroups.value, [key]: !expandedGroups.value[key] }
}

const groupedMessages = computed((): GroupedItem[] => {
  const result: GroupedItem[] = []
  // Filter out internal bookkeeping roles (cost, system) that should never
  // appear in the thread panel. The backend already filters these, but this
  // is a safety net for stale data or WS-streamed messages.
  const msgs = props.messages.filter(m => (m.role as string) !== 'cost' && (m.role as string) !== 'system')
  let i = 0
  while (i < msgs.length) {
    const m = msgs[i]!
    if (m.role === 'tool_call' || m.role === 'tool_result') {
      // Collect the contiguous block of tool_call + tool_result rows
      const calls: ThreadMessage[] = []
      const results: ThreadMessage[] = []
      const startIdx = i
      while (i < msgs.length && (msgs[i]!.role === 'tool_call' || msgs[i]!.role === 'tool_result')) {
        if (msgs[i]!.role === 'tool_call') calls.push(msgs[i]!)
        else results.push(msgs[i]!)
        i++
      }
      result.push({
        type: 'toolgroup',
        key: `tg-${startIdx}`,
        calls,
        results,
      })
    } else {
      result.push({ type: 'message', msg: m })
      i++
    }
  }
  return result
})

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'accept-artifact', id: string): void
  (e: 'reject-artifact', id: string): void
  (e: 'inject', threadId: string, content: string): void
}>()

// ── Injection input ──────────────────────────────────────────────────
const injectInput = ref('')
type InjectState = 'idle' | 'sending' | 'sent' | 'failed'
const injectState = ref<InjectState>('idle')

function handleInject() {
  const content = injectInput.value.trim()
  if (!content || !props.threadId) return
  injectState.value = 'sending'
  emit('inject', props.threadId, content)
  // State will be resolved by parent after ack/error
}

function onInjectAck() {
  injectInput.value = ''
  injectState.value = 'sent'
  setTimeout(() => { injectState.value = 'idle' }, 2000)
}

function onInjectError() {
  injectState.value = 'failed'
  setTimeout(() => { injectState.value = 'idle' }, 3000)
}

defineExpose({ onInjectAck, onInjectError })

const panelStyle = computed(() => ({
  width: props.visible ? '400px' : '0px',
  minWidth: props.visible ? '400px' : '0px',
}))

const primaryAgent = computed(() => {
  return props.messages.find(m => m.role === 'assistant')?.agent ?? 'Agent'
})
</script>
