// useCheckpoints.ts — composable for Huginn's Run Checkpoints system
// (internal/checkpoint/http.go). Owns its own fetch helpers built on
// useApi.ts's exported apiFetch/getToken/ensureToken — deliberately NOT a
// new namespace on the shared `api` object (another agent owns useApi.ts
// this wave).
//
// JSON field names below mirror internal/checkpoint/checkpoint.go verbatim:
// RunRecord/RevertResult have no `json:` struct tags, so Go's default
// encoding/json emits the exported Go field names as-is (PascalCase).

import { ref, computed } from 'vue'
import { apiFetch, ensureToken, getToken } from './useApi'

export type RunStatus = 'active' | 'completed' | 'capture_failed' | 'reverted'

export interface RunRecord {
  ThreadID: string
  AgentID: string
  TaskSummary: string
  Status: RunStatus
  PreSnapshot: string
  PostSnapshot: string
  TouchedPaths: string[] | null
  Pushed: boolean
  PRURL: string
  CreatedAt: string
  CompletedAt: string
  // CaptureError is set (and Status === 'capture_failed') when a snapshot
  // attempt failed. Never empty when Status is capture_failed — this is
  // the run that is NOT protected; surface it, never hide it.
  CaptureError: string
  IgnoredAtBegin: string[] | null
  IgnoredTouched: string[] | null
}

export interface RevertResult {
  Restored: string[] | null
  Deleted: string[] | null
  SkippedEdited: string[] | null
  NotRestorable: string[] | null
  Failed: Record<string, string> | null
  Warning: string
  NothingCaptured: boolean
}

export interface RevertOptions {
  all?: boolean
  only_paths?: string[]
  allow_after_push?: boolean
}

// Module-level shared state (singleton across all component instances) —
// same pattern as useSessions.ts, so ChatView's inline badges and the
// Checkpoints settings tab always see the same list without double-fetching.
const runs = ref<RunRecord[]>([])
const loading = ref(false)
const error = ref('')
const loaded = ref(false)

export function useCheckpoints() {
  const runByThread = computed(() => {
    const m = new Map<string, RunRecord>()
    for (const r of runs.value) m.set(r.ThreadID, r)
    return m
  })

  async function fetchRuns(limit = 100): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const res = await apiFetch<RunRecord[]>(`/api/v1/checkpoints/?limit=${limit}`)
      runs.value = res ?? []
      loaded.value = true
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to load checkpoints'
    } finally {
      loading.value = false
    }
  }

  // ensureLoaded fetches once, lazily — safe to call from every badge's
  // onMounted without triggering a fetch storm (a shared in-flight guard
  // via `loading`).
  async function ensureLoaded(): Promise<void> {
    if (!loaded.value && !loading.value) await fetchRuns()
  }

  function getRunForThread(threadId: string): RunRecord | undefined {
    return runByThread.value.get(threadId)
  }

  async function fetchRun(threadId: string): Promise<RunRecord> {
    return apiFetch<RunRecord>(`/api/v1/checkpoints/${encodeURIComponent(threadId)}`)
  }

  // fetchDiff hits the one endpoint that returns text/plain, not JSON, so
  // it can't go through apiFetch's res.json() — a small hand-rolled fetch
  // with the same auth/retry shape instead.
  async function fetchDiff(threadId: string): Promise<string> {
    await ensureToken()
    const url = `/api/v1/checkpoints/${encodeURIComponent(threadId)}/diff`
    let res = await fetch(url, { headers: { Authorization: `Bearer ${getToken()}` } })
    if (res.status === 401) {
      await ensureToken()
      res = await fetch(url, { headers: { Authorization: `Bearer ${getToken()}` } })
    }
    if (!res.ok) {
      const body = await res.text().catch(() => '')
      throw new Error(body || `${res.status}`)
    }
    return res.text()
  }

  async function revert(threadId: string, opts: RevertOptions = {}): Promise<RevertResult> {
    const body = {
      all: !!opts.all,
      only_paths: opts.only_paths ?? [],
      allow_after_push: !!opts.allow_after_push,
    }
    const result = await apiFetch<RevertResult>(
      `/api/v1/checkpoints/${encodeURIComponent(threadId)}/revert`,
      { method: 'POST', body: JSON.stringify(body) },
    )
    // Refresh the ledger so status chips (e.g. -> reverted) update
    // everywhere the composable is used, not just the caller's local view.
    await fetchRuns()
    return result
  }

  return {
    runs,
    loading,
    error,
    loaded,
    runByThread,
    fetchRuns,
    ensureLoaded,
    getRunForThread,
    fetchRun,
    fetchDiff,
    revert,
  }
}
