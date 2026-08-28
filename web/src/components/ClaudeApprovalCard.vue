<template>
  <div
    class="rounded-lg px-3 py-2 text-xs"
    style="background:rgba(210,153,34,0.10);border:1px solid rgba(210,153,34,0.35)"
    data-testid="approval-card"
  >
    <div class="flex items-start gap-2">
      <span class="font-semibold flex-shrink-0">{{ approval.tool_name }}</span>
      <span class="opacity-60 flex-shrink-0">·</span>
      <!-- The command is the thing being approved. For Bash the excerpt is
           always empty, so this is its ONLY rendering anywhere: it wraps and
           scrolls inside a bounded box rather than being clipped to one line,
           because an Allow button next to an unreadable command is worse than
           no card at all. -->
      <span
        class="font-mono break-all whitespace-pre-wrap max-h-24 overflow-auto min-w-0 flex-1"
        data-testid="approval-summary"
        :title="approval.summary"
      >{{ approval.summary }}</span>
      <span class="ml-auto font-mono opacity-70 flex-shrink-0" data-testid="approval-countdown">{{ countdown }}</span>
    </div>

    <div v-if="approval.cwd" class="mt-1 opacity-60 font-mono">cwd {{ approval.cwd }}</div>

    <pre
      v-if="approval.excerpt"
      class="mt-2 max-h-32 overflow-auto whitespace-pre-wrap font-mono opacity-80"
    >{{ approval.excerpt }}</pre>

    <div class="mt-2 flex items-center gap-2">
      <button
        class="px-2 py-1 rounded font-semibold"
        style="background:rgba(46,160,67,0.20)"
        data-testid="approval-allow"
        @click="emit('decide', 'allow')"
      >Allow</button>
      <button
        class="px-2 py-1 rounded font-semibold"
        style="background:rgba(248,81,73,0.18)"
        data-testid="approval-deny"
        @click="emit('decide', 'deny')"
      >Deny</button>
    </div>

    <div class="mt-2 flex flex-col gap-1">
      <button
        v-if="approval.can_remember"
        class="text-left opacity-70 hover:opacity-100"
        data-testid="approval-allow-command"
        @click="emit('decide', 'allow_command')"
      >⤷ Always allow this command (until Huginn restarts)</button>

      <button
        v-if="!confirmingTool"
        class="text-left opacity-70 hover:opacity-100"
        data-testid="approval-allow-tool"
        @click="confirmingTool = true"
      >⤷ Always allow {{ approval.tool_name }} for {{ approval.agent_name }}…</button>

      <button
        v-else
        class="text-left font-semibold"
        style="color:rgba(248,81,73,0.95)"
        data-testid="approval-allow-tool-confirm"
        @click="emit('decide', 'allow_tool')"
      >⤷ Confirm: {{ approval.tool_name }} is never gated for {{ approval.agent_name }} again</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import type { ClaudeApproval, ApprovalDecision } from '../composables/useClaudeApprovals'

const props = defineProps<{ approval: ClaudeApproval }>()
const emit = defineEmits<{ (e: 'decide', d: ApprovalDecision): void }>()

// Promotion permanently ungates a tool for an agent — after it, no card ever
// appears for that tool again, and Phase 1's only undo is editing the config
// file. It therefore must not share the one-click path that grants a single
// call.
const confirmingTool = ref(false)

// Reset the confirm gate whenever this card is reused for a different
// approval. Today the parent keys by approval.id so Vue remounts instead of
// patching, and this never fires — but the two-click gate guards a permanent
// privilege escalation, so it defends its own invariant rather than trusting
// every future caller to key the list correctly.
watch(() => props.approval.id, () => { confirmingTool.value = false })

// The countdown ticks LOCALLY, for display only. The server stays
// authoritative: every refresh delivers a fresh remaining_ms, and the elapsed
// baseline resets to it below. Without a local tick a lone pending card showed
// one frozen number for the full 285s and then vanished — no sense of urgency,
// which is the one thing this card exists to convey.
const baseline = ref(Date.now())
const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  ticker = setInterval(() => { now.value = Date.now() }, 1000)
})
onUnmounted(() => {
  if (ticker !== undefined) clearInterval(ticker)
})

// A fresh server value restarts the local clock rather than continuing to
// subtract from a stale baseline.
watch(() => props.approval.remaining_ms, () => {
  baseline.value = Date.now()
  now.value = baseline.value
})

const countdown = computed(() => {
  const remaining = props.approval.remaining_ms - (now.value - baseline.value)
  const total = Math.max(0, Math.floor(remaining / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
})
</script>
