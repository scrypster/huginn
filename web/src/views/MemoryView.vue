<!-- web/src/views/MemoryView.vue -->
<template>
  <div class="h-full flex flex-col bg-huginn-bg">
    <!-- Header -->
    <div class="flex-shrink-0 px-4 pt-4 pb-3 border-b border-huginn-border">
      <h1 class="text-sm font-semibold text-huginn-text">Memory</h1>
      <p class="text-xs text-huginn-muted mt-0.5">Browse and search agent vault memories</p>
    </div>

    <!-- Error banner -->
    <div v-if="error" class="flex-shrink-0 mx-4 mt-3 px-3 py-2 rounded bg-huginn-red/10 text-huginn-red text-xs">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex-1 flex items-center justify-center">
      <div class="w-4 h-4 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
    </div>

    <!-- No MuninnDB configured — purposeful empty state -->
    <div v-else-if="!connected" data-testid="memory-empty-no-vault" class="flex-1 flex flex-col items-center justify-center gap-4 pb-16 px-6">
      <div class="w-14 h-14 rounded-2xl flex items-center justify-center select-none"
        style="background:rgba(88,166,255,0.08);border:1px solid rgba(88,166,255,0.2)">
        <svg class="w-7 h-7 text-huginn-blue opacity-60" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 2a3 3 0 0 0-3 3v1a4 4 0 0 0-3 3.87V13a4 4 0 0 0 2 3.46V19a3 3 0 0 0 3 3h2a3 3 0 0 0 3-3v-2.54A4 4 0 0 0 18 13v-3.13A4 4 0 0 0 15 6V5a3 3 0 0 0-3-3z"/>
        </svg>
      </div>
      <div class="text-center space-y-1 max-w-xs">
        <p class="text-huginn-text text-sm font-medium">No memory vault connected</p>
        <p class="text-huginn-muted text-xs">This is where agents' long-term memories live — facts, preferences, and context they recall across conversations.</p>
      </div>
      <router-link to="/connections"
        class="flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium text-huginn-blue border border-huginn-blue/30 hover:bg-huginn-blue/10 transition-all duration-150 active:scale-95">
        Connect MuninnDB
      </router-link>
    </div>

    <template v-else>
      <!-- Vault selector + search row -->
      <div class="flex-shrink-0 px-4 pt-3 pb-2 flex gap-2 items-center">
        <select
          v-model="selectedVault"
          @change="onVaultChange"
          class="flex-shrink-0 text-xs rounded px-2 py-1.5 bg-huginn-surface border border-huginn-border text-huginn-text max-w-[180px]"
        >
          <option value="">Select vault…</option>
          <option v-for="v in vaults" :key="v" :value="v">{{ v }}</option>
        </select>
        <input
          v-model="searchQuery"
          @keydown.enter="onSearch"
          placeholder="Search memories…"
          class="flex-1 text-xs rounded px-2 py-1.5 bg-huginn-surface border border-huginn-border text-huginn-text placeholder-huginn-muted/50"
        />
        <button
          @click="onSearch"
          :disabled="!selectedVault || searchLoading"
          class="flex-shrink-0 text-xs px-3 py-1.5 rounded bg-huginn-blue text-white disabled:opacity-40"
        >
          {{ searchLoading ? '…' : 'Search' }}
        </button>
      </div>

      <!-- Two-panel layout: list + detail -->
      <div class="flex flex-1 overflow-hidden divide-x divide-huginn-border">

        <!-- Memory list -->
        <div class="w-72 flex-shrink-0 overflow-y-auto">
          <div v-if="searchLoading" class="p-4 text-xs text-huginn-muted">Searching…</div>
          <!-- Brand-new vault: nothing has ever been searched yet -->
          <div v-else-if="memories.length === 0 && !searched && !selectedVault"
            data-testid="memory-empty-no-selection"
            class="flex flex-col items-center justify-center h-32 gap-2 px-4 text-center opacity-60">
            <svg class="w-6 h-6 text-huginn-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
              <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
            </svg>
            <p class="text-xs text-huginn-muted">Pick a vault, then search to see what's remembered.</p>
          </div>
          <div v-else-if="memories.length === 0 && searched" class="p-4 text-xs text-huginn-muted">No memories found.</div>
          <div v-else-if="memories.length === 0" class="p-4 text-xs text-huginn-muted">Enter a search term and press Search.</div>
          <button
            v-for="mem in memories"
            :key="mem.id"
            @click="selectMemory(mem)"
            class="w-full text-left px-4 py-3 border-b border-huginn-border hover:bg-huginn-surface transition-colors"
            :class="{ 'bg-huginn-blue/10': selectedMemory?.id === mem.id }"
          >
            <p class="text-xs font-medium text-huginn-text truncate">{{ mem.concept || mem.id }}</p>
            <p class="text-xs text-huginn-muted mt-0.5 line-clamp-2">{{ mem.content }}</p>
          </button>
        </div>

        <!-- Memory detail -->
        <div class="flex-1 overflow-y-auto p-4">
          <div v-if="!selectedMemory" class="h-full flex items-center justify-center text-xs text-huginn-muted">
            Select a memory to view details
          </div>
          <template v-else>
            <div class="flex items-start justify-between mb-4">
              <h2 class="text-sm font-semibold text-huginn-text">{{ selectedMemory.concept }}</h2>
              <button
                @click="forgetMemory"
                :disabled="forgetLoading"
                class="text-xs px-2 py-1 rounded bg-huginn-red/10 text-huginn-red hover:bg-huginn-red/20 disabled:opacity-40"
              >
                {{ forgetLoading ? 'Forgetting…' : 'Forget' }}
              </button>
            </div>
            <div class="text-xs text-huginn-text whitespace-pre-wrap leading-relaxed">{{ selectedMemory.content }}</div>
            <div v-if="selectedMemory.entities?.length" class="mt-4">
              <p class="text-xs font-medium text-huginn-muted mb-1">Entities</p>
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="e in selectedMemory.entities"
                  :key="e"
                  class="text-xs px-2 py-0.5 rounded-full"
                  style="background:rgba(88,166,255,0.12);color:rgba(88,166,255,0.9)"
                >{{ e }}</span>
              </div>
            </div>
            <div v-if="selectedMemory.decay_score !== undefined" class="mt-3 text-xs text-huginn-muted">
              Decay score: {{ (selectedMemory.decay_score * 100).toFixed(0) }}%
            </div>
          </template>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, apiFetch } from '../composables/useApi'

interface Memory {
  id: string
  concept: string
  content: string
  entities?: string[]
  decay_score?: number
}

interface MuninnToolResponse {
  result?: {
    memories?: RawMemory[]
    content?: RawMemory[]
    [key: string]: unknown
  }
}

interface RawMemory {
  id?: string
  memory_id?: string
  concept?: string
  name?: string
  content?: unknown
  entities?: string[]
  decay_score?: number
  [key: string]: unknown
}

const loading = ref(true)
const connected = ref(false)
const error = ref('')
const vaults = ref<string[]>([])
const selectedVault = ref('')
const searchQuery = ref('')
const searchLoading = ref(false)
const memories = ref<Memory[]>([])
const searched = ref(false)
const selectedMemory = ref<Memory | null>(null)
const forgetLoading = ref(false)

onMounted(async () => {
  try {
    const status = await api.muninn.status()
    connected.value = status.connected
    if (status.connected) {
      const res = await api.muninn.vaults()
      vaults.value = res.vaults ?? []
      if (vaults.value.length > 0) {
        selectedVault.value = vaults.value[0] ?? ''
      }
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load vaults'
  } finally {
    loading.value = false
  }
})

function onVaultChange() {
  memories.value = []
  selectedMemory.value = null
  searched.value = false
}

async function onSearch() {
  if (!selectedVault.value) return
  searchLoading.value = true
  searched.value = true
  selectedMemory.value = null
  error.value = ''
  try {
    const res = await apiFetch<MuninnToolResponse>('/api/v1/muninn/tool', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        vault: selectedVault.value,
        tool: 'muninn_recall',
        args: { context: searchQuery.value || 'recent memories', limit: 30 },
      }),
    })
    const raw = res.result
    const list: RawMemory[] = raw?.memories ?? raw?.content ?? (Array.isArray(raw) ? raw : [])
    memories.value = list.map((item) => ({
      id: String(item.id ?? item.memory_id ?? Math.random()),
      concept: String(item.concept ?? item.name ?? ''),
      content: typeof item.content === 'string' ? item.content : JSON.stringify(item.content),
      entities: item.entities ?? [],
      decay_score: item.decay_score,
    }))
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Search failed'
  } finally {
    searchLoading.value = false
  }
}

function selectMemory(mem: Memory) {
  selectedMemory.value = mem
}

async function forgetMemory() {
  if (!selectedMemory.value || !selectedVault.value) return
  if (!window.confirm(`Forget "${selectedMemory.value.concept || selectedMemory.value.id}"? This cannot be undone.`)) return
  const toForget = selectedMemory.value
  forgetLoading.value = true
  error.value = ''
  try {
    await apiFetch('/api/v1/muninn/tool', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        vault: selectedVault.value,
        tool: 'muninn_forget',
        args: { id: toForget.id },
      }),
    })
    memories.value = memories.value.filter(m => m.id !== toForget.id)
    selectedMemory.value = null
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Forget failed'
  } finally {
    forgetLoading.value = false
  }
}
</script>
