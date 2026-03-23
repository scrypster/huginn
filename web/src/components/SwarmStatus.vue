<template>
  <div v-if="swarmState"
    class="swarm-status rounded-xl border overflow-hidden"
    :class="swarmState.complete ? 'border-huginn-border/50' : 'border-huginn-blue/30'"
    style="background:rgba(22,27,34,0.8)">

    <!-- Header -->
    <div class="flex items-center gap-2 px-3 py-2.5"
      :style="swarmState.complete ? '' : 'background:rgba(88,166,255,0.05)'">
      <span class="text-huginn-blue text-xs font-bold flex-shrink-0">⚡</span>
      <span class="text-xs font-semibold text-huginn-text flex-1 truncate">
        {{ swarmState.complete ? 'Swarm complete' : 'Swarm running' }}
        <span v-if="swarmState.cancelled" class="text-huginn-muted"> (cancelled)</span>
      </span>
      <span class="text-[10px] text-huginn-muted whitespace-nowrap">
        {{ swarmState.agents.length }} agents
      </span>
    </div>

    <!-- Agent list -->
    <div class="px-3 pb-3 space-y-2">
      <div v-for="ag in swarmState.agents" :key="ag.id"
        class="swarm-agent flex items-start gap-2">
        <!-- Status indicator -->
        <span class="text-[10px] flex-shrink-0 w-4 text-center mt-0.5">
          <span v-if="ag.status === 'running'" class="text-huginn-blue animate-pulse">◉</span>
          <span v-else-if="ag.status === 'done' && ag.success !== false" class="text-huginn-green">✓</span>
          <span v-else-if="ag.status === 'error' || ag.success === false" class="text-huginn-red">✗</span>
          <span v-else-if="ag.status === 'cancelled'" class="text-huginn-muted">⊘</span>
          <span v-else class="text-huginn-muted/50">○</span>
        </span>
        <!-- Agent label -->
        <span class="text-[10px] font-mono text-huginn-muted w-24 flex-shrink-0 truncate">
          {{ ag.name }}
        </span>
        <!-- Output / error -->
        <span v-if="ag.error" class="text-[10px] text-huginn-red truncate flex-1">
          {{ ag.error }}
        </span>
        <span v-else-if="ag.output" class="text-[10px] text-huginn-muted truncate flex-1 font-mono">
          {{ ag.output.slice(-120) }}
        </span>
      </div>
    </div>

    <!-- Dropped events warning -->
    <div v-if="swarmState.droppedEvents > 0"
      class="px-3 pb-2 text-[10px] text-huginn-muted/60">
      ⚠ {{ swarmState.droppedEvents }} event(s) dropped under load
    </div>
  </div>
</template>

<script setup lang="ts">
import { useSwarmStatus } from '../composables/useSwarmStatus'

const { swarmState } = useSwarmStatus()
</script>
