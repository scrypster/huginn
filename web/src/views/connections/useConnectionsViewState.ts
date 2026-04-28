import { ref, computed, watch, onMounted, onUnmounted, inject, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Connection, type SystemToolStatus } from '../../composables/useApi'
import type { HuginnWS, WSMessage } from '../../composables/useHuginnWS'
import {
  CATEGORY_LABELS,
  hydrateOAuth,
  hydrateSystem,
  hydrateCredentials,
  type CatalogEntry,
  type CatalogConnection,
  type ConnectionCategory,
} from '../../composables/useConnectionsCatalog'
import { fetchCredentialCatalog, type CredentialCatalogEntry } from '../../composables/useCredentialCatalog'

const VALID_CATEGORIES = new Set<ConnectionCategory>([
  'all', 'my_connections', 'communication', 'dev_tools',
  'cloud', 'productivity', 'databases', 'observability', 'system',
])

export function useConnectionsViewState() {
  const route = useRoute()
  const router = useRouter()

  const activeCategory = ref<ConnectionCategory>('all')
  const search = ref('')
  const loading = ref(false)
  const catalogLoading = ref(true)
  const error = ref('')
  const catalogError = ref('')
  const waitingFor = ref<string | null>(null)
  const pendingDisconnect = ref<string | null>(null)
  const activeModal = ref<string | null>(null)

  const oauthConnections = ref<Connection[]>([])
  const systemTools = ref<SystemToolStatus[]>([])
  const muninnConnected = ref(false)
  const muninnIdentity = ref('')
  const serverCatalogEntries = ref<CredentialCatalogEntry[]>([])

  const wsRef = inject<Ref<HuginnWS | null>>('ws')
  const refreshErrors = ref<Record<string, string>>({})

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

  let pollInterval: ReturnType<typeof setInterval> | null = null
  let pollTimeout: ReturnType<typeof setTimeout> | null = null
  let snapshotBefore = new Set<string>()

  const hydratedCatalog = computed<CatalogConnection[]>(() =>
    serverCatalogEntries.value.map(e => {
      const entry: CatalogEntry = {
        id: e.id,
        name: e.name,
        description: e.description,
        category: e.category as ConnectionCategory,
        icon: e.icon,
        iconColor: e.icon_color,
        type: e.type as CatalogEntry['type'],
        multiAccount: e.multi_account,
      }
      if (entry.type === 'coming_soon') return { ...entry, state: null }
      if (entry.id === 'muninn') {
        return { ...entry, state: { connected: muninnConnected.value, identity: muninnIdentity.value || undefined } }
      }
      if (entry.type === 'oauth') return { ...entry, state: hydrateOAuth(entry, oauthConnections.value) }
      if (entry.type === 'system') return { ...entry, state: hydrateSystem(entry, systemTools.value) }
      if (entry.type === 'credentials' || entry.type === 'database') {
        return { ...entry, state: hydrateCredentials(entry, oauthConnections.value) }
      }
      return { ...entry, state: null }
    }),
  )

  const connectedCount = computed(() =>
    hydratedCatalog.value.filter(c => c.state?.connected).length,
  )

  const connectedItems = computed(() =>
    hydratedCatalog.value.filter(c => c.state?.connected && c.type !== 'coming_soon'),
  )

  const filteredCatalog = computed(() => {
    let list = hydratedCatalog.value
    if (activeCategory.value !== 'all' && activeCategory.value !== 'my_connections') {
      list = list.filter(c => c.category === activeCategory.value)
    }
    const q = search.value.trim().toLowerCase()
    if (q) {
      list = list.filter(c =>
        c.name.toLowerCase().includes(q) ||
        c.description.toLowerCase().includes(q),
      )
    }
    return list
  })

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
    const qCat = route.query.category as string | undefined
    if (qCat && VALID_CATEGORIES.has(qCat as ConnectionCategory)) {
      activeCategory.value = qCat as ConnectionCategory
    }
    if (route.query.search) search.value = route.query.search as string
    if (route.query.error) error.value = `OAuth error: ${route.query.error}. Please try again.`
    await refresh()
    await loadMuninnStatus()
    try {
      serverCatalogEntries.value = await fetchCredentialCatalog()
    } catch (e) {
      catalogError.value = e instanceof Error ? e.message : 'Failed to load connections catalog'
    } finally {
      catalogLoading.value = false
    }
    const ws = wsRef?.value
    if (ws?.on) {
      ws.on('connection_token_refresh_failed', onRefreshFailed)
      ws.on('connection_token_refreshed', onRefreshed)
    }
  })

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

  async function refresh() {
    loading.value = true
    error.value = ''
    try {
      const [conns, tools] = await Promise.all([
        api.connections.list(),
        api.system.tools(),
      ])
      oauthConnections.value = conns
      systemTools.value = tools
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
      muninnIdentity.value = status.endpoint || ''
    } catch (e) {
      muninnConnected.value = false
      muninnIdentity.value = ''
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
      pollTimeout = setTimeout(() => {
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
    }
  }

  function cancelWait() {
    if (pollInterval) { clearInterval(pollInterval); pollInterval = null }
    if (pollTimeout) { clearTimeout(pollTimeout); pollTimeout = null }
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

  return {
    CATEGORY_LABELS,
    activeCategory,
    search,
    loading,
    catalogLoading,
    error,
    catalogError,
    waitingFor,
    pendingDisconnect,
    activeModal,
    oauthConnections,
    refreshErrors,
    hydratedCatalog,
    connectedCount,
    connectedItems,
    filteredCatalog,
    expiryBadge,
    refresh,
    handleConnect,
    startOAuthConnect,
    cancelWait,
    handleSetDefault,
    handleDisconnect,
    doDisconnect,
    onModalConnected,
  }
}
