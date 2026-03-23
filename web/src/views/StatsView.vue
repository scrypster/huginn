<template>
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-2 border-b border-huginn-border bg-huginn-surface flex-shrink-0">
      <span class="text-huginn-blue text-sm font-bold">stats</span>
      <div class="flex items-center gap-3">
        <span v-if="lastRefreshed" class="text-huginn-muted/50 text-[11px]">refreshed {{ lastRefreshed }}</span>
        <!-- Auto-refresh toggle -->
        <button @click="toggleAutoRefresh"
          class="flex items-center gap-1.5 text-xs transition-colors"
          :class="autoRefresh ? 'text-huginn-green' : 'text-huginn-muted hover:text-huginn-text'"
          :title="autoRefresh ? 'Auto-refresh on (every 10s) — click to disable' : 'Enable auto-refresh'"
        >
          <svg class="w-3 h-3" :class="autoRefresh ? 'animate-spin' : ''" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M23 4v6h-6"/><path d="M1 20v-6h6"/>
            <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
          </svg>
          {{ autoRefresh ? 'live' : 'auto' }}
        </button>
        <button @click="fetchAll" class="text-huginn-muted text-xs hover:text-huginn-blue">refresh</button>
      </div>
    </div>

    <div class="flex-1 overflow-y-auto p-4">
      <div v-if="loading" class="text-huginn-muted text-sm">Loading stats...</div>
      <div v-else-if="error" class="text-huginn-red text-sm">{{ error }}</div>
      <div v-else class="space-y-6">

        <!-- Section 1: Overview -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Overview</h2>
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Total Sessions</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ totalSessions }}</p>
            </div>
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Active Sessions</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ activeSessions }}</p>
            </div>
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Total Messages</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ totalMessages.toLocaleString() }}</p>
            </div>
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Session Cost</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ formattedCost }}</p>
            </div>
          </div>
        </section>

        <!-- Section 2: Last LLM Call -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Last LLM Call</h2>
          <div class="grid grid-cols-2 gap-3">
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Prompt Tokens</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ (statsData.last_prompt_tokens ?? 0).toLocaleString() }}</p>
            </div>
            <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
              <p class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-1">Completion Tokens</p>
              <p class="text-2xl font-semibold text-huginn-text">{{ (statsData.last_completion_tokens ?? 0).toLocaleString() }}</p>
            </div>
          </div>
        </section>

        <!-- Section 3: Top Agents -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Top Agents</h2>
          <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-3 space-y-2">
            <div v-if="topAgents.length === 0" class="text-huginn-muted text-xs">No session data.</div>
            <div
              v-for="item in topAgents"
              :key="item.name"
              class="flex items-center gap-3"
            >
              <span class="text-xs text-huginn-text font-mono w-40 truncate flex-shrink-0">{{ item.name || '(unknown)' }}</span>
              <div class="flex-1 h-1.5 bg-huginn-border/40 rounded-full overflow-hidden">
                <div
                  class="h-full bg-huginn-blue/50 rounded-full"
                  :style="{ width: barWidth(item.count, topAgents) }"
                />
              </div>
              <span class="text-[11px] text-huginn-muted w-6 text-right flex-shrink-0">{{ item.count }}</span>
            </div>
          </div>
        </section>

        <!-- Section 4: Top Models -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Top Models</h2>
          <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-3 space-y-2">
            <div v-if="topModels.length === 0" class="text-huginn-muted text-xs">No session data.</div>
            <div
              v-for="item in topModels"
              :key="item.name"
              class="flex items-center gap-3"
            >
              <span class="text-xs text-huginn-text font-mono w-40 truncate flex-shrink-0">{{ item.name || '(unknown)' }}</span>
              <div class="flex-1 h-1.5 bg-huginn-border/40 rounded-full overflow-hidden">
                <div
                  class="h-full bg-huginn-blue/40 rounded-full"
                  :style="{ width: barWidth(item.count, topModels) }"
                />
              </div>
              <span class="text-[11px] text-huginn-muted w-6 text-right flex-shrink-0">{{ item.count }}</span>
            </div>
          </div>
        </section>

        <!-- Section 5: Cost History (24h) -->
        <section v-if="costHistory.length > 0">
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Cost History (24h)</h2>
          <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-3 space-y-1.5">
            <div
              v-for="row in costHistory.slice(0, 10)"
              :key="row.ts"
              class="flex items-center gap-3 text-xs"
            >
              <span class="text-huginn-muted font-mono w-16 flex-shrink-0 text-[10px]">{{ new Date(row.ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }}</span>
              <span class="text-huginn-muted truncate flex-1 text-[10px]">{{ row.session_id }}</span>
              <span class="text-huginn-text font-mono flex-shrink-0">${{ row.cost_usd.toFixed(4) }}</span>
              <span class="text-huginn-muted text-[10px] flex-shrink-0">{{ (row.prompt_tokens + row.completion_tokens).toLocaleString() }} tok</span>
            </div>
            <div v-if="costHistory.length > 10" class="text-huginn-muted text-[10px] pt-1">
              + {{ costHistory.length - 10 }} more
            </div>
          </div>
        </section>

        <!-- Section 6: Server -->
        <section>
          <h2 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest mb-3">Server</h2>
          <div class="rounded-xl border border-huginn-border bg-huginn-surface/50 px-4 py-4">
            <div v-if="healthData" class="flex items-center gap-4 text-xs flex-wrap">
              <div class="flex items-center gap-1.5">
                <div
                  class="w-2 h-2 rounded-full"
                  :class="healthData.status === 'ok' ? 'bg-huginn-green' : 'bg-huginn-red'"
                />
                <span class="text-huginn-text">{{ healthData.status }}</span>
              </div>
              <span class="text-huginn-muted">v{{ healthData.version }}</span>
              <span v-if="healthData.backend_status && healthData.backend_status !== 'unknown'"
                class="px-2 py-0.5 rounded-full text-[10px] border"
                :class="cbStatusClass(healthData.backend_status)">
                Backend: {{ healthData.backend_status }}
              </span>
            </div>
            <div v-else class="text-huginn-muted text-xs">Unavailable</div>
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { api } from '../composables/useApi'

interface SessionManifest {
  session_id?: string
  title?: string
  agent?: string
  model?: string
  message_count?: number
  created_at?: string
  updated_at?: string
  status?: string
}

interface RankedItem {
  name: string
  count: number
}

const statsData = ref<Record<string, number>>({})
const costData = ref<{ session_total_usd: number } | null>(null)
const healthData = ref<{ status: string; version: string; satellite_connected: boolean; backend_status: string } | null>(null)
const sessionsData = ref<SessionManifest[]>([])
const costHistory = ref<Array<{ ts: number; session_id: string; cost_usd: number; prompt_tokens: number; completion_tokens: number }>>([])
const loading = ref(true)
const error = ref('')
const lastRefreshed = ref('')

const totalSessions = computed(() => sessionsData.value.length)

const activeSessions = computed(() =>
  sessionsData.value.filter((s) => s.status === 'active').length
)

const totalMessages = computed(() =>
  sessionsData.value.reduce((sum, s) => sum + (s.message_count ?? 0), 0)
)

const formattedCost = computed(() => {
  const val = costData.value?.session_total_usd
  if (!val) return '—'
  return `$${val.toFixed(4)}`
})

function groupBy(field: keyof SessionManifest): RankedItem[] {
  const counts: Record<string, number> = {}
  for (const s of sessionsData.value) {
    const key = String(s[field] ?? '')
    counts[key] = (counts[key] ?? 0) + 1
  }
  return Object.entries(counts)
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5)
}

const topAgents = computed(() => groupBy('agent'))
const topModels = computed(() => groupBy('model'))

function barWidth(count: number, items: RankedItem[]): string {
  const max = items[0]?.count ?? 1
  return `${(count / max) * 100}%`
}

function cbStatusClass(state: string): string {
  switch (state) {
    case 'closed': return 'border-huginn-green/40 text-huginn-green'
    case 'open': return 'border-huginn-red/40 text-huginn-red'
    case 'half-open': return 'border-huginn-amber/40 text-huginn-amber'
    default: return 'border-huginn-border text-huginn-muted'
  }
}

async function fetchAll() {
  error.value = ''  // clear at start so stale errors don't persist into a fresh attempt
  try {
    const since24h = Math.floor(Date.now() / 1000) - 86400
    const [s, c, h, sess, hist] = await Promise.all([
      api.stats().catch(() => ({} as Record<string, number>)),
      api.cost().catch(() => null),
      api.health().catch(() => null),
      api.sessions.list().catch(() => [] as Array<Record<string, unknown>>),
      api.statsHistory(since24h).catch(() => null),
    ])
    statsData.value = s
    costData.value = c
    healthData.value = h
    sessionsData.value = (sess as unknown) as SessionManifest[]
    if (hist?.cost) {
      costHistory.value = [...hist.cost].sort((a, b) => b.ts - a.ts)
    }
    // Detect all-fallback state: if health + cost are both null and stats is empty,
    // the server is likely unreachable. Show a warning instead of a blank dashboard.
    if (!h && !c && Object.keys(s).length === 0) {
      error.value = 'Unable to reach server — stats may be stale'
    }
    const now = new Date()
    lastRefreshed.value = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load stats'
  } finally {
    loading.value = false
  }
}

// ── Auto-refresh ────────────────────────────────────────────────────
const autoRefresh = ref(false)
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    autoRefreshTimer = setInterval(fetchAll, 10_000)
  } else {
    if (autoRefreshTimer) clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

onMounted(fetchAll)
onUnmounted(() => {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer)
})
</script>
