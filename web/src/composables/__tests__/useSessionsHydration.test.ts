import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'

// Mock useApi before importing useSessions
vi.mock('../useApi', () => ({
  api: {
    sessions: {
      list:         vi.fn(),
      create:       vi.fn(),
      rename:       vi.fn(),
      getMessages:  vi.fn(),
    },
  },
  getToken: vi.fn().mockReturnValue('test-token'),
}))

// Import after mock is set up
import { useSessions } from '../useSessions'
import { api } from '../useApi'

describe('useSessions - Hydration & Message Fetching', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset module-level shared state between tests
    const { sessions, loading } = useSessions()
    sessions.value = []
    loading.value = false
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ── queueIfHydrating: returns false when not hydrating ──────────────────────
  describe('queueIfHydrating - not hydrating', () => {
    it('returns false when session is not in preHydrationQueue', async () => {
      const { queueIfHydrating } = useSessions()
      const handler = vi.fn()

      const queued = queueIfHydrating('sess-1', handler)

      expect(queued).toBe(false)
      expect(handler).not.toHaveBeenCalled()
    })
  })

  // ── queueIfHydrating: buffers handlers during hydration ─────────────────────
  describe('queueIfHydrating - while hydrating', () => {
    it('returns true and queues handler when session is hydrating', async () => {
      const { queueIfHydrating, fetchMessages } = useSessions()

      // Start a fetch (which sets preHydrationQueue)
      vi.mocked(api.sessions.getMessages).mockImplementationOnce(
        () => new Promise(resolve => setTimeout(() => resolve([]), 50))
      )

      const fetchPromise = fetchMessages('sess-2')

      // Immediately try to queue a handler while fetch is in flight
      const handler = vi.fn()
      const queued = queueIfHydrating('sess-2', handler)

      expect(queued).toBe(true)
      expect(handler).not.toHaveBeenCalled() // not called yet

      // Wait for fetch to complete
      await fetchPromise
      await flushPromises()

      // Handler should have been called after fetch completes
      expect(handler).toHaveBeenCalled()
    })

    it('queues multiple handlers in order', async () => {
      const { queueIfHydrating, fetchMessages } = useSessions()
      const calls: string[] = []

      vi.mocked(api.sessions.getMessages).mockImplementationOnce(
        () => new Promise(resolve => setTimeout(() => resolve([]), 50))
      )

      const fetchPromise = fetchMessages('sess-3')

      // Queue multiple handlers
      queueIfHydrating('sess-3', () => calls.push('first'))
      queueIfHydrating('sess-3', () => calls.push('second'))
      queueIfHydrating('sess-3', () => calls.push('third'))

      await fetchPromise
      await flushPromises()

      expect(calls).toEqual(['first', 'second', 'third'])
    })
  })

  // ── fetchMessages: loads from server and marks as hydrated ─────────────────
  describe('fetchMessages - successful fetch', () => {
    it('loads message history and marks session as hydrated', async () => {
      const { fetchMessages, getMessages } = useSessions()

      const fixture = [
        { id: 'msg-1', role: 'user', content: 'hello', type: 'text' },
        { id: 'msg-2', role: 'assistant', content: 'hi', type: 'text' },
      ]
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)

      await fetchMessages('sess-4')

      const msgs = getMessages('sess-4')
      expect(msgs).toHaveLength(2)
      expect(msgs[0].content).toBe('hello')
      expect(msgs[1].content).toBe('hi')
    })

    it('filters out non-chat messages (cost messages)', async () => {
      const { fetchMessages, getMessages } = useSessions()

      const fixture = [
        { id: 'msg-1', role: 'user', content: 'hello', type: 'text' },
        { id: 'msg-2', role: 'assistant', content: 'cost data', type: 'cost' }, // filtered
        { id: 'msg-3', role: 'assistant', content: 'hi', type: 'text' },
      ]
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)

      await fetchMessages('sess-5')

      const msgs = getMessages('sess-5')
      expect(msgs).toHaveLength(2)
      expect(msgs.map(m => m.content)).toEqual(['hello', 'hi'])
    })

    it('handles empty message history gracefully', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([])

      await fetchMessages('sess-6')

      const msgs = getMessages('sess-6')
      expect(msgs).toHaveLength(0)
    })

    it('handles fetch error gracefully (still marks hydrated)', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockRejectedValueOnce(new Error('network error'))

      await expect(fetchMessages('sess-7')).resolves.not.toThrow()

      const msgs = getMessages('sess-7')
      expect(msgs).toHaveLength(0) // empty array, session marked hydrated
    })
  })

  // ── fetchMessages: prevents double-fetch ────────────────────────────────────
  describe('fetchMessages - double-fetch prevention', () => {
    it('returns early if already hydrated', async () => {
      const { fetchMessages } = useSessions()

      const fixture = [
        { id: 'msg-1', role: 'user', content: 'hello', type: 'text' },
      ]
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)

      // First fetch
      await fetchMessages('sess-8')
      expect(vi.mocked(api.sessions.getMessages)).toHaveBeenCalledTimes(1)

      // Second fetch should not hit API
      await fetchMessages('sess-8')
      expect(vi.mocked(api.sessions.getMessages)).toHaveBeenCalledTimes(1)
    })

    it('does not fetch if already in flight', async () => {
      const { fetchMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockImplementationOnce(
        () => new Promise(resolve => setTimeout(() => resolve([]), 100))
      )

      // Fire two concurrent fetches
      const p1 = fetchMessages('sess-9')
      const p2 = fetchMessages('sess-9')

      await Promise.all([p1, p2])

      // API should only be called once (second returned early)
      expect(vi.mocked(api.sessions.getMessages)).toHaveBeenCalledTimes(1)
    })

    it('skips fetch for empty session ID', async () => {
      const { fetchMessages } = useSessions()

      await fetchMessages('')

      expect(vi.mocked(api.sessions.getMessages)).not.toHaveBeenCalled()
    })
  })

  // ── deleteSession: clears hydration state ─────────────────────────────────
  describe('deleteSession - hydration cleanup', () => {
    it('clears hydration cache and preHydrationQueue when session deleted', async () => {
      const { fetchMessages, deleteSession } = useSessions()

      const fixture = [
        { id: 'msg-1', role: 'user', content: 'hello', type: 'text' },
      ]
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)

      // Fetch and hydrate
      await fetchMessages('sess-10')

      // Verify it's hydrated by checking that a second fetch returns early
      const apiCallsBefore = vi.mocked(api.sessions.getMessages).mock.calls.length
      await fetchMessages('sess-10')
      expect(vi.mocked(api.sessions.getMessages).mock.calls.length).toBe(apiCallsBefore)

      // Delete session
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 200 }))
      await deleteSession('sess-10')

      // Now a new fetch should hit the API
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)
      await fetchMessages('sess-10')
      expect(vi.mocked(api.sessions.getMessages).mock.calls.length).toBe(2)
    })

    it('discards buffered handlers when session deleted', async () => {
      const { fetchMessages, deleteSession, queueIfHydrating } = useSessions()

      vi.mocked(api.sessions.getMessages).mockImplementationOnce(
        () => new Promise(resolve => setTimeout(() => resolve([]), 100))
      )

      const fetchPromise = fetchMessages('sess-11')

      // Queue a handler while hydrating
      const handler = vi.fn()
      queueIfHydrating('sess-11', handler)

      // Delete session before fetch completes
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response('', { status: 200 }))
      await deleteSession('sess-11')

      // Wait for pending fetch
      await fetchPromise
      await flushPromises()

      // Handler should NOT have been called (queue was discarded)
      expect(handler).not.toHaveBeenCalled()
    })
  })

  // ── Integration: WS event buffering during hydration ─────────────────────────
  describe('Integration - WS event buffering', () => {
    it('buffers WS events while hydrating, then flushes on completion', async () => {
      const { fetchMessages, queueIfHydrating } = useSessions()
      const events: string[] = []

      // Slow fetch to allow queueing
      vi.mocked(api.sessions.getMessages).mockImplementationOnce(
        () => new Promise(resolve => setTimeout(() => resolve([]), 50))
      )

      const fetchPromise = fetchMessages('sess-12')

      // Simulate WS events arriving during hydration
      queueIfHydrating('sess-12', () => events.push('ws-msg-1'))
      queueIfHydrating('sess-12', () => events.push('ws-msg-2'))

      // Events not flushed yet
      expect(events).toEqual([])

      // Wait for hydration to complete
      await fetchPromise
      await flushPromises()

      // Events should be flushed in order
      expect(events).toEqual(['ws-msg-1', 'ws-msg-2'])
    })

    it('handles immediate WS events after hydration completes', async () => {
      const { fetchMessages, queueIfHydrating } = useSessions()
      const events: string[] = []

      const fixture = [
        { id: 'msg-1', role: 'user', content: 'hello', type: 'text' },
      ]
      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce(fixture)

      await fetchMessages('sess-13')

      // After hydration, queueIfHydrating should return false (not queued)
      const queued = queueIfHydrating('sess-13', () => events.push('ws-msg-after'))

      expect(queued).toBe(false)
      expect(events).toEqual([]) // handler not called by queueIfHydrating

      // Caller is responsible for handling the message immediately
    })
  })

  // ── fetchMessages: agent + createdAt field mapping ────────────────────────
  // Regression: fetchMessages must map agent and ts fields from the API response
  // so per-message attribution (AgentMessageHeader) works after page reload.
  describe('fetchMessages - agent and createdAt field mapping', () => {
    it('maps agent field from API response to message.agent', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([
        { id: 'msg-1', role: 'assistant', content: 'hello', agent: 'Sam', ts: '2026-03-19T10:00:00Z' },
      ] as never)

      await fetchMessages('sess-agent-map')
      await flushPromises()

      const msgs = getMessages('sess-agent-map')
      expect(msgs).toHaveLength(1)
      expect(msgs[0].agent).toBe('Sam')
    })

    it('maps ts field from API response to message.createdAt', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([
        { id: 'msg-2', role: 'assistant', content: 'hi', agent: 'Tom', ts: '2026-03-19T12:30:00Z' },
      ] as never)

      await fetchMessages('sess-createdat-map')
      await flushPromises()

      const msgs = getMessages('sess-createdat-map')
      expect(msgs[0].createdAt).toBe('2026-03-19T12:30:00Z')
    })

    it('leaves agent undefined when API response has no agent field', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([
        { id: 'msg-3', role: 'user', content: 'question', ts: '2026-03-19T09:00:00Z' },
      ] as never)

      await fetchMessages('sess-no-agent')
      await flushPromises()

      const msgs = getMessages('sess-no-agent')
      expect(msgs[0].agent).toBeUndefined()
    })

    it('leaves createdAt undefined when API response has no ts field', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([
        { id: 'msg-4', role: 'assistant', content: 'answer' },
      ] as never)

      await fetchMessages('sess-no-ts')
      await flushPromises()

      const msgs = getMessages('sess-no-ts')
      expect(msgs[0].createdAt).toBeUndefined()
    })

    it('filters out cost-type messages when mapping', async () => {
      const { fetchMessages, getMessages } = useSessions()

      vi.mocked(api.sessions.getMessages).mockResolvedValueOnce([
        { id: 'msg-5', role: 'assistant', content: 'real', agent: 'Sam', ts: '2026-03-19T10:00:00Z' },
        { id: 'cost-1', role: 'assistant', content: '0.01', type: 'cost', ts: '2026-03-19T10:00:01Z' },
      ] as never)

      await fetchMessages('sess-filter-cost')
      await flushPromises()

      const msgs = getMessages('sess-filter-cost')
      expect(msgs).toHaveLength(1)
      expect(msgs[0].id).toBe('msg-5')
    })
  })
})
