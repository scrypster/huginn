<template>
  <div class="observation-deck mt-4 border-t border-huginn-border/40 pt-3">
    <button
      @click="deckOpen = !deckOpen"
      class="flex items-center gap-1.5 text-xs text-huginn-muted/70 hover:text-huginn-muted transition-colors w-full"
    >
      <span class="text-[10px]">{{ deckOpen ? '▲' : '▼' }}</span>
      <span>How {{ agentName }} did this</span>
    </button>

    <div v-if="deckOpen" class="mt-3 space-y-0">
      <ol class="space-y-1.5 pl-0">
        <li
          v-for="(step, i) in steps"
          :key="i"
          class="flex items-start gap-2 text-xs text-huginn-muted"
        >
          <span class="text-huginn-blue/50 flex-shrink-0 font-mono text-[10px] mt-0.5 w-4 text-right">
            {{ i + 1 }}.
          </span>
          <span class="leading-relaxed">{{ step }}</span>
        </li>
      </ol>
      <p v-if="steps.length === 0" class="text-xs text-huginn-muted/40 ml-6">
        No steps recorded for this thread.
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { ThreadMessage } from '../composables/useThreadDetail'

const props = defineProps<{
  messages: ThreadMessage[]
  agentName: string
}>()

const deckOpen = ref(false)

const steps = computed((): string[] => {
  const result: string[] = []
  const agent = props.agentName || 'Agent'
  const msgs = props.messages ?? []

  const toolCalls = msgs.filter(m => m.role === 'tool_call')
  const toolResults = msgs.filter(m => m.role === 'tool_result')

  const reads = toolCalls.filter(t => {
    const name = extractToolName(t.content)
    return name.toLowerCase().includes('read') || name.toLowerCase().includes('get') || name.toLowerCase().includes('fetch')
  })
  const writes = toolCalls.filter(t => {
    const name = extractToolName(t.content)
    return name.toLowerCase().includes('write') || name.toLowerCase().includes('edit') || name.toLowerCase().includes('create')
  })
  const recalls = toolCalls.filter(t => {
    const name = extractToolName(t.content)
    return name.toLowerCase().includes('recall') || name.toLowerCase().includes('muninn') || name.toLowerCase().includes('memory')
  })
  const searches = toolCalls.filter(t => {
    const name = extractToolName(t.content)
    return name.toLowerCase().includes('search') || name.toLowerCase().includes('grep') || name.toLowerCase().includes('glob')
  })
  const bashes = toolCalls.filter(t => {
    const name = extractToolName(t.content)
    return name.toLowerCase().includes('bash') || name.toLowerCase().includes('shell') || name.toLowerCase().includes('exec')
  })
  const delegations = msgs.filter(m => m.type === 'delegation_chain')

  // Build narrative steps
  if (recalls.length > 0) {
    result.push(`${agent} recalled ${recalls.length} ${recalls.length === 1 ? 'memory' : 'memories'} to orient context`)
  }
  if (reads.length > 0) {
    result.push(`${agent} read ${reads.length} ${reads.length === 1 ? 'file' : 'files'} for context`)
  }
  if (searches.length > 0) {
    result.push(`${agent} searched the codebase ${searches.length} ${searches.length === 1 ? 'time' : 'times'}`)
  }
  if (bashes.length > 0) {
    result.push(`${agent} ran ${bashes.length} shell ${bashes.length === 1 ? 'command' : 'commands'}`)
  }
  if (writes.length > 0) {
    result.push(`${agent} made ${writes.length} ${writes.length === 1 ? 'edit' : 'edits'} to files`)
  }
  if (delegations.length > 0) {
    result.push(`${agent} delegated to ${delegations.length} sub-${delegations.length === 1 ? 'agent' : 'agents'}`)
  }
  if (toolResults.length > 0 && result.length === 0) {
    result.push(`${agent} used ${toolResults.length} ${toolResults.length === 1 ? 'tool' : 'tools'} to complete the task`)
  }

  // Summarize assistant messages
  const assistantMsgs = msgs.filter(m => m.role === 'assistant' && m.content)
  if (assistantMsgs.length > 0) {
    result.push(`${agent} produced ${assistantMsgs.length} response ${assistantMsgs.length === 1 ? 'message' : 'messages'}`)
  }

  return result
})

function extractToolName(content: string): string {
  try {
    const parsed = JSON.parse(content)
    return parsed.name ?? parsed.tool ?? content
  } catch {
    return content
  }
}
</script>
