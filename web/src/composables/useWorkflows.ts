import { ref, onUnmounted, getCurrentInstance } from 'vue'
import { getToken } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

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
  max_retries?: number
  retry_delay?: string
  timeout?: string
  inputs?: Array<{ from_step: string; as: string }>
  notify?: {
    on_success?: boolean
    on_failure?: boolean
    deliver_to?: Array<{ type: string; space_id?: string }>
  }
  // Phase 7: run this step against a different model than the agent's default.
  // Empty string means "use the agent's configured model".
  model_override?: string
  // Phase 8: conditional execution. After {{...}} substitution, falsy values
  // ("false", "0", "no", "off", "") cause the step to be skipped.
  when?: string
  // Phase 8: invoke another workflow (by id) synchronously as this step's body.
  // When set, agent/prompt/model_override are ignored.
  sub_workflow?: string
  // Legacy field kept for migration period
  routine?: string
}

export interface WorkflowNotification {
  on_success?: boolean
  on_failure?: boolean
  severity?: string
  deliver_to?: Array<{ type: string; space_id?: string; user?: string; from?: string; to?: string }>
}

export interface WorkflowChainConfig {
  next: string
  on_success?: boolean
  on_failure?: boolean
}

export interface WorkflowRetryConfig {
  max_retries?: number
  delay?: string
}

export interface WorkflowStepResult {
  position: number
  slug: string
  routine_id: string
  session_id?: string
  status: 'success' | 'failed' | 'skipped'
  error?: string
  output?: string
  started_at?: string
  completed_at?: string
  latency_ms?: number
  tokens_in?: number
  tokens_out?: number
  cost_usd?: number
  /** Set when status is skipped (e.g. when_false). */
  skip_reason?: string
  when_resolved?: string
}

export interface ForkWorkflowRunBody {
  inputs?: Record<string, string>
  use_live_definition?: boolean
}

/** Row from GET /api/v1/sessions/{id}/artifacts */
export interface SessionArtifactSummary {
  id: string
  kind: string
  title: string
  mime_type?: string
  agent_name: string
  session_id: string
  status: string
  created_at: string
  updated_at: string
}

export interface WorkflowRun {
  id: string
  workflow_id: string
  status: 'running' | 'complete' | 'failed' | 'partial' | 'cancelled'
  steps: WorkflowStepResult[]
  started_at: string
  completed_at?: string
  error?: string
  // Phase 6: replay/fork support
  trigger_inputs?: Record<string, string>
  workflow_snapshot?: Workflow
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
  // Phase 5: workflow chaining — trigger a downstream workflow on completion
  chain?: WorkflowChainConfig
  // Phase 8: workflow-level retry defaults inherited by steps that don't set their own
  retry?: WorkflowRetryConfig
}

export interface WorkflowEvent {
  type:
    | 'workflow_started'
    | 'workflow_step_started'
    | 'workflow_step_complete'
    | 'workflow_step_token'
    | 'workflow_complete'
    | 'workflow_failed'
    | 'workflow_partial'
    | 'workflow_skipped'
    | 'workflow_cancelled'
  workflow_id: string
  run_id: string
  workflow_name?: string
  position?: number
  slug?: string
  status?: string
  session_id?: string
  error?: string
  /** Token chunk from workflow_step_token streaming events. */
  token?: string
  step_name?: string
  step_position?: number
  /** True when the backend truncated step output at the 64KB limit. */
  truncated?: boolean
  /** Present on workflow_skipped: reason for skipping (e.g. "concurrency_limit" or "when_false"). */
  reason?: string
  /** Present on workflow_skipped when reason is "when_false": the resolved When expression. */
  when_resolved?: string
  /** Present on workflow_step_started when the step is a sub-workflow call. */
  sub_workflow?: string
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

  async function replayWorkflowRun(workflowId: string, runId: string): Promise<Record<string, unknown>> {
    const res = await fetch(`/api/v1/workflows/${workflowId}/runs/${runId}/replay`, {
      method: 'POST',
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`replay failed: ${res.status}`)
    return res.json() as Promise<Record<string, unknown>>
  }

  async function forkWorkflowRun(
    workflowId: string,
    runId: string,
    body?: ForkWorkflowRunBody,
  ): Promise<Record<string, unknown>> {
    const res = await fetch(`/api/v1/workflows/${workflowId}/runs/${runId}/fork`, {
      method: 'POST',
      headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify(body ?? {}),
    })
    if (!res.ok) throw new Error(`fork failed: ${res.status}`)
    return res.json() as Promise<Record<string, unknown>>
  }

  async function diffWorkflowRuns(
    workflowId: string,
    runId: string,
    otherRunId: string,
  ): Promise<Record<string, unknown>> {
    const res = await fetch(`/api/v1/workflows/${workflowId}/runs/${runId}/diff/${otherRunId}`, {
      headers: authHeaders(),
    })
    if (!res.ok) throw new Error(`diff failed: ${res.status}`)
    return res.json() as Promise<Record<string, unknown>>
  }

  async function fetchSessionArtifacts(sessionId: string): Promise<SessionArtifactSummary[]> {
    try {
      const res = await fetch(`/api/v1/sessions/${encodeURIComponent(sessionId)}/artifacts`, {
        headers: authHeaders(),
      })
      if (!res.ok) return []
      const data = await res.json()
      return Array.isArray(data) ? (data as SessionArtifactSummary[]) : []
    } catch {
      return []
    }
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

  // wireWS attaches to the HuginnWS message bus and routes workflow_*
  // events into liveEvents (keyed by workflow_id) so the WorkflowsView
  // "Live Execution" panel can render them.
  //
  // App.vue must call useWorkflows().wireWS(ws) at startup; without this
  // call the panel renders nothing because the WS messages are dispatched
  // to ws.on(type) handlers rather than raw 'message' listeners.
  function wireWS(ws: HuginnWS) {
    const wfEventTypes = [
      'workflow_started',
      'workflow_step_started',
      'workflow_step_token',
      'workflow_step_complete',
      'workflow_complete',
      'workflow_failed',
      'workflow_partial',
      'workflow_skipped',
      'workflow_cancelled',
    ] as const

    const terminalTypes = new Set([
      'workflow_complete',
      'workflow_failed',
      'workflow_partial',
      'workflow_cancelled',
    ])

    const handler = (msg: WSMessage) => {
      // HuginnWS already JSON-parsed the payload; messages without a
      // workflow_id (or with an empty one) are ignored — this prevents
      // accidental cross-talk with notification_new etc.
      const wfId = (msg as unknown as { workflow_id?: string }).workflow_id
      if (!wfId) return
      const evt = msg as unknown as WorkflowEvent

      // Fresh-run hygiene: when a new run starts, drop any prior events
      // for the same workflow_id so the live execution panel shows only
      // the active run. Per-workflow only — other workflows are untouched.
      if (msg.type === 'workflow_started') {
        liveEvents.value[wfId] = [evt]
      } else {
        if (!liveEvents.value[wfId]) liveEvents.value[wfId] = []
        liveEvents.value[wfId]!.push(evt)
        if (liveEvents.value[wfId]!.length > 100) {
          liveEvents.value[wfId]!.shift()
        }
      }

      if (terminalTypes.has(msg.type)) {
        fetchWorkflows()
      }
    }

    const registered: Array<readonly [string, (m: WSMessage) => void]> = []
    for (const t of wfEventTypes) {
      ws.on(t, handler)
      registered.push([t, handler] as const)
    }

    // Best-effort cleanup when the parent component unmounts. Skipped when
    // there is no active setup instance (e.g. wireWS called outside a
    // component, like the App.vue startup path which lives for the lifetime
    // of the page).
    if (getCurrentInstance() != null) {
      onUnmounted(() => {
        for (const [t, h] of registered) ws.off(t, h)
      })
    }
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
    replayWorkflowRun,
    forkWorkflowRun,
    diffWorkflowRuns,
    fetchSessionArtifacts,
    wireWS,
    makeWorkflowConflictError,
  }
}
