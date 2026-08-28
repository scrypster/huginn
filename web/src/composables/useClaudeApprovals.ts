import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { apiFetch } from './useApi'

export interface ClaudeApproval {
  id: string
  agent_name: string
  tool_name: string
  summary: string
  excerpt: string
  cwd: string
  /** Remaining time computed server-side. Never an absolute timestamp: client
   *  clock skew must not be able to make a card look expired when it is not. */
  remaining_ms: number
  /** True only for Bash — the one gated tool with a stable identifying
   *  argument, so the only one that can carry exact-command memory. */
  can_remember: boolean
}

export type ApprovalDecision = 'allow' | 'deny' | 'allow_command' | 'allow_tool'

// Module-level singletons so the nav badge and the chat cards read the same
// list. A second reactive copy is how badge counts drift from what they count
// — see unseenSessions.ts for the bug that behaviour caused.
const approvals: Ref<ClaudeApproval[]> = ref([])

export function useClaudeApprovals() {
  /**
   * refresh pulls the authoritative pending set and REPLACES the local list.
   * It never merges: a card resolved while this client was disconnected must
   * disappear, and merging would keep it forever.
   */
  async function refresh(): Promise<void> {
    try {
      const body = await apiFetch<{ approvals?: ClaudeApproval[] }>('/api/v1/claude/approvals')
      approvals.value = Array.isArray(body?.approvals) ? body.approvals : []
    } catch {
      // Keep the last known list on a transient failure rather than blanking
      // the UI. The server stays authoritative on the next success.
    }
  }

  async function decide(id: string, decision: ApprovalDecision): Promise<void> {
    try {
      await apiFetch('/api/v1/claude/approve/decide', {
        method: 'POST',
        body: JSON.stringify({ id, decision }),
      })
    } catch {
      // Swallow: a failed decide (e.g. a transient network drop) must not
      // throw into the caller. The refresh below re-syncs with the server's
      // authoritative state either way.
    } finally {
      await refresh()
    }
  }

  /**
   * handleApprovalsChanged responds to the `claude_approvals_changed` websocket
   * message. That message is a HINT with no payload — the server's list is the
   * data — so any hint heals drift left by an earlier dropped broadcast.
   */
  function handleApprovalsChanged(): void {
    void refresh()
  }

  const pendingCount: ComputedRef<number> = computed(() => approvals.value.length)

  /**
   * approvalsFor returns the cards belonging to one agent's conversation.
   *
   * With no agent selected yet it returns EVERY approval rather than none: an
   * over-strict filter makes a card invisible, and an invisible card silently
   * ages out to a deny with no human ever seeing it. Misplaced beats invisible.
   * This fallback lives here, in the one exported filter, so no caller has to
   * remember to reimplement it.
   */
  function approvalsFor(agentName: string | null | undefined): ClaudeApproval[] {
    if (!agentName) return approvals.value
    return approvals.value.filter(a => a.agent_name === agentName)
  }

  return { approvals, pendingCount, approvalsFor, refresh, decide, handleApprovalsChanged }
}
