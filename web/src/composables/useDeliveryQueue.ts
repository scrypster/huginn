import { ref, computed } from 'vue'
import { getToken, ensureToken } from './useApi'

export interface DeliveryQueueEntry {
  id: string
  workflow_id: string
  run_id: string
  endpoint: string
  channel: 'webhook' | 'email'
  status: 'pending' | 'retrying' | 'delivered' | 'failed' | 'superseded'
  attempt_count: number
  max_attempts: number
  retry_window_s: number
  next_retry_at: string
  created_at: string
  last_attempt_at?: string
  last_error?: string
}

const badgeCount = ref(0)
const actionableEntries = ref<DeliveryQueueEntry[]>([])
const loading = ref(false)

export function useDeliveryQueue() {
  // Gate every request on a ready token: this composable polls independently
  // of App.vue's initApp() boot sequence (Vue fires child onMounted() before
  // the parent's, so a caller that mounts early can otherwise fire before
  // initApp() has set the token — producing a pre-auth 401 that's logged to
  // the console). ensureToken() no-ops once the token is already set.
  async function authHeaders(): Promise<Record<string, string>> {
    await ensureToken()
    return { Authorization: `Bearer ${getToken()}` }
  }

  async function fetchBadge(): Promise<void> {
    try {
      const res = await fetch('/api/v1/delivery-queue/badge', { headers: await authHeaders() })
      if (!res.ok) return
      const data = await res.json()
      badgeCount.value = data.count ?? 0
    } catch {
      // Non-critical — badge stays at last value
    }
  }

  async function fetchActionable(): Promise<void> {
    loading.value = true
    try {
      const res = await fetch('/api/v1/delivery-queue', { headers: await authHeaders() })
      if (!res.ok) return
      actionableEntries.value = await res.json()
      badgeCount.value = actionableEntries.value.length
    } finally {
      loading.value = false
    }
  }

  async function retryEntry(id: string): Promise<void> {
    const res = await fetch(`/api/v1/delivery-queue/${id}/retry`, {
      method: 'POST',
      headers: await authHeaders(),
    })
    if (!res.ok) throw new Error(`retry failed: ${res.status}`)
    await fetchActionable()
  }

  async function dismissEntry(id: string): Promise<void> {
    const res = await fetch(`/api/v1/delivery-queue/${id}`, {
      method: 'DELETE',
      headers: await authHeaders(),
    })
    if (!res.ok) throw new Error(`dismiss failed: ${res.status}`)
    actionableEntries.value = actionableEntries.value.filter(e => e.id !== id)
    badgeCount.value = actionableEntries.value.length
  }

  // Call this from the WS message handler when a delivery_badge_update event arrives.
  function handleBadgeUpdate(count: number): void {
    badgeCount.value = count
    if (count > 0) fetchActionable()
  }

  const hasIssues = computed(() => badgeCount.value > 0)

  return {
    badgeCount,
    actionableEntries,
    loading,
    hasIssues,
    fetchBadge,
    fetchActionable,
    retryEntry,
    dismissEntry,
    handleBadgeUpdate,
  }
}
