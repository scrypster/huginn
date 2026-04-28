<template>
  <div class="space-y-6">
    <p class="text-xs text-huginn-muted">Model Context Protocol servers provide external tools and data to your agents.</p>

    <section v-if="mcpServers.length > 0" class="space-y-3">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Configured Servers</h3>
      <div class="space-y-2">
        <div
          v-for="(srv, idx) in mcpServers"
          :key="srv.name"
          class="px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="flex-1 min-w-0 space-y-0.5">
              <p class="text-xs font-medium text-huginn-text font-mono">{{ srv.name }}</p>
              <p class="text-[11px] text-huginn-muted">
                <span class="px-1.5 py-0.5 rounded border border-huginn-border text-[10px]">{{ srv.transport }}</span>
                <span class="ml-2 font-mono truncate">{{ srv.command || srv.url || '' }}</span>
              </p>
              <div v-if="srv.env && Object.keys(srv.env).length > 0" class="text-[10px] text-huginn-muted/70 font-mono space-y-0.5 mt-1">
                <div v-for="(val, key) in srv.env" :key="key">{{ key }}={{ val }}</div>
              </div>
            </div>
            <button
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-colors flex-shrink-0"
              @click="$emit('removeMcpServer', idx)"
            >
              Remove
            </button>
          </div>
        </div>
      </div>
    </section>

    <div v-if="mcpServers.length === 0" class="py-4 text-center">
      <p class="text-huginn-muted text-xs">No MCP servers configured.</p>
    </div>

    <div class="border-t border-huginn-border" />

    <section class="space-y-4">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Add Server</h3>
      <SettingsFieldRow label="Name" hint="Unique identifier">
        <input v-model="newMcp.name" placeholder="my-mcp-server" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Transport" hint="Connection method">
        <div class="relative">
          <select
            v-model="newMcp.transport"
            class="w-full appearance-none bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-8 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors cursor-pointer"
          >
            <option value="stdio">stdio (subprocess)</option>
            <option value="sse">sse (HTTP Server-Sent Events)</option>
            <option value="http">http (streamable HTTP)</option>
          </select>
          <svg class="absolute right-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="6 9 12 15 18 9" /></svg>
        </div>
      </SettingsFieldRow>
      <SettingsFieldRow v-if="newMcp.transport === 'stdio'" label="Command" hint="Executable path">
        <input v-model="newMcp.command" placeholder="/usr/local/bin/mcp-server" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono" />
      </SettingsFieldRow>
      <SettingsFieldRow v-if="newMcp.transport === 'stdio'" label="Args" hint="One arg per line">
        <textarea v-model="newMcp.argsText" rows="3" placeholder="--port&#10;8080" class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y" />
      </SettingsFieldRow>
      <SettingsFieldRow v-if="newMcp.transport !== 'stdio'" label="URL" hint="Server URL (https://)">
        <input v-model="newMcp.url" placeholder="https://my-mcp-server.example.com" class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors font-mono" />
      </SettingsFieldRow>
      <SettingsFieldRow label="Environment variables" hint="KEY=VALUE pairs, one per line. Secret values are redacted in display.">
        <textarea v-model="newMcp.envText" rows="4" placeholder="MY_API_TOKEN=sk-...&#10;BASE_URL=https://api.example.com" class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors resize-y" />
      </SettingsFieldRow>
      <p v-if="mcpAddError" class="text-xs text-huginn-red">{{ mcpAddError }}</p>
      <button
        class="px-4 py-2 rounded-lg text-xs font-medium border border-huginn-green/30 text-huginn-green hover:bg-huginn-green/10 transition-all"
        @click="$emit('addMcpServer')"
      >
        Add server
      </button>
    </section>
  </div>
</template>

<script setup lang="ts">
import SettingsFieldRow from './SettingsFieldRow.vue'
import type { MCPServer } from '../../composables/useConfig'

defineProps<{
  mcpServers: MCPServer[]
  newMcp: {
    name: string
    transport: string
    command: string
    argsText: string
    url: string
    envText: string
  }
  mcpAddError: string
}>()

defineEmits<{
  (e: 'addMcpServer'): void
  (e: 'removeMcpServer', idx: number): void
}>()
</script>
