<template>
  <div class="flex h-full bg-huginn-bg overflow-hidden">

    <!-- ── Left sidebar: category nav (220px) ──────────────────────────────── -->
    <div class="w-[220px] flex-shrink-0 border-r border-huginn-border flex flex-col">
      <!-- Sidebar header -->
      <div class="px-4 py-4 border-b border-huginn-border flex-shrink-0">
        <h1 class="text-huginn-text font-semibold text-sm">Connections</h1>
        <p class="text-[10px] text-huginn-muted mt-0.5">
          {{ connectedCount }} connected
        </p>
      </div>
      <!-- Category nav -->
      <CategoryNav
        :category="activeCategory"
        :connections="hydratedCatalog"
        :loading="catalogLoading"
        @update:category="activeCategory = $event"
        class="flex-1"
      />
    </div>

    <!-- ── Main content area ──────────────────────────────────────────────── -->
    <div class="flex-1 flex flex-col min-w-0 overflow-hidden">

      <!-- Header: search + status -->
      <div class="flex items-center gap-3 px-6 py-3 border-b border-huginn-border flex-shrink-0">
        <div class="flex-1 relative">
          <svg class="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
            <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
          </svg>
          <input
            v-model="search"
            placeholder="Search connections…"
            class="w-full bg-huginn-surface border border-huginn-border rounded-lg pl-9 pr-3 py-1.5 text-xs text-huginn-text placeholder-huginn-muted/60 focus:outline-none focus:border-huginn-blue/50 transition-colors"
          />
        </div>
        <button @click="refresh" class="text-huginn-muted hover:text-huginn-text transition-colors p-1 rounded">
          <svg class="w-3.5 h-3.5" :class="loading ? 'animate-spin' : ''" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
            <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/>
            <path d="M3 3v5h5"/>
            <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16"/>
            <path d="M16 16h5v5"/>
          </svg>
        </button>
      </div>

      <!-- Error banner -->
      <div v-if="error || catalogError" class="mx-6 mt-3 px-4 py-2.5 rounded-xl border border-huginn-red/40 text-huginn-red text-xs bg-huginn-red/8 flex-shrink-0">
        {{ error || catalogError }}
      </div>

      <!-- ── My Connections: list view ───────────────────────────────────── -->
      <div v-if="activeCategory === 'my_connections'" class="flex-1 overflow-y-auto px-6 py-5">
        <div v-if="connectedItems.length === 0" class="text-huginn-muted text-xs mt-8 text-center">
          No connections yet — browse the catalog to add one.
        </div>
        <div v-else class="flex flex-col gap-2 max-w-xl">
          <div
            v-for="conn in connectedItems"
            :key="conn.id"
            class="flex items-start gap-3 px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50"
          >
            <!-- Icon -->
            <div
              class="w-7 h-7 rounded-lg flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0 mt-0.5"
              :style="{ backgroundColor: conn.iconColor }"
            >{{ conn.icon }}</div>

            <!-- Name + accounts -->
            <div class="flex-1 min-w-0">
              <div class="flex items-center justify-between">
                <span class="text-xs font-medium text-huginn-text">{{ conn.name }}</span>
                <button
                  v-if="conn.multiAccount"
                  @click="handleConnect(conn)"
                  class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0 ml-3"
                >+ Add Account</button>
              </div>

              <!-- Per-connection refresh error banner -->
              <div
                v-if="conn.type === 'oauth' && conn.state?.accounts?.some(a => refreshErrors[a.id])"
                class="mt-1.5 px-2 py-1 rounded-lg border border-huginn-red/40 text-huginn-red text-[10px] bg-huginn-red/8 flex items-center gap-2"
              >
                <span class="flex-1">Token refresh failed — re-authorize to restore access.</span>
                <button
                  v-for="acct in conn.state.accounts.filter(a => refreshErrors[a.id])"
                  :key="acct.id"
                  @click="delete refreshErrors[acct.id]"
                  class="text-huginn-red/60 hover:text-huginn-red transition-colors flex-shrink-0"
                >×</button>
              </div>

              <!-- Account rows (OAuth and system tools with multiple accounts) -->
              <div v-if="conn.state?.accounts?.length" class="flex flex-col gap-0.5 mt-1">
                <div
                  v-for="(acct, idx) in conn.state.accounts"
                  :key="acct.id"
                  class="flex items-center gap-1.5"
                >
                  <div class="w-1.5 h-1.5 rounded-full flex-shrink-0"
                    :class="refreshErrors[acct.id] ? 'bg-huginn-red' : 'bg-huginn-green'"
                  />
                  <span class="text-[10px] text-huginn-muted truncate flex-1">{{ acct.label }}</span>

                  <!-- Expiry badge (OAuth tokens only) -->
                  <template v-if="conn.type === 'oauth'">
                    <span
                      v-if="expiryBadge(oauthConnections.find(c => c.id === acct.id)?.expires_at ?? '')"
                      class="text-[9px] px-1 py-0.5 rounded border flex-shrink-0"
                      :class="expiryBadge(oauthConnections.find(c => c.id === acct.id)?.expires_at ?? '')!.cls"
                    >{{ expiryBadge(oauthConnections.find(c => c.id === acct.id)?.expires_at ?? '')!.label }}</span>
                  </template>

                  <!-- Default badge -->
                  <span
                    v-if="(conn.type === 'oauth' && idx === 0 && (conn.state.accounts?.length ?? 0) > 1) ||
                          (conn.type === 'system' && acct.label === conn.state.identity)"
                    class="text-[9px] px-1 py-0.5 rounded border border-huginn-border text-huginn-muted">
                    default
                  </span>

                  <!-- Set Default (system tools, non-active accounts) -->
                  <button
                    v-if="conn.type === 'system' && acct.label !== conn.state.identity"
                    @click="handleSetDefault(conn, acct.id)"
                    class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0"
                    :title="`Set ${acct.label} as default`"
                  >Set Default</button>

                  <!-- Set Default (OAuth, non-first accounts) -->
                  <button
                    v-else-if="conn.type === 'oauth' && idx > 0"
                    @click="handleSetDefault(conn, acct.id)"
                    class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors flex-shrink-0"
                    :title="`Make ${acct.label} the default`"
                  >Set Default</button>

                  <!-- Disconnect (OAuth only) -->
                  <button
                    v-if="conn.type === 'oauth'"
                    @click="handleDisconnect(acct.id)"
                    class="text-[10px] text-huginn-muted hover:text-huginn-red transition-colors flex-shrink-0"
                    :title="`Disconnect ${acct.label}`"
                  >×</button>
                </div>
              </div>

              <!-- System tools / fallback (AWS, gcloud, GitHub CLI).
                   These are CLI-managed tools with no API delete endpoint — no disconnect button is intentional. -->
              <div v-else class="flex items-center gap-1.5 mt-1">
                <div class="w-1.5 h-1.5 rounded-full bg-huginn-green flex-shrink-0" />
                <span class="text-[10px] text-huginn-muted truncate flex-1">{{ conn.state?.identity || 'Connected' }}</span>
                <template v-if="conn.state?.profiles?.length">
                  <span v-for="p in conn.state.profiles" :key="p"
                    class="text-[9px] px-1 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ p }}</span>
                </template>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Catalog grid view ───────────────────────────────────────────── -->
      <div v-else class="flex-1 overflow-y-auto px-6 py-5">

        <!-- Category title -->
        <div v-if="activeCategory !== 'all'" class="text-[10px] text-huginn-muted font-medium tracking-widest uppercase mb-4">
          {{ CATEGORY_LABELS[activeCategory] }}
        </div>

        <!-- Waiting for OAuth -->
        <div v-if="waitingFor" class="mb-4 flex items-center gap-3 px-4 py-3 rounded-xl border border-huginn-yellow/40 text-huginn-yellow text-xs" style="background:rgba(210,153,34,0.07)">
          <svg class="w-3.5 h-3.5 animate-spin flex-shrink-0" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/>
          </svg>
          Waiting for {{ waitingFor }} authorization…
          <button @click="cancelWait" class="ml-auto text-huginn-yellow/60 hover:text-huginn-yellow transition-colors">Cancel</button>
        </div>

        <!-- Skeleton loader -->
        <div v-if="catalogLoading" class="grid grid-cols-3 gap-3">
          <div
            v-for="n in 12"
            :key="n"
            class="flex flex-col rounded-xl border border-huginn-border bg-huginn-surface/50 overflow-hidden animate-pulse"
          >
            <div class="flex items-start gap-3 px-4 pt-4 pb-3">
              <div class="w-8 h-8 rounded-lg bg-huginn-border flex-shrink-0" />
              <div class="flex-1 min-w-0 space-y-2 pt-0.5">
                <div class="h-2.5 w-2/5 rounded bg-huginn-border" />
                <div class="h-2 w-full rounded bg-huginn-border/60" />
                <div class="h-2 w-3/4 rounded bg-huginn-border/40" />
              </div>
            </div>
            <div class="px-4 pb-3 flex items-center justify-between mt-auto">
              <div class="h-2 w-1/4 rounded bg-huginn-border/50" />
              <div class="h-2 w-1/5 rounded bg-huginn-border/50" />
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-else-if="filteredCatalog.length === 0" class="text-huginn-muted text-xs mt-8 text-center">
          No connections match "{{ search }}"
        </div>

        <!-- Card grid -->
        <div v-else class="grid grid-cols-3 gap-3">
          <ConnectionCard
            v-for="conn in filteredCatalog"
            :key="conn.id"
            :conn="conn"
            @connect="handleConnect(conn)"
            @disconnect="handleDisconnect($event)"
            @setDefault="handleSetDefault(conn, $event)"
          />
        </div>
      </div>

    </div>

    <!-- Credential connect modal -->
    <CredentialModal
      :provider="activeModal"
      @close="activeModal = null"
      @connected="onModalConnected"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, inject } from 'vue'
import type { Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Connection, type SystemToolStatus } from '../composables/useApi'
import type { HuginnWS, WSMessage } from '../composables/useHuginnWS'
import {
  CATEGORY_LABELS,
  hydrateOAuth,
  hydrateSystem,
  hydrateCredentials,
  type CatalogEntry,
  type CatalogConnection,
  type ConnectionCategory,
} from '../composables/useConnectionsCatalog'
import { fetchCredentialCatalog, type CredentialCatalogEntry } from '../composables/useCredentialCatalog'
import CategoryNav from '../components/connections/CategoryNav.vue'
import ConnectionCard from '../components/connections/ConnectionCard.vue'
import CredentialModal from '../components/connections/CredentialModal.vue'

const route  = useRoute()
const router = useRouter()

const VALID_CATEGORIES = new Set<ConnectionCategory>([
  'all', 'my_connections', 'communication', 'dev_tools',
  'cloud', 'productivity', 'databases', 'observability', 'system',
])

// ── State ─────────────────────────────────────────────────────────────────────

const activeCategory = ref<ConnectionCategory>('all')
const search         = ref('')
const loading            = ref(false)
const catalogLoading     = ref(true)
const error              = ref('')
const catalogError       = ref('')
const waitingFor         = ref<string | null>(null)
const pendingDisconnect  = ref<string | null>(null)
const activeModal = ref<string | null>(null)

// Raw API data
const oauthConnections     = ref<Connection[]>([])
const systemTools          = ref<SystemToolStatus[]>([])
const muninnConnected      = ref(false)
const muninnIdentity       = ref('')
const serverCatalogEntries = ref<CredentialCatalogEntry[]>([])

// WS inject for token refresh events
const wsRef = inject<Ref<HuginnWS | null>>('ws')

// Per-connection refresh failure messages (keyed by connection_id). Dismissed per-item.
const refreshErrors = ref<Record<string, string>>({})

// ── Helpers ───────────────────────────────────────────────────────────────────

function expiryBadge(expiresAt: string): { label: string; cls: string } | null {
  if (!expiresAt) return null
  const d = new Date(expiresAt)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return null
  const msLeft = d.getTime() - Date.now()
  if (msLeft <= 0) return { label: 'expired', cls: 'border-huginn-red/40 text-huginn-red' }
  const hrs = msLeft / 3_600_000
  if (hrs < 1) return { label: `exp ${Math.ceil(msLeft / 60000)}m`, cls: 'border-huginn-red/40 text-huginn-red' }
  if (hrs < 24) return { label: `exp ${Math.ceil(hrs)}h`, cls: 'border-huginn-amber/40 text-huginn-amber' }
  const days = Math.ceil(hrs / 24)
  if (days <= 7) return { label: `exp ${days}d`, cls: 'border-huginn-yellow/40 text-huginn-yellow' }
  return { label: `exp ${days}d`, cls: 'border-huginn-green/40 text-huginn-green' }
}

// OAuth polling
let pollInterval: ReturnType<typeof setInterval> | null = null
let pollTimeout:  ReturnType<typeof setTimeout>  | null = null
let snapshotBefore = new Set<string>()

// ── Computed ──────────────────────────────────────────────────────────────────

const hydratedCatalog = computed<CatalogConnection[]>(() =>
  serverCatalogEntries.value.map(e => {
    const entry: CatalogEntry = {
      id:           e.id,
      name:         e.name,
      description:  e.description,
      category:     e.category as ConnectionCategory,
      icon:         e.icon,
      iconColor:    e.icon_color,
      type:         e.type as CatalogEntry['type'],
      multiAccount: e.multi_account,
    }
    if (entry.type === 'coming_soon') {
      return { ...entry, state: null }
    }
    if (entry.id === 'muninn') {
      return { ...entry, state: { connected: muninnConnected.value, identity: muninnIdentity.value || undefined } }
    }
    if (entry.type === 'oauth') {
      return { ...entry, state: hydrateOAuth(entry, oauthConnections.value) }
    }
    if (entry.type === 'system') {
      return { ...entry, state: hydrateSystem(entry, systemTools.value) }
    }
    if (entry.type === 'credentials' || entry.type === 'database') {
      return { ...entry, state: hydrateCredentials(entry, oauthConnections.value) }
    }
    return { ...entry, state: null }
  })
)

const connectedCount = computed(() =>
  hydratedCatalog.value.filter(c => c.state?.connected).length
)

const connectedItems = computed(() =>
  hydratedCatalog.value.filter(c => c.state?.connected && c.type !== 'coming_soon')
)

const filteredCatalog = computed(() => {
  let list = hydratedCatalog.value

  // Category filter (skip for special views)
  if (activeCategory.value !== 'all' && activeCategory.value !== 'my_connections') {
    list = list.filter(c => c.category === activeCategory.value)
  }

  // Search filter
  const q = search.value.trim().toLowerCase()
  if (q) {
    list = list.filter(c =>
      c.name.toLowerCase().includes(q) ||
      c.description.toLowerCase().includes(q)
    )
  }

  return list
})

// ── Lifecycle ─────────────────────────────────────────────────────────────────

function onRefreshFailed(msg: WSMessage) {
  const p = (msg.payload ?? msg) as Record<string, unknown>
  const id = p.connection_id as string
  const errMsg = (p.error as string) || 'Token refresh failed'
  if (id) refreshErrors.value = { ...refreshErrors.value, [id]: errMsg }
}

function onRefreshed(msg: WSMessage) {
  const p = (msg.payload ?? msg) as Record<string, unknown>
  const id = p.connection_id as string
  if (id) {
    const errs = { ...refreshErrors.value }
    delete errs[id]
    refreshErrors.value = errs
  }
}

onMounted(async () => {
  // Initialise from query params
  const qCat = route.query.category as string | undefined
  if (qCat && VALID_CATEGORIES.has(qCat as ConnectionCategory)) {
    activeCategory.value = qCat as ConnectionCategory
  }
  if (route.query.search) {
    search.value = route.query.search as string
  }
  if (route.query.error) {
    error.value = `OAuth error: ${route.query.error}. Please try again.`
  }
  await refresh()
  await loadMuninnStatus()

  // Fetch server-side credential catalog.
  try {
    serverCatalogEntries.value = await fetchCredentialCatalog()
  } catch (e) {
    catalogError.value = e instanceof Error ? e.message : 'Failed to load connections catalog'
  } finally {
    catalogLoading.value = false
  }

  // Listen for token refresh WS events.
  const ws = wsRef?.value
  if (ws?.on) {
    ws.on('connection_token_refresh_failed', onRefreshFailed)
    ws.on('connection_token_refreshed', onRefreshed)
  }
})

// Sync state → URL query params (replace so back button skips intermediate states)
watch(activeCategory, cat => {
  router.replace({ query: { ...route.query, category: cat === 'all' ? undefined : cat, search: search.value || undefined } })
})
watch(search, q => {
  router.replace({ query: { ...route.query, search: q || undefined } })
})

onUnmounted(() => {
  cancelWait()
  const ws = wsRef?.value
  if (ws?.off) {
    ws.off('connection_token_refresh_failed', onRefreshFailed)
    ws.off('connection_token_refreshed', onRefreshed)
  }
})

// ── Methods ───────────────────────────────────────────────────────────────────

async function refresh() {
  loading.value = true
  error.value   = ''
  try {
    const [conns, tools] = await Promise.all([
      api.connections.list(),
      api.system.tools(),
    ])
    oauthConnections.value = conns
    systemTools.value      = tools
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load connections'
  } finally {
    loading.value = false
  }
}

async function loadMuninnStatus() {
  try {
    const status = await api.muninn.status()
    muninnConnected.value = status.connected
    muninnIdentity.value  = status.endpoint || ''
  } catch (e) {
    // Muninn is optional — reset state to "not connected" and log for observability.
    // Do not set error.value: users without Muninn configured would see a false-alarm banner.
    muninnConnected.value = false
    muninnIdentity.value  = ''
    console.warn('huginn: muninn status check failed', e)
  }
}

function handleConnect(conn: CatalogConnection) {
  if (conn.type === 'oauth') {
    startOAuthConnect(conn.id)
  } else if (conn.type === 'credentials' || conn.type === 'database') {
    activeModal.value = conn.id
  }
}

async function startOAuthConnect(providerName: string) {
  // Prevent concurrent OAuth flows — cancel any in-progress one first
  if (waitingFor.value) cancelWait()
  error.value = ''
  try {
    const { auth_url } = await api.connections.start(providerName)
    if (!auth_url) {
      error.value = 'No authorization URL returned. Please try again.'
      return
    }
    snapshotBefore = new Set(oauthConnections.value.map(c => c.id))
    waitingFor.value = providerName
    window.open(auth_url, '_blank')
    pollInterval = setInterval(pollForNewConnection, 2000)
    pollTimeout  = setTimeout(() => {
      cancelWait()
      error.value = 'Authorization timed out. Please try again.'
    }, 2 * 60 * 1000)
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to start authorization'
  }
}

async function pollForNewConnection() {
  try {
    const conns = await api.connections.list()
    const newConn = conns.find(c => !snapshotBefore.has(c.id))
    if (newConn) {
      cancelWait()
      oauthConnections.value = conns
    }
  } catch {
    // Transient network error — keep polling until timeout
  }
}

function cancelWait() {
  if (pollInterval) { clearInterval(pollInterval); pollInterval = null }
  if (pollTimeout)  { clearTimeout(pollTimeout);   pollTimeout  = null }
  waitingFor.value = null
}

async function handleSetDefault(conn: CatalogConnection, accountId: string) {
  if (conn.type === 'oauth') {
    try {
      await api.connections.setDefault(accountId)
      await refresh()
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to set default account'
    }
    return
  }
  if (conn.id === 'github_cli') {
    try {
      await api.system.githubSwitch(accountId)
      await refresh()
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to switch GitHub account'
    }
  }
}

function handleDisconnect(connectionId: string) {
  if (!connectionId) return
  pendingDisconnect.value = connectionId
}

async function doDisconnect() {
  if (!pendingDisconnect.value) return
  const id = pendingDisconnect.value
  try {
    await api.connections.delete(id)
    oauthConnections.value = oauthConnections.value.filter(c => c.id !== id)
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to disconnect'
  } finally {
    pendingDisconnect.value = null
  }
}

async function onModalConnected() {
  activeModal.value = null
  await refresh()
  await loadMuninnStatus()
}

defineExpose({
  pendingDisconnect,
  handleDisconnect,
  doDisconnect,
  error,
  catalogError,
  waitingFor,
  cancelWait,
  connectedItems,
})
</script>
