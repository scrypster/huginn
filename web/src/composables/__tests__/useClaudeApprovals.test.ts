/**
 * useClaudeApprovals tests
 *
 * Mocks the useApi module's apiFetch (not globalThis.fetch) because the
 * composable must go through apiFetch to attach the bearer token — both
 * endpoints it calls live inside the server's authenticated api(...) wrapper.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('../useApi', () => ({ apiFetch: vi.fn() }))

import { apiFetch } from '../useApi'
import { useClaudeApprovals } from '../useClaudeApprovals'

const mockApiFetch = vi.mocked(apiFetch)

function card(over: Partial<any> = {}) {
  return {
    id: 'a1', agent_name: 'codey', tool_name: 'Bash',
    summary: 'go test ./...', excerpt: '', cwd: '/tmp',
    remaining_ms: 285000, can_remember: true, ...over,
  }
}

beforeEach(async () => {
  mockApiFetch.mockReset()
  // `approvals` is a module-level singleton by design, so state leaks between
  // tests unless each one starts from a known-empty list.
  mockApiFetch.mockResolvedValueOnce({ approvals: [] })
  await useClaudeApprovals().refresh()
  mockApiFetch.mockReset()
})

describe('useClaudeApprovals', () => {
  it('refresh replaces the list wholesale rather than merging', async () => {
    const { approvals, refresh } = useClaudeApprovals()
    mockApiFetch
      .mockResolvedValueOnce({ approvals: [card(), card({ id: 'a2' })] })
      .mockResolvedValueOnce({ approvals: [card({ id: 'a2' })] })
    await refresh()
    expect(approvals.value.map(a => a.id)).toEqual(['a1', 'a2'])
    await refresh()
    // a1 resolved server-side while we were away: it must disappear, which a
    // merge would not do.
    expect(approvals.value.map(a => a.id)).toEqual(['a2'])
  })

  it('a card resolved while disconnected leaves the count at zero', async () => {
    const { approvals, pendingCount, refresh } = useClaudeApprovals()
    mockApiFetch
      .mockResolvedValueOnce({ approvals: [card()] })
      .mockResolvedValueOnce({ approvals: [] })
    await refresh()
    expect(pendingCount.value).toBe(1)
    await refresh()
    expect(pendingCount.value).toBe(0)
    expect(approvals.value).toEqual([])
  })

  it('a failed refresh does not strand a stale list', async () => {
    const { approvals, refresh } = useClaudeApprovals()
    mockApiFetch
      .mockResolvedValueOnce({ approvals: [card()] })
      .mockRejectedValueOnce(new Error('offline'))
    await refresh()
    expect(approvals.value).toHaveLength(1)
    await refresh()
    // Keep the last known list on a transient failure rather than blanking the
    // UI; the server remains authoritative on the next success.
    expect(approvals.value).toHaveLength(1)
  })

  it('approvalsFor filters by agent', async () => {
    const { approvalsFor, refresh } = useClaudeApprovals()
    mockApiFetch.mockResolvedValue({
      approvals: [card(), card({ id: 'a2', agent_name: 'other' })],
    })
    await refresh()
    expect(approvalsFor('codey').map(a => a.id)).toEqual(['a1'])
    expect(approvalsFor('other').map(a => a.id)).toEqual(['a2'])
  })

  it('approvalsFor shows EVERY approval when no agent is selected', async () => {
    // Misplaced beats invisible: an over-strict filter hides a card, and a
    // hidden card silently ages out to a deny with no human ever seeing it.
    // This fallback used to live inline in ChatView while the exported helper
    // went uncalled — a green test for a function nothing used.
    const { approvalsFor, refresh } = useClaudeApprovals()
    mockApiFetch.mockResolvedValue({
      approvals: [card(), card({ id: 'a2', agent_name: 'other' })],
    })
    await refresh()
    expect(approvalsFor('').map(a => a.id)).toEqual(['a1', 'a2'])
    expect(approvalsFor(undefined).map(a => a.id)).toEqual(['a1', 'a2'])
    expect(approvalsFor(null).map(a => a.id)).toEqual(['a1', 'a2'])
  })

  it('decide posts the decision and refreshes', async () => {
    const { decide } = useClaudeApprovals()
    mockApiFetch
      .mockResolvedValueOnce({ status: 'ok' })
      .mockResolvedValueOnce({ approvals: [] })
    await decide('a1', 'allow')
    const [path, opts] = mockApiFetch.mock.calls[0]
    expect(String(path)).toContain('/api/v1/claude/approve/decide')
    expect((opts as RequestInit).method).toBe('POST')
    expect(JSON.parse((opts as RequestInit).body as string)).toEqual({ id: 'a1', decision: 'allow' })
    expect(mockApiFetch).toHaveBeenCalledTimes(2)
  })

  it('decide still refreshes when the POST rejects', async () => {
    const { decide } = useClaudeApprovals()
    mockApiFetch
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ approvals: [] })
    await expect(decide('a1', 'allow')).resolves.toBeUndefined()
    expect(mockApiFetch).toHaveBeenCalledTimes(2)
  })

  it('handleApprovalsChanged triggers a refresh', async () => {
    const { handleApprovalsChanged } = useClaudeApprovals()
    mockApiFetch.mockResolvedValue({ approvals: [] })
    handleApprovalsChanged()
    await Promise.resolve()
    expect(mockApiFetch).toHaveBeenCalled()
  })
})
