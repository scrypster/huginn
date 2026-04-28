import { ref, computed, watch, nextTick, onMounted, type Ref } from 'vue'
import type { Router } from 'vue-router'
import {
  useWorkflows,
  type Workflow,
  type WorkflowStep,
  type WorkflowTemplate,
  type WorkflowRun,
  type WorkflowEvent,
  type WorkflowStepResult,
  type SessionArtifactSummary,
} from '../../composables/useWorkflows'
import { getToken } from '../../composables/useApi'
import { useAgents } from '../../composables/useAgents'
import { useDeliveryQueue } from '../../composables/useDeliveryQueue'
import { remapIndex } from '../../utils/remapIndex'

type RouteParams = {
  id: Ref<string | undefined>
  runId: Ref<string | undefined>
}

export function useWorkflowsViewState(routeParams: RouteParams, router: Router) {
  const {
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
  } = useWorkflows()
  const { agents: agentList } = useAgents()
  const { actionableEntries, retryEntry, dismissEntry } = useDeliveryQueue()

  const search = ref('')
  const selectedId = ref<string | null>(routeParams.id.value || null)
  const selectedWorkflow = ref<Workflow | null>(null)
  const showCreate = ref(false)
  const showHistory = ref(false)
  const saving = ref(false)
  const saveMsg = ref('')
  const saveError = ref(false)
  const running = ref(false)
  const cancelling = ref(false)
  const expandedSteps = ref<Set<number>>(new Set())
  const dragFrom = ref<number | null>(null)
  const dragOver = ref<number | null>(null)
  const runs = ref<WorkflowRun[]>([])
  const loadingHistory = ref(false)
  const expandedRunId = ref<string | null>(null)
  const runDetailTab = ref<'steps' | 'deliveries'>('steps')
  const runDeliveries = computed(() =>
    actionableEntries.value.filter(e => e.run_id === expandedRunId.value),
  )

  const historyFeedback = ref<{ text: string; err: boolean } | null>(null)
  const showForkModal = ref(false)
  const forkTargetRun = ref<WorkflowRun | null>(null)
  const forkInputsJson = ref('')
  const forkUseLive = ref(false)
  const forkSubmitting = ref(false)
  const showDiffModal = ref(false)
  const diffBaseRun = ref<WorkflowRun | null>(null)
  const diffOtherRunId = ref('')
  const diffLoading = ref(false)
  const diffResultJson = ref('')
  const sessionArtifactsById = ref<Record<string, SessionArtifactSummary[]>>({})
  const sessionArtifactsLoading = ref<Record<string, boolean>>({})
  const sessionArtifactsFetched = ref<Record<string, boolean>>({})
  const artifactPopoverSessionId = ref<string | null>(null)
  const templates = ref<WorkflowTemplate[]>([])
  const loadingTemplates = ref(false)
  const eventsRef = ref<HTMLElement | null>(null)

  const stepAgentDetails = ref<Record<number, Record<string, unknown>>>({})
  const availableSpaces = ref<Array<{ id: string; name: string; kind: string }>>([])
  const showWorkflowAdvanced = ref(false)
  const stepOutputModal = ref<{ title: string; body: string } | null>(null)
  const expandedTokenBatchIndex = ref<number | null>(null)

  const chainCandidateWorkflows = computed(() => {
    const id = selectedId.value
    return workflows.value.filter(w => !id || w.id !== id)
  })

  function isSubWorkflowStep(step: WorkflowStep): boolean {
    return !!(step.sub_workflow && String(step.sub_workflow).trim())
  }

  function pickSubWorkflowStepId(step: WorkflowStep, e: Event) {
    const sel = e.target as HTMLSelectElement
    if (sel.value) step.sub_workflow = sel.value
    sel.value = ''
  }

  const editForm = ref<{
    name: string
    description: string
    enabled: boolean
    schedule: string
    timeout_minutes: number
    tags: string[]
    steps: WorkflowStep[]
    retry: { max_retries: number; delay: string }
    chain: { next: string; on_success: boolean; on_failure: boolean }
    notification: {
      on_success?: boolean
      on_failure?: boolean
      severity?: string
      deliver_to?: Array<{ type: string; space_id?: string }>
    }
  }>({
    name: '',
    description: '',
    enabled: false,
    schedule: '',
    timeout_minutes: 0,
    tags: [],
    steps: [],
    retry: { max_retries: 0, delay: '' },
    chain: { next: '', on_success: true, on_failure: false },
    notification: { on_success: false, on_failure: true, severity: 'info' },
  })

  const filteredWorkflows = computed(() => {
    if (!search.value) return workflows.value
    const q = search.value.toLowerCase()
    return workflows.value.filter(w =>
      w.name.toLowerCase().includes(q) ||
      (w.description || '').toLowerCase().includes(q) ||
      (w.tags || []).some(t => t.toLowerCase().includes(q)),
    )
  })

  const currentRunEvents = computed(() => {
    if (!selectedId.value) return []
    return liveEvents.value[selectedId.value] || []
  })

  type TokenBatchRow = { kind: 'token_batch'; workflow_id: string; run_id: string; text: string; count: number }
  type LiveDisplayRow = WorkflowEvent | TokenBatchRow

  function isTokenBatchRow(row: LiveDisplayRow): row is TokenBatchRow {
    return (row as TokenBatchRow).kind === 'token_batch'
  }

  function flattenLiveEvents(events: WorkflowEvent[]): LiveDisplayRow[] {
    const out: LiveDisplayRow[] = []
    let buf = ''
    let bufCount = 0
    let bufWf = ''
    let bufRun = ''
    const flush = () => {
      if (bufCount === 0) return
      out.push({ kind: 'token_batch', workflow_id: bufWf, run_id: bufRun, text: buf, count: bufCount })
      buf = ''
      bufCount = 0
    }
    for (const ev of events) {
      if (ev.type === 'workflow_step_token') {
        if (bufCount === 0) {
          bufWf = ev.workflow_id
          bufRun = ev.run_id
        }
        buf += ev.token ?? ''
        bufCount++
      } else {
        flush()
        out.push(ev)
      }
    }
    flush()
    return out
  }

  const displayedLiveEvents = computed(() => flattenLiveEvents(currentRunEvents.value))

  onMounted(async () => {
    await fetchWorkflows()
    fetchSpaces()
    if (routeParams.id.value) openById(routeParams.id.value)
  })

  watch(routeParams.id, (id) => {
    if (id) openById(id)
    else closeWorkflow()
  })

  watch(running, (isRunning) => {
    if (!isRunning) cancelling.value = false
  })

  async function loadRunsForHistory() {
    if (!selectedId.value) return
    loadingHistory.value = true
    try {
      runs.value = await fetchWorkflowRuns(selectedId.value)
    } finally {
      loadingHistory.value = false
    }
  }

  watch(showHistory, async (open) => {
    if (open && selectedId.value) {
      if (!routeParams.runId.value) expandedRunId.value = null
      await loadRunsForHistory()
      if (routeParams.runId.value && runs.value.some(r => r.id === routeParams.runId.value)) {
        expandedRunId.value = routeParams.runId.value
      }
    } else {
      expandedRunId.value = null
      historyFeedback.value = null
      showForkModal.value = false
      showDiffModal.value = false
      forkTargetRun.value = null
      diffBaseRun.value = null
      artifactPopoverSessionId.value = null
      sessionArtifactsById.value = {}
      sessionArtifactsFetched.value = {}
      sessionArtifactsLoading.value = {}
    }
  })

  watch(routeParams.runId, async (runId) => {
    if (!runId || !selectedId.value) return
    if (!showHistory.value) {
      showHistory.value = true
      return
    }
    if (runs.value.length === 0) {
      await loadRunsForHistory()
    }
    if (runs.value.some(r => r.id === runId)) {
      expandedRunId.value = runId
    }
  })

  watch(showCreate, async (open) => {
    if (open && !templates.value.length) {
      loadingTemplates.value = true
      templates.value = await fetchTemplates()
      loadingTemplates.value = false
    }
  })

  watch(displayedLiveEvents, async () => {
    await nextTick()
    if (eventsRef.value) {
      eventsRef.value.scrollTop = eventsRef.value.scrollHeight
    }
  })

  async function fetchSpaces() {
    try {
      const token = getToken()
      const data = await fetch('/api/v1/spaces', {
        headers: { Authorization: `Bearer ${token}` },
      }).then(r => r.json())
      availableSpaces.value = Array.isArray(data) ? data : []
    } catch {
    }
  }

  function onAgentSelected(stepIdx: number, agent: Record<string, unknown>) {
    stepAgentDetails.value[stepIdx] = agent
  }

  watch(
    () => editForm.value.steps.map(s => s.agent),
    (agents) => {
      agents.forEach((agent, idx) => {
        if (!agent && stepAgentDetails.value[idx]) {
          const next = { ...stepAgentDetails.value }
          delete next[idx]
          stepAgentDetails.value = next
        }
      })
    },
    { deep: false },
  )

  function addStepInput(step: WorkflowStep) {
    if (!step.inputs) step.inputs = []
    step.inputs.push({ from_step: '', as: '' })
  }

  function removeStepInput(step: WorkflowStep, idx: number) {
    step.inputs?.splice(idx, 1)
  }

  function addWorkflowDeliveryTarget() {
    if (!editForm.value.notification) editForm.value.notification = {}
    if (!editForm.value.notification.deliver_to) editForm.value.notification.deliver_to = []
    editForm.value.notification.deliver_to.push({ type: 'inbox' })
  }

  function removeWorkflowDeliveryTarget(idx: number) {
    editForm.value.notification?.deliver_to?.splice(idx, 1)
  }

  function addStepDeliveryTarget(step: WorkflowStep) {
    if (!step.notify) step.notify = {}
    if (!step.notify.deliver_to) step.notify.deliver_to = []
    step.notify.deliver_to.push({ type: 'inbox' })
  }

  function toggleStepNotify(step: WorkflowStep, enabled: boolean) {
    if (enabled) {
      step.notify = { on_failure: true }
    } else {
      step.notify = undefined
    }
  }

  function openWorkflow(wf: Workflow) {
    selectedId.value = wf.id
    selectedWorkflow.value = wf
    const ch = wf.chain
    const rt = wf.retry
    editForm.value = {
      name: wf.name,
      description: wf.description || '',
      enabled: wf.enabled,
      schedule: wf.schedule || '',
      timeout_minutes: wf.timeout_minutes ?? 0,
      tags: [...(wf.tags || [])],
      retry: rt
        ? { max_retries: rt.max_retries ?? 0, delay: rt.delay || '' }
        : { max_retries: 0, delay: '' },
      chain: ch
        ? {
            next: ch.next || '',
            on_success: ch.on_success !== false,
            on_failure: !!ch.on_failure,
          }
        : { next: '', on_success: true, on_failure: false },
      steps: wf.steps.map(s => ({
        ...s,
        inputs: s.inputs ? s.inputs.map(inp => ({ ...inp })) : [],
        notify: s.notify
          ? {
              ...s.notify,
              deliver_to: s.notify.deliver_to ? s.notify.deliver_to.map(d => ({ ...d })) : undefined,
            }
          : undefined,
      })),
      notification: wf.notification
        ? {
            ...wf.notification,
            deliver_to: wf.notification.deliver_to ? wf.notification.deliver_to.map(d => ({ ...d })) : [],
          }
        : { on_success: false, on_failure: true, severity: 'info', deliver_to: [] },
    }
    const details: Record<number, Record<string, unknown>> = {}
    editForm.value.steps.forEach((s, idx) => {
      if (s.agent) {
        const found = agentList.value.find(a => a.name === s.agent)
        if (found) details[idx] = found as Record<string, unknown>
      }
    })
    stepAgentDetails.value = details
    expandedSteps.value = new Set()
    router.push(`/workflows/${wf.id}`)
  }

  function openById(id: string) {
    const wf = workflows.value.find(w => w.id === id)
    if (wf) openWorkflow(wf)
    else selectedId.value = id
  }

  function closeWorkflow() {
    selectedId.value = null
    selectedWorkflow.value = null
    router.push('/workflows')
  }

  function toggleRun(runId: string) {
    if (!selectedId.value) return
    if (expandedRunId.value === runId) {
      expandedRunId.value = null
      router.replace(`/workflows/${selectedId.value}`)
    } else {
      expandedRunId.value = runId
      router.replace(`/workflows/${selectedId.value}/runs/${runId}`)
    }
  }

  function closeHistory() {
    showHistory.value = false
    historyFeedback.value = null
    showForkModal.value = false
    showDiffModal.value = false
    forkTargetRun.value = null
    diffBaseRun.value = null
    artifactPopoverSessionId.value = null
    sessionArtifactsById.value = {}
    sessionArtifactsFetched.value = {}
    sessionArtifactsLoading.value = {}
    if (selectedId.value && routeParams.runId.value) {
      router.replace(`/workflows/${selectedId.value}`)
    }
  }

  async function toggleArtifactPopover(sessionId: string) {
    if (artifactPopoverSessionId.value === sessionId) {
      artifactPopoverSessionId.value = null
      return
    }
    artifactPopoverSessionId.value = sessionId
    if (sessionArtifactsFetched.value[sessionId]) return
    sessionArtifactsLoading.value = { ...sessionArtifactsLoading.value, [sessionId]: true }
    try {
      const list = await fetchSessionArtifacts(sessionId)
      sessionArtifactsById.value = { ...sessionArtifactsById.value, [sessionId]: list }
      sessionArtifactsFetched.value = { ...sessionArtifactsFetched.value, [sessionId]: true }
    } finally {
      sessionArtifactsLoading.value = { ...sessionArtifactsLoading.value, [sessionId]: false }
    }
  }

  async function startReplay(run: WorkflowRun) {
    if (!selectedId.value) return
    historyFeedback.value = null
    try {
      await replayWorkflowRun(selectedId.value, run.id)
      historyFeedback.value = { text: 'Replay triggered.', err: false }
      await loadRunsForHistory()
    } catch (e) {
      historyFeedback.value = { text: e instanceof Error ? e.message : 'Replay failed', err: true }
    }
  }

  function openForkModal(run: WorkflowRun) {
    forkTargetRun.value = run
    forkInputsJson.value = ''
    forkUseLive.value = false
    showForkModal.value = true
  }

  async function submitFork() {
    if (!selectedId.value || !forkTargetRun.value) return
    forkSubmitting.value = true
    historyFeedback.value = null
    try {
      const raw = forkInputsJson.value.trim()
      let body: { inputs?: Record<string, string>; use_live_definition?: boolean }
      if (raw) {
        let o: Record<string, unknown>
        try {
          o = JSON.parse(raw) as Record<string, unknown>
        } catch {
          historyFeedback.value = { text: 'Invalid JSON for inputs', err: true }
          return
        }
        const inputs: Record<string, string> = {}
        for (const [k, v] of Object.entries(o)) inputs[k] = String(v)
        body = { inputs, use_live_definition: forkUseLive.value }
      } else {
        body = { use_live_definition: forkUseLive.value }
      }
      await forkWorkflowRun(selectedId.value, forkTargetRun.value.id, body)
      historyFeedback.value = { text: 'Fork triggered — a new run was started.', err: false }
      showForkModal.value = false
      await loadRunsForHistory()
    } catch (e) {
      historyFeedback.value = { text: e instanceof Error ? e.message : 'Fork failed', err: true }
    } finally {
      forkSubmitting.value = false
    }
  }

  function openDiffModal(run: WorkflowRun) {
    diffBaseRun.value = run
    const others = runs.value.filter(r => r.id !== run.id)
    diffOtherRunId.value = others[0]?.id ?? ''
    diffResultJson.value = ''
    showDiffModal.value = true
  }

  async function runDiffCompare() {
    if (!selectedId.value || !diffBaseRun.value || !diffOtherRunId.value) return
    diffLoading.value = true
    diffResultJson.value = ''
    historyFeedback.value = null
    try {
      const d = await diffWorkflowRuns(selectedId.value, diffBaseRun.value.id, diffOtherRunId.value)
      diffResultJson.value = JSON.stringify(d, null, 2)
    } catch (e) {
      historyFeedback.value = { text: e instanceof Error ? e.message : 'Diff failed', err: true }
    } finally {
      diffLoading.value = false
    }
  }

  function stepMetricsLine(s: WorkflowStepResult): string {
    const parts: string[] = []
    if (s.latency_ms != null && s.latency_ms > 0) parts.push(`${s.latency_ms} ms`)
    if ((s.tokens_in ?? 0) > 0 || (s.tokens_out ?? 0) > 0) {
      parts.push(`tokens in ${s.tokens_in ?? 0} / out ${s.tokens_out ?? 0}`)
    }
    if ((s.cost_usd ?? 0) > 0) parts.push(`≈ $${(s.cost_usd ?? 0).toFixed(4)}`)
    return parts.join(' · ')
  }

  function skipStepTooltip(s: WorkflowStepResult): string {
    if (s.status !== 'skipped') return ''
    if (s.skip_reason === 'when_false') {
      return `Skipped: when was falsy after substitution (${s.when_resolved ?? '—'})`
    }
    if (s.skip_reason) return `Skipped (${s.skip_reason})`
    return 'Skipped'
  }

  function stepPillTitle(s: WorkflowStepResult): string {
    if (s.status === 'skipped') return skipStepTooltip(s)
    if (isPlaceholderError(s.error)) return '⚠ Template placeholder not resolved — check from_step references'
    return s.error || ''
  }

  async function copyStepOutput(text: string) {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
    }
  }

  function toggleTokenBatchExpand(i: number) {
    expandedTokenBatchIndex.value = expandedTokenBatchIndex.value === i ? null : i
  }

  function addStep() {
    const pos = editForm.value.steps.length
    editForm.value.steps.push({
      name: '',
      agent: '',
      prompt: '',
      connections: {},
      vars: {},
      position: pos,
      on_failure: 'stop',
      inputs: [],
      model_override: undefined,
      when: undefined,
      sub_workflow: undefined,
    })
    expandedSteps.value = new Set([...expandedSteps.value, pos])
  }

  function removeStep(idx: number) {
    editForm.value.steps.splice(idx, 1)
    editForm.value.steps.forEach((s, i) => { s.position = i })
    const newExpanded = new Set<number>()
    for (const n of expandedSteps.value) {
      if (n < idx) newExpanded.add(n)
      else if (n > idx) newExpanded.add(n - 1)
    }
    expandedSteps.value = newExpanded
    const newDetails: Record<number, Record<string, unknown>> = {}
    for (const key in stepAgentDetails.value) {
      const k = Number(key)
      if (k < idx) newDetails[k] = stepAgentDetails.value[k]!
      else if (k > idx) newDetails[k - 1] = stepAgentDetails.value[k]!
    }
    stepAgentDetails.value = newDetails
  }

  function toggleStep(idx: number) {
    const next = new Set(expandedSteps.value)
    if (next.has(idx)) next.delete(idx)
    else next.add(idx)
    expandedSteps.value = next
  }

  function onDragStart(idx: number, e: DragEvent) {
    dragFrom.value = idx
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }

  function onDragOver(idx: number) {
    dragOver.value = idx
  }

  function onDrop(toIdx: number) {
    if (dragFrom.value === null || dragFrom.value === toIdx) {
      dragFrom.value = null
      dragOver.value = null
      return
    }
    const fromIdx = dragFrom.value
    const steps = [...editForm.value.steps]
    const [moved] = steps.splice(fromIdx, 1)
    if (!moved) return
    steps.splice(toIdx, 0, moved)
    steps.forEach((s, i) => { s.position = i })
    editForm.value.steps = steps

    const newExpanded = new Set<number>()
    for (const n of expandedSteps.value) {
      const remapped = remapIndex(n, fromIdx, toIdx)
      if (remapped !== null) newExpanded.add(remapped)
    }
    expandedSteps.value = newExpanded

    const newDetails: Record<number, Record<string, unknown>> = {}
    for (const key in stepAgentDetails.value) {
      const k = Number(key)
      const remapped = remapIndex(k, fromIdx, toIdx)
      if (remapped !== null) newDetails[remapped] = stepAgentDetails.value[k]!
    }
    stepAgentDetails.value = newDetails

    dragFrom.value = null
    dragOver.value = null
  }

  async function saveWorkflow() {
    if (!selectedId.value || !selectedWorkflow.value) return
    saving.value = true
    saveError.value = false
    saveMsg.value = ''
    try {
      const steps = editForm.value.steps.map((s, i) => {
        const row = { ...s, position: i } as WorkflowStep
        if (!row.sub_workflow?.trim()) delete row.sub_workflow
        if (!row.when?.trim()) delete row.when
        if (!row.model_override?.trim()) delete row.model_override
        return row
      })
      const wf: Workflow = {
        ...selectedWorkflow.value,
        ...editForm.value,
        steps,
      }
      const c = editForm.value.chain
      if (!c.next?.trim()) {
        delete wf.chain
      } else {
        wf.chain = { next: c.next.trim(), on_success: c.on_success, on_failure: c.on_failure }
      }
      const r = editForm.value.retry
      if ((!r.max_retries || r.max_retries <= 0) && !r.delay?.trim()) {
        delete wf.retry
      } else {
        const mr = r.max_retries > 0 ? Math.min(10, r.max_retries) : 0
        const d = r.delay?.trim() || ''
        wf.retry = mr > 0 ? { max_retries: mr, ...(d ? { delay: d } : {}) } : d ? { delay: d } : { max_retries: mr }
      }
      const updated = await updateWorkflow(selectedId.value, wf)
      selectedWorkflow.value = updated
    } catch (e) {
      saveError.value = true
      saveMsg.value = e instanceof Error ? e.message : 'Failed to save workflow. Please try again.'
    } finally {
      saving.value = false
    }
  }

  const RUNNING_WATCHDOG_MS = 30 * 60 * 1000
  let runningWatchdog: ReturnType<typeof setTimeout> | null = null

  async function triggerRun() {
    if (!selectedId.value || running.value) return
    running.value = true
    cancelling.value = false
    if (runningWatchdog !== null) {
      clearTimeout(runningWatchdog)
      runningWatchdog = null
    }
    runningWatchdog = setTimeout(() => {
      console.warn('[WorkflowsView] running watchdog fired — no terminal WS event after 30m')
      running.value = false
      runningWatchdog = null
    }, RUNNING_WATCHDOG_MS)
    try {
      await triggerWorkflow(selectedId.value)
    } catch {
    }
  }

  const TERMINAL_EVENT_TYPES = new Set([
    'workflow_complete',
    'workflow_failed',
    'workflow_partial',
    'workflow_cancelled',
  ])

  watch(currentRunEvents, (events) => {
    if (events.length === 0) return
    const latest = events[events.length - 1]!
    if (latest.type === 'workflow_started') {
      running.value = true
    } else if (TERMINAL_EVENT_TYPES.has(latest.type)) {
      running.value = false
      if (runningWatchdog !== null) {
        clearTimeout(runningWatchdog)
        runningWatchdog = null
      }
    }
  }, { deep: true })

  async function cancelRun() {
    if (!selectedId.value || cancelling.value) return
    cancelling.value = true
    try {
      await cancelWorkflow(selectedId.value)
    } catch {
      cancelling.value = false
    }
  }

  const pendingDelete = ref<{ id: string; name: string } | null>(null)

  function confirmDelete() {
    if (!selectedWorkflow.value) return
    pendingDelete.value = selectedWorkflow.value
  }

  async function doDeleteWorkflow() {
    if (!pendingDelete.value) return
    await deleteWorkflow(pendingDelete.value.id)
    pendingDelete.value = null
    closeWorkflow()
  }

  function clearRunEvents() {
    if (selectedId.value) {
      delete liveEvents.value[selectedId.value]
    }
  }

  async function createBlank() {
    showCreate.value = false
    const wf = await createWorkflow({
      name: 'New Workflow',
      enabled: false,
      schedule: '',
      steps: [],
    })
    openWorkflow(wf)
  }

  async function createFromTemplate(tpl: WorkflowTemplate) {
    showCreate.value = false
    const wf = await createWorkflow({
      name: tpl.workflow.name,
      description: tpl.workflow.description,
      enabled: false,
      schedule: tpl.workflow.schedule,
      steps: tpl.workflow.steps,
      notification: tpl.workflow.notification,
    })
    openWorkflow(wf)
  }

  function isPlaceholderError(error?: string): boolean {
    return !!error && error.includes('unresolved template placeholders')
  }

  function eventIcon(ev: WorkflowEvent): string {
    if (ev.type === 'workflow_step_complete' && ev.status === 'failed' && isPlaceholderError(ev.error)) {
      return '⚠'
    }
    switch (ev.type) {
      case 'workflow_started': return '▶'
      case 'workflow_step_started': return '▷'
      case 'workflow_step_complete': return ev.status === 'success' ? '✓' : '✗'
      case 'workflow_complete': return '✓'
      case 'workflow_failed': return '✗'
      case 'workflow_partial': return '◐'
      case 'workflow_skipped': return ev.position != null ? '⏭' : '⏸'
      case 'workflow_cancelled': return '⊘'
      default: return '·'
    }
  }

  function eventLabel(ev: WorkflowEvent): string {
    switch (ev.type) {
      case 'workflow_started': return `Started: ${ev.workflow_name || 'workflow'}`
      case 'workflow_step_started': {
        const sub = ev.sub_workflow ? ` → sub:${ev.sub_workflow}` : ''
        return `Step ${ev.position ?? '?'}: ${ev.slug || '…'} started${sub}`
      }
      case 'workflow_step_complete': return `Step ${ev.position}: ${ev.slug || 'done'} [${ev.status}]`
      case 'workflow_complete': return 'Workflow completed'
      case 'workflow_failed': return 'Workflow failed'
      case 'workflow_partial': return 'Workflow finished (partial — some steps failed)'
      case 'workflow_skipped': {
        const r = ev.reason || 'unknown'
        if (ev.position != null) return `Step ${ev.position} skipped (${r})`
        return `Workflow skipped (${r})`
      }
      case 'workflow_cancelled': return 'Workflow cancelled by user'
      default: return ev.type
    }
  }

  return {
    workflows,
    loading,
    liveEvents,
    search,
    selectedId,
    selectedWorkflow,
    showCreate,
    showHistory,
    saving,
    saveMsg,
    saveError,
    running,
    cancelling,
    expandedSteps,
    dragFrom,
    dragOver,
    runs,
    loadingHistory,
    expandedRunId,
    runDetailTab,
    runDeliveries,
    historyFeedback,
    showForkModal,
    forkTargetRun,
    forkInputsJson,
    forkUseLive,
    forkSubmitting,
    showDiffModal,
    diffBaseRun,
    diffOtherRunId,
    diffLoading,
    diffResultJson,
    sessionArtifactsById,
    sessionArtifactsLoading,
    sessionArtifactsFetched,
    artifactPopoverSessionId,
    templates,
    loadingTemplates,
    eventsRef,
    stepAgentDetails,
    availableSpaces,
    showWorkflowAdvanced,
    stepOutputModal,
    expandedTokenBatchIndex,
    chainCandidateWorkflows,
    editForm,
    filteredWorkflows,
    currentRunEvents,
    displayedLiveEvents,
    isSubWorkflowStep,
    pickSubWorkflowStepId,
    isTokenBatchRow,
    fetchSpaces,
    onAgentSelected,
    addStepInput,
    removeStepInput,
    addWorkflowDeliveryTarget,
    removeWorkflowDeliveryTarget,
    addStepDeliveryTarget,
    toggleStepNotify,
    openWorkflow,
    openById,
    closeWorkflow,
    toggleRun,
    closeHistory,
    toggleArtifactPopover,
    startReplay,
    openForkModal,
    submitFork,
    openDiffModal,
    runDiffCompare,
    stepMetricsLine,
    skipStepTooltip,
    stepPillTitle,
    copyStepOutput,
    toggleTokenBatchExpand,
    addStep,
    removeStep,
    toggleStep,
    onDragStart,
    onDragOver,
    onDrop,
    saveWorkflow,
    triggerRun,
    cancelRun,
    pendingDelete,
    confirmDelete,
    doDeleteWorkflow,
    clearRunEvents,
    createBlank,
    createFromTemplate,
    isPlaceholderError,
    eventIcon,
    eventLabel,
    retryEntry,
    dismissEntry,
  }
}
