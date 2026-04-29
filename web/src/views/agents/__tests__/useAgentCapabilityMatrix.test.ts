import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useAgentCapabilityMatrix } from '../useAgentCapabilityMatrix'

const {
  mockCapabilityMatrix,
  mockValidateCapabilityMatrix,
} = vi.hoisted(() => ({
  mockCapabilityMatrix: vi.fn(),
  mockValidateCapabilityMatrix: vi.fn(),
}))

vi.mock('../../../composables/useApi', () => ({
  api: {
    agents: {
      capabilityMatrix: mockCapabilityMatrix,
      validateCapabilityMatrix: mockValidateCapabilityMatrix,
    },
  },
}))

describe('useAgentCapabilityMatrix', () => {
  beforeEach(() => {
    mockCapabilityMatrix.mockReset()
    mockValidateCapabilityMatrix.mockReset()
  })

  it('refreshMatrix loads allowed connection IDs', async () => {
    mockCapabilityMatrix.mockResolvedValueOnce({
      connections: [{ connection_id: 'conn-gh', provider: 'github' }],
      providers: [],
    })
    const state = useAgentCapabilityMatrix()
    await state.refreshMatrix()

    expect(state.isAssignableConnection({ id: 'conn-gh' } as any)).toBe(true)
    expect(state.isAssignableConnection({ id: 'conn-slack' } as any)).toBe(false)
    expect(state.connectionBlockedReason({ id: 'conn-slack' } as any)).toContain('unavailable')
  })

  it('validateToolbelt maps deny reason for invalid assignments', async () => {
    mockValidateCapabilityMatrix.mockResolvedValueOnce({
      valid: false,
      decisions: [
        {
          entry: { connection_id: 'stale', provider: 'github', approval_gate: false },
          allowed: false,
          reason_code: 'unknown_connection_id',
          reason: 'connection_id "stale" is not available',
        },
      ],
    })
    const state = useAgentCapabilityMatrix()
    const ok = await state.validateToolbelt([
      { connection_id: 'stale', provider: 'github', approval_gate: false },
    ])

    expect(ok).toBe(false)
    expect(state.firstReason([{ connection_id: 'stale', provider: 'github', approval_gate: false }]))
      .toContain('Connection is no longer available')
  })

  it('validateToolbelt bypasses system entries', async () => {
    const state = useAgentCapabilityMatrix()
    const ok = await state.validateToolbelt([
      { connection_id: 'system:github', provider: 'github_cli', approval_gate: false },
    ])
    expect(ok).toBe(true)
    expect(mockValidateCapabilityMatrix).not.toHaveBeenCalled()
  })

  it('hasIssues detects wildcard assignment locally', () => {
    const state = useAgentCapabilityMatrix()
    expect(state.hasIssues([{ connection_id: '*', provider: '*', approval_gate: false }])).toBe(true)
    expect(state.firstReason([{ connection_id: '*', provider: '*', approval_gate: false }]))
      .toContain('Wildcard provider assignment is not allowed')
  })
})
