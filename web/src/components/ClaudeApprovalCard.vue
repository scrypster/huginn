<template>
  <div
    class="rounded-lg px-3 py-2 text-xs"
    style="background:rgba(210,153,34,0.10);border:1px solid rgba(210,153,34,0.35)"
    data-testid="approval-card"
  >
    <div class="flex items-center gap-2">
      <span class="font-semibold">{{ approval.tool_name }}</span>
      <span class="opacity-60">·</span>
      <span class="font-mono truncate">{{ approval.summary }}</span>
      <span class="ml-auto font-mono opacity-70" data-testid="approval-countdown">{{ countdown }}</span>
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
import { ref, computed, watch } from 'vue'
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

const countdown = computed(() => {
  const total = Math.max(0, Math.floor(props.approval.remaining_ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
})
</script>
