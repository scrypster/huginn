import { ref, computed, watch, onScopeDispose, getCurrentScope } from 'vue'
import type { Ref } from 'vue'
import { apiFetch } from './useApi'

interface ReplicationStatus {
  pending: number
  failed: number
  dead: number
  connected: boolean
}

export function useReplicationStatus(spaceId: Ref<string | undefined>) {
  const status = ref<ReplicationStatus | null>(null)
  const vaultCount = ref(0)
  let timer: ReturnType<typeof setInterval> | null = null

  async function poll() {
    try {
      const [repl, vaultsRes] = await Promise.all([
        apiFetch<ReplicationStatus>('/api/v1/memory/replication-status'),
        apiFetch<{ vaults?: string[] }>('/api/v1/muninn/vaults').catch(() => ({ vaults: [] as string[] })),
      ])
      status.value = repl
      vaultCount.value = vaultsRes.vaults?.length ?? 0
    } catch { /* swallow — chip stays hidden */ }
  }

  function stopPolling() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  function startPolling() {
    stopPolling()
    poll()
    timer = setInterval(poll, 30_000)
  }

  watch(spaceId, (id) => {
    if (id) {
      startPolling()
    } else {
      stopPolling()
      status.value = null
    }
  }, { immediate: true })

  if (getCurrentScope()) {
    onScopeDispose(stopPolling)
  }

  const chipText = computed<string>(() => {
    const s = status.value
    // Parked Memory (disconnected or empty vault dropdown) stays quiet.
    if (!s || !s.connected || vaultCount.value === 0) return ''
    if (s.pending > 0) return '🧠 Memory syncing…'
    if (s.failed > 0 || s.dead > 0) return '🧠 Memory sync issues'
    return '🧠 Memory synced'
  })

  const chipClass = computed<string>(() => {
    const s = status.value
    if (!s) return ''
    if (s.failed > 0 || s.dead > 0) return 'text-amber-400 bg-amber-400/10'
    if (s.pending > 0) return 'text-huginn-blue bg-huginn-blue/10 animate-pulse'
    return 'text-huginn-muted bg-huginn-surface/60'
  })

  return { chipText, chipClass }
}
