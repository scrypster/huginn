<template>
  <div
    data-testid="agent-card"
    @click="$emit('click')"
    class="relative flex flex-col gap-3 p-4 rounded-xl border border-huginn-border bg-huginn-surface hover:bg-huginn-surface/80 cursor-pointer transition-colors group"
  >
    <!-- Edit button: top-right, appears on hover, stops propagation so it doesn't trigger card click -->
    <button
      data-testid="agent-card-edit"
      @click.stop="$emit('edit')"
      class="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity text-[10px] px-2 py-1 rounded"
      style="background:rgba(255,255,255,0.06);color:var(--color-text-muted, #8b949e)"
    >Edit</button>

    <!-- Avatar -->
    <div
      class="w-12 h-12 rounded-xl flex items-center justify-center text-lg font-bold text-white flex-shrink-0 select-none"
      :style="{ background: agent.color || '#58a6ff' }"
    >
      {{ agent.icon || (agent.name?.[0]?.toUpperCase() ?? '?') }}
    </div>

    <!-- Name + description -->
    <div class="min-w-0">
      <p class="text-sm font-semibold truncate" style="color:var(--color-text, #e6edf3)">{{ agent.name }}</p>
      <p class="text-xs mt-0.5 leading-relaxed" style="color:var(--color-text-muted, #8b949e);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">
        {{ displayDescription }}
      </p>
    </div>

    <!-- Badges -->
    <div class="flex items-center gap-1.5 flex-wrap">
      <!-- Heartbeat -->
      <span
        v-if="agent.heartbeat_enabled"
        class="text-[10px] px-1.5 py-0.5 rounded-full flex items-center gap-1"
        style="background:rgba(63,185,80,0.1);color:#3fb950"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse inline-block" />
        Heartbeat
      </span>

      <!-- Memory — hidden when the Memory product is parked (no usable vaults) -->
      <span
        v-if="advertiseMemory && agent.vault_name"
        class="text-[10px] px-1.5 py-0.5 rounded-full"
        style="background:rgba(88,166,255,0.1);color:#58a6ff"
      >🧠 Memory</span>
      <span
        v-else-if="advertiseMemory"
        class="text-[10px] px-1.5 py-0.5 rounded-full"
        style="background:rgba(255,255,255,0.06);color:#8b949e"
      >No memory</span>
    </div>

    <ModelToolWarning v-if="unreliableForTools" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { AgentSummary } from '../composables/useAgents'
import { agentDisplayDescription } from '../utils/agentDescription'
import { modelUnreliableForTools } from '../views/agents/modelToolCapabilities'
import ModelToolWarning from './ModelToolWarning.vue'

const props = withDefaults(defineProps<{
  agent: AgentSummary
  advertiseMemory?: boolean
  supportsTools?: boolean
}>(), {
  advertiseMemory: true,
})
defineEmits<{ (e: 'click'): void; (e: 'edit'): void }>()

const displayDescription = computed(() => agentDisplayDescription(props.agent))
const unreliableForTools = computed(() =>
  modelUnreliableForTools({
    name: props.agent.model,
    supportsTools: props.supportsTools ?? (props.agent as { supportsTools?: boolean }).supportsTools,
  }),
)
</script>
