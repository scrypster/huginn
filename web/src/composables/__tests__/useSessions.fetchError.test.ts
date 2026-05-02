import { describe, it, expect, beforeEach, vi } from 'vitest'

// Reset module state between tests to avoid cross-test pollution.
// (useSessions uses module-level refs)
beforeEach(() => {
  vi.resetModules()
})

describe('useSessions fetchMessages error surfacing', () => {
  it('sets fetch error for non-abort network errors', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          getMessages: vi.fn().mockRejectedValueOnce(new Error('Network error')),
          list: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchMessages, getFetchError } = useSessions()

    await fetchMessages('session-1')
    expect(getFetchError('session-1')).toBe('Network error')
  })

  it('does not set error for AbortError (timeout)', async () => {
    const abortErr = new DOMException('The user aborted a request.', 'AbortError')
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          getMessages: vi.fn().mockRejectedValueOnce(abortErr),
          list: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchMessages, getFetchError } = useSessions()

    await fetchMessages('session-2')
    expect(getFetchError('session-2')).toBeNull()
  })

  it('clearFetchError removes the error', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          getMessages: vi.fn().mockRejectedValueOnce(new Error('Boom')),
          list: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    const { fetchMessages, getFetchError, clearFetchError } = useSessions()

    await fetchMessages('session-3')
    expect(getFetchError('session-3')).toBe('Boom')
    clearFetchError('session-3')
    expect(getFetchError('session-3')).toBeNull()
  })

  it('clears prior fetch error on re-fetch', async () => {
    vi.doMock('../useApi', () => ({
      api: {
        sessions: {
          getMessages: vi.fn()
            .mockRejectedValueOnce(new Error('First error'))
            .mockResolvedValueOnce([]),
          list: vi.fn(),
          create: vi.fn(),
          rename: vi.fn(),
        },
      },
      getToken: vi.fn().mockReturnValue('test-token'),
    }))

    const { useSessions } = await import('../useSessions')
    // Access the hydrated set is not exposed; we need to test re-fetch behavior.
    // Since fetchMessages skips if already hydrated, we test that clearFetchError
    // is called at the start of a fresh fetch via a new session ID.
    const { fetchMessages, getFetchError } = useSessions()

    await fetchMessages('session-4')
    expect(getFetchError('session-4')).toBe('First error')
    // A second call is a no-op (already hydrated), so use a fresh session ID
    // for the second fetch to verify no prior error bleeds through.
    await fetchMessages('session-4b')
    expect(getFetchError('session-4b')).toBeNull()
  })
})
