import { describe, it, expect, beforeEach, vi } from 'vitest'

// Reset module state between tests to avoid cross-test pollution.
// (useSessions uses module-level refs)
beforeEach(() => {
  vi.resetModules()
})

describe('useSessions fetchSessions error surfacing', () => {
  it('sets fetchSessionsError when API throws', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          list: vi.fn().mockRejectedValueOnce(new Error('Network error')),
          getMessages: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchSessions, fetchSessionsError } = useSessions()

    await fetchSessions()
    expect(fetchSessionsError.value).toBe('Network error')
  })

  it('clears fetchSessionsError on retry', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          list: vi.fn()
            .mockRejectedValueOnce(new Error('First failure'))
            .mockResolvedValueOnce([]),
          getMessages: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchSessions, fetchSessionsError } = useSessions()

    await fetchSessions()
    expect(fetchSessionsError.value).toBe('First failure')

    await fetchSessions()
    expect(fetchSessionsError.value).toBeNull()
  })

  it('fetchSessionsError is null after a successful fetch', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          list: vi.fn().mockResolvedValueOnce([]),
          getMessages: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchSessions, fetchSessionsError } = useSessions()

    await fetchSessions()
    expect(fetchSessionsError.value).toBeNull()
  })
})
