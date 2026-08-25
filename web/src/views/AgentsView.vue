<template>
  <div class="flex flex-col h-full bg-huginn-bg">

    <!-- No agent selected -->
    <div v-if="!agentName" class="flex-1 overflow-y-auto p-6">

      <!-- Empty state: no agents at all -->
      <div v-if="agents.length === 0 && !loading" class="flex flex-col items-center justify-center h-full gap-5 pb-16">
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

      <!-- Card grid: agents exist -->
      <template v-else>
        <div class="grid gap-4" style="grid-template-columns:repeat(auto-fill,minmax(220px,1fr))">
          <AgentCard
            v-for="agent in agents"
            :key="agent.name"
            :agent="agent"
            @click="openDM(agent)"
            @edit="router.push('/agents/' + agent.name)"
          />
        </div>

        <div class="mt-6 flex justify-center">
          <button data-testid="new-agent-btn" @click="createNew"
            class="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-huginn-blue border border-huginn-blue/30 hover:bg-huginn-blue/10 transition-all duration-150 active:scale-95">
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
            </svg>
            New agent
          </button>
        </div>
      </template>

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
        <!-- Wildcard strip info banner -->
        <div v-if="wildcardStripped"
          class="flex items-center gap-2 px-4 py-2.5 border-b border-huginn-amber/20 bg-huginn-amber/8 text-huginn-amber text-xs"
        >
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
          Removed legacy wildcard connection. Save to persist.
          <button @click="wildcardStripped = false" class="ml-auto text-huginn-amber/60 hover:text-huginn-amber transition-colors">✕</button>
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

            <!-- Description -->
            <div class="space-y-2">
              <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">Description</p>
              <input
                data-testid="agent-description-input"
                v-model="form.description" @input="markDirty"
                placeholder="One-line role — or leave blank to use the system prompt"
                maxlength="200"
                class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 transition-colors placeholder:text-huginn-muted/40" />
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
              <div v-if="connectionValidationIssues.length"
                data-testid="toolbelt-validation-warning"
                class="px-3 py-2 rounded-lg border border-huginn-amber/40 bg-huginn-amber/8">
                <p class="text-[11px] text-huginn-amber font-medium">Some assignments need attention before save:</p>
                <p v-for="issue in connectionValidationIssues"
                  :key="issue.entry.connection_id + ':' + (issue.entry.profile || '')"
                  class="text-[10px] text-huginn-amber/80 mt-0.5">
                  {{ connectionLabel(issue.entry.connection_id) }}: {{ issue.reason }}
                </p>
              </div>
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

              <!-- Heartbeat -->
              <div class="mt-4 border-t border-huginn-border/20 pt-4">
                <div class="flex items-center justify-between mb-2">
                  <div>
                    <div class="text-[11px] font-medium text-huginn-text">Send me regular updates</div>
                    <div class="text-[10px] text-huginn-muted/60 mt-0.5">Agent checks in via DM on a schedule</div>
                  </div>
                  <button
                    type="button"
                    @click="() => { form.heartbeat_enabled = !form.heartbeat_enabled; markDirty() }"
                    :class="form.heartbeat_enabled
                      ? 'bg-huginn-blue'
                      : 'bg-huginn-muted/20'"
                    class="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none">
                    <span
                      :class="form.heartbeat_enabled ? 'translate-x-4' : 'translate-x-0'"
                      class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out" />
                  </button>
                </div>

                <div v-if="form.heartbeat_enabled" class="mt-2">
                  <label class="text-[10px] text-huginn-muted/60 font-medium uppercase tracking-wide block mb-1">Frequency</label>
                  <select
                    v-model="form.heartbeat_cron"
                    @change="markDirty"
                    class="w-full bg-huginn-bg/50 border border-huginn-border/30 rounded px-2 py-1 text-[11px] text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                    <option value="">Every 4 hours (default)</option>
                    <option value="0 */12 * * *">Twice daily</option>
                    <option value="0 8 * * *">Daily at 8am</option>
                    <option value="0 8 * * 1">Weekly (Monday 8am)</option>
                  </select>
                  <div class="mt-1 text-[10px] text-huginn-muted/40">
                    Cron: {{ form.heartbeat_cron || '0 */4 * * *' }}
                  </div>
                </div>
              </div>
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
            <button data-testid="save-agent-btn-sticky" @click="() => save()" :disabled="saving || connectionValidationIssues.length > 0"
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

                    <div v-if="modalEntryIssueReason(entry)"
                      class="px-3.5 py-2 border-t border-huginn-red/20 bg-huginn-red/6">
                      <p class="text-[10px] text-huginn-red/85">
                        {{ modalEntryIssueReason(entry) }}
                      </p>
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
              :disabled="modalConnectionValidationIssues.length > 0"
              class="px-5 py-2 text-xs font-semibold text-white rounded-lg transition-all duration-150 active:scale-[0.97]"
              :style="modalConnectionValidationIssues.length > 0
                ? 'background:linear-gradient(135deg,rgba(88,166,255,0.45),rgba(58,130,246,0.45));box-shadow:none;cursor:not-allowed'
                : 'background:linear-gradient(135deg,rgba(88,166,255,0.95),rgba(58,130,246,0.95));box-shadow:0 2px 14px rgba(88,166,255,0.28)'">
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
              <p class="text-[10px] text-huginn-muted/60 mt-0.5">{{ availableModels.length }} models available</p>
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
                @click="selectModel(m.name, m.source)"
                class="w-full flex items-center gap-3 pl-10 pr-4 py-2 text-left transition-colors hover:bg-huginn-surface/60"
                :class="form.model === m.name ? 'bg-huginn-blue/8' : ''">
                <div class="w-7 h-7 rounded-lg flex items-center justify-center flex-shrink-0" :style="{ background: group.color + '15' }">
                  <span class="text-[10px] font-semibold" :style="{ color: group.color }">{{ m.details?.parameter_size?.replace(/[^0-9.BMGKbmgk]+/g,'').slice(0,4) || group.icon }}</span>
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
import { toRef } from 'vue'
import { useRouter } from 'vue-router'
import AgentCard from '../components/AgentCard.vue'
import { useAgentsViewState } from './agents/useAgentsViewState'

const props = defineProps<{ agentName?: string }>()
const router = useRouter()

const {
  agents,
  loading,
  openDM,
  isStaleRefreshing,
  form,
  dirty,
  saving,
  saveMsg,
  saveError,
  loadError,
  loadErrorMsg,
  showDeleteConfirm,
  wildcardStripped,
  availableModels,
  showModelPicker,
  modelSearch,
  selectModel,
  markDirty,
  muninnConnected,
  existingVaults,
  allVaultNames,
  vaultHealth,
  memoryModal,
  selectMuninnDB,
  cancelMemoryModal,
  openMemoryModal,
  saveMemoryModal,
  availableConnections,
  systemTools,
  showConnectionsModal,
  modalToolbelt,
  showSkillsModal,
  modalSkills,
  colorPalette,
  filteredModelGroups,
  memoryModes,
  availableSkills,
  connectionLabel,
  connectionIcon,
  openConnectionsModal,
  saveConnectionsModal,
  modalAddableConnections,
  modalAddableSystemToolsForModal,
  modalIsProfileAdded,
  modalAddConnection,
  modalAddSystemTool,
  modalRemoveEntry,
  modalRemoveAll,
  modalAddAll,
  modalToggleApprovalGate,
  openSkillsModal,
  saveSkillsModal,
  modalAddableSkills,
  modalAddSkill,
  modalRemoveSkill,
  addAllSkills,
  clearAllSkills,
  isLocalAllowAll,
  localAccessSummary,
  toggleLocalAllowAll,
  showLocalAccessModal,
  LOCAL_TOOL_CATALOG,
  SHELL_TOOLS,
  modalLocalTools,
  hoveredGrantedIdx,
  hoveredAvailableName,
  hoveredAvailableConn,
  hoveredAssignedIdx,
  openLocalAccessModal,
  saveLocalAccessModal,
  localModalGrant,
  localModalGrantAll,
  isShellTool,
  toolLabel,
  toolDescription,
  isConnectionsAllowAll,
  connectionsSummary,
  connectionValidationIssues,
  modalConnectionValidationIssues,
  modalEntryIssueReason,
  toggleConnectionsAllowAll,
  save,
  discard,
  confirmDelete,
  deleteAgent,
  createNew,
} = useAgentsViewState(toRef(props, 'agentName'), router)
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
