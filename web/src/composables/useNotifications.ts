import { ref } from 'vue'
import { getToken } from './useApi'
import type { WSMessage } from './useHuginnWS'

export interface Notification {
  id: string
  routine_id: string
  run_id: string
  summary: string
  detail: string
  severity: 'info' | 'warning' | 'urgent'
  status: 'pending' | 'seen' | 'dismissed' | 'approved' | 'executed' | 'failed'
  session_id?: string
  proposed_actions?: ProposedAction[]
  created_at: string
  updated_at: string
  expires_at?: string
}

export interface ProposedAction {
  id: string
  label: string
  tool_name: string
  tool_params: Record<string, unknown>
  destructive: boolean
}

export interface DeliveryFailure {
  workflowId: string
  runId: string
  deliveryType: string
  /** Redacted target: webhook → scheme+host, email → *@domain */
  target: string
  error: string
  timestamp: string
}

const notifications = ref<Notification[]>([])
const pendingCount = ref(0)
const loading = ref(false)
/** Last 50 notification delivery failures received over WS. */
const deliveryFailures = ref<DeliveryFailure[]>([])

export function useNotifications() {
  async function fetchNotifications() {
    loading.value = true
    try {
      const data = await fetch(`/api/v1/notifications`, {
        headers: { Authorization: `Bearer ${getToken()}` },
      }).then(r => r.json())
      notifications.value = Array.isArray(data) ? data : []
      pendingCount.value = notifications.value.filter((n: Notification) => n.status === 'pending').length
    } catch {
      // ignore
    } finally {
      loading.value = false
    }
  }

  async function fetchSummary() {
    try {
      const data = await fetch('/api/v1/inbox/summary', {
        headers: { Authorization: `Bearer ${getToken()}` },
      }).then(r => r.json())
      pendingCount.value = data.pending_count ?? 0
    } catch { /* ignore */ }
  }

  async function applyAction(id: string, action: string, proposedActionId?: string) {
    const res = await fetch(`/api/v1/notifications/${id}/action`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${getToken()}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ action, proposed_action_id: proposedActionId }),
    })
    if (!res.ok) throw new Error(`action failed: ${res.status}`)
    const updated = await res.json() as any
    const idx = notifications.value.findIndex((n: Notification) => n.id === id)
    if (idx !== -1 && notifications.value[idx]) {
      notifications.value[idx]!.status = updated.status ?? 'pending'
    }
    pendingCount.value = updated.pending_count ?? pendingCount.value
    return updated
  }

  function wireWS(ws: any) {
    if (!ws) return
    ws.on?.('notification_new', (payload: { notification: Notification }) => {
      notifications.value.unshift(payload.notification)
    })
    ws.on?.('notification_update', (payload: { id: string; status: string }) => {
      const n = notifications.value.find((n: Notification) => n.id === payload.id)
      if (n) (n as any).status = payload.status
    })
    ws.on?.('inbox_badge', (payload: { pending_count: number }) => {
      pendingCount.value = payload.pending_count
    })
    ws.on?.('notification_delivery_failed', (msg: WSMessage) => {
      const p = msg.payload
      if (!p) return
      deliveryFailures.value.push({
        workflowId: p['workflow_id'] as string ?? '',
        runId: p['run_id'] as string ?? '',
        deliveryType: p['delivery_type'] as string ?? '',
        target: p['target'] as string ?? '',
        error: p['error'] as string ?? '',
        timestamp: new Date().toISOString(),
      })
      // Keep last 50 failures to bound memory.
      if (deliveryFailures.value.length > 50) {
        deliveryFailures.value.shift()
      }
    })
  }

  return {
    notifications,
    pendingCount,
    loading,
    deliveryFailures,
    fetchNotifications,
    fetchSummary,
    applyAction,
    wireWS,
  }
}
