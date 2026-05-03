import { computed, ref } from 'vue'
import { api, type AgentCapabilityValidation, type Connection, type ToolbeltEntry } from '../../composables/useApi'

const DENY_REASON_COPY: Record<string, string> = {
  missing_connection_id: 'Connection is missing from this assignment.',
  duplicate_connection_id: 'Duplicate tool assignment detected for the same connection/profile.',
  unknown_connection_id: 'Connection is no longer available. Remove it or reconnect it.',
  wildcard_provider_forbidden: 'Legacy wildcard connection — click Remove to fix and unlock save.',
  provider_mismatch: 'Assigned provider does not match the selected connection.',
  single_account_provider: 'This provider can only have one assigned connection for an agent.',
}

function entryKey(entry: ToolbeltEntry): string {
  return `${entry.connection_id}::${entry.profile ?? ''}`
}

function isSystemEntry(entry: ToolbeltEntry): boolean {
  return entry.connection_id.startsWith('system:')
}

export function useAgentCapabilityMatrix() {
  const matrixConnectionIDs = ref(new Set<string>())
  const matrixLoading = ref(false)
  const matrixError = ref('')

  const validation = ref<AgentCapabilityValidation | null>(null)
  const validationError = ref('')

  const deniedByEntryKey = computed(() => {
    const denied = new Map<string, { reason_code?: string; reason?: string }>()
    for (const d of validation.value?.decisions || []) {
      if (!d.allowed) {
        denied.set(entryKey(d.entry), { reason_code: d.reason_code, reason: d.reason })
      }
    }
    return denied
  })

  function resetValidation() {
    validation.value = null
    validationError.value = ''
  }

  function reasonText(reasonCode?: string, fallbackReason?: string): string {
    if (reasonCode && DENY_REASON_COPY[reasonCode]) return DENY_REASON_COPY[reasonCode]
    return fallbackReason || 'Invalid connection assignment.'
  }

  function localReason(entry: ToolbeltEntry): string | null {
    if (isSystemEntry(entry)) return null
    if (!entry.connection_id) return DENY_REASON_COPY.missing_connection_id || 'Connection is missing from this assignment.'
    if (matrixConnectionIDs.value.size > 0 && !matrixConnectionIDs.value.has(entry.connection_id)) {
      return DENY_REASON_COPY.unknown_connection_id || 'Connection is no longer available. Remove it or reconnect it.'
    }
    if (entry.provider === '*') return DENY_REASON_COPY.wildcard_provider_forbidden || 'Wildcard provider assignment is not allowed.'
    return null
  }

  function entryReason(entry: ToolbeltEntry): string | null {
    const denied = deniedByEntryKey.value.get(entryKey(entry))
    if (denied) {
      return reasonText(denied.reason_code, denied.reason)
    }
    return localReason(entry)
  }

  function firstReason(toolbelt: ToolbeltEntry[]): string | null {
    if (validationError.value) return validationError.value
    for (const entry of toolbelt) {
      const reason = entryReason(entry)
      if (reason) return reason
    }
    return null
  }

  function hasIssues(toolbelt: ToolbeltEntry[]): boolean {
    return !!firstReason(toolbelt)
  }

  function isAssignableConnection(conn: Connection): boolean {
    if (matrixConnectionIDs.value.size === 0) return true
    return matrixConnectionIDs.value.has(conn.id)
  }

  function connectionBlockedReason(conn: Connection): string | null {
    if (isAssignableConnection(conn)) return null
    return 'Connection is unavailable in current capability matrix.'
  }

  async function refreshMatrix() {
    matrixLoading.value = true
    matrixError.value = ''
    try {
      const matrix = await api.agents.capabilityMatrix()
      matrixConnectionIDs.value = new Set((matrix.connections || []).map(c => c.connection_id))
    } catch (err) {
      matrixConnectionIDs.value = new Set()
      matrixError.value = err instanceof Error ? err.message : 'Failed to load capability matrix.'
    } finally {
      matrixLoading.value = false
    }
  }

  async function validateToolbelt(toolbelt: ToolbeltEntry[]): Promise<boolean> {
    resetValidation()

    // System entries are validated by runtime/tool execution rules, not by
    // connection capability matrix.
    const managedEntries = toolbelt.filter(e => !isSystemEntry(e))
    if (managedEntries.length === 0) return true

    try {
      validation.value = await api.agents.validateCapabilityMatrix(managedEntries)
      return !!validation.value.valid
    } catch (err) {
      validationError.value = err instanceof Error ? err.message : 'Capability validation failed.'
      return false
    }
  }

  return {
    matrixLoading,
    matrixError,
    validation,
    validationError,
    refreshMatrix,
    resetValidation,
    validateToolbelt,
    entryReason,
    firstReason,
    hasIssues,
    isAssignableConnection,
    connectionBlockedReason,
  }
}
