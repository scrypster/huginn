import { ref, onUnmounted } from 'vue'
import { getToken } from './useApi'

export interface WorkflowConflictError extends Error {
  currentVersion?: number
}

export function makeWorkflowConflictError(currentVersion?: number): WorkflowConflictError {
  const err = new Error('Workflow was modified by another session. Please reload and retry.') as WorkflowConflictError
  err.name = 'WorkflowConflictError'
  err.currentVersion = currentVersion
  return err
}

export interface WorkflowStep {
  // Inline step fields (unified model)
  name?: string
  agent?: string
  prompt?: string
  connections?: Record<string, string>
  vars?: Record<string, string>
  position: number
  on_failure?: string  // "stop" | "continue"
  inputs?: Array<{ from_step: string; as: string }>
  notify?: {
    on_success?: boolean
    on_failure?: boolean
    deliver_to?: Array<{ type: string; space_id?: string }>
  }
  // Legacy field kept for migration period
  routine?: string
}

export interface WorkflowNotification {
  on_success?: boolean
  on_failure?: boolean
  severity?: string
  deliver_to?: Array<{ type: string; space_id?: string }>
}

export interface WorkflowRun {
  id: string
  workflow_id: string
  status: 'running' | 'complete' | 'failed' | 'partial' | 'cancelled'
  steps: Array<{
    position: number
    slug: string
    routine_id: string
    session_id?: string
    status: 'success' | 'failed' | 'skipped'
    error?: string
    output?: string
  }>
  started_at: string
  completed_at?: string
  error?: string
}

export interface WorkflowTemplate {
  id: string
  name: string
  description: string
  workflow: Workflow
}

export interface Workflow {
  id: string
  slug?: string
  name: string
  description?: string
  enabled: boolean
  schedule: string
  tags?: string[]
  steps: WorkflowStep[]
  notification?: WorkflowNotification
  file_path?: string
  created_at?: string
  updated_at?: string
  version?: number
  timeout_minutes?: number
}

export interface WorkflowEvent {
  type: 'workflow_started' | 'workflow_step_complete' | 'workflow_complete' | 'workflow_failed' | 'workflow_partial' | 'workflow_skipped' | 'workflow_cancelled'
  workflow_id: string
  run_id: string
  workflow_name?: string
  position?: number
  slug?: string
  status?: string
  session_id?: string
  error?: string
  /** True when the backend truncated step output at the 64KB limit. */
  truncated?: boolean
  /** Present on workflow_skipped: reason for skipping (e.g. "concurrency_limit"). */
  reason?: string
}

const workflows = ref<Workflow[]>([])
const loading = ref(false)
const liveEvents = ref<Record<string, WorkflowEvent[]>>({})

export function useWorkflows() {
  function authHeaders(): Record<string, string> {
    return { Authorization: `Bearer ${getToken()}` }
  }

  async function fetchWorkflows() {
    loading.value = true
    try {
      const data = await fetch('/api/v1/workflows', {
        headers: authHeaders(),
      }).then(r => r.json())
      workflows.value = Array.isArray(data) ? data : []
    } finally {
      loading.value = false
    }
  }

  async function fetchTemplates(): Promise<WorkflowTemplate[]> {
    try {
      const data = await fetch('/api/v1/workflows/templates', {
        headers: authHeaders(),
      }).then(r => r.json())
      return Array.isArray(data) ? data : []
    } catch {
      return []
    }
  }

  async function createWorkflow(data: Partial<Workflow>): Promise<Workflow> {
    const res = await fetch('/api/v1/workflows', {
      method: 'POST',
      headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    })
    if (!res.ok) throw new Error(`create failed: ${res.status}`)
    const created: Workflow = await res.json()
    if (created.id) workflows.value.unshift(created)
    return created
  }

  async function updateWorkflow(id: string, data: Workflow): Promise<Workflow> {
    const res = await fetch(`/api/v1/workflows/${id}`, {
      method: 'PUT',
      headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    })
    if (res.status === 409) {
      const body = await res.json().catch(() => ({}))
      throw makeWorkflowConflictError(body?.current_version)
    }
    if (!res.ok) throw new Error(`update failed: ${res.status}`)
    const updated: Workflow = await res.json()
    if (updated.id) {
      const idx = workflows.value.findIndex(w => w.id === id)
      if (idx !== -1) workflows.value[idx] = updated
    }
    return updated
  }

  async function deleteWorkflow(id: string) {
    const res = await fetch(`/api/v1/workflows/${id}`, {
      method: 'DELETE',
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`delete failed: ${res.status}`)
    workflows.value = workflows.value.filter(w => w.id !== id)
  }

  async function triggerWorkflow(id: string) {
    const res = await fetch(`/api/v1/workflows/${id}/run`, {
      method: 'POST',
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`trigger failed: ${res.status}`)
    return res.json()
  }

  async function cancelWorkflow(id: string): Promise<void> {
    const res = await fetch(`/api/v1/workflows/${id}/cancel`, {
      method: 'POST',
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`cancel failed: ${res.status}`)
  }

  async function fetchWorkflowRuns(id: string): Promise<WorkflowRun[]> {
    try {
      const data = await fetch(`/api/v1/workflows/${id}/runs`, {
        headers: authHeaders(),
      }).then(r => r.json())
      return Array.isArray(data) ? data : []
    } catch {
      return []
    }
  }

  function wireWS(ws: WebSocket) {
    const handler = (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data)
        const wfEvents = ['workflow_started', 'workflow_step_complete', 'workflow_complete', 'workflow_failed', 'workflow_partial', 'workflow_skipped', 'workflow_cancelled']
        if (wfEvents.includes(msg.type) && msg.workflow_id) {
          if (!liveEvents.value[msg.workflow_id]) {
            liveEvents.value[msg.workflow_id] = []
          }
          liveEvents.value[msg.workflow_id]!.push(msg as WorkflowEvent)
          if (liveEvents.value[msg.workflow_id]!.length > 100) {
            liveEvents.value[msg.workflow_id]!.shift()
          }
          if (msg.type === 'workflow_complete' || msg.type === 'workflow_failed' || msg.type === 'workflow_partial' || msg.type === 'workflow_cancelled') {
            fetchWorkflows()
          }
        }
      } catch (err) {
        console.warn('[useWorkflows] malformed WS message:', err, 'data:', (event.data as string | undefined)?.slice?.(0, 200))
      }
    }
    ws.addEventListener('message', handler)
    onUnmounted(() => ws.removeEventListener('message', handler))
  }

  return {
    workflows,
    loading,
    liveEvents,
    fetchWorkflows,
    fetchTemplates,
    createWorkflow,
    updateWorkflow,
    deleteWorkflow,
    triggerWorkflow,
    cancelWorkflow,
    fetchWorkflowRuns,
    wireWS,
    makeWorkflowConflictError,
  }
}
