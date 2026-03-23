import { ref } from 'vue'
import { apiFetch } from './useApi'

export interface ImpactEntry {
  path: string
  line?: number
  confidence: string
}

export interface ImpactReport {
  symbol: string
  high:   ImpactEntry[]
  medium: ImpactEntry[]
  low:    ImpactEntry[]
}

/**
 * useSymbols provides symbol graph queries against the local huginn server.
 *
 * - getImpact: returns files that import/call/extend a named symbol, grouped by confidence
 * - searchSymbols: returns symbol names containing the query string (case-insensitive)
 *
 * Both functions accept an optional AbortSignal for cancellation (e.g. debounced inputs).
 * Loading state is tracked separately per operation.
 */
export function useSymbols() {
  const impactLoading = ref(false)
  const searchLoading = ref(false)
  const error = ref<string | null>(null)

  async function getImpact(
    symbolName: string,
    signal?: AbortSignal,
  ): Promise<ImpactReport | null> {
    if (!symbolName.trim()) return null
    impactLoading.value = true
    error.value = null
    try {
      return await apiFetch<ImpactReport>(
        `/api/v1/symbols/impact/${encodeURIComponent(symbolName)}`,
        { signal },
      )
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        error.value = e instanceof Error ? e.message : 'Failed to fetch impact'
      }
      return null
    } finally {
      impactLoading.value = false
    }
  }

  async function searchSymbols(
    query: string,
    opts?: { limit?: number; signal?: AbortSignal },
  ): Promise<{ symbols: string[]; truncated: boolean }> {
    if (!query.trim()) return { symbols: [], truncated: false }
    searchLoading.value = true
    error.value = null
    try {
      const params = new URLSearchParams({ q: query })
      if (opts?.limit) params.set('limit', String(opts.limit))
      return await apiFetch<{ symbols: string[]; truncated: boolean }>(
        `/api/v1/symbols/search?${params}`,
        { signal: opts?.signal },
      )
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        error.value = e instanceof Error ? e.message : 'Failed to search symbols'
      }
      return { symbols: [], truncated: false }
    } finally {
      searchLoading.value = false
    }
  }

  return { getImpact, searchSymbols, impactLoading, searchLoading, error }
}
