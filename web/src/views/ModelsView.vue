<template>
  <div class="flex h-full bg-huginn-bg">
    <!-- Left sidebar -->
    <div class="w-48 flex-shrink-0 flex flex-col border-r border-huginn-border"
      style="background:rgba(22,27,34,0.6)">
      <div class="flex items-center px-4 h-11 border-b border-huginn-border flex-shrink-0">
        <span class="text-xs font-semibold text-huginn-muted uppercase tracking-widest">Models</span>
      </div>
      <nav class="flex-1 overflow-y-auto py-2">
        <button v-for="p in providers" :key="p.value"
          @click="selectProvider(p.value)"
          class="w-full flex items-center gap-2.5 px-4 py-2 text-xs transition-all duration-150"
          :class="currentProvider === p.value
            ? 'text-huginn-blue bg-huginn-blue/10'
            : 'text-huginn-muted hover:text-huginn-text hover:bg-white/4'">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="3" /><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83" />
          </svg>
          <span class="flex-1 text-left font-medium">{{ p.label }}</span>
          <div v-if="currentProvider === p.value" class="w-1 h-1 rounded-full bg-huginn-blue flex-shrink-0" />
        </button>
      </nav>
    </div>

    <!-- Main content -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- Content header -->
      <div class="flex items-center justify-between px-5 h-11 border-b border-huginn-border flex-shrink-0"
        style="background:rgba(22,27,34,0.6)">
        <span class="text-sm font-medium text-huginn-text">
          {{ providers.find(p => p.value === currentProvider)?.label }}
        </span>
        <!-- Ollama status in header when on ollama page -->
        <div v-if="currentProvider === 'ollama'">
          <div v-if="ollamaStatus === 'connected'" class="flex items-center gap-1.5 text-huginn-green text-xs">
            <div class="w-1.5 h-1.5 rounded-full bg-huginn-green" style="box-shadow:0 0 4px rgba(63,185,80,0.6)" />
            Connected
          </div>
          <div v-else-if="ollamaStatus === 'error'" class="flex items-center gap-1.5 text-huginn-muted text-xs">
            <div class="w-1.5 h-1.5 rounded-full bg-huginn-muted/50" />
            Offline
          </div>
        </div>
        <!-- Built-in status in header -->
        <div v-if="currentProvider === 'builtin'">
          <div v-if="builtinStatus?.backend_type === 'managed'" class="flex items-center gap-1.5 text-huginn-green text-xs">
            <div class="w-1.5 h-1.5 rounded-full bg-huginn-green" style="box-shadow:0 0 4px rgba(63,185,80,0.6)" />
            Active
          </div>
          <div v-else-if="builtinNotConfigured" class="flex items-center gap-1.5 text-huginn-muted text-xs">
            <div class="w-1.5 h-1.5 rounded-full bg-huginn-muted/50" />
            Not configured
          </div>
          <div v-else class="flex items-center gap-1.5 text-huginn-muted text-xs">
            <div class="w-1.5 h-1.5 rounded-full bg-huginn-muted/50" />
            Inactive
          </div>
        </div>
      </div>

      <!-- Config changed banner -->
      <div v-if="externallyChanged" class="mx-4 mt-3 flex-shrink-0">
        <div class="flex items-center gap-3 px-4 py-2.5 rounded-xl border border-huginn-yellow/40 text-huginn-yellow text-xs"
          style="background:rgba(210,153,34,0.07)">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          Config was updated externally — page reflects latest values.
          <button @click="externallyChanged = false" class="ml-auto text-huginn-muted hover:text-huginn-text">×</button>
        </div>
      </div>

      <div v-if="loading" class="flex items-center justify-center h-full">
        <div class="w-5 h-5 border-2 border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
      </div>

      <div v-else class="flex-1 flex min-h-0 overflow-hidden">

        <!-- Non-Ollama providers: keep original centered layout -->
        <template v-if="currentProvider !== 'ollama'">
          <div class="flex-1 overflow-y-auto">
            <div class="max-w-2xl mx-auto px-5 py-6 space-y-8">

              <!-- Save feedback -->
              <div v-if="saveMsg" class="px-4 py-2.5 rounded-xl border text-xs"
                :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
                {{ saveMsg }}
              </div>

              <!-- ── Provider config ───────────────────────────────────── -->
              <section v-if="currentProvider !== 'builtin'" class="space-y-4">
                <div class="space-y-3">
                  <!-- Endpoint -->
                  <div class="space-y-1.5">
                    <label class="text-xs text-huginn-muted">Endpoint URL</label>
                    <input v-model="form.backend_endpoint" @input="dirty = true"
                      :placeholder="providerEndpointPlaceholder"
                      class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors" />
                  </div>

                  <!-- API Key (shown for non-ollama) -->
                  <div v-if="currentProvider !== 'ollama'" class="space-y-1.5">
                    <label class="text-xs text-huginn-muted">API Key</label>
                    <div class="relative">
                      <input v-model="form.backend_api_key" @input="dirty = true"
                        :type="showApiKey ? 'text' : 'password'"
                        placeholder="sk-... or $ANTHROPIC_API_KEY"
                        class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 pr-10 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors" />
                      <button @click="showApiKey = !showApiKey"
                        class="absolute right-2.5 top-1/2 -translate-y-1/2 text-huginn-muted hover:text-huginn-text transition-colors">
                        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <path v-if="!showApiKey" d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" /><circle v-if="!showApiKey" cx="12" cy="12" r="3" />
                          <path v-if="showApiKey" d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24" />
                          <line v-if="showApiKey" x1="1" y1="1" x2="23" y2="23" />
                        </svg>
                      </button>
                    </div>
                    <p v-if="isApiKeyRedacted" class="text-[11px] text-huginn-green">Key is saved — enter a new value to replace it</p>
                    <p v-else class="text-[11px] text-huginn-muted">Use <code class="text-huginn-blue">$ENV_VAR</code> syntax to reference an environment variable</p>
                  </div>

                  <!-- Save / Discard -->
                  <div v-if="dirty" class="flex gap-2 pt-1">
                    <button @click="discardChanges"
                      class="px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-muted border border-huginn-border hover:bg-white/5 transition-all">
                      Discard
                    </button>
                    <button @click="save" :disabled="saving"
                      class="px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-green border border-huginn-green/30 hover:bg-huginn-green/15 transition-all disabled:opacity-50">
                      {{ saving ? 'Saving…' : 'Save changes' }}
                    </button>
                  </div>
                </div>
              </section>

              <!-- ── Built-in (llama.cpp) content ──────────────────────── -->
          <template v-if="currentProvider === 'builtin'">
            <!-- Activate feedback -->
            <div v-if="builtinActivateMsg" class="px-4 py-2.5 rounded-xl border text-xs"
              :class="builtinActivateError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
              {{ builtinActivateMsg }}
            </div>

            <!-- Restart notice when managed backend is active -->
            <div v-if="builtinStatus?.backend_type === 'managed' && builtinStatus?.active_model"
              class="flex items-center gap-3 px-4 py-2.5 rounded-xl border border-huginn-blue/30 text-huginn-blue text-xs"
              style="background:rgba(88,166,255,0.06)">
              <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
              Built-in backend is active with model <span class="font-mono ml-1">{{ builtinStatus.active_model }}</span>. Restart Huginn to apply model changes.
            </div>

            <!-- Runtime binary card -->
            <section class="space-y-3">
              <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Runtime Binary (llama-server)</h3>
              <div class="px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50 space-y-2">
                <div v-if="builtinLoading" class="flex items-center gap-2 text-huginn-muted text-xs">
                  <div class="w-3.5 h-3.5 border border-huginn-muted border-t-huginn-blue rounded-full animate-spin" />
                  Loading...
                </div>
                <template v-else-if="builtinStatus">
                  <div class="flex items-center justify-between">
                    <div class="space-y-0.5">
                      <p class="text-xs font-medium text-huginn-text">
                        {{ builtinStatus.installed ? 'Installed' : 'Not installed' }}
                      </p>
                      <p class="text-[11px] text-huginn-muted font-mono">v{{ builtinStatus.version }}</p>
                      <p v-if="builtinStatus.installed" class="text-[11px] text-huginn-muted font-mono truncate max-w-xs">{{ builtinStatus.binary_path }}</p>
                    </div>
                    <button v-if="!builtinStatus.installed || !builtinDownloading"
                      @click="startDownloadRuntime"
                      :disabled="builtinDownloading"
                      class="px-3 py-1.5 rounded-lg text-xs font-medium border transition-all duration-150 disabled:opacity-50"
                      :class="builtinStatus.installed
                        ? 'border-huginn-border text-huginn-muted hover:border-huginn-blue/40 hover:text-huginn-blue'
                        : 'border-huginn-green/30 text-huginn-green hover:bg-huginn-green/10'">
                      {{ builtinStatus.installed ? 'Re-download' : 'Download' }}
                    </button>
                  </div>
                  <!-- Download progress -->
                  <div v-if="builtinDownloading || builtinDownloadProgress" class="space-y-1.5">
                    <div class="flex items-center justify-between text-[11px] text-huginn-muted">
                      <span>{{ builtinDownloading ? 'Downloading...' : 'Download complete' }}</span>
                      <span v-if="builtinDownloadProgress">{{ formatBuiltinProgress(builtinDownloadProgress.downloaded, builtinDownloadProgress.total) }}</span>
                    </div>
                    <div class="w-full bg-huginn-border rounded-full h-1">
                      <div class="bg-huginn-blue h-1 rounded-full transition-all"
                        :style="{ width: builtinDownloadProgress && builtinDownloadProgress.total > 0 ? `${Math.min(100, (builtinDownloadProgress.downloaded / builtinDownloadProgress.total) * 100).toFixed(1)}%` : '0%' }" />
                    </div>
                  </div>
                  <p v-if="builtinDownloadError" class="text-xs text-huginn-red">{{ builtinDownloadError }}</p>
                </template>
                <div v-else-if="builtinNotConfigured" class="text-xs text-huginn-muted">
                  Built-in runtime is not configured — start Huginn with <code class="text-huginn-blue font-mono">--builtin</code> to enable it.
                </div>
                <div v-else class="text-xs text-huginn-muted">Runtime manager not available.</div>
              </div>
            </section>

            <div class="border-t border-huginn-border" />

            <!-- Model catalog -->
            <section class="space-y-3">
              <div class="flex items-center justify-between">
                <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Model Catalog</h3>
                <button @click="loadBuiltinData" class="text-xs text-huginn-muted hover:text-huginn-blue transition-colors">Refresh</button>
              </div>

              <div v-if="builtinLoading" class="flex items-center gap-2 text-huginn-muted text-xs py-4">
                <div class="w-3.5 h-3.5 border border-huginn-muted border-t-huginn-blue rounded-full animate-spin" />
                Loading catalog...
              </div>

              <div v-else-if="builtinCatalog.length === 0" class="py-6 text-center">
                <p class="text-huginn-muted text-xs">No models in catalog.</p>
              </div>

              <div v-else class="space-y-2">
                <div v-for="m in builtinCatalog" :key="m.name"
                  class="px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50 space-y-2">
                  <div class="flex items-start justify-between gap-3">
                    <div class="flex-1 min-w-0 space-y-0.5">
                      <div class="flex items-center gap-2 flex-wrap">
                        <p class="text-xs font-medium text-huginn-text font-mono">{{ m.name }}</p>
                        <span v-if="builtinStatus?.active_model === m.name" class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-blue/30 text-huginn-blue">Active</span>
                        <span v-for="tag in m.tags" :key="tag" class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ tag }}</span>
                      </div>
                      <p v-if="m.description" class="text-[11px] text-huginn-muted">{{ m.description }}</p>
                      <p class="text-[11px] text-huginn-muted">
                        {{ formatSize(m.size_bytes) }}
                        <span v-if="m.min_ram_gb"> · {{ m.min_ram_gb }}GB RAM min</span>
                        <span v-if="m.context_length"> · {{ m.context_length.toLocaleString() }} ctx</span>
                      </p>
                    </div>
                    <div class="flex gap-1.5 flex-shrink-0 items-center">
                      <!-- Installed checkmark badge -->
                      <span v-if="m.installed && !builtinPulling[m.name]"
                        class="flex items-center gap-1 text-[10px] px-2 py-1 rounded-lg border border-huginn-green/40 text-huginn-green bg-huginn-green/8 font-medium">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <polyline points="20 6 9 17 4 12" />
                        </svg>
                        Installed
                      </span>
                      <button v-if="!m.installed"
                        @click="startPullModel(m.name)"
                        :disabled="!!builtinPulling[m.name] || !builtinStatus?.installed"
                        class="px-3 py-1.5 rounded-lg text-xs font-medium border transition-all duration-150 disabled:opacity-50"
                        :class="builtinPulling[m.name]
                          ? 'border-huginn-blue/30 text-huginn-blue bg-huginn-blue/10'
                          : 'border-huginn-green/30 text-huginn-green bg-huginn-green/8 hover:bg-huginn-green/15'">
                        {{ builtinPulling[m.name] ? 'Downloading...' : 'Install' }}
                      </button>
                      <button v-if="m.installed && builtinStatus?.active_model !== m.name"
                        @click="activateBuiltin(m.name)"
                        :disabled="builtinActivating"
                        class="px-3 py-1.5 rounded-lg text-xs font-medium border border-huginn-blue/30 text-huginn-blue hover:bg-huginn-blue/10 transition-all duration-150 disabled:opacity-50">
                        Activate
                      </button>
                      <button v-if="m.installed && !builtinPulling[m.name]"
                        @click="deleteBuiltinModel(m.name)"
                        :disabled="deletingBuiltin.has(m.name)"
                        class="px-3 py-1.5 rounded-lg text-xs font-medium border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-all duration-150 disabled:opacity-50">
                        {{ deletingBuiltin.has(m.name) ? '…' : 'Remove' }}
                      </button>
                    </div>
                  </div>
                  <!-- Pull progress -->
                  <div v-if="builtinPulling[m.name] && builtinPullProgress[m.name]" class="space-y-1.5">
                    <div class="flex items-center justify-between text-[11px] text-huginn-muted">
                      <span>Downloading...</span>
                      <span>{{ formatBuiltinProgress(builtinPullProgress[m.name]?.downloaded ?? 0, builtinPullProgress[m.name]?.total ?? 0) }}</span>
                    </div>
                    <div class="w-full bg-huginn-border rounded-full h-1">
                      <div class="bg-huginn-blue h-1 rounded-full transition-all"
                        :style="{ width: (builtinPullProgress[m.name]?.total ?? 0) > 0 ? `${Math.min(100, ((builtinPullProgress[m.name]?.downloaded ?? 0) / (builtinPullProgress[m.name]?.total ?? 1)) * 100).toFixed(1)}%` : '0%' }" />
                    </div>
                  </div>
                  <p v-if="builtinPullError[m.name]" class="text-xs text-huginn-red">{{ builtinPullError[m.name] }}</p>
                </div>
              </div>
            </section>
          </template>
            </div>
          </div>
        </template>
        <!-- end non-ollama -->

        <!-- ── Ollama: full-width two-panel layout ──────────────────── -->
        <template v-else>
          <div class="flex flex-1 min-h-0 overflow-hidden gap-0">

            <!-- Left column: ~320px, fixed, scrollable -->
            <div class="w-80 flex-shrink-0 flex flex-col gap-3 overflow-y-auto border-r border-huginn-border px-4 py-5"
              style="background:rgba(22,27,34,0.35)">

              <!-- Save feedback -->
              <div v-if="saveMsg" class="px-3 py-2 rounded-xl border text-xs"
                :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
                {{ saveMsg }}
              </div>

              <!-- Connection card -->
              <div class="bg-huginn-surface/50 border border-huginn-border rounded-xl px-4 py-3 space-y-3">
                <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Connection</p>
                <div class="space-y-1.5">
                  <label class="text-xs text-huginn-muted">Endpoint URL</label>
                  <input v-model="form.backend_endpoint" @input="dirty = true"
                    :placeholder="providerEndpointPlaceholder"
                    class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors" />
                </div>
                <div v-if="dirty" class="flex gap-2">
                  <button @click="discardChanges"
                    class="flex-1 px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-muted border border-huginn-border hover:bg-white/5 transition-all duration-150">
                    Discard
                  </button>
                  <button @click="save" :disabled="saving"
                    class="flex-1 px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-green border border-huginn-green/30 hover:bg-huginn-green/15 transition-all duration-150 disabled:opacity-50">
                    {{ saving ? 'Saving…' : 'Save' }}
                  </button>
                </div>
              </div>

              <!-- Pull new model card -->
              <div class="bg-huginn-surface/50 border border-huginn-border rounded-xl px-4 py-3 space-y-3">
                <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Pull Model</p>
                <div class="flex items-center gap-2">
                  <input
                    v-model="pullModelName"
                    @keydown.enter="pullModel(pullModelName)"
                    placeholder="e.g. llama3.2:3b"
                    class="flex-1 min-w-0 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text font-mono outline-none focus:border-huginn-blue/50 transition-colors"
                  />
                  <button
                    @click="pullModel(pullModelName)"
                    :disabled="!pullModelName || pulling"
                    class="px-3 py-2 rounded-lg text-xs font-medium border transition-all duration-150 disabled:opacity-50 flex-shrink-0"
                    :class="pulling
                      ? 'border-huginn-blue/30 text-huginn-blue bg-huginn-blue/10'
                      : 'border-huginn-border text-huginn-muted hover:border-huginn-blue/40 hover:text-huginn-blue'"
                  >
                    {{ pulling ? 'Pulling…' : 'Pull' }}
                  </button>
                </div>

                <!-- Pull feedback -->
                <div v-if="pullMsg"
                  class="px-3 py-2 rounded-xl border text-xs"
                  :class="pullError
                    ? 'border-red-400/40 text-red-400 bg-red-400/8'
                    : 'border-green-400/40 text-green-400 bg-green-400/8'">
                  {{ pullMsg }}
                </div>

                <!-- Delete error -->
                <div v-if="deleteError"
                  class="flex items-center justify-between gap-2 px-3 py-2 rounded-xl border border-huginn-red/30 bg-huginn-red/8 text-huginn-red text-xs">
                  <span>{{ deleteError }}</span>
                  <button @click="deleteError = null" class="opacity-60 hover:opacity-100">✕</button>
                </div>
              </div>
            </div>
            <!-- end left column -->

            <!-- Right column: flex-1, full width, scrollable -->
            <div class="flex-1 flex flex-col min-w-0 overflow-hidden">

              <!-- Header row -->
              <div class="flex items-center justify-between px-5 py-3.5 border-b border-huginn-border flex-shrink-0"
                style="background:rgba(22,27,34,0.3)">
                <div class="flex items-center gap-3">
                  <span class="text-xs font-semibold text-huginn-text">Installed Models</span>
                  <template v-if="availableModels.length > 0">
                    <span class="text-[11px] text-huginn-muted px-1.5 py-0.5 rounded border border-huginn-border bg-huginn-surface/50">
                      {{ availableModels.length }} model{{ availableModels.length !== 1 ? 's' : '' }}
                    </span>
                    <span class="text-[11px] text-huginn-muted">
                      {{ formatSize(availableModels.reduce((acc, m) => acc + (m.size ?? 0), 0)) }}
                    </span>
                  </template>
                </div>
                <button @click="loadAvailableModels"
                  class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs text-huginn-muted border border-huginn-border hover:border-huginn-blue/30 hover:text-huginn-blue hover:bg-huginn-blue/5 transition-all duration-150">
                  <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
                  </svg>
                  Refresh
                </button>
              </div>

              <!-- Model grid body -->
              <div class="flex-1 overflow-y-auto px-5 py-5">

                <div v-if="modelsLoading" class="flex items-center gap-2 text-huginn-muted text-xs py-8 justify-center">
                  <div class="w-3.5 h-3.5 border border-huginn-muted border-t-huginn-blue rounded-full animate-spin" />
                  Checking Ollama...
                </div>

                <div v-else-if="ollamaStatus === 'error'" class="py-12 text-center space-y-1.5">
                  <p class="text-huginn-red/80 text-xs font-medium">Ollama is not running</p>
                  <p class="text-huginn-muted text-[11px]">Start Ollama to manage and pull models</p>
                  <code class="text-[11px] text-huginn-muted/60">ollama serve</code>
                </div>

                <div v-else-if="availableModels.length === 0" class="py-12 text-center space-y-1">
                  <p class="text-huginn-muted text-xs">No models installed yet</p>
                  <p class="text-[11px] text-huginn-muted/60">Pull a model using the panel on the left</p>
                </div>

                <!-- 2-column model grid -->
                <div v-else class="grid grid-cols-2 gap-3">
                  <div v-for="m in availableModels" :key="m.name"
                    class="group flex items-start gap-3 px-3.5 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50 transition-all duration-150 hover:border-huginn-blue/30 hover:bg-huginn-surface/80 hover:scale-[1.01]"
                    style="transform-origin:center">

                    <!-- Layers icon in blue-tinted square -->
                    <div class="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0 mt-0.5"
                      style="background:rgba(88,166,255,0.08);border:1px solid rgba(88,166,255,0.15)">
                      <svg class="w-4 h-4 text-huginn-blue/70" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                        <path d="M12 2L2 7l10 5 10-5-10-5z" /><path d="M2 17l10 5 10-5" /><path d="M2 12l10 5 10-5" />
                      </svg>
                    </div>

                    <!-- Model info -->
                    <div class="flex-1 min-w-0 space-y-1.5">
                      <p class="text-sm font-bold text-huginn-text font-mono truncate leading-tight">{{ m.name }}</p>
                      <div v-if="m.details?.parameter_size || m.size" class="flex flex-wrap gap-1">
                        <span v-if="m.details?.parameter_size"
                          class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted bg-huginn-surface/80">
                          {{ m.details.parameter_size }}
                        </span>
                        <span v-if="m.details?.quantization_level"
                          class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted bg-huginn-surface/80">
                          {{ m.details.quantization_level }}
                        </span>
                        <span v-if="m.size"
                          class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted bg-huginn-surface/80">
                          {{ formatSize(m.size) }}
                        </span>
                      </div>
                      <!-- Agent badges -->
                      <div v-if="agentsUsingModel(m.name).length > 0" class="flex flex-wrap gap-1">
                        <span
                          v-for="agentName in agentsUsingModel(m.name)"
                          :key="agentName"
                          class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-blue/30 text-huginn-blue bg-huginn-blue/5">
                          {{ agentName }}
                        </span>
                      </div>
                    </div>

                    <!-- Trash icon button -->
                    <button
                      @click="deleteOllamaModel(m.name)"
                      :disabled="deletingModels.has(m.name)"
                      class="flex-shrink-0 w-7 h-7 flex items-center justify-center rounded-lg border transition-all duration-150 mt-0.5 disabled:opacity-50"
                      :class="deletingModels.has(m.name)
                        ? 'border-huginn-border text-huginn-muted opacity-100'
                        : 'border-transparent text-huginn-muted/30 opacity-0 group-hover:opacity-100 hover:border-huginn-red/40 hover:text-huginn-red hover:bg-huginn-red/8'"
                      title="Remove model">
                      <!-- Spinner when deleting -->
                      <div v-if="deletingModels.has(m.name)"
                        class="w-3.5 h-3.5 border border-huginn-muted border-t-huginn-blue rounded-full animate-spin" />
                      <!-- Trash icon when idle -->
                      <svg v-else class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/>
                      </svg>
                    </button>
                  </div>
                </div>
              </div>
            </div>
            <!-- end right column -->

          </div>
        </template>
        <!-- end ollama two-panel -->

      </div>
    </div>
  </div>
  <!-- ── Delete Confirmation Modal ─────────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="deleteConfirm"
        class="fixed inset-0 z-[200] flex items-center justify-center p-4"
        @mousedown.self="deleteConfirm = null">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />
        <div class="relative w-full max-w-sm bg-[#13151a] border border-white/[0.07] rounded-2xl overflow-hidden"
          style="box-shadow:0 25px 60px rgba(0,0,0,0.55)">
          <!-- Red accent line -->
          <div class="h-px" style="background:linear-gradient(90deg,transparent,rgba(248,81,73,0.5),transparent)" />
          <!-- Header -->
          <div class="flex items-center gap-3.5 px-5 pt-4 pb-3.5 border-b border-white/[0.06]">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0"
              style="background:rgba(248,81,73,0.12);border:1px solid rgba(248,81,73,0.2)">
              <svg class="w-4 h-4" style="color:rgba(248,81,73,0.85)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/>
              </svg>
            </div>
            <div class="flex-1 min-w-0">
              <p class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">Delete model?</p>
              <p class="text-[11px] mt-0.5 font-mono truncate" style="color:rgba(255,255,255,0.45)">{{ deleteConfirm?.name }}</p>
            </div>
          </div>
          <!-- Body -->
          <div class="px-5 py-4">
            <p class="text-xs leading-relaxed" style="color:rgba(255,255,255,0.5)">
              This will permanently remove the model
              {{ deleteConfirm?.type === 'ollama' ? 'from Ollama' : 'from disk' }}.
              You can reinstall it later by pulling it again.
            </p>
          </div>
          <!-- Actions -->
          <div class="flex justify-end gap-2 px-5 pb-4">
            <button @click="deleteConfirm = null"
              class="px-4 py-1.5 text-xs text-huginn-muted border border-huginn-border rounded-lg hover:bg-huginn-surface transition-all">
              Cancel
            </button>
            <button @click="confirmDeleteModel"
              class="px-4 py-1.5 text-xs font-medium text-white rounded-lg transition-all"
              style="background:rgba(248,81,73,0.8)" @mouseenter="e => (e.currentTarget as HTMLElement).style.background='rgba(248,81,73,1)'" @mouseleave="e => (e.currentTarget as HTMLElement).style.background='rgba(248,81,73,0.8)'">
              Delete
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, type BuiltinStatus, type BuiltinCatalogEntry, type BuiltinInstalledModel } from '../composables/useApi'
import { useConfig } from '../composables/useConfig'

const props = defineProps<{ provider?: string }>()
const router = useRouter()
const { config, loading, externallyChanged, loadConfig, saveConfig } = useConfig()

interface OllamaModel {
  name: string
  size?: number
  details?: { parameter_size?: string; quantization_level?: string }
}

const providers = [
  { value: 'ollama', label: 'Ollama (local)' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'builtin', label: 'Built-in (llama.cpp)' },
]

// Derive current provider from route prop, fallback to saved config value
const currentProvider = ref(props.provider || 'ollama')

const form = ref({ backend_endpoint: '', backend_api_key: '' })
// Per-provider saved form state so switching tabs preserves edits
const perProviderForm = ref<Record<string, { endpoint: string; apiKey: string }>>({})

const agentsList = ref<Array<{ name: string; model?: string }>>([])
const dirty = ref(false)
const saving = ref(false)
const saveMsg = ref('')
const saveError = ref(false)
const showApiKey = ref(false)
const isApiKeyRedacted = computed(() => form.value.backend_api_key === '[REDACTED]')
const availableModels = ref<OllamaModel[]>([])
const modelsLoading = ref(false)
const ollamaStatus = ref<'unknown' | 'connected' | 'error'>('unknown')
const pullModelName = ref('')
const pulling = ref(false)
const pullMsg = ref('')
const pullError = ref(false)
const deletingModels = ref<Set<string>>(new Set())
const deleteError = ref<string | null>(null)
const deleteConfirm = ref<{ name: string; type: 'ollama' | 'builtin' } | null>(null)

// Built-in (llama.cpp) state
const builtinStatus = ref<BuiltinStatus | null>(null)
const builtinNotConfigured = ref(false)
const builtinCatalog = ref<BuiltinCatalogEntry[]>([])
const builtinInstalled = ref<BuiltinInstalledModel[]>([])
const builtinLoading = ref(false)
const builtinDownloading = ref(false)
const builtinDownloadProgress = ref<{ downloaded: number; total: number } | null>(null)
const builtinDownloadError = ref('')
const builtinPulling = ref<Record<string, boolean>>({})
const builtinPullProgress = ref<Record<string, { downloaded: number; total: number; speed: number }>>({})
const builtinPullError = ref<Record<string, string>>({})
const builtinActivating = ref(false)
const builtinActivateMsg = ref('')
const builtinActivateError = ref(false)
const deletingBuiltin = ref<Set<string>>(new Set())

const providerEndpointPlaceholder = computed(() => {
  switch (currentProvider.value) {
    case 'ollama': return 'http://localhost:11434'
    case 'anthropic': return 'https://api.anthropic.com'
    case 'openai': return 'https://api.openai.com/v1'
    case 'openrouter': return 'https://openrouter.ai/api/v1'
    default: return 'https://...'
  }
})

function formatBuiltinProgress(downloaded: number, total: number): string {
  const toMB = (b: number) => (b / 1e6).toFixed(1)
  if (total > 0) return `${toMB(downloaded)} / ${toMB(total)} MB`
  return `${toMB(downloaded)} MB`
}

async function loadBuiltinData() {
  builtinLoading.value = true
  builtinNotConfigured.value = false
  try {
    const [status, catalog, installed] = await Promise.all([
      api.builtin.status().catch((e: unknown) => {
        if (e instanceof Error && e.message.includes(': 503')) {
          builtinNotConfigured.value = true
        }
        return null
      }),
      api.builtin.catalog().catch(() => [] as BuiltinCatalogEntry[]),
      api.builtin.installedModels().catch(() => [] as BuiltinInstalledModel[]),
    ])
    builtinStatus.value = status
    builtinCatalog.value = catalog
    builtinInstalled.value = installed
  } finally {
    builtinLoading.value = false
  }
}

function startDownloadRuntime() {
  if (builtinDownloading.value) return
  builtinDownloading.value = true
  builtinDownloadProgress.value = null
  builtinDownloadError.value = ''
  api.builtin.downloadRuntimeStream(
    (e) => { builtinDownloadProgress.value = e },
    () => {
      builtinDownloading.value = false
      loadBuiltinData()
    },
    (msg) => {
      builtinDownloading.value = false
      builtinDownloadError.value = msg
    },
  )
}

function startPullModel(name: string) {
  if (builtinPulling.value[name]) return
  builtinPulling.value = { ...builtinPulling.value, [name]: true }
  builtinPullProgress.value = { ...builtinPullProgress.value, [name]: { downloaded: 0, total: 0, speed: 0 } }
  builtinPullError.value = { ...builtinPullError.value, [name]: '' }
  api.builtin.pullModelStream(
    name,
    (e) => { builtinPullProgress.value = { ...builtinPullProgress.value, [name]: e } },
    () => {
      builtinPulling.value = { ...builtinPulling.value, [name]: false }
      loadBuiltinData()
    },
    (msg) => {
      builtinPulling.value = { ...builtinPulling.value, [name]: false }
      builtinPullError.value = { ...builtinPullError.value, [name]: msg }
    },
  )
}

function deleteBuiltinModel(name: string) {
  if (deletingBuiltin.value.has(name)) return
  deleteConfirm.value = { name, type: 'builtin' }
}

async function activateBuiltin(model: string) {
  if (builtinActivating.value) return
  builtinActivating.value = true
  builtinActivateMsg.value = ''
  builtinActivateError.value = false
  try {
    await api.builtin.activate(model)
    builtinActivateMsg.value = `Activated ${model}. Restart Huginn to apply.`
    await loadBuiltinData()
  } catch (e: unknown) {
    builtinActivateMsg.value = e instanceof Error ? e.message : 'Activation failed'
    builtinActivateError.value = true
  } finally {
    builtinActivating.value = false
    setTimeout(() => { builtinActivateMsg.value = '' }, 5000)
  }
}

function selectProvider(value: string) {
  // Save current form state for this provider before switching
  perProviderForm.value[currentProvider.value] = {
    endpoint: form.value.backend_endpoint,
    apiKey: form.value.backend_api_key,
  }
  currentProvider.value = value
  router.replace(`/models/${value}`)
  // Restore saved state for the new provider (or config defaults)
  const saved = perProviderForm.value[value]
  const cfg = config.value?.backend
  form.value.backend_endpoint = saved?.endpoint ?? (cfg?.provider === value ? cfg?.endpoint || '' : '')
  form.value.backend_api_key = saved?.apiKey ?? (cfg?.provider === value ? cfg?.api_key || '' : '')
  dirty.value = false
  showApiKey.value = false
  if (value === 'builtin') {
    loadBuiltinData()
  }
}

async function discardChanges() {
  const cfg = await loadConfig()
  form.value.backend_endpoint = cfg.backend?.endpoint || ''
  form.value.backend_api_key = cfg.backend?.api_key || ''
  dirty.value = false
}

function formatSize(bytes: number): string {
  const gb = bytes / 1e9
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(bytes / 1e6).toFixed(0)} MB`
}

async function loadAvailableModels() {
  modelsLoading.value = true
  try {
    const data = await (api as unknown as { models: { available(): Promise<{ models?: OllamaModel[]; error?: string }> } }).models.available()
    if (data.error) { ollamaStatus.value = 'error'; availableModels.value = [] }
    else { ollamaStatus.value = 'connected'; availableModels.value = data.models ?? [] }
  } catch {
    ollamaStatus.value = 'error'
    availableModels.value = []
  } finally {
    modelsLoading.value = false
  }
}

async function pullModel(name: string) {
  if (!name || pulling.value) return
  pulling.value = true
  pullMsg.value = ''
  pullError.value = false
  try {
    await (api as unknown as { models: { pull(n: string): Promise<{ status: string }> } }).models.pull(name)
    pullMsg.value = `Pulled ${name} successfully`
    pullModelName.value = ''
    await loadAvailableModels()
  } catch (e: unknown) {
    pullMsg.value = e instanceof Error ? e.message : 'Pull failed'
    pullError.value = true
  } finally {
    pulling.value = false
    setTimeout(() => { pullMsg.value = '' }, 5000)
  }
}

async function deleteOllamaModel(name: string) {
  if (deletingModels.value.has(name)) return
  deleteConfirm.value = { name, type: 'ollama' }
}

async function confirmDeleteModel() {
  if (!deleteConfirm.value) return
  const { name, type } = deleteConfirm.value
  deleteConfirm.value = null
  deleteError.value = null
  if (type === 'ollama') {
    deletingModels.value = new Set([...deletingModels.value, name])
    try {
      await (api as unknown as { models: { delete(n: string): Promise<{ deleted: boolean }> } }).models.delete(name)
      await loadAvailableModels()
    } catch (e) {
      deleteError.value = e instanceof Error ? e.message : 'Delete failed'
    } finally {
      const next = new Set(deletingModels.value)
      next.delete(name)
      deletingModels.value = next
    }
  } else {
    deletingBuiltin.value = new Set([...deletingBuiltin.value, name])
    try {
      await api.builtin.delete(name)
      await loadBuiltinData()
    } catch {
      // silently ignore — may already be gone
    } finally {
      const next = new Set(deletingBuiltin.value)
      next.delete(name)
      deletingBuiltin.value = next
    }
  }
}

async function loadAgentsList() {
  try {
    const data = await api.agents.list() as Array<{ name: string; model?: string }>
    agentsList.value = data
  } catch {
    agentsList.value = []
  }
}

function agentsUsingModel(modelName: string): string[] {
  return agentsList.value.filter(a => a.model === modelName).map(a => a.name)
}

async function save() {
  saving.value = true
  saveMsg.value = ''
  saveError.value = false
  try {
    if (!config.value) throw new Error('Config not loaded')
    const updated = {
      ...config.value,
      backend: {
        ...config.value.backend,
        provider: currentProvider.value,
        endpoint: form.value.backend_endpoint,
        api_key: form.value.backend_api_key,
      },
    }
    await saveConfig(updated)
    dirty.value = false
    saveMsg.value = 'Saved'
    setTimeout(() => { saveMsg.value = '' }, 3000)
  } catch (e: unknown) {
    saveMsg.value = e instanceof Error ? e.message : 'Save failed'
    saveError.value = true
  } finally {
    saving.value = false
  }
}

// Sync if route changes externally (e.g. browser back/forward)
watch(() => props.provider, (val) => {
  if (val && val !== currentProvider.value) {
    currentProvider.value = val
  }
})

onMounted(async () => {
  const cfg = await loadConfig()
  // If no provider in URL, default to the saved provider or ollama
  const savedProvider = cfg.backend?.provider || 'ollama'
  if (!props.provider) {
    currentProvider.value = savedProvider
    router.replace(`/models/${currentProvider.value}`)
  }
  form.value.backend_endpoint = cfg.backend?.endpoint || ''
  form.value.backend_api_key = cfg.backend?.api_key || ''
  await loadAvailableModels()
  await loadAgentsList()
  if (currentProvider.value === 'builtin') {
    await loadBuiltinData()
  }
})
</script>

<style scoped>
.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .relative, .modal-fade-leave-to .relative { transform: scale(0.96) translateY(6px); }
</style>
