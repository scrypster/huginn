<!-- web/src/views/MemoryView.vue -->
<template>
  <div class="h-full flex flex-col bg-[var(--color-bg)]">
    <!-- Header -->
    <div class="flex-shrink-0 px-4 pt-4 pb-3 border-b border-[var(--color-border)]">
      <h1 class="text-sm font-semibold text-[var(--color-text)]">Memory</h1>
      <p class="text-xs text-[var(--color-text-muted)] mt-0.5">Browse and search agent vault memories</p>
    </div>

    <!-- Error banner -->
    <div v-if="error" class="flex-shrink-0 mx-4 mt-3 px-3 py-2 rounded bg-red-500/10 text-red-400 text-xs">
      {{ error }}
    </div>

    <!-- No MuninnDB configured -->
    <div v-if="!connected && !loading" class="flex-1 flex items-center justify-center">
      <div class="text-center text-[var(--color-text-muted)]">
        <p class="text-sm">MuninnDB not connected</p>
        <p class="text-xs mt-1">Configure in <router-link to="/connections" class="underline">Connections</router-link></p>
      </div>
    </div>

    <template v-else-if="connected">
      <!-- Vault selector + search row -->
      <div class="flex-shrink-0 px-4 pt-3 pb-2 flex gap-2 items-center">
        <select
          v-model="selectedVault"
          @change="onVaultChange"
          class="flex-shrink-0 text-xs rounded px-2 py-1.5 bg-[var(--color-input-bg)] border border-[var(--color-border)] text-[var(--color-text)] max-w-[180px]"
        >
          <option value="">Select vault…</option>
          <option v-for="v in vaults" :key="v" :value="v">{{ v }}</option>
        </select>
        <input
          v-model="searchQuery"
          @keydown.enter="onSearch"
          placeholder="Search memories…"
          class="flex-1 text-xs rounded px-2 py-1.5 bg-[var(--color-input-bg)] border border-[var(--color-border)] text-[var(--color-text)] placeholder:text-[var(--color-text-muted)]"
        />
        <button
          @click="onSearch"
          :disabled="!selectedVault || searchLoading"
          class="flex-shrink-0 text-xs px-3 py-1.5 rounded bg-[var(--color-accent)] text-white disabled:opacity-40"
        >
          {{ searchLoading ? '…' : 'Search' }}
        </button>
      </div>

      <!-- Two-panel layout: list + detail -->
      <div class="flex flex-1 overflow-hidden divide-x divide-[var(--color-border)]">

        <!-- Memory list -->
        <div class="w-72 flex-shrink-0 overflow-y-auto">
          <div v-if="searchLoading" class="p-4 text-xs text-[var(--color-text-muted)]">Searching…</div>
          <div v-else-if="memories.length === 0 && searched" class="p-4 text-xs text-[var(--color-text-muted)]">No memories found.</div>
          <div v-else-if="memories.length === 0" class="p-4 text-xs text-[var(--color-text-muted)]">Enter a search term and press Search.</div>
          <button
            v-for="mem in memories"
            :key="mem.id"
            @click="selectMemory(mem)"
            class="w-full text-left px-4 py-3 border-b border-[var(--color-border)] hover:bg-[var(--color-hover)]"
            :class="{ 'bg-[var(--color-selected)]': selectedMemory?.id === mem.id }"
          >
            <p class="text-xs font-medium text-[var(--color-text)] truncate">{{ mem.concept || mem.id }}</p>
            <p class="text-xs text-[var(--color-text-muted)] mt-0.5 line-clamp-2">{{ mem.content }}</p>
          </button>
        </div>

        <!-- Memory detail -->
        <div class="flex-1 overflow-y-auto p-4">
          <div v-if="!selectedMemory" class="h-full flex items-center justify-center text-xs text-[var(--color-text-muted)]">
            Select a memory to view details
          </div>
          <template v-else>
            <div class="flex items-start justify-between mb-4">
              <h2 class="text-sm font-semibold text-[var(--color-text)]">{{ selectedMemory.concept }}</h2>
              <button
                @click="forgetMemory"
                :disabled="forgetLoading"
                class="text-xs px-2 py-1 rounded bg-red-500/10 text-red-400 hover:bg-red-500/20 disabled:opacity-40"
              >
                {{ forgetLoading ? 'Forgetting…' : 'Forget' }}
              </button>
            </div>
            <div class="text-xs text-[var(--color-text)] whitespace-pre-wrap leading-relaxed">{{ selectedMemory.content }}</div>
            <div v-if="selectedMemory.entities?.length" class="mt-4">
              <p class="text-xs font-medium text-[var(--color-text-muted)] mb-1">Entities</p>
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="e in selectedMemory.entities"
                  :key="e"
                  class="text-xs px-2 py-0.5 rounded-full bg-[var(--color-tag-bg)] text-[var(--color-tag-text)]"
                >{{ e }}</span>
              </div>
            </div>
            <div v-if="selectedMemory.decay_score !== undefined" class="mt-3 text-xs text-[var(--color-text-muted)]">
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
  content?: string | unknown
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
