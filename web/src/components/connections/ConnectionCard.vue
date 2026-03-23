<!-- web/src/components/connections/ConnectionCard.vue -->
<template>
  <div
    class="flex flex-col rounded-xl border transition-all duration-150 overflow-hidden"
    :class="cardClass"
  >
    <!-- Card header -->
    <div class="flex items-start gap-3 px-4 pt-4 pb-3">
      <!-- Icon -->
      <div
        class="w-8 h-8 rounded-lg flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
        :style="{ backgroundColor: conn.iconColor }"
      >{{ conn.icon }}</div>

      <!-- Name + description -->
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2">
          <span class="text-xs font-medium text-huginn-text truncate">{{ conn.name }}</span>
          <span
            v-if="conn.type === 'coming_soon'"
            class="text-[9px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted flex-shrink-0"
          >Soon</span>
        </div>
        <p class="text-[10px] text-huginn-muted mt-0.5 leading-relaxed line-clamp-2">{{ conn.description }}</p>
      </div>
    </div>

    <!-- Connected state: account rows -->
    <template v-if="conn.state?.connected">
      <!-- + Add Account button (multi-account providers only) -->
      <div class="flex items-center justify-end px-4 pb-1 mt-auto">
        <button
          v-if="conn.multiAccount"
          @click="$emit('connect')"
          class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0"
        >+ Add Account</button>
      </div>

      <!-- Account rows -->
      <div class="flex flex-col gap-0.5 px-4 pb-3">
        <!-- Account rows (OAuth and system tools with multiple accounts) -->
        <div
          v-for="(acct, idx) in conn.state.accounts"
          :key="acct.id"
          class="flex items-center gap-1.5 min-w-0 py-0.5"
        >
          <div class="w-1.5 h-1.5 rounded-full bg-huginn-green flex-shrink-0" style="box-shadow:0 0 4px rgba(63,185,80,0.5)" />
          <span class="text-[10px] text-huginn-muted truncate flex-1">{{ acct.label }}</span>

          <!-- Default badge: first OAuth account, or active system account -->
          <span
            v-if="(conn.type === 'oauth' && idx === 0 && (conn.state.accounts?.length ?? 0) > 1) ||
                  (conn.type === 'system' && acct.label === conn.state.identity)"
            class="text-[9px] px-1 py-0.5 rounded border border-huginn-border text-huginn-muted flex-shrink-0">
            default
          </span>

          <!-- Set Default button (system tools, non-active accounts only) -->
          <button
            v-if="conn.type === 'system' && acct.label !== conn.state.identity"
            @click="$emit('setDefault', acct.id)"
            class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0 ml-1"
            :title="`Set ${acct.label} as default`"
          >Set Default</button>

          <!-- Disconnect button (OAuth only) -->
          <button
            v-else-if="conn.type === 'oauth'"
            @click="$emit('disconnect', acct.id)"
            class="text-[10px] text-huginn-muted hover:text-huginn-red transition-colors flex-shrink-0 ml-1"
            :title="`Disconnect ${acct.label}`"
          >×</button>
        </div>

        <!-- Fallback: system tools (AWS, gcloud, GitHub CLI) or legacy single-account without accounts array.
             System tools have no API delete endpoint — no disconnect button is intentional. -->
        <div v-if="!conn.state.accounts?.length" class="flex items-center gap-1.5 min-w-0 py-0.5">
          <div class="w-1.5 h-1.5 rounded-full bg-huginn-green flex-shrink-0" style="box-shadow:0 0 4px rgba(63,185,80,0.5)" />
          <span class="text-[10px] text-huginn-muted truncate flex-1">{{ conn.state.identity || 'Connected' }}</span>
          <template v-if="conn.state.profiles?.length">
            <span v-for="p in conn.state.profiles" :key="p"
              class="text-[9px] px-1 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ p }}</span>
          </template>
        </div>
      </div>
    </template>

    <!-- Footer: available / coming soon states only -->
    <div v-if="!conn.state?.connected" class="flex items-center justify-between px-4 pb-3 mt-auto">
      <!-- Available -->
      <template v-if="conn.type !== 'coming_soon'">
        <span class="text-[10px] text-huginn-muted/50">Not connected</span>
        <button
          @click="$emit('connect')"
          class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0"
        >Connect →</button>
      </template>

      <!-- Coming soon -->
      <template v-else>
        <span class="text-[10px] text-huginn-muted/40">Coming soon</span>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogConnection } from '../../composables/useConnectionsCatalog'

const props = defineProps<{
  conn: CatalogConnection
}>()

defineEmits<{
  connect: []
  disconnect: [id: string]   // OAuth: connection UUID to DELETE
  setDefault: [id: string]   // system tools: username to set as active
}>()

const cardClass = computed(() => {
  if (props.conn.type === 'coming_soon') {
    return 'border-huginn-border bg-huginn-surface/20 opacity-60'
  }
  if (props.conn.state?.connected) {
    return 'border-huginn-green/30 bg-huginn-surface/60'
  }
  return 'border-huginn-border bg-huginn-surface/50 hover:border-huginn-border/80'
})
</script>
