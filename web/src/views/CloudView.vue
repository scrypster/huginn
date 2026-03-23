<template>
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-2 border-b border-huginn-border bg-huginn-surface flex-shrink-0">
      <span class="text-huginn-blue text-sm font-bold">cloud</span>
      <button @click="fetchStatus" class="text-huginn-muted text-xs hover:text-huginn-blue">refresh</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      <div v-if="loading" class="text-huginn-muted text-sm">Loading...</div>
      <div v-else-if="error" class="text-huginn-red text-sm">{{ error }}</div>
      <div v-else class="space-y-6 max-w-lg">

        <!-- Status card -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Connection</h2>
          <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4 space-y-3">

            <!-- Status row -->
            <div class="flex items-center gap-2.5">
              <div class="w-2 h-2 rounded-full flex-shrink-0"
                :class="status.connected ? 'bg-huginn-green' : status.registered ? 'bg-huginn-yellow' : 'bg-huginn-muted'"
                :style="status.connected ? 'box-shadow:0 0 6px rgba(63,185,80,0.5)' : ''" />
              <span class="text-sm text-huginn-text">
                {{ status.connected ? 'Connected to HuginnCloud' : status.registered ? 'Registered — relay not running' : 'Not connected' }}
              </span>
            </div>

            <!-- Machine ID -->
            <div v-if="status.machine_id" class="flex items-center gap-2">
              <span class="text-[11px] text-huginn-muted uppercase tracking-widest w-24">Machine</span>
              <span class="text-xs font-mono text-huginn-text">{{ status.machine_id }}</span>
            </div>

            <!-- Cloud URL -->
            <div v-if="status.cloud_url" class="flex items-center gap-2">
              <span class="text-[11px] text-huginn-muted uppercase tracking-widest w-24">Endpoint</span>
              <span class="text-xs font-mono text-huginn-muted">{{ status.cloud_url }}</span>
            </div>
          </div>
        </section>

        <!-- Actions -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Actions</h2>
          <div class="flex gap-3">
            <button v-if="!status.registered" @click="connect" :disabled="connecting"
              class="px-4 py-2 text-sm font-medium text-white rounded-lg transition-all disabled:opacity-50 bg-huginn-blue/90">
              {{ connecting ? 'Connecting…' : 'Connect to HuginnCloud' }}
            </button>
            <button v-else @click="disconnect" :disabled="disconnecting"
              class="px-4 py-2 text-sm font-medium text-huginn-muted border border-huginn-border rounded-lg hover:text-huginn-red hover:border-huginn-red transition-all disabled:opacity-50">
              {{ disconnecting ? 'Disconnecting…' : 'Disconnect' }}
            </button>
          </div>
          <p v-if="connecting" class="text-xs text-huginn-muted mt-2">
            A browser window should open — complete login there.
          </p>
        </section>

      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useCloud } from '../composables/useCloud'

const { status, loading, connecting, disconnecting, error, fetchStatus, connect, disconnect } = useCloud()

onMounted(fetchStatus)
</script>
