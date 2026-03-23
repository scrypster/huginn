<template>
  <div class="flex flex-col h-full bg-huginn-bg">

    <!-- No agent selected -->
    <div v-if="!agentName" class="flex flex-col items-center justify-center h-full gap-5 pb-16">
      <div class="w-16 h-16 rounded-2xl flex items-center justify-center select-none"
        style="background:rgba(88,166,255,0.08);border:1px solid rgba(88,166,255,0.2)">
        <svg class="w-8 h-8 text-huginn-blue opacity-60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
          <circle cx="12" cy="8" r="4" /><path d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2" />
        </svg>
      </div>
      <div class="text-center space-y-1">
        <p class="text-huginn-text text-sm font-medium">Select an agent</p>
        <p class="text-huginn-muted text-xs">Choose from the sidebar or create a new one</p>
      </div>
      <button data-testid="new-agent-btn" @click="createNew"
        class="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-huginn-blue border border-huginn-blue/30 hover:bg-huginn-blue/10 transition-all duration-150 active:scale-95">
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
        </svg>
        New agent
      </button>
    </div>

    <!-- Agent editor -->
    <template v-else>

      <!-- Banners (delete confirm, save feedback) — always at top, full width -->
      <div class="flex-shrink-0 space-y-0">
        <div v-if="showDeleteConfirm" class="px-4 pt-3">
          <div class="flex items-center gap-3 px-4 py-3 rounded-xl border border-huginn-red/40 bg-huginn-red/8">
            <p class="text-xs text-huginn-red flex-1">Delete <strong>{{ form.name }}</strong>? This cannot be undone.</p>
            <button @click="deleteAgent" class="px-3 py-1.5 text-xs font-medium text-huginn-red border border-huginn-red/40 rounded-lg hover:bg-huginn-red/15 transition-all">Delete</button>
            <button @click="showDeleteConfirm = false" class="px-3 py-1.5 text-xs text-huginn-muted border border-huginn-border rounded-lg hover:bg-huginn-surface transition-all">Cancel</button>
          </div>
        </div>
        <div v-if="saveMsg" class="px-4 pt-3">
          <div class="px-4 py-2.5 rounded-xl border text-xs"
            :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
            {{ saveMsg }}
          </div>
        </div>
        <div v-if="loadError" class="px-4 pt-3">
          <div class="px-4 py-2.5 rounded-xl border text-xs border-huginn-red/40 text-huginn-red bg-huginn-red/8">
            {{ loadErrorMsg }}
          </div>
        </div>
        <div v-if="isStaleRefreshing" class="px-4 pt-3">
          <div class="flex items-center gap-2 px-4 py-2 rounded-xl border border-huginn-border text-xs text-huginn-muted bg-huginn-surface">
            <div class="w-3 h-3 border border-huginn-border border-t-huginn-blue rounded-full animate-spin flex-shrink-0"/>
            Refreshing…
          </div>
        </div>
      </div>

      <!-- Two-panel layout: identity sidebar + configuration main -->
      <div class="flex-1 overflow-hidden flex min-h-0">

        <!-- ── Left panel: Agent identity card ──────────────────────── -->
        <div class="w-64 flex-shrink-0 border-r border-huginn-border flex flex-col overflow-y-auto">

          <!-- Avatar hero -->
          <div class="flex flex-col items-center px-6 pt-8 pb-5 gap-4">
            <!-- Large live-preview avatar -->
            <div
              class="w-20 h-20 rounded-2xl flex items-center justify-center text-3xl font-bold text-white select-none shadow-lg transition-all duration-300"
              :style="{ background: form.color || '#58a6ff', boxShadow: `0 8px 24px ${form.color || '#58a6ff'}33` }">
              {{ form.icon || form.name?.[0]?.toUpperCase() || '?' }}
            </div>

            <!-- Inline name edit — looks like a heading, not a form field -->
            <div class="w-full space-y-0.5 text-center">
              <input
                v-model="form.name" @input="markDirty"
                placeholder="Agent name"
                class="w-full bg-transparent text-base font-semibold text-huginn-text text-center outline-none border-b border-transparent focus:border-huginn-blue/40 transition-colors placeholder:text-huginn-muted/40 pb-0.5" />
              <!-- Model selector — opens modal -->
              <button @click="showModelPicker = true"
                class="inline-flex items-center gap-1 group focus:outline-none">
                <!-- No model: amber attention pill -->
                <div v-if="!form.model"
                  class="flex items-center gap-1 px-2.5 py-1 rounded-full border border-huginn-amber/50 bg-huginn-amber/10 animate-pulse">
                  <svg class="w-2.5 h-2.5 text-huginn-amber flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
                  <span class="text-[11px] text-huginn-amber font-medium">No model selected</span>
                  <svg class="w-2.5 h-2.5 text-huginn-amber/70 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
                </div>
                <!-- Model set: subtle muted label -->
                <div v-else class="flex items-center gap-1">
                  <span class="text-[11px] text-huginn-muted group-hover:text-huginn-text transition-colors truncate max-w-[150px]">{{ form.model }}</span>
                  <svg class="w-2.5 h-2.5 text-huginn-muted/50 group-hover:text-huginn-muted transition-colors flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
                </div>
              </button>
            </div>
          </div>

          <!-- Divider -->
          <div class="mx-5 border-t border-huginn-border" />

          <!-- Identity fields -->
          <div class="flex-1 px-5 py-5 space-y-5">

            <!-- Color -->
            <div class="space-y-2.5">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">Color</p>
              <div class="flex items-center gap-2 flex-wrap">
                <button v-for="c in colorPalette" :key="c"
                  @click="form.color = c; markDirty()"
                  class="w-6 h-6 rounded-md transition-all duration-150 hover:scale-110 active:scale-95"
                  :class="form.color === c ? 'ring-2 ring-offset-2 ring-offset-huginn-bg scale-110' : ''"
                  :style="{ background: c }" />
                <input type="color" v-model="form.color" @change="markDirty"
                  class="w-6 h-6 rounded-md cursor-pointer bg-huginn-surface border border-huginn-border overflow-hidden" title="Custom color" />
              </div>
            </div>

            <!-- Icon letter -->
            <div class="space-y-2">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">Icon letter</p>
              <input v-model="form.icon" @input="markDirty" placeholder="A" maxlength="2"
                class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text text-center font-bold outline-none focus:border-huginn-blue/50 transition-colors tracking-widest" />
            </div>

            <!-- Memory -->
            <div class="space-y-2">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">Memory</p>

              <!-- Tier 1: None -->
              <button @click="form.memory_type = 'none'; form.memory_enabled = false; form.context_notes_enabled = false; markDirty()"
                class="w-full flex items-center gap-2.5 px-3 py-2.5 rounded-lg border text-left transition-all duration-150 active:scale-[0.98]"
                :class="form.memory_type === 'none'
                  ? 'border-huginn-border bg-huginn-surface/80 ring-1 ring-huginn-border'
                  : 'border-huginn-border/50 bg-transparent hover:bg-huginn-surface/40'">
                <div class="w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0 transition-colors"
                  :class="form.memory_type === 'none' ? 'border-huginn-text' : 'border-huginn-muted/40'">
                  <div v-if="form.memory_type === 'none'" class="w-1.5 h-1.5 rounded-full bg-huginn-text" />
                </div>
                <div class="flex-1 min-w-0">
                  <p class="text-[11px] font-medium leading-none" :class="form.memory_type === 'none' ? 'text-huginn-text' : 'text-huginn-muted'">No memory</p>
                  <p class="text-[10px] text-huginn-muted/60 mt-0.5 leading-snug">Starts fresh each conversation</p>
                </div>
              </button>

              <!-- Tier 2: Notes (static context) -->
              <button @click="form.memory_type = 'context'; form.memory_enabled = false; form.context_notes_enabled = true; markDirty()"
                class="w-full flex items-center gap-2.5 px-3 py-2.5 rounded-lg border text-left transition-all duration-150 active:scale-[0.98]"
                :class="form.memory_type === 'context'
                  ? 'border-huginn-blue/40 bg-huginn-blue/5 ring-1 ring-huginn-blue/20'
                  : 'border-huginn-border/50 bg-transparent hover:bg-huginn-surface/40'">
                <div class="w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0 transition-colors"
                  :class="form.memory_type === 'context' ? 'border-huginn-blue' : 'border-huginn-muted/40'">
                  <div v-if="form.memory_type === 'context'" class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
                </div>
                <div class="flex-1 min-w-0">
                  <p class="text-[11px] font-medium leading-none" :class="form.memory_type === 'context' ? 'text-huginn-text' : 'text-huginn-muted'">Context notes</p>
                  <p class="text-[10px] text-huginn-muted/60 mt-0.5 leading-snug">Agent writes to its own memory file</p>
                  <!-- File path info — visible when selected -->
                  <div v-if="form.memory_type === 'context'" class="mt-2 space-y-1" @click.stop>
                    <p class="text-[10px] text-huginn-muted/70 font-mono truncate">~/.huginn/agents/{{ form.name || 'agent' }}.memory.md</p>
                    <p class="text-[10px] text-huginn-muted/50">Edit this file directly to update the agent's memory.</p>
                  </div>
                </div>
              </button>

              <!-- Tier 3: MuninnDB — upgrade glow when not connected -->
              <button @click="muninnConnected ? selectMuninnDB() : null"
                :class="[
                  'w-full flex items-start gap-2.5 px-3 py-2.5 rounded-lg border text-left transition-all duration-150',
                  form.memory_type === 'muninndb'
                    ? 'border-huginn-blue/50 bg-huginn-blue/8 ring-1 ring-huginn-blue/30'
                    : muninnConnected
                      ? 'border-huginn-border/50 bg-transparent hover:bg-huginn-surface/40 active:scale-[0.98] cursor-pointer'
                      : 'border-huginn-amber/30 bg-huginn-amber/5 cursor-default',
                ]"
                :style="!muninnConnected && form.memory_type !== 'muninndb'
                  ? 'box-shadow: 0 0 12px rgba(227,179,65,0.12)'
                  : ''">
                <div class="mt-0.5 w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0 transition-colors"
                  :class="form.memory_type === 'muninndb' ? 'border-huginn-blue' : muninnConnected ? 'border-huginn-muted/40' : 'border-huginn-amber/40'">
                  <div v-if="form.memory_type === 'muninndb'" class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
                </div>
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-1.5 flex-wrap">
                    <p class="text-[11px] font-medium leading-none"
                      :class="form.memory_type === 'muninndb' ? 'text-huginn-text' : muninnConnected ? 'text-huginn-muted' : 'text-huginn-amber'">
                      MuninnDB
                    </p>
                    <!-- Connected indicator -->
                    <span v-if="muninnConnected && form.memory_type === 'muninndb'"
                      class="flex items-center gap-1 text-[8px] text-huginn-green font-medium">
                      <span class="w-1 h-1 rounded-full bg-huginn-green" />connected
                    </span>
                    <!-- Upgrade badge when not connected -->
                    <span v-if="!muninnConnected"
                      class="text-[8px] px-1.5 py-0.5 rounded-full font-semibold border border-huginn-amber/50 text-huginn-amber bg-huginn-amber/10">
                      ✦ Upgrade
                    </span>
                  </div>
                  <p class="text-[10px] mt-0.5 leading-snug"
                    :class="muninnConnected ? 'text-huginn-muted/60' : 'text-huginn-amber/70'">
                    Cognitive, brain-like memory
                  </p>
                  <!-- Not connected CTA -->
                  <router-link v-if="!muninnConnected"
                    :to="{ path: '/connections', query: { category: 'databases', search: 'muninndb' } }"
                    @click.stop
                    class="inline-flex items-center gap-1 mt-1.5 text-[10px] text-huginn-amber hover:text-huginn-amber/80 font-medium transition-colors">
                    Connect MuninnDB →
                  </router-link>
                  <!-- Vault input when selected + connected -->
                  <div v-if="form.memory_type === 'muninndb' && muninnConnected" class="mt-2 space-y-1.5" @click.stop>
                    <!-- Compact summary row -->
                    <div class="flex items-center gap-2">
                      <!-- Vault chip -->
                      <span v-if="form.vault_name"
                        class="flex-1 min-w-0 truncate text-[10px] font-mono text-huginn-muted/70 bg-huginn-bg/60 rounded px-1.5 py-0.5 border border-huginn-border/30">
                        {{ form.vault_name }}
                        <span v-if="!allVaultNames.includes(form.vault_name)"
                          class="ml-1 text-[9px] font-sans text-huginn-amber/70 uppercase tracking-wide">new</span>
                      </span>
                      <span v-else class="flex-1 text-[10px] text-huginn-muted/40 italic">Not configured</span>
                      <!-- Mode badge -->
                      <span v-if="form.memory_mode && form.memory_mode !== 'conversational'"
                        class="text-[9px] uppercase tracking-wide font-medium px-1.5 py-0.5 rounded"
                        :class="form.memory_mode === 'immersive' ? 'bg-huginn-blue/10 text-huginn-blue/70' : 'bg-huginn-muted/10 text-huginn-muted/50'">
                        {{ form.memory_mode }}
                      </span>
                      <!-- Vault health indicator: dot + latency -->
                      <span v-if="vaultHealth.status !== 'unknown'"
                        class="flex items-center gap-1 shrink-0"
                        :title="vaultHealth.warning || (vaultHealth.status === 'ok' ? `${vaultHealth.tools_count} tools` : vaultHealth.status)">
                        <span class="w-1.5 h-1.5 rounded-full inline-block"
                          :class="{
                            'bg-huginn-green': vaultHealth.status === 'ok',
                            'bg-huginn-amber': vaultHealth.status === 'degraded',
                            'bg-huginn-red': vaultHealth.status === 'unavailable',
                          }"></span>
                        <span v-if="vaultHealth.status === 'ok'" class="text-[9px] text-huginn-muted/60 tabular-nums">{{ vaultHealth.latency_ms }}ms</span>
                      </span>
                      <!-- Configure button -->
                      <button @click.stop="openMemoryModal()"
                        class="shrink-0 text-[10px] text-huginn-blue/70 hover:text-huginn-blue px-1.5 py-0.5 rounded border border-huginn-blue/20 hover:border-huginn-blue/50 transition-colors">
                        Configure…
                      </button>
                    </div>
                  </div>
                </div>
              </button>
            </div>

          </div>

          <!-- Bottom actions -->
          <div class="px-5 py-4 border-t border-huginn-border space-y-2 flex-shrink-0">
            <button
              v-if="!isActive && agentName && agentName !== 'new'"
              @click="setActive"
              class="w-full flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg text-xs text-huginn-muted border border-huginn-border hover:border-huginn-blue/40 hover:text-huginn-blue transition-all duration-150">
              <svg class="w-3 h-3 opacity-60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>
              Set as default
            </button>
            <div v-if="isActive"
              class="flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg border border-huginn-green/40 text-huginn-green text-xs font-medium">
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              Default agent
            </div>
            <button @click="confirmDelete"
              class="w-full flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg text-xs text-huginn-muted border border-huginn-border hover:border-huginn-red/40 hover:text-huginn-red transition-all duration-150">
              <svg class="w-3 h-3 opacity-60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6M14 11v6M9 6V4a1 1 0 011-1h4a1 1 0 011 1v2"/></svg>
              Delete agent
            </button>
          </div>
        </div>

        <!-- ── Right panel: Configuration ───────────────────────────── -->
        <div class="flex-1 overflow-y-auto">
          <div class="px-8 py-6 space-y-7 max-w-3xl">

            <!-- System prompt -->
            <section class="space-y-3">
              <div class="flex items-center justify-between">
                <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">System Prompt</h3>
                <span class="text-[11px] text-huginn-muted tabular-nums">{{ form.system_prompt?.length || 0 }} chars</span>
              </div>
              <textarea v-model="form.system_prompt" @input="markDirty"
                placeholder="You are a helpful AI agent. Describe the agent's personality, expertise, and communication style..."
                rows="12"
                class="w-full bg-huginn-surface border border-huginn-border rounded-xl px-4 py-3 text-sm text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors resize-y leading-relaxed min-h-[200px]" />
            </section>

            <div class="border-t border-huginn-border" />

            <!-- ── Local Access ───────────────────────────────────────────── -->
            <section data-testid="local-access-section" class="space-y-3 pb-8">
              <div class="flex items-center justify-between">
                <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Local Access</h3>
                <span class="text-[11px] text-huginn-muted">{{ localAccessSummary }}</span>
              </div>
              <p class="text-[11px] text-huginn-muted leading-relaxed">Grant this agent access to the local file system, git, and shell.</p>
              <!-- Allow-all quick toggle -->
              <div class="flex items-center gap-2">
                <button
                  data-testid="local-access-allow-all-btn"
                  @click="toggleLocalAllowAll"
                  class="px-2 py-1 rounded text-[11px] font-semibold transition-all"
                  :style="isLocalAllowAll ? 'background:rgba(63,185,80,0.15);border:1px solid #3fb950;color:#3fb950' : 'border:1px solid rgba(255,255,255,0.12);color:rgba(255,255,255,0.35)'"
                >
                  {{ isLocalAllowAll ? '✓ Allow all' : 'Allow all' }}
                </button>
              </div>
              <div class="flex flex-wrap gap-2 items-center min-h-[24px]">
                <template v-if="form.local_tools.length && !isLocalAllowAll">
                  <span
                    v-for="name in form.local_tools" :key="name"
                    class="px-2 py-0.5 rounded text-[11px] font-mono"
                    style="background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.12);color:rgba(255,255,255,0.55)"
                  >{{ name }}</span>
                </template>
                <span v-else-if="!isLocalAllowAll" class="text-[11px] text-huginn-muted/50 italic self-center">No local access granted</span>
              </div>
              <button data-testid="manage-local-access-btn" @click="openLocalAccessModal"
                class="flex items-center gap-1.5 px-3 py-1.5 rounded text-[11px] transition-all"
                style="border:1px solid rgba(255,255,255,0.12);color:rgba(255,255,255,0.55)">
                <span>✏</span> Manage local access
              </button>
            </section>

            <div class="border-t border-huginn-border" />

            <!-- Connections / Toolbelt -->
            <section data-testid="toolbelt-section" class="space-y-3 pb-8">
              <div class="flex items-center justify-between">
                <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Connections</h3>
                <span class="text-[11px] text-huginn-muted">{{ connectionsSummary }}</span>
              </div>
              <p class="text-[11px] text-huginn-muted leading-relaxed">Grant this agent access to external services and cloud tools.</p>
              <div class="flex items-center gap-2">
                <button
                  data-testid="connections-allow-all-btn"
                  @click="toggleConnectionsAllowAll"
                  class="px-2 py-1 rounded text-[11px] font-semibold transition-all"
                  :style="isConnectionsAllowAll ? 'background:rgba(63,185,80,0.15);border:1px solid #3fb950;color:#3fb950' : 'border:1px solid rgba(255,255,255,0.12);color:rgba(255,255,255,0.35)'"
                >
                  {{ isConnectionsAllowAll ? '✓ Allow all' : 'Allow all' }}
                </button>
              </div>

              <!-- Assigned chips summary -->
              <div class="flex flex-wrap gap-1.5 min-h-[24px]">
                <template v-if="form.toolbelt.length">
                  <span
                    v-for="entry in form.toolbelt"
                    :key="entry.connection_id + ':' + (entry.profile ?? '')"
                    data-testid="toolbelt-entry"
                    class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-huginn-surface border border-huginn-border text-[11px] text-huginn-text">
                    <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue flex-shrink-0" />
                    <span data-testid="toolbelt-provider-badge">{{ connectionLabel(entry.connection_id) }}</span>
                    <span v-if="entry.profile" class="text-huginn-muted font-mono">({{ entry.profile }})</span>
                  </span>
                </template>
                <span v-else class="text-[11px] text-huginn-muted/50 italic self-center">No connections granted</span>
              </div>

              <button data-testid="add-toolbelt-btn" @click="openConnectionsModal"
                class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-huginn-border text-xs text-huginn-muted hover:border-huginn-blue/40 hover:text-huginn-blue transition-all duration-150 active:scale-95">
                <svg class="w-3.5 h-3.5 opacity-70" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 013 3L7 19l-4 1 1-4 12.5-12.5z"/>
                </svg>
                Manage connections
              </button>
            </section>

            <div class="border-t border-huginn-border" />

            <!-- Skills -->
            <section class="space-y-3 pb-8">
              <div class="flex items-center justify-between">
                <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Skills</h3>
                <span class="text-[11px] text-huginn-muted">{{ form.skills.length ? form.skills.length + ' assigned' : 'none' }}</span>
              </div>
              <p class="text-[11px] text-huginn-muted leading-relaxed">Assign skills to shape how this agent thinks and works.</p>

              <!-- Assigned chips summary -->
              <div class="flex flex-wrap gap-1.5 min-h-[24px]">
                <template v-if="form.skills.length">
                  <span v-for="skill in form.skills" :key="skill"
                    class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-huginn-surface border border-huginn-border text-[11px] text-huginn-text">
                    <span class="w-1.5 h-1.5 rounded-full bg-huginn-green flex-shrink-0" />
                    {{ skill }}
                  </span>
                </template>
                <span v-else class="text-[11px] text-huginn-muted/50 italic self-center">No skills assigned — uses global defaults</span>
              </div>

              <button @click="openSkillsModal"
                class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-huginn-border text-xs text-huginn-muted hover:border-huginn-blue/40 hover:text-huginn-blue transition-all duration-150 active:scale-95">
                <svg class="w-3.5 h-3.5 opacity-70" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 013 3L7 19l-4 1 1-4 12.5-12.5z"/>
                </svg>
                Manage skills
              </button>
            </section>

          </div>
        </div>
      </div>

      <!-- Bottom save bar -->
      <div v-if="dirty" class="flex-shrink-0 px-5 py-3 border-t border-huginn-border/50">
        <div class="flex items-center justify-between px-4 py-3 rounded-xl border border-huginn-blue/30 bg-huginn-blue/8">
          <div class="flex items-center gap-2">
            <span class="w-1.5 h-1.5 rounded-full bg-huginn-yellow animate-pulse" />
            <p class="text-xs text-huginn-muted">Unsaved changes</p>
          </div>
          <div class="flex gap-2">
            <button @click="discard" class="px-3 py-1.5 text-xs text-huginn-muted border border-huginn-border rounded-lg hover:bg-huginn-surface transition-all">Discard</button>
            <button data-testid="save-agent-btn-sticky" @click="save" :disabled="saving"
              class="px-4 py-1.5 text-xs font-medium text-white rounded-lg transition-all active:scale-95 disabled:opacity-50"
              style="background:rgba(88,166,255,0.9)">
              {{ saving ? 'Saving...' : 'Save changes' }}
            </button>
          </div>
        </div>
      </div>
    </template>
  </div>

  <!-- ── Connections Manager Modal ───────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="showConnectionsModal"
        class="fixed inset-0 z-[200] flex items-center justify-center p-4"
        @mousedown.self="showConnectionsModal = false">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />

        <div class="relative w-full max-w-2xl bg-[#13151a] border border-white/[0.07] rounded-2xl flex flex-col overflow-hidden" style="max-height:80vh;box-shadow:0 25px 60px rgba(0,0,0,0.55)">

          <!-- Blue accent line at top -->
          <div class="h-px flex-shrink-0" style="background:linear-gradient(90deg,transparent,rgba(88,166,255,0.5),transparent)" />

          <!-- Header -->
          <div class="flex items-center gap-3.5 px-5 pt-4 pb-3.5 border-b border-white/[0.06] flex-shrink-0">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0" style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.2)">
              <svg class="w-4 h-4" style="color:rgba(88,166,255,0.85)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
              </svg>
            </div>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">Manage Connections</p>
              <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.35)">
                {{ modalToolbelt.length ? `${modalToolbelt.length} connection${modalToolbelt.length !== 1 ? 's' : ''} assigned` : 'Add connections to grant access' }}
              </p>
            </div>
            <button @click="showConnectionsModal = false"
              class="w-7 h-7 flex items-center justify-center rounded-lg transition-all duration-150"
              style="color:rgba(255,255,255,0.3)"
              @mouseenter="e => (e.target as HTMLElement).style.color='rgba(255,255,255,0.7)'"
              @mouseleave="e => (e.target as HTMLElement).style.color='rgba(255,255,255,0.3)'">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>

          <!-- Two-panel body -->
          <div class="flex flex-1 min-h-0 overflow-hidden">

            <!-- Left: Available sidebar -->
            <div class="w-[44%] flex-shrink-0 flex flex-col overflow-hidden" style="background:#0a0d12;border-right:1px solid rgba(255,255,255,0.055)">
              <div class="px-4 py-3 flex-shrink-0 flex items-center justify-between">
                <p class="text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.28);letter-spacing:0.14em">Available</p>
                <button
                  v-if="modalAddableConnections.length > 0 || modalAddableSystemToolsForModal.length > 0"
                  @click="modalAddAll"
                  class="text-[10px] transition-colors"
                  style="color:rgba(88,166,255,0.7)"
                  @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(88,166,255,1)'"
                  @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(88,166,255,0.7)'">
                  Add all →
                </button>
              </div>
              <div class="flex-1 overflow-y-auto pb-2">

                <!-- Empty state -->
                <div v-if="modalAddableConnections.length === 0 && modalAddableSystemToolsForModal.length === 0"
                  class="flex flex-col items-center justify-center px-6 py-10 gap-3 text-center">
                  <div class="w-10 h-10 rounded-xl flex items-center justify-center" style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.07)">
                    <svg class="w-5 h-5" style="color:rgba(255,255,255,0.2)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
                  </div>
                  <p class="text-xs" style="color:rgba(255,255,255,0.28)">
                    {{ availableConnections.length === 0 && systemTools.length === 0 ? 'No connections yet' : 'All assigned' }}
                  </p>
                  <router-link v-if="availableConnections.length === 0 && systemTools.length === 0"
                    to="/connections" @click="showConnectionsModal = false"
                    class="text-huginn-blue text-[11px] hover:underline">Set up →</router-link>
                </div>

                <!-- MCP Connections -->
                <template v-if="modalAddableConnections.length">
                  <p class="px-4 pt-1 pb-1.5 text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.2);letter-spacing:0.12em">MCP Connections</p>
                  <div class="px-2 space-y-0.5">
                    <button v-for="conn in modalAddableConnections" :key="conn.id"
                      @click="modalAddConnection(conn)"
                      class="w-full group flex items-center gap-3 px-3 py-2.5 rounded-xl text-left relative"
                      :style="hoveredAvailableConn === conn.id
                        ? 'background:rgba(88,166,255,0.06);border:1px solid #58a6ff;box-shadow:0 0 0 1px rgba(88,166,255,0.2),0 0 10px rgba(88,166,255,0.1);transition:all 0.15s'
                        : 'background:transparent;border:1px solid transparent;transition:all 0.15s'"
                      @mouseover="hoveredAvailableConn = conn.id"
                      @mouseout="hoveredAvailableConn = ''">
                      <div class="w-8 h-8 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0 transition-transform duration-100"
                        :style="{ background: connectionIcon(conn.id).bg, color: connectionIcon(conn.id).fg }">
                        {{ connectionIcon(conn.id).label }}
                      </div>
                      <div class="flex-1 min-w-0">
                        <p class="text-[13px] font-medium truncate transition-colors duration-100" style="color:rgba(255,255,255,0.72)">{{ conn.account_label || conn.provider }}</p>
                        <p class="text-[10px]" style="color:rgba(255,255,255,0.3)">{{ conn.provider }}</p>
                      </div>
                      <svg class="w-3.5 h-3.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity duration-100" style="color:rgba(88,166,255,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                    </button>
                  </div>
                </template>

                <!-- System CLI tools -->
                <template v-if="modalAddableSystemToolsForModal.length">
                  <p class="px-4 pt-3 pb-1.5 text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.2);letter-spacing:0.12em">System CLI</p>
                  <div class="px-2 space-y-0.5">
                    <div v-for="tool in modalAddableSystemToolsForModal" :key="tool.name">
                      <template v-if="tool.profiles && tool.profiles.length > 1">
                        <template v-for="p in tool.profiles" :key="p">
                          <button v-if="!modalIsProfileAdded(tool, p)"
                            @click="modalAddSystemTool(tool, p)"
                            class="w-full group flex items-center gap-3 px-3 py-2.5 rounded-xl text-left"
                            :style="hoveredAvailableConn === 'system:' + tool.name + ':' + p
                              ? 'background:rgba(88,166,255,0.06);border:1px solid #58a6ff;box-shadow:0 0 0 1px rgba(88,166,255,0.2),0 0 10px rgba(88,166,255,0.1);transition:all 0.15s'
                              : 'background:transparent;border:1px solid transparent;transition:all 0.15s'"
                            @mouseover="hoveredAvailableConn = 'system:' + tool.name + ':' + p"
                            @mouseout="hoveredAvailableConn = ''">
                            <div class="w-8 h-8 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                              :style="{ background: connectionIcon('system:' + tool.name).bg, color: connectionIcon('system:' + tool.name).fg }">
                              {{ connectionIcon('system:' + tool.name).label }}
                            </div>
                            <div class="flex-1 min-w-0 flex items-center gap-2">
                              <p class="text-[13px] font-medium" style="color:rgba(255,255,255,0.72)">{{ tool.name }}</p>
                              <span class="text-[11px] font-mono px-1.5 py-0.5 rounded-md" style="background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.09);color:rgba(255,255,255,0.4)">{{ p }}</span>
                            </div>
                            <svg class="w-3.5 h-3.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity duration-100" style="color:rgba(88,166,255,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                          </button>
                        </template>
                      </template>
                      <template v-else>
                        <button @click="modalAddSystemTool(tool, tool.profiles?.[0] || '')"
                          class="w-full group flex items-center gap-3 px-3 py-2.5 rounded-xl text-left"
                          :style="hoveredAvailableConn === 'system:' + tool.name
                            ? 'background:rgba(88,166,255,0.06);border:1px solid #58a6ff;box-shadow:0 0 0 1px rgba(88,166,255,0.2),0 0 10px rgba(88,166,255,0.1);transition:all 0.15s'
                            : 'background:transparent;border:1px solid transparent;transition:all 0.15s'"
                          @mouseover="hoveredAvailableConn = 'system:' + tool.name"
                          @mouseout="hoveredAvailableConn = ''">
                          <div class="w-8 h-8 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                            :style="{ background: connectionIcon('system:' + tool.name).bg, color: connectionIcon('system:' + tool.name).fg }">
                            {{ connectionIcon('system:' + tool.name).label }}
                          </div>
                          <div class="flex-1 min-w-0">
                            <div class="flex items-center gap-1.5">
                              <p class="text-[13px] font-medium" style="color:rgba(255,255,255,0.72)">{{ tool.name }}</p>
                              <span class="text-[9px] px-1.5 py-0.5 rounded-md font-mono" style="background:rgba(255,255,255,0.06);border:1px solid rgba(255,255,255,0.09);color:rgba(255,255,255,0.3)">CLI</span>
                            </div>
                          </div>
                          <svg class="w-3.5 h-3.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity duration-100" style="color:rgba(88,166,255,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                        </button>
                      </template>
                    </div>
                  </div>
                </template>

              </div>
            </div>

            <!-- Right: Assigned -->
            <div class="flex-1 flex flex-col overflow-hidden">
              <div class="px-4 py-3 flex items-center justify-between flex-shrink-0">
                <div class="flex items-center gap-2">
                  <p class="text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.28);letter-spacing:0.14em">Assigned</p>
                  <span v-if="modalToolbelt.length" class="text-[10px] font-semibold tabular-nums" style="color:rgba(88,166,255,0.75)">{{ modalToolbelt.length }}</span>
                </div>
                <button
                  v-if="modalToolbelt.length > 0"
                  @click="modalRemoveAll"
                  class="text-[10px] transition-colors"
                  style="color:rgba(248,81,73,0.6)"
                  @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(248,81,73,1)'"
                  @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(248,81,73,0.6)'">
                  Remove all
                </button>
              </div>
              <div class="flex-1 overflow-y-auto px-3 pb-3">

                <!-- Empty state -->
                <div v-if="!modalToolbelt.length" class="flex flex-col items-center justify-center py-12 gap-3 text-center">
                  <div class="w-12 h-12 rounded-2xl flex items-center justify-center" style="background:rgba(255,255,255,0.03);border:1px solid rgba(255,255,255,0.06)">
                    <svg class="w-5 h-5" style="color:rgba(255,255,255,0.15)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
                  </div>
                  <div>
                    <p class="text-xs" style="color:rgba(255,255,255,0.22)">No connections granted</p>
                    <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.14)">Add connections to grant access</p>
                  </div>
                </div>

                <!-- Assigned cards -->
                <TransitionGroup name="list-item" tag="div" class="space-y-2.5">
                  <div v-for="(entry, idx) in modalToolbelt"
                    :key="entry.connection_id + ':' + (entry.profile ?? '')"
                    data-testid="toolbelt-entry"
                    class="rounded-xl overflow-hidden"
                    :style="hoveredAssignedIdx === idx
                      ? 'background:rgba(248,81,73,0.04);border:1px solid rgba(248,81,73,0.4);box-shadow:0 0 0 1px rgba(248,81,73,0.15),0 0 12px rgba(248,81,73,0.08);transition:all 0.15s'
                      : entry.approval_gate
                        ? 'background:rgba(227,179,65,0.05);border:1px solid rgba(227,179,65,0.22);transition:all 0.15s'
                        : 'background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.08);transition:all 0.15s'"
                    @mouseover="hoveredAssignedIdx = idx"
                    @mouseout="hoveredAssignedIdx = -1">

                    <!-- Top: connection info -->
                    <div class="flex items-center gap-3 px-3.5 pt-3 pb-2.5">
                      <div class="w-8 h-8 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                        :style="{ background: connectionIcon(entry.connection_id).bg, color: connectionIcon(entry.connection_id).fg }">
                        {{ connectionIcon(entry.connection_id).label }}
                      </div>
                      <div class="flex-1 min-w-0">
                        <p class="text-[13px] font-semibold truncate" style="color:rgba(255,255,255,0.88)">
                          <span data-testid="toolbelt-provider-badge">{{ connectionLabel(entry.connection_id) }}</span>
                        </p>
                        <div class="flex items-center gap-1.5">
                          <p class="text-[10px]" style="color:rgba(255,255,255,0.38)">{{ entry.provider }}</p>
                          <span v-if="entry.profile" class="text-[9px] px-1.5 py-0.5 rounded-md font-mono" style="background:rgba(0,0,0,0.3);border:1px solid rgba(255,255,255,0.1);color:rgba(255,255,255,0.35)">{{ entry.profile }}</span>
                        </div>
                      </div>
                      <button @click="modalRemoveEntry(idx)"
                        class="w-6 h-6 flex items-center justify-center rounded-lg flex-shrink-0 transition-all duration-150"
                        :style="hoveredAssignedIdx === idx ? 'color:rgba(248,81,73,0.65)' : 'color:rgba(255,255,255,0.22)'"
                        @mouseenter="e => { (e.currentTarget as HTMLElement).style.color='#f85149'; (e.currentTarget as HTMLElement).style.background='rgba(248,81,73,0.12)' }"
                        @mouseleave="e => { (e.currentTarget as HTMLElement).style.background='transparent' }">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                      </button>
                    </div>

                    <!-- Bottom: approval gate (MCP connections only) -->
                    <div v-if="!entry.connection_id.startsWith('system:')"
                      class="flex items-center justify-between px-3.5 py-2.5 transition-colors duration-200"
                      :style="entry.approval_gate
                        ? 'border-top:1px solid rgba(227,179,65,0.15)'
                        : 'border-top:1px solid rgba(255,255,255,0.05)'">
                      <div>
                        <p class="text-[11px] font-medium transition-colors duration-200"
                          :style="{ color: entry.approval_gate ? 'rgba(227,179,65,0.9)' : 'rgba(255,255,255,0.38)' }">
                          Require approval
                        </p>
                        <p class="text-[10px] transition-colors duration-200"
                          :style="{ color: entry.approval_gate ? 'rgba(227,179,65,0.5)' : 'rgba(255,255,255,0.22)' }">
                          {{ entry.approval_gate ? 'Agent will ask before making changes' : 'Agent acts without asking' }}
                        </p>
                      </div>
                      <button @click="modalToggleApprovalGate(idx)"
                        class="relative flex-shrink-0 ml-4 w-9 h-5 rounded-full transition-colors duration-200 focus:outline-none"
                        :style="{ background: entry.approval_gate ? '#e3b341' : 'rgba(255,255,255,0.14)' }">
                        <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow-md transition-transform duration-200"
                          :class="entry.approval_gate ? 'translate-x-4' : 'translate-x-0'" />
                      </button>
                    </div>

                  </div>
                </TransitionGroup>

              </div>
            </div>
          </div>

          <!-- Footer -->
          <div class="flex items-center justify-end gap-2.5 px-5 py-3.5 flex-shrink-0"
            style="border-top:1px solid rgba(255,255,255,0.06);background:rgba(255,255,255,0.015)">
            <button @click="showConnectionsModal = false"
              class="px-4 py-2 text-xs font-medium rounded-lg transition-all duration-150"
              style="color:rgba(255,255,255,0.45);border:1px solid rgba(255,255,255,0.1)"
              @mouseenter="e => { (e.currentTarget as HTMLElement).style.background='rgba(255,255,255,0.05)'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.65)' }"
              @mouseleave="e => { (e.currentTarget as HTMLElement).style.background='transparent'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.45)' }">
              Cancel
            </button>
            <button @click="saveConnectionsModal"
              class="px-5 py-2 text-xs font-semibold text-white rounded-lg transition-all duration-150 active:scale-[0.97]"
              style="background:linear-gradient(135deg,rgba(88,166,255,0.95),rgba(58,130,246,0.95));box-shadow:0 2px 14px rgba(88,166,255,0.28)">
              Save
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- ── Skills Manager Modal ──────────────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="showSkillsModal"
        class="fixed inset-0 z-[200] flex items-center justify-center p-4"
        @mousedown.self="showSkillsModal = false">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />

        <div class="relative w-full max-w-2xl bg-[#13151a] border border-white/[0.07] rounded-2xl flex flex-col overflow-hidden" style="max-height:80vh;box-shadow:0 25px 60px rgba(0,0,0,0.55)">

          <!-- Green accent line at top -->
          <div class="h-px flex-shrink-0" style="background:linear-gradient(90deg,transparent,rgba(63,185,80,0.5),transparent)" />

          <!-- Header -->
          <div class="flex items-center gap-3.5 px-5 pt-4 pb-3.5 border-b border-white/[0.06] flex-shrink-0">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0" style="background:rgba(63,185,80,0.12);border:1px solid rgba(63,185,80,0.2)">
              <svg class="w-4 h-4" style="color:rgba(63,185,80,0.85)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
              </svg>
            </div>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">Manage Skills</p>
              <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.35)">
                {{ modalSkills.length ? `${modalSkills.length} skill${modalSkills.length !== 1 ? 's' : ''} assigned` : 'No skills — agent uses global defaults' }}
              </p>
            </div>
            <button @click="showSkillsModal = false"
              class="w-7 h-7 flex items-center justify-center rounded-lg transition-all duration-150"
              style="color:rgba(255,255,255,0.3)"
              @mouseenter="e => (e.target as HTMLElement).style.color='rgba(255,255,255,0.7)'"
              @mouseleave="e => (e.target as HTMLElement).style.color='rgba(255,255,255,0.3)'">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>

          <!-- Two-panel body -->
          <div class="flex flex-1 min-h-0 overflow-hidden">

            <!-- Left: Available sidebar -->
            <div class="w-[44%] flex-shrink-0 flex flex-col overflow-hidden" style="background:#0a0d12;border-right:1px solid rgba(255,255,255,0.055)">
              <div class="px-4 py-3 flex-shrink-0 flex items-center justify-between">
                <p class="text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.28);letter-spacing:0.14em">Available</p>
                <button
                  v-if="modalAddableSkills.length > 0"
                  @click="addAllSkills"
                  class="text-[10px] transition-colors"
                  style="color:rgba(63,185,80,0.7)"
                  @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(63,185,80,1)'"
                  @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(63,185,80,0.7)'">
                  Add all →
                </button>
              </div>
              <div class="flex-1 overflow-y-auto pb-2">

                <div v-if="modalAddableSkills.length === 0" class="flex flex-col items-center justify-center px-6 py-10 gap-3 text-center">
                  <div class="w-10 h-10 rounded-xl flex items-center justify-center" style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.07)">
                    <svg class="w-5 h-5" style="color:rgba(255,255,255,0.2)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>
                  </div>
                  <div>
                    <p class="text-xs" style="color:rgba(255,255,255,0.28)">{{ availableSkills.length === 0 ? 'No skills installed' : 'All assigned' }}</p>
                    <router-link v-if="availableSkills.length === 0" to="/skills" @click="showSkillsModal = false" class="text-huginn-blue text-[11px] mt-1 inline-block hover:underline">Browse →</router-link>
                  </div>
                </div>

                <div class="px-2 space-y-0.5">
                  <button v-for="skill in modalAddableSkills" :key="skill.name"
                    @click="modalAddSkill(skill.name)"
                    class="w-full group flex items-center gap-3 px-3 py-2.5 rounded-xl text-left transition-colors duration-100"
                    :style="{ background: 'transparent' }"
                    @mouseenter="e => (e.currentTarget as HTMLElement).style.background='rgba(255,255,255,0.04)'"
                    @mouseleave="e => (e.currentTarget as HTMLElement).style.background='transparent'">
                    <div class="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0 transition-transform duration-100 group-hover:scale-105"
                      style="background:rgba(63,185,80,0.12);border:1px solid rgba(63,185,80,0.18)">
                      <svg class="w-3.5 h-3.5" style="color:rgba(63,185,80,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>
                    </div>
                    <p class="flex-1 text-[13px] font-medium truncate transition-colors duration-100" style="color:rgba(255,255,255,0.68)">{{ skill.name }}</p>
                    <svg class="w-3.5 h-3.5 flex-shrink-0 opacity-0 group-hover:opacity-100 transition-opacity duration-100" style="color:rgba(63,185,80,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                  </button>
                </div>

              </div>
            </div>

            <!-- Right: Assigned -->
            <div class="flex-1 flex flex-col overflow-hidden">
              <div class="px-4 py-3 flex items-center justify-between flex-shrink-0">
                <p class="text-[9px] font-semibold uppercase" style="color:rgba(255,255,255,0.28);letter-spacing:0.14em">Assigned</p>
                <div class="flex items-center gap-2">
                  <span v-if="modalSkills.length" class="text-[10px] font-semibold tabular-nums" style="color:rgba(63,185,80,0.8)">{{ modalSkills.length }}</span>
                  <button
                    v-if="modalSkills.length > 0"
                    @click="clearAllSkills"
                    class="text-[10px] transition-colors"
                    style="color:rgba(248,81,73,0.5)"
                    @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(248,81,73,0.9)'"
                    @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(248,81,73,0.5)'">
                    Clear all
                  </button>
                </div>
              </div>
              <div class="flex-1 overflow-y-auto px-3 pb-3">

                <div v-if="!modalSkills.length" class="flex flex-col items-center justify-center py-12 gap-3 text-center">
                  <div class="w-12 h-12 rounded-2xl flex items-center justify-center" style="background:rgba(255,255,255,0.03);border:1px solid rgba(255,255,255,0.06)">
                    <svg class="w-5 h-5" style="color:rgba(255,255,255,0.15)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>
                  </div>
                  <div>
                    <p class="text-xs" style="color:rgba(255,255,255,0.22)">No skills assigned</p>
                    <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.14)">Uses global defaults</p>
                  </div>
                </div>

                <TransitionGroup name="list-item" tag="div" class="space-y-2">
                  <div v-for="(skillName, idx) in modalSkills" :key="skillName"
                    class="flex items-center gap-3 px-3.5 py-3 rounded-xl transition-all duration-150"
                    style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.08)"
                    @mouseenter="e => (e.currentTarget as HTMLElement).style.borderColor='rgba(255,255,255,0.13)'"
                    @mouseleave="e => (e.currentTarget as HTMLElement).style.borderColor='rgba(255,255,255,0.08)'">
                    <div class="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0"
                      style="background:rgba(63,185,80,0.12);border:1px solid rgba(63,185,80,0.18)">
                      <svg class="w-3.5 h-3.5" style="color:rgba(63,185,80,0.7)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/></svg>
                    </div>
                    <p class="flex-1 text-[13px] font-medium truncate" style="color:rgba(255,255,255,0.85)">{{ skillName }}</p>
                    <button @click="modalRemoveSkill(idx)"
                      class="w-6 h-6 flex items-center justify-center rounded-lg flex-shrink-0 transition-all duration-150"
                      style="color:rgba(255,255,255,0.22)"
                      @mouseenter="e => { (e.currentTarget as HTMLElement).style.color='#f85149'; (e.currentTarget as HTMLElement).style.background='rgba(248,81,73,0.1)' }"
                      @mouseleave="e => { (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.22)'; (e.currentTarget as HTMLElement).style.background='transparent' }">
                      <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    </button>
                  </div>
                </TransitionGroup>

              </div>
            </div>
          </div>

          <!-- Footer -->
          <div class="flex items-center justify-end gap-2.5 px-5 py-3.5 flex-shrink-0"
            style="border-top:1px solid rgba(255,255,255,0.06);background:rgba(255,255,255,0.015)">
            <button @click="showSkillsModal = false"
              class="px-4 py-2 text-xs font-medium rounded-lg transition-all duration-150"
              style="color:rgba(255,255,255,0.45);border:1px solid rgba(255,255,255,0.1)"
              @mouseenter="e => { (e.currentTarget as HTMLElement).style.background='rgba(255,255,255,0.05)'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.65)' }"
              @mouseleave="e => { (e.currentTarget as HTMLElement).style.background='transparent'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.45)' }">
              Cancel
            </button>
            <button @click="saveSkillsModal"
              class="px-5 py-2 text-xs font-semibold text-white rounded-lg transition-all duration-150 active:scale-[0.97]"
              style="background:linear-gradient(135deg,rgba(63,185,80,0.95),rgba(35,134,54,0.95));box-shadow:0 2px 14px rgba(63,185,80,0.25)">
              Save
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- ── Model Picker Modal ───────────────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="showModelPicker"
        class="fixed inset-0 z-[200] flex items-center justify-center p-4"
        @mousedown.self="showModelPicker = false">
        <!-- Backdrop -->
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />

        <!-- Panel -->
        <div class="relative w-full max-w-md bg-[#161b22] border border-huginn-border/60 rounded-2xl shadow-2xl flex flex-col max-h-[80vh] overflow-hidden">

          <!-- Header -->
          <div class="flex items-center justify-between px-4 pt-4 pb-3 border-b border-huginn-border/40">
            <div>
              <p class="text-sm font-semibold text-huginn-text">Select model</p>
              <p class="text-[10px] text-huginn-muted/60 mt-0.5">{{ availableModels.length }} local models available</p>
            </div>
            <button @click="showModelPicker = false"
              class="w-6 h-6 flex items-center justify-center rounded-lg text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-colors">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>

          <!-- Search -->
          <div class="px-4 py-2.5 border-b border-huginn-border/30">
            <div class="flex items-center gap-2 bg-huginn-bg border border-huginn-border/50 rounded-lg px-3 py-1.5 focus-within:border-huginn-blue/40 transition-colors">
              <svg class="w-3.5 h-3.5 text-huginn-muted/50 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
              <input v-model="modelSearch" ref="modelSearchInput" placeholder="Search models…"
                class="flex-1 bg-transparent text-xs text-huginn-text placeholder:text-huginn-muted/40 outline-none" />
              <button v-if="modelSearch" @click="modelSearch = ''" class="text-huginn-muted/40 hover:text-huginn-muted transition-colors">
                <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
              </button>
            </div>
          </div>

          <!-- List -->
          <div class="overflow-y-auto flex-1 py-2">

            <!-- None option -->
            <button v-if="!modelSearch"
              @click="selectModel('')"
              class="w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-huginn-surface/60 group"
              :class="!form.model ? 'bg-huginn-surface/40' : ''">
              <div class="w-7 h-7 rounded-lg bg-huginn-surface border border-huginn-border/60 flex items-center justify-center flex-shrink-0">
                <svg class="w-3.5 h-3.5 text-huginn-muted/60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>
              </div>
              <div class="flex-1 min-w-0">
                <p class="text-xs font-medium" :class="!form.model ? 'text-huginn-text' : 'text-huginn-muted'">No model</p>
                <p class="text-[10px] text-huginn-muted/50">Agent will prompt for model</p>
              </div>
              <svg v-if="!form.model" class="w-3.5 h-3.5 text-huginn-blue flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
            </button>

            <!-- Provider groups -->
            <template v-for="group in filteredModelGroups" :key="group.provider">
              <!-- Provider header -->
              <div class="flex items-center gap-2.5 px-4 py-2 mt-1 bg-huginn-bg/60 border-y border-huginn-border/25">
                <div class="w-5 h-5 rounded-md flex items-center justify-center flex-shrink-0" :style="{ background: group.color + '25', borderColor: group.color + '50' }" style="border-width:1px">
                  <span class="text-[9px] font-bold" :style="{ color: group.color }">{{ group.icon }}</span>
                </div>
                <p class="text-[10px] font-bold text-huginn-muted/80 uppercase tracking-widest">{{ group.provider }}</p>
                <div class="flex-1" />
                <span class="text-[9px] text-huginn-muted/35 tabular-nums">{{ group.models.length }}</span>
              </div>

              <!-- Models in group — indented -->
              <button v-for="m in group.models" :key="m.name"
                @click="selectModel(m.name)"
                class="w-full flex items-center gap-3 pl-10 pr-4 py-2 text-left transition-colors hover:bg-huginn-surface/60"
                :class="form.model === m.name ? 'bg-huginn-blue/8' : ''">
                <div class="w-7 h-7 rounded-lg flex items-center justify-center flex-shrink-0" :style="{ background: group.color + '15' }">
                  <span class="text-[10px] font-semibold" :style="{ color: group.color }">{{ m.details?.parameter_size?.replace(/[^0-9.BMGKbmgk]+/g,'').slice(0,4) || '?' }}</span>
                </div>
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-1.5 min-w-0">
                    <p class="text-xs truncate" :class="form.model === m.name ? 'text-huginn-text font-medium' : 'text-huginn-text/80'">{{ m.name }}</p>
                    <span v-if="m._family" class="flex-shrink-0 text-[9px] px-1 py-0.5 rounded bg-huginn-surface border border-huginn-border/50 text-huginn-muted/60 leading-none">{{ m._family }}</span>
                  </div>
                  <p v-if="m.details?.parameter_size" class="text-[10px] text-huginn-muted/50">{{ m.details.parameter_size }}{{ m.details.quantization_level ? ' · ' + m.details.quantization_level : '' }}</p>
                </div>
                <svg v-if="form.model === m.name" class="w-3.5 h-3.5 text-huginn-blue flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
              </button>
            </template>

            <!-- No results -->
            <div v-if="modelSearch && filteredModelGroups.length === 0"
              class="px-4 py-8 text-center">
              <p class="text-xs text-huginn-muted/50">No models match "{{ modelSearch }}"</p>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- MuninnDB Memory Configure Modal -->
  <Teleport to="body">
  <Transition name="modal-fade">
    <div v-if="memoryModal.open"
      class="fixed inset-0 z-[200] flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm"
      @mousedown.self="cancelMemoryModal()">

      <!-- Modal panel — wider on desktop for 2-column layout -->
      <div class="relative w-full max-w-md sm:max-w-2xl bg-huginn-surface border border-huginn-border/40 rounded-xl shadow-2xl flex flex-col max-h-[90vh]">

        <!-- Header -->
        <div class="flex items-center justify-between px-5 py-4 border-b border-huginn-border/30">
          <div>
            <h3 class="text-sm font-semibold text-huginn-text">Muninn Memory Configuration</h3>
            <p class="text-[11px] text-huginn-muted/60 mt-0.5">{{ form.name || 'This agent' }}'s long-term memory vault</p>
          </div>
          <button @click="cancelMemoryModal()"
            class="text-huginn-muted/50 hover:text-huginn-muted transition-colors p-1 rounded">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <!-- Scrollable body -->
        <div class="overflow-y-auto flex-1 px-5 py-4">
          <div class="flex flex-col sm:flex-row sm:gap-6">

            <!-- Left column: Vault picker + Memory mode selector -->
            <div class="flex-1 min-w-0 space-y-5">

              <!-- Vault section -->
              <div>
                <p class="text-[11px] font-medium text-huginn-muted/70 uppercase tracking-wide mb-2">Vault</p>

                <!-- Create new / Use existing toggle -->
                <div class="flex gap-3 mb-3">
                  <button @click="memoryModal.vaultChoice = 'new'"
                    class="flex items-center gap-2 text-[11px] transition-colors"
                    :class="memoryModal.vaultChoice === 'new' ? 'text-huginn-text' : 'text-huginn-muted/50 hover:text-huginn-muted'">
                    <div class="w-3.5 h-3.5 rounded-full border-2 flex items-center justify-center shrink-0"
                      :class="memoryModal.vaultChoice === 'new' ? 'border-huginn-blue' : 'border-huginn-muted/40'">
                      <div v-if="memoryModal.vaultChoice === 'new'" class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
                    </div>
                    Create new
                  </button>
                  <button @click="memoryModal.vaultChoice = 'existing'"
                    class="flex items-center gap-2 text-[11px] transition-colors"
                    :class="memoryModal.vaultChoice === 'existing' ? 'text-huginn-text' : 'text-huginn-muted/50 hover:text-huginn-muted'">
                    <div class="w-3.5 h-3.5 rounded-full border-2 flex items-center justify-center shrink-0"
                      :class="memoryModal.vaultChoice === 'existing' ? 'border-huginn-blue' : 'border-huginn-muted/40'">
                      <div v-if="memoryModal.vaultChoice === 'existing'" class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
                    </div>
                    Use existing
                  </button>
                </div>

                <!-- Use existing: dropdown -->
                <div v-if="memoryModal.vaultChoice === 'existing'">
                  <select v-model="memoryModal.selectedVault"
                    class="w-full bg-huginn-bg border border-huginn-border/40 rounded px-2.5 py-1.5 text-[11px] font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/50 appearance-none cursor-pointer">
                    <option value="" disabled>Select a vault…</option>
                    <option v-for="v in existingVaults" :key="v.name" :value="v.name">
                      {{ v.name }}{{ v.linked ? ' ✓' : ' (will link)' }}
                    </option>
                  </select>
                  <p v-if="memoryModal.selectedVault" class="text-[10px] mt-1 flex items-center gap-1"
                    :class="existingVaults.find(v => v.name === memoryModal.selectedVault)?.linked ? 'text-huginn-green/70' : 'text-huginn-amber/60'">
                    <span class="inline-block w-1.5 h-1.5 rounded-full shrink-0"
                      :class="existingVaults.find(v => v.name === memoryModal.selectedVault)?.linked ? 'bg-huginn-green/70' : 'bg-huginn-amber/60'"></span>
                    {{ existingVaults.find(v => v.name === memoryModal.selectedVault)?.linked ? 'Token already configured' : 'Token will be linked on save' }}
                  </p>
                </div>

                <!-- Create new: name + description inputs -->
                <div v-if="memoryModal.vaultChoice === 'new'" class="space-y-2">
                  <div>
                    <label class="text-[10px] text-huginn-muted/60 font-medium uppercase tracking-wide block mb-1">Vault name</label>
                    <input v-model="memoryModal.newVaultName"
                      class="w-full bg-huginn-bg border border-huginn-border/40 rounded px-2.5 py-1.5 text-[11px] font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/50"
                      :class="memoryModal.newVaultName && allVaultNames.includes(memoryModal.newVaultName) ? 'border-huginn-red/50' : ''"
                      placeholder="huginn-alice" />
                    <p v-if="memoryModal.newVaultName && allVaultNames.includes(memoryModal.newVaultName)"
                      class="text-[10px] text-huginn-red/70 mt-0.5">↳ this name is already taken — choose a different name or switch to "Use existing"</p>
                    <p v-else-if="memoryModal.newVaultName"
                      class="text-[10px] text-huginn-muted/50 mt-0.5">↳ will be created on save</p>
                  </div>
                  <div>
                    <label class="text-[10px] text-huginn-muted/60 font-medium uppercase tracking-wide block mb-1">Description <span class="font-normal normal-case">(optional — helps the agent understand its memory)</span></label>
                    <textarea v-model="memoryModal.newVaultDesc"
                      rows="2"
                      class="w-full bg-huginn-bg border border-huginn-border/40 rounded px-2.5 py-1.5 text-[11px] text-huginn-text resize-none focus:outline-none focus:border-huginn-blue/50"
                      placeholder="e.g. Alice's coding memory for the huginn project" />
                  </div>
                </div>
              </div>

              <!-- Memory mode selector -->
              <div>
                <p class="text-[11px] font-medium text-huginn-muted/70 uppercase tracking-wide mb-2">Memory mode</p>
                <div class="space-y-1.5">
                  <button v-for="m in memoryModes" :key="m.value"
                    @click="memoryModal.mode = m.value"
                    class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg border text-left transition-all"
                    :class="memoryModal.mode === m.value
                      ? 'border-huginn-blue/50 bg-huginn-blue/5'
                      : 'border-huginn-border/30 hover:border-huginn-border/60'">
                    <div class="w-3.5 h-3.5 rounded-full border-2 flex items-center justify-center shrink-0"
                      :class="memoryModal.mode === m.value ? 'border-huginn-blue' : 'border-huginn-muted/40'">
                      <div v-if="memoryModal.mode === m.value"
                        class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
                    </div>
                    <p class="text-[11px] font-medium"
                      :class="memoryModal.mode === m.value ? 'text-huginn-text' : 'text-huginn-muted'">{{ m.label }}</p>
                  </button>
                </div>
              </div>

            </div>

            <!-- Vertical divider (desktop only) -->
            <div class="hidden sm:block w-px bg-huginn-border/30 self-stretch shrink-0" />

            <!-- Right column: Selected mode description + behaviors -->
            <div class="flex-1 min-w-0 mt-5 sm:mt-0">
              <template v-for="m in memoryModes" :key="m.value">
                <div v-if="memoryModal.mode === m.value" class="space-y-3">
                  <!-- Mode name + description -->
                  <div>
                    <p class="text-[13px] font-semibold text-huginn-text mb-1">{{ m.label }}</p>
                    <p class="text-[11px] text-huginn-muted/70 leading-relaxed">{{ m.description }}</p>
                  </div>
                  <!-- Behavior bullets -->
                  <div>
                    <p class="text-[11px] font-medium text-huginn-muted/70 uppercase tracking-wide mb-2">What this mode does</p>
                    <div class="space-y-1.5">
                      <div v-for="b in m.behaviors" :key="b" class="flex items-start gap-2">
                        <span class="text-huginn-blue/50 shrink-0 mt-px text-[11px] leading-none">•</span>
                        <p class="text-[10px] text-huginn-muted/70 leading-snug">{{ b }}</p>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </div>

        <!-- Footer -->
        <div class="flex items-center justify-end gap-2.5 px-5 py-3.5 border-t border-huginn-border/30">
          <button @click="cancelMemoryModal()"
            class="text-[11px] text-huginn-muted/70 hover:text-huginn-muted px-3 py-1.5 rounded transition-colors">
            Cancel
          </button>
          <button @click="saveMemoryModal()"
            :disabled="(memoryModal.vaultChoice === 'new' && (!memoryModal.newVaultName || allVaultNames.includes(memoryModal.newVaultName))) || (memoryModal.vaultChoice === 'existing' && !memoryModal.selectedVault)"
            class="text-[11px] font-medium px-3.5 py-1.5 rounded bg-huginn-blue/90 hover:bg-huginn-blue text-white transition-colors disabled:opacity-40 disabled:cursor-not-allowed">
            Done
          </button>
        </div>
      </div>
    </div>
  </Transition>
  </Teleport>

  <!-- ── Local Access Modal ──────────────────────────────────────── -->
  <Teleport to="body">
    <div v-if="showLocalAccessModal"
      class="fixed inset-0 z-50 flex items-center justify-center"
      style="background:rgba(0,0,0,0.6);backdrop-filter:blur(2px)"
      @mousedown.self="showLocalAccessModal = false">
      <div class="rounded-xl shadow-2xl flex flex-col overflow-hidden"
        style="background:#161b22;border:1px solid #30363d;width:680px;max-height:80vh">
        <!-- Header -->
        <div class="flex items-center gap-3 px-5 py-4 border-b" style="border-color:#30363d">
          <div class="w-8 h-8 rounded-lg flex items-center justify-center text-sm" style="background:#0d1117">🔧</div>
          <div>
            <p class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">Manage Local Access</p>
            <p class="text-xs" style="color:rgba(255,255,255,0.35)">Add capabilities to grant access</p>
          </div>
          <button @click="showLocalAccessModal = false" class="ml-auto" style="color:rgba(255,255,255,0.35)">✕</button>
        </div>
        <!-- Body: two columns -->
        <div class="flex flex-1 overflow-hidden" style="min-height:0">
          <!-- Available -->
          <div class="w-1/2 border-r overflow-y-auto p-4 space-y-4" style="border-color:#30363d">
            <div class="flex items-center justify-between">
              <p class="text-[10px] font-semibold uppercase tracking-widest" style="color:rgba(255,255,255,0.2)">Available</p>
              <button @click="localModalGrantAll"
                class="text-[10px] px-2 py-0.5 rounded transition-all hover:bg-huginn-blue/10"
                style="color:rgba(88,166,255,0.5);border:1px solid rgba(88,166,255,0.15)">Add all</button>
            </div>
            <!-- Categories -->
            <div v-for="cat in LOCAL_TOOL_CATALOG" :key="cat.category" class="space-y-1">
              <p class="text-[10px] font-semibold uppercase px-1" style="color:rgba(255,255,255,0.2);letter-spacing:0.1em">
                {{ cat.icon }} {{ cat.category }}
              </p>
              <button v-for="tool in cat.tools" :key="tool.name"
                @click="localModalGrant(tool.name)"
                :disabled="modalLocalTools.includes(tool.name)"
                class="group w-full text-left px-3 py-2 rounded text-xs disabled:opacity-40"
                :style="!modalLocalTools.includes(tool.name) && hoveredAvailableName === tool.name
                  ? 'background:rgba(88,166,255,0.06);border:1px solid #58a6ff;box-shadow:0 0 0 1px rgba(88,166,255,0.2),0 0 10px rgba(88,166,255,0.1);color:rgba(255,255,255,0.9);transition:all 0.15s'
                  : 'background:#0d1117;border:1px solid #30363d;color:rgba(255,255,255,0.75);transition:all 0.15s'"
                @mouseover="hoveredAvailableName = tool.name"
                @mouseout="hoveredAvailableName = ''">
                <div class="flex items-center gap-2">
                  <div class="flex-1">
                    <div class="font-semibold">{{ tool.label }}</div>
                    <div class="text-[10px] mt-0.5" style="color:rgba(255,255,255,0.3)">{{ tool.description }}</div>
                  </div>
                  <span class="text-base opacity-0 group-hover:opacity-100 -translate-x-1 group-hover:translate-x-0 transition-all duration-150 shrink-0 text-huginn-blue" style="text-shadow:0 0 10px #58a6ff">→</span>
                </div>
              </button>
            </div>
            <!-- Shell — separated with warning -->
            <div class="pt-2 border-t" style="border-color:#30363d">
              <p class="text-[10px] font-semibold uppercase px-1 mb-1" style="color:#f85149;letter-spacing:0.1em">
                ⚡ Shell — Dangerous
              </p>
              <button v-for="tool in SHELL_TOOLS" :key="tool.name"
                @click="localModalGrant(tool.name)"
                :disabled="modalLocalTools.includes(tool.name)"
                class="group w-full text-left px-3 py-2 rounded text-xs disabled:opacity-40"
                :style="!modalLocalTools.includes(tool.name) && hoveredAvailableName === tool.name
                  ? 'background:rgba(248,81,73,0.1);border:1px solid #f85149;box-shadow:0 0 0 1px rgba(248,81,73,0.25),0 0 10px rgba(248,81,73,0.12);color:rgba(255,255,255,0.9);transition:all 0.15s'
                  : 'background:rgba(248,81,73,0.05);border:1px solid rgba(248,81,73,0.25);color:rgba(255,255,255,0.75);transition:all 0.15s'"
                @mouseover="hoveredAvailableName = tool.name"
                @mouseout="hoveredAvailableName = ''">
                <div class="flex items-center gap-2">
                  <div class="flex-1">
                    <div class="font-semibold">{{ tool.label }}</div>
                    <div class="text-[10px] mt-0.5" style="color:rgba(255,255,255,0.3)">{{ tool.description }}</div>
                  </div>
                  <span class="text-base opacity-0 group-hover:opacity-100 -translate-x-1 group-hover:translate-x-0 transition-all duration-150 shrink-0" style="color:#f85149;text-shadow:0 0 10px #f85149">→</span>
                </div>
              </button>
            </div>
          </div>
          <!-- Assigned / Granted -->
          <div class="w-1/2 overflow-y-auto p-4 flex flex-col gap-2">
            <div class="flex items-center justify-between">
              <p class="text-[10px] font-semibold uppercase tracking-widest" style="color:rgba(255,255,255,0.2)">Granted</p>
              <button v-if="modalLocalTools.length" @click="modalLocalTools = []"
                class="text-[10px] px-2 py-0.5 rounded transition-all hover:bg-red-500/10"
                style="color:rgba(248,81,73,0.5);border:1px solid rgba(248,81,73,0.15)">Remove all</button>
            </div>
            <div v-if="!modalLocalTools.length" class="flex flex-col items-center justify-center flex-1 py-12 gap-2 text-center">
              <p class="text-xs" style="color:rgba(255,255,255,0.25)">No local access granted</p>
              <p class="text-[10px]" style="color:rgba(255,255,255,0.14)">Add capabilities from the left</p>
            </div>
            <div v-for="(name, idx) in modalLocalTools" :key="name"
              @click="modalLocalTools.splice(idx, 1)"
              class="group flex items-center justify-between px-3 py-2 rounded text-xs cursor-pointer"
              :style="hoveredGrantedIdx === idx
                ? 'background:rgba(248,81,73,0.08);border:1px solid #f85149;box-shadow:0 0 0 1px rgba(248,81,73,0.25),0 0 10px rgba(248,81,73,0.12);transition:all 0.15s'
                : isShellTool(name)
                  ? 'background:rgba(248,81,73,0.05);border:1px solid rgba(248,81,73,0.25);transition:all 0.15s'
                  : 'background:#0d1117;border:1px solid #30363d;transition:all 0.15s'"
              @mouseover="hoveredGrantedIdx = idx"
              @mouseout="hoveredGrantedIdx = -1">
              <div class="flex-1">
                <div class="transition-all duration-150 group-hover:text-red-400" style="color:rgba(255,255,255,0.75)">{{ toolLabel(name) }}</div>
                <div class="text-[10px] mt-0.5 transition-all duration-150 group-hover:text-red-400/50" style="color:rgba(255,255,255,0.3)">{{ toolDescription(name) }}</div>
              </div>
              <span class="text-sm opacity-30 group-hover:opacity-100 group-hover:scale-110 transition-all duration-150" style="color:#f85149;text-shadow:0 0 8px #f85149">✕</span>
            </div>
          </div>
        </div>
        <!-- Footer -->
        <div class="flex justify-end gap-2 px-5 py-3 border-t" style="border-color:#30363d">
          <button @click="showLocalAccessModal = false"
            class="px-4 py-1.5 rounded text-sm"
            style="border:1px solid #30363d;color:rgba(255,255,255,0.55)">Cancel</button>
          <button @click="saveLocalAccessModal"
            class="px-4 py-1.5 rounded text-sm font-semibold"
            style="background:#58a6ff;color:#0d1117">Save</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { api, getToken } from '../composables/useApi'
import type { Connection, ToolbeltEntry, SystemToolStatus } from '../composables/useApi'
import { useInstalledSkills } from '../composables/useSkills'
import { useAgents } from '../composables/useAgents'

const props = defineProps<{ agentName?: string }>()
const router = useRouter()
const { updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()

// Stale data recovery: when the tab returns from background or the window
// regains focus, silently re-fetch the current agent after a 500 ms debounce.
// This prevents showing stale form data after long absences (sleep, tab switch).
const isStaleRefreshing = ref(false)
let staleDebounceTimer: ReturnType<typeof setTimeout> | null = null

function scheduleStaleRefresh() {
  if (staleDebounceTimer !== null) return // already scheduled
  staleDebounceTimer = setTimeout(async () => {
    staleDebounceTimer = null
    if (!props.agentName || props.agentName === 'new') {
      // No agent open — just refresh the sidebar list quietly.
      fetchAgents().catch(() => {})
      return
    }
    isStaleRefreshing.value = true
    try {
      await Promise.all([
        loadAgent(props.agentName),
        fetchAgents(),
      ])
    } catch { /* ignore */ } finally {
      isStaleRefreshing.value = false
    }
  }, 500)
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') scheduleStaleRefresh()
}

function onWindowFocus() {
  scheduleStaleRefresh()
}

interface OllamaModel {
  name: string
  source?: string
  size_bytes?: number
  details?: { parameter_size?: string; quantization_level?: string }
}

type MemoryType = 'none' | 'context' | 'muninndb'
type MemoryMode = 'passive' | 'conversational' | 'immersive'

interface AgentForm {
  name: string
  model: string
  system_prompt: string
  color: string
  icon: string
  memory_type: MemoryType
  memory_enabled: boolean
  context_notes_enabled: boolean
  vault_name: string
  memory_mode: MemoryMode
  vault_description: string
  toolbelt: ToolbeltEntry[]
  skills: string[]
  local_tools: string[]
}

const form = ref<AgentForm>({ name: '', model: '', system_prompt: '', color: '#58a6ff', icon: '', memory_type: 'none', memory_enabled: false, context_notes_enabled: false, vault_name: '', memory_mode: 'conversational', vault_description: '', toolbelt: [], skills: [], local_tools: [] })
const original = ref('')
const dirty = ref(false)
const saving = ref(false)
const saveMsg = ref('')
const saveError = ref(false)
const loadError = ref(false)
const loadErrorMsg = ref('')
const showDeleteConfirm = ref(false)
const availableModels = ref<OllamaModel[]>([])
const modelsLoading = ref(false)
const modelsError = ref('')
const isActive = ref(false)

// Model picker modal
const showModelPicker = ref(false)
const modelSearch = ref('')
const modelSearchInput = ref<HTMLInputElement | null>(null)

// Memory section reactive state
const muninnConnected = ref(false)
const muninnEndpoint = ref('')
interface VaultItem { name: string; linked: boolean }
const existingVaults = ref<VaultItem[]>([])
const linkedVaultNames = computed(() => existingVaults.value.filter(v => v.linked).map(v => v.name))
const allVaultNames = computed(() => existingVaults.value.map(v => v.name))
const vaultCheckTimeout = ref<ReturnType<typeof setTimeout> | null>(null)

// Vault health polling state — updated every 60s when an agent panel is open.
type VaultHealthStatus = 'ok' | 'degraded' | 'unavailable' | 'unknown'
interface VaultHealth {
  status: VaultHealthStatus
  tools_count: number
  warning: string
  latency_ms: number
}
const vaultHealth = ref<VaultHealth>({ status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 })
let vaultHealthInterval: ReturnType<typeof setInterval> | null = null

type MemoryModalState = {
  open: boolean
  vaultChoice: 'existing' | 'new'
  selectedVault: string
  newVaultName: string
  newVaultDesc: string
  mode: MemoryMode
}

const memoryModal = ref<MemoryModalState>({
  open: false,
  vaultChoice: 'existing',
  selectedVault: '',
  newVaultName: '',
  newVaultDesc: '',
  mode: 'conversational',
})

const previousMemoryType = ref<MemoryType>('none')

function selectMuninnDB() {
  previousMemoryType.value = form.value.memory_type
  form.value.memory_type = 'muninndb'
  form.value.memory_enabled = true
  markDirty()
  // First time: no vault set yet — force configure flow
  if (!form.value.vault_name) {
    openMemoryModal()
  }
}

function cancelMemoryModal() {
  memoryModal.value.open = false
  // If vault still unconfigured (user cancelled first-time setup), revert memory type
  if (!form.value.vault_name && form.value.memory_type === 'muninndb') {
    form.value.memory_type = previousMemoryType.value
    form.value.memory_enabled = previousMemoryType.value === 'muninndb'
    form.value.context_notes_enabled = previousMemoryType.value === 'context'
  }
}

function openMemoryModal() {
  const isNew = form.value.vault_name && !allVaultNames.value.includes(form.value.vault_name)
  memoryModal.value = {
    open: true,
    vaultChoice: isNew || !form.value.vault_name ? 'new' : 'existing',
    selectedVault: allVaultNames.value.includes(form.value.vault_name) ? form.value.vault_name : (allVaultNames.value[0] || ''),
    newVaultName: isNew ? form.value.vault_name : '',
    newVaultDesc: form.value.vault_description,
    mode: form.value.memory_mode || 'conversational',
  }
  // Auto-suggest vault name for new vaults when no name is set yet
  if (memoryModal.value.vaultChoice === 'new' && !memoryModal.value.newVaultName && form.value.name) {
    const slug = form.value.name.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/-{2,}/g, '-').replace(/^-|-$/g, '')
    if (slug) memoryModal.value.newVaultName = 'huginn-' + slug
  }
}

function saveMemoryModal() {
  if (memoryModal.value.vaultChoice === 'existing') {
    form.value.vault_name = memoryModal.value.selectedVault
    form.value.vault_description = ''
  } else {
    form.value.vault_name = memoryModal.value.newVaultName
    form.value.vault_description = memoryModal.value.newVaultDesc
  }
  form.value.memory_mode = memoryModal.value.mode
  memoryModal.value.open = false
  markDirty()
  // Refresh vault list so newly created vaults appear immediately next time
  loadMuninnInfo()
}

// Connections / Toolbelt
const availableConnections = ref<Connection[]>([])
const systemTools = ref<SystemToolStatus[]>([])

// Connections modal state
const showConnectionsModal = ref(false)
const modalToolbelt = ref<ToolbeltEntry[]>([])

// Skills modal state
const showSkillsModal = ref(false)
const modalSkills = ref<string[]>([])

const colorPalette = ['#58a6ff', '#3fb950', '#d29922', '#f85149', '#bc8cff', '#79c0ff']

// Provider detection for model picker
interface ModelGroup { provider: string; icon: string; color: string; models: (OllamaModel & { _family?: string })[] }

function detectProvider(name: string, source?: string): { provider: string; icon: string; color: string; family: string } {
  if (source === 'built-in') return { provider: 'Built-in', icon: 'H', color: '#e3b341', family: 'llama.cpp' }
  const n = name.toLowerCase()
  if (n.startsWith('claude')) return { provider: 'Anthropic', icon: 'A', color: '#cc785c', family: '' }
  if (n.startsWith('gpt') || n.startsWith('o1') || n.startsWith('o3') || n.startsWith('o4')) return { provider: 'OpenAI', icon: 'O', color: '#10a37f', family: '' }
  if (n.startsWith('gemini')) return { provider: 'Google', icon: 'G', color: '#4285f4', family: '' }
  if (n.startsWith('nomic') || n.startsWith('mxbai') || n.includes('embed')) return { provider: 'Embeddings', icon: 'E', color: '#64748b', family: '' }
  // All local Ollama models grouped together — family shown on the row
  if (n.startsWith('llama')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Meta' }
  if (n.startsWith('qwen')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Qwen' }
  if (n.startsWith('deepseek')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'DeepSeek' }
  if (n.startsWith('phi')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Microsoft' }
  if (n.startsWith('mistral') || n.startsWith('mixtral')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Mistral' }
  if (n.startsWith('gemma')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Google' }
  if (n.startsWith('codellama')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Meta' }
  return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: '' }
}

const filteredModelGroups = computed((): ModelGroup[] => {
  const search = modelSearch.value.toLowerCase().trim()
  const groups: Record<string, ModelGroup> = {}

  // Include current model even if not in list
  const allModels = [...availableModels.value]
  if (form.value.model && !allModels.some(m => m.name === form.value.model)) {
    allModels.unshift({ name: form.value.model })
  }

  for (const m of allModels) {
    if (search && !m.name.toLowerCase().includes(search)) continue
    const { provider, icon, color, family } = detectProvider(m.name, m.source)
    if (!groups[provider]) groups[provider] = { provider, icon, color, models: [] }
    groups[provider].models.push({ ...m, _family: family })
  }

  const order: Record<string, number> = { Anthropic: 0, OpenAI: 1, Google: 2, OpenRouter: 3, Ollama: 4, 'Built-in': 5, Embeddings: 6 }
  return Object.values(groups).sort((a, b) => {
    const oa = order[a.provider] ?? 3
    const ob = order[b.provider] ?? 3
    return oa !== ob ? oa - ob : a.provider.localeCompare(b.provider)
  })
})

function selectModel(name: string) {
  form.value.model = name
  markDirty()
  showModelPicker.value = false
  modelSearch.value = ''
}

// Focus search when modal opens
watch(showModelPicker, (v) => {
  if (v) nextTick(() => modelSearchInput.value?.focus())
})

function markDirty() { dirty.value = true }

const memoryModes: { value: MemoryMode; label: string; description: string; behaviors: string[] }[] = [
  {
    value: 'passive',
    label: 'Passive',
    description: 'Uses memory only when you explicitly ask. Minimal footprint — good for focused single-task agents.',
    behaviors: [
      'Recalls only when you say "recall" or "what do you remember"',
      'Stores only when you say "remember this"',
      'Extracts entities from what you ask it to store',
      'No automatic memory activity between requests',
    ],
  },
  {
    value: 'conversational',
    label: 'Conversational',
    description: 'Proactively recalls at session start, writes new learnings, links related memories, and signals helpful/unhelpful recalls. The balanced default.',
    behaviors: [
      'Recalls context at the start of every conversation',
      'Re-recalls when the topic shifts significantly',
      'Stores facts, decisions, preferences, and project context',
      'Uses batch writes when multiple topics are covered',
      'Extracts entities and builds knowledge graph relationships',
      'Links related memories with typed relationships (supports, depends_on, contradicts…)',
      'Records decisions with rationale and alternatives via muninn_decide',
      'Evolves stale memories instead of creating duplicates',
      'Signals helpful/unhelpful recalls to improve recall quality over time',
    ],
  },
  {
    value: 'immersive',
    label: 'Immersive',
    description: 'Full knowledge-graph stewardship. Orients at every session start, recalls before every action, maintains lifecycle, and continuously improves recall quality.',
    behaviors: [
      'Calls "where did we leave off?" at every session start',
      'Recalls before every significant decision or action',
      'Uses deep, causal, and adversarial recall modes for complex topics',
      'Stores every fact, decision, observation, and preference atomically',
      'Always extracts entities and entity relationships at write time',
      'Links memories proactively; surfaces contradictions to you',
      'Evolves changed facts with a reason — no duplicates',
      'Consolidates fragmented memories on the same topic',
      'Records decisions with rationale, alternatives, and supporting memory IDs',
      'Tracks goal and task lifecycle (active → completed → blocked…)',
      'Stores hierarchical knowledge as memory trees (plans, specs, breakdowns)',
      'Continuous feedback loop on every recalled memory improves scoring over time',
    ],
  },
]


async function loadMuninnInfo() {
  try {
    const statusData = await api.muninn.status()
    muninnConnected.value = statusData.connected
    muninnEndpoint.value = statusData.connected ? `${statusData.username} @ ${statusData.endpoint}` : ''
    const vaultData = await api.muninn.vaults()
    existingVaults.value = (vaultData.vaults || []) as unknown as VaultItem[]
  } catch { /* ignore */ }
}

// Fetch vault health for the currently-open agent and update the vaultHealth ref.
async function pollVaultHealth() {
  const name = props.agentName
  if (!name || name === 'new') {
    vaultHealth.value = { status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 }
    return
  }
  try {
    const resp = await fetch(`/api/v1/agents/${encodeURIComponent(name)}/vault-status`, {
      headers: { Authorization: `Bearer ${getToken()}` },
    })
    if (resp.ok) {
      const data = await resp.json() as VaultHealth
      vaultHealth.value = data
    }
  } catch { /* silently ignore — health display is best-effort */ }
}

function startVaultHealthPolling() {
  stopVaultHealthPolling()
  pollVaultHealth()
  vaultHealthInterval = setInterval(pollVaultHealth, 60_000)
}

function stopVaultHealthPolling() {
  if (vaultHealthInterval !== null) {
    clearInterval(vaultHealthInterval)
    vaultHealthInterval = null
  }
  vaultHealth.value = { status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 }
}

function connectionLabel(id: string): string {
  if (id.startsWith('system:')) {
    return id.slice('system:'.length) + ' (CLI)'
  }
  const c = availableConnections.value.find(c => c.id === id)
  return c ? (c.account_label || c.provider) : id
}

function connectionIcon(connId: string): { bg: string; fg: string; label: string } {
  if (connId.startsWith('system:')) {
    const name = connId.slice('system:'.length)
    if (name === 'github') return { bg: 'rgba(240,246,252,0.09)', fg: 'rgba(240,246,252,0.75)', label: 'GH' }
    if (name === 'aws') return { bg: 'rgba(255,153,0,0.18)', fg: '#FF9900', label: 'AW' }
    if (name === 'gcloud') return { bg: 'rgba(66,133,244,0.18)', fg: '#4285F4', label: 'GC' }
    const n = name.slice(0, 2).toUpperCase()
    return { bg: 'rgba(100,116,139,0.18)', fg: 'rgba(148,163,184,0.85)', label: n }
  }
  const c = availableConnections.value.find(conn => conn.id === connId)
  const provider = (c?.provider || connId).toLowerCase()
  if (provider.includes('google')) return { bg: 'rgba(66,133,244,0.18)', fg: '#4285F4', label: 'G' }
  if (provider.includes('github')) return { bg: 'rgba(240,246,252,0.09)', fg: 'rgba(240,246,252,0.75)', label: 'GH' }
  if (provider.includes('slack')) return { bg: 'rgba(74,21,75,0.35)', fg: '#C4B5FD', label: 'SL' }
  if (provider.includes('linear')) return { bg: 'rgba(94,106,210,0.22)', fg: '#818CF8', label: 'LN' }
  if (provider.includes('notion')) return { bg: 'rgba(255,255,255,0.09)', fg: 'rgba(255,255,255,0.75)', label: 'N' }
  const chars = ((c?.account_label || c?.provider || connId).slice(0, 2)).toUpperCase()
  return { bg: 'rgba(88,166,255,0.15)', fg: '#58a6ff', label: chars }
}

async function loadConnections() {
  loadError.value = false
  loadErrorMsg.value = ''
  try {
    const [conns, tools] = await Promise.all([
      api.connections.list() as Promise<Connection[]>,
      api.system.tools(),
    ])
    availableConnections.value = conns
    systemTools.value = tools.filter(t => t.authed)
  } catch (e) {
    console.error('loadConnections failed:', e)
    loadError.value = true
    loadErrorMsg.value = 'Failed to load connections. Please refresh.'
  }
}

// Skills picker state
const availableSkills = ref<{ name: string }[]>([])

async function loadAvailableSkills() {
  try {
    const { skills, load } = useInstalledSkills()
    await load()
    availableSkills.value = skills.value.map(s => ({ name: s.name }))
  } catch { /* ignore */ }
}

// ── Connections modal helpers ──────────────────────────────────────────
function openConnectionsModal() {
  modalToolbelt.value = JSON.parse(JSON.stringify(form.value.toolbelt))
  showConnectionsModal.value = true
}

async function saveConnectionsModal() {
  form.value.toolbelt = JSON.parse(JSON.stringify(modalToolbelt.value))
  showConnectionsModal.value = false
  if (props.agentName && props.agentName !== 'new') {
    await save()
  } else {
    markDirty()
  }
}

const modalAddableConnections = computed(() =>
  availableConnections.value.filter(c => !modalToolbelt.value.some(e => e.connection_id === c.id))
)

const modalAddableSystemToolsForModal = computed(() =>
  systemTools.value.filter(t => {
    const connId = 'system:' + t.name
    if (t.profiles && t.profiles.length > 1) {
      return !t.profiles.every(p => modalToolbelt.value.some(e => e.connection_id === connId && e.profile === p))
    }
    return !modalToolbelt.value.some(e => e.connection_id === connId)
  })
)

function modalIsProfileAdded(tool: SystemToolStatus, profile: string): boolean {
  return modalToolbelt.value.some(e => e.connection_id === 'system:' + tool.name && e.profile === profile)
}

function modalAddConnection(conn: Connection) {
  modalToolbelt.value.push({ connection_id: conn.id, provider: conn.provider, approval_gate: false })
}

function modalAddSystemTool(tool: SystemToolStatus, profile: string) {
  const providerMap: Record<string, string> = { github: 'github_cli', aws: 'aws', gcloud: 'gcloud' }
  const provider = providerMap[tool.name] || tool.name
  modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: profile || undefined, approval_gate: false })
}

function modalRemoveEntry(idx: number) {
  modalToolbelt.value.splice(idx, 1)
}

function modalRemoveAll() {
  modalToolbelt.value = []
}

function modalAddAll() {
  modalAddableConnections.value.forEach(conn => {
    modalToolbelt.value.push({ connection_id: conn.id, provider: conn.provider, approval_gate: false })
  })
  const provMap: Record<string, string> = { github: 'github_cli', aws: 'aws', gcloud: 'gcloud' }
  modalAddableSystemToolsForModal.value.forEach(tool => {
    const provider = provMap[tool.name] || tool.name
    if (tool.profiles && tool.profiles.length > 1) {
      tool.profiles.forEach(p => {
        if (!modalIsProfileAdded(tool, p)) {
          modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: p, approval_gate: false })
        }
      })
    } else {
      const profile = tool.profiles?.[0] || ''
      modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: profile || undefined, approval_gate: false })
    }
  })
}

function modalToggleApprovalGate(idx: number) {
  const entry = modalToolbelt.value[idx]
  if (entry) entry.approval_gate = !entry.approval_gate
}

// ── Skills modal helpers ───────────────────────────────────────────────
function openSkillsModal() {
  modalSkills.value = [...form.value.skills]
  showSkillsModal.value = true
}

async function saveSkillsModal() {
  form.value.skills = [...modalSkills.value]
  showSkillsModal.value = false
  if (props.agentName && props.agentName !== 'new') {
    await save()
  } else {
    markDirty()
  }
}

const modalAddableSkills = computed(() =>
  availableSkills.value.filter(s => !modalSkills.value.includes(s.name))
)

function modalAddSkill(name: string) {
  if (!modalSkills.value.includes(name)) modalSkills.value.push(name)
}

function modalRemoveSkill(idx: number) {
  modalSkills.value.splice(idx, 1)
}

function addAllSkills() {
  const toAdd = modalAddableSkills.value.map(s => s.name)
  modalSkills.value = [...modalSkills.value, ...toAdd]
}

function clearAllSkills() {
  modalSkills.value = []
}

// ── Local Access ─────────────────────────────────────────────────

const isLocalAllowAll = computed(() =>
  form.value.local_tools.length === 1 && form.value.local_tools[0] === '*'
)

const localAccessSummary = computed(() => {
  if (!form.value.local_tools.length) return 'none'
  if (isLocalAllowAll.value) return 'all (including shell ⚡)'
  return form.value.local_tools.join(' · ')
})

function toggleLocalAllowAll() {
  if (isLocalAllowAll.value) {
    form.value.local_tools = []
  } else {
    form.value.local_tools = ['*']
  }
  markDirty()
}

const showLocalAccessModal = ref(false)

// Local Access modal
const LOCAL_TOOL_CATALOG = [
  {
    category: 'File System',
    icon: '📁',
    tools: [
      { name: 'read_file', label: 'Read files', description: 'Read file contents' },
      { name: 'list_dir', label: 'List directory', description: 'List files in a directory' },
      { name: 'search_files', label: 'Search files', description: 'Search file contents by pattern' },
      { name: 'grep', label: 'Grep', description: 'Search for text across files' },
      { name: 'write_file', label: 'Write files', description: 'Create or overwrite files ⚠' },
      { name: 'edit_file', label: 'Edit files', description: 'Edit specific lines in a file ⚠' },
    ],
  },
  {
    category: 'Git',
    icon: '🌿',
    tools: [
      { name: 'git_status', label: 'Git status', description: 'Show working tree status' },
      { name: 'git_log', label: 'Git log', description: 'Show commit history' },
      { name: 'git_diff', label: 'Git diff', description: 'Show file diffs' },
      { name: 'git_blame', label: 'Git blame', description: 'Show who last modified each line' },
      { name: 'git_commit', label: 'Git commit', description: 'Commit staged changes ⚠' },
      { name: 'git_branch', label: 'Git branch', description: 'Create or switch branches ⚠' },
      { name: 'git_stash', label: 'Git stash', description: 'Stash or restore working changes ⚠' },
    ],
  },
  {
    category: 'Code Intelligence',
    icon: '🔍',
    tools: [
      { name: 'find_definition', label: 'Find definition', description: 'Jump to symbol definition (LSP)' },
      { name: 'list_symbols', label: 'List symbols', description: 'List all symbols in a file (LSP)' },
    ],
  },
  {
    category: 'Web',
    icon: '🌐',
    tools: [
      { name: 'fetch_url', label: 'Fetch URL', description: 'Fetch content from a URL' },
      { name: 'web_search', label: 'Web search', description: 'Search the web (requires Brave API key)' },
    ],
  },
] as const

const SHELL_TOOLS = [
  { name: 'bash', label: 'Bash', description: 'Run arbitrary shell commands. Requires approval on every use.' },
  { name: 'run_tests', label: 'Run tests', description: 'Run the project test suite. Requires approval on every use.' },
] as const

const modalLocalTools = ref<string[]>([])
const hoveredGrantedIdx = ref(-1)
const hoveredAvailableName = ref('')

// Connections modal hover state
const hoveredAvailableConn = ref('')
const hoveredAssignedIdx = ref(-1)

function openLocalAccessModal() {
  modalLocalTools.value = [...form.value.local_tools.filter(n => n !== '*')]
  showLocalAccessModal.value = true
}

function saveLocalAccessModal() {
  form.value.local_tools = [...modalLocalTools.value]
  showLocalAccessModal.value = false
}

function localModalGrant(name: string) {
  if (!modalLocalTools.value.includes(name)) {
    modalLocalTools.value.push(name)
  }
}

function localModalGrantAll() {
  const all = [
    ...LOCAL_TOOL_CATALOG.flatMap(cat => cat.tools.map(t => t.name)),
    ...SHELL_TOOLS.map(t => t.name),
  ]
  for (const name of all) {
    if (!modalLocalTools.value.includes(name)) {
      modalLocalTools.value.push(name)
    }
  }
}

const SHELL_TOOL_NAMES = new Set(['bash', 'run_tests'])
function isShellTool(name: string) { return SHELL_TOOL_NAMES.has(name) }

function toolLabel(name: string): string {
  for (const cat of LOCAL_TOOL_CATALOG) {
    const found = cat.tools.find((t) => t.name === name)
    if (found) return found.label
  }
  const shell = SHELL_TOOLS.find((t) => t.name === name)
  return shell?.label ?? name
}

function toolDescription(name: string): string {
  for (const cat of LOCAL_TOOL_CATALOG) {
    const found = cat.tools.find((t) => t.name === name)
    if (found) return found.description
  }
  const shell = SHELL_TOOLS.find((t) => t.name === name)
  return shell?.description ?? ''
}

// ── Connections Allow All ─────────────────────────────────────────

const isConnectionsAllowAll = computed(() =>
  form.value.toolbelt.length === 1 && form.value.toolbelt[0]?.provider === '*'
)

const connectionsSummary = computed(() => {
  if (!form.value.toolbelt.length) return 'none'
  if (isConnectionsAllowAll.value) return 'all connections'
  return form.value.toolbelt.length + ' attached'
})

function toggleConnectionsAllowAll() {
  if (isConnectionsAllowAll.value) {
    form.value.toolbelt = []
  } else {
    form.value.toolbelt = [{ connection_id: '*', provider: '*', profile: '', approval_gate: false }]
  }
  markDirty()
}


async function ensureVault() {
  if (form.value.memory_type !== 'muninndb' || !form.value.vault_name) return
  // Skip only if already linked (Huginn has a token). Unlinked vaults still need key generation.
  if (linkedVaultNames.value.includes(form.value.vault_name)) return
  await api.muninn.createVault({ vault_name: form.value.vault_name, agent_label: `huginn-${form.value.name}` })
}

function deriveMemoryType(data: any): MemoryType {
  if (data.memory_type) {
    // Migrate agents saved before the 'notes' → 'context' rename.
    if (data.memory_type === 'notes') return 'context'
    return data.memory_type as MemoryType
  }
  // Backwards compat: derive from canonical boolean fields.
  if (data.context_notes_enabled) return 'context'
  return data.memory_enabled ? 'muninndb' : 'none'
}

async function loadAgent(name: string) {
  try {
    const data = await api.agents.get(name) as unknown as AgentForm
    const memType = deriveMemoryType(data)
    form.value = {
      name: data.name || name,
      model: data.model || '',
      system_prompt: data.system_prompt || '',
      color: (data as AgentForm & { color?: string }).color || '#58a6ff',
      icon: (data as AgentForm & { icon?: string }).icon || '',
      memory_type: memType,
      memory_enabled: memType === 'muninndb',
      context_notes_enabled: !!(data as any).context_notes_enabled,
      vault_name: (data as any).vault_name || '',
      memory_mode: (data.memory_mode as MemoryMode) || 'conversational',
      vault_description: (data as any).vault_description || '',
      toolbelt: (data as any).toolbelt || [],
      skills: (data as any).skills || [],
      local_tools: (data as any).local_tools ?? [],
      version: (data as any).version ?? 0,
    }
    original.value = JSON.stringify(form.value)
    dirty.value = false
    saveMsg.value = ''
  } catch (e) {
    console.error('Failed to load agent', e)
  }
}

async function loadActiveState() {
  if (!props.agentName || props.agentName === 'new') {
    isActive.value = false
    return
  }
  try {
    const active = await api.agents.getActive()
    isActive.value = active.name === props.agentName
  } catch {
    isActive.value = false
  }
}

async function setActive() {
  if (!form.value.name) return
  try {
    await api.agents.setActive(form.value.name)
    isActive.value = true
  } catch (e: unknown) {
    saveMsg.value = e instanceof Error ? e.message : 'Failed to set active agent'
    saveError.value = true
  }
}

async function loadAvailableModels() {
  modelsLoading.value = true
  modelsError.value = ''
  try {
    const data = await api.models.available() as { models?: OllamaModel[]; builtin_models?: OllamaModel[]; error?: string }
    if (data.error && !data.models?.length && !data.builtin_models?.length) {
      modelsError.value = 'Ollama not reachable'
    }
    const ollamaModels = (data.models ?? []) as OllamaModel[]
    const builtinModels = ((data.builtin_models ?? []) as OllamaModel[]).map(m => ({ ...m, source: 'built-in' }))
    availableModels.value = [...ollamaModels, ...builtinModels]
  } catch {
    modelsError.value = 'Ollama not reachable'
    availableModels.value = []
  } finally {
    modelsLoading.value = false
  }
}

function validateAgentForm(): string | null {
  if (!form.value.name?.trim()) return 'Agent name is required'
  if (/[/\\\0:]/.test(form.value.name) || /[\x00-\x1f]/.test(form.value.name)) {
    return 'Agent name contains invalid characters'
  }
  return null
}

async function save() {
  const validationError = validateAgentForm()
  if (validationError) {
    saveMsg.value = validationError
    saveError.value = true
    return
  }
  saving.value = true
  saveMsg.value = ''
  saveError.value = false
  try {
    await ensureVault()
    await api.agents.update(form.value.name, form.value)
    updateAgent(form.value.name, { ...form.value })
    original.value = JSON.stringify(form.value)
    dirty.value = false
    saveMsg.value = 'Saved successfully'
    setTimeout(() => { saveMsg.value = '' }, 3000)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'Save failed'
    if (msg.includes('409') || msg.toLowerCase().includes('conflict')) {
      saveMsg.value = 'Agent was modified by another client — please reload'
    } else {
      saveMsg.value = msg
    }
    saveError.value = true
  } finally {
    saving.value = false
  }
}

function discard() {
  form.value = JSON.parse(original.value)
  dirty.value = false
}

function confirmDelete() { showDeleteConfirm.value = true }

async function deleteAgent() {
  try {
    const resp = await fetch(`/api/v1/agents/${form.value.name}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${getToken()}` },
    })
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({ error: 'Delete failed' }))
      saveMsg.value = body.error || `Delete failed (${resp.status})`
      saveError.value = true
      showDeleteConfirm.value = false
      return
    }
    removeFromList(form.value.name)
    router.push('/agents')
  } catch (e: unknown) {
    saveMsg.value = e instanceof Error ? e.message : 'Network error'
    saveError.value = true
    showDeleteConfirm.value = false
  }
}

function createNew() {
  // Navigate to /agents/new — handled by same view but with preset form
  form.value = { name: '', model: '', system_prompt: '', color: '#58a6ff', icon: '', memory_type: 'none', memory_enabled: false, context_notes_enabled: false, vault_name: '', memory_mode: 'conversational', vault_description: '', toolbelt: [], skills: [], local_tools: [] }
  dirty.value = true
}

watch(() => props.agentName, (name) => {
  showDeleteConfirm.value = false
  loadActiveState()
  if (name && name !== 'new') {
    loadAgent(name)
    startVaultHealthPolling()
  } else {
    stopVaultHealthPolling()
    if (name === 'new') {
      form.value = { name: '', model: '', system_prompt: '', color: '#58a6ff', icon: '', memory_type: 'none', memory_enabled: false, context_notes_enabled: false, vault_name: '', memory_mode: 'conversational', vault_description: '', toolbelt: [], skills: [], local_tools: [] }
      original.value = ''
      dirty.value = true
    }
  }
}, { immediate: true })

onMounted(() => {
  loadAvailableModels()
  loadMuninnInfo()
  loadConnections()
  loadAvailableSkills()
  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('focus', onWindowFocus)
})

onBeforeUnmount(() => {
  if (vaultCheckTimeout.value) clearTimeout(vaultCheckTimeout.value)
  stopVaultHealthPolling()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('focus', onWindowFocus)
  if (staleDebounceTimer !== null) {
    clearTimeout(staleDebounceTimer)
    staleDebounceTimer = null
  }
})

// Close modals on ESC (connections and skills first, then model picker)
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (showConnectionsModal.value) { showConnectionsModal.value = false; return }
  if (showSkillsModal.value) { showSkillsModal.value = false; return }
  if (memoryModal.value.open) { cancelMemoryModal(); return }
  if (showModelPicker.value) { showModelPicker.value = false; modelSearch.value = '' }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<style scoped>
.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .relative, .modal-fade-leave-to .relative { transform: scale(0.96) translateY(6px); }

.list-item-enter-active, .list-item-leave-active { transition: all 0.18s ease; }
.list-item-enter-from { opacity: 0; transform: translateY(-4px) scale(0.97); }
.list-item-leave-to { opacity: 0; transform: translateX(10px) scale(0.97); }
.list-item-leave-active { position: absolute; width: calc(100% - 1.5rem); }
.list-item-move { transition: transform 0.18s ease; }
</style>
