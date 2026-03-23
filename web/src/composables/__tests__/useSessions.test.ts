import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'

// Mock useApi before importing useSessions
vi.mock('../useApi', () => ({
  api: {
    sessions: {
      list:   vi.fn(),
      create: vi.fn(),
      rename: vi.fn(),
    },
  },
  getToken: vi.fn().mockReturnValue('test-token'),
}))

// Import after mock is set up
import { useSessions } from '../useSessions'
import { api } from '../useApi'

describe('useSessions', () => {
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

  // ── fetchSessions ──────────────────────────────────────────────────────────
  describe('fetchSessions', () => {
    it('populates sessions from array response', async () => {
      const fixture = [
        { id: 's1', agent_id: 'default', state: 'idle', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      ]
      vi.mocked(api.sessions.list).mockResolvedValueOnce(fixture as never)

      const { fetchSessions, sessions } = useSessions()
      await fetchSessions()

      expect(sessions.value).toHaveLength(1)
      expect(sessions.value[0].id).toBe('s1')
    })

    it('populates sessions from { sessions: [...] } response', async () => {
      const fixture = [
        { id: 's2', agent_id: 'default', state: 'idle', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
      ]
      vi.mocked(api.sessions.list).mockResolvedValueOnce({ sessions: fixture } as never)

      const { fetchSessions, sessions } = useSessions()
      await fetchSessions()

      expect(sessions.value[0].id).toBe('s2')
    })

    it('does not throw when api call fails', async () => {
      vi.mocked(api.sessions.list).mockRejectedValueOnce(new Error('network error'))

      const { fetchSessions, sessions } = useSessions()
      await expect(fetchSessions()).resolves.not.toThrow()
      expect(sessions.value).toHaveLength(0)
    })

    it('resets loading to false after success', async () => {
      vi.mocked(api.sessions.list).mockResolvedValueOnce([])
      const { fetchSessions, loading } = useSessions()
      await fetchSessions()
      expect(loading.value).toBe(false)
    })

    it('resets loading to false after failure', async () => {
      vi.mocked(api.sessions.list).mockRejectedValueOnce(new Error('oops'))
      const { fetchSessions, loading } = useSessions()
      await fetchSessions()
      expect(loading.value).toBe(false)
    })
  })

  // ── createSession ──────────────────────────────────────────────────────────
  describe('createSession', () => {
    it('returns a session with id from session_id field', async () => {
      vi.mocked(api.sessions.create).mockResolvedValueOnce({ session_id: 'new-session-abc' } as never)

      const { createSession } = useSessions()
      const session = await createSession()

      expect(session.id).toBe('new-session-abc')
      expect(session.agent_id).toBe('default')
      expect(session.state).toBe('idle')
    })

    it('returns a session with id from id field when session_id is absent', async () => {
      vi.mocked(api.sessions.create).mockResolvedValueOnce({ id: 'sess-xyz' } as never)

      const { createSession } = useSessions()
      const session = await createSession()

      expect(session.id).toBe('sess-xyz')
    })

    it('prepends new session to sessions list', async () => {
      const existing = { id: 'old', agent_id: 'default', state: 'idle', created_at: '', updated_at: '' }
      const { sessions, createSession } = useSessions()
      sessions.value = [existing]

      vi.mocked(api.sessions.create).mockResolvedValueOnce({ session_id: 'new-one' } as never)
      await createSession()

      expect(sessions.value[0].id).toBe('new-one')
      expect(sessions.value[1].id).toBe('old')
    })
  })

  // ── renameSession ──────────────────────────────────────────────────────────
  describe('renameSession', () => {
    it('updates the session title optimistically', async () => {
      vi.mocked(api.sessions.rename).mockResolvedValueOnce(undefined as never)

      const { sessions, renameSession } = useSessions()
      sessions.value = [{ id: 's1', agent_id: 'default', state: 'idle', created_at: '', updated_at: '', title: 'Old Title' }]

      await renameSession('s1', 'New Title')

      expect(sessions.value[0].title).toBe('New Title')
    })

    it('reverts to previous title when api call fails', async () => {
      vi.mocked(api.sessions.rename).mockRejectedValueOnce(new Error('server error'))

      const { sessions, renameSession } = useSessions()
      sessions.value = [{ id: 's1', agent_id: 'default', state: 'idle', created_at: '', updated_at: '', title: 'Original' }]

      await renameSession('s1', 'Failed Title')

      expect(sessions.value[0].title).toBe('Original')
    })

    it('does nothing silently when session id not found', async () => {
      vi.mocked(api.sessions.rename).mockResolvedValueOnce(undefined as never)

      const { sessions, renameSession } = useSessions()
      sessions.value = []

      // Should not throw
      await expect(renameSession('nonexistent', 'X')).resolves.not.toThrow()
    })
  })

  // ── getMessages ────────────────────────────────────────────────────────────
  describe('getMessages', () => {
    it('returns empty array for new session', () => {
      const { getMessages } = useSessions()
      const msgs = getMessages('brand-new-session')
      expect(msgs).toEqual([])
    })

    it('returns the same array reference on subsequent calls', () => {
      const { getMessages } = useSessions()
      const a = getMessages('sess-1')
      const b = getMessages('sess-1')
      expect(a).toBe(b)
    })
  })

  // ── formatSessionLabel ─────────────────────────────────────────────────────
  describe('formatSessionLabel', () => {
    it('returns title when set', () => {
      const { formatSessionLabel } = useSessions()
      const label = formatSessionLabel({ id: 's1', agent_id: 'a', state: 'idle', created_at: '', updated_at: '', title: 'My Chat' })
      expect(label).toBe('My Chat')
    })

    it('returns formatted date when no title', () => {
      const { formatSessionLabel } = useSessions()
      const label = formatSessionLabel({ id: 's1', agent_id: 'a', state: 'idle', created_at: '2026-03-10T14:30:00Z', updated_at: '' })
      // Should be a non-empty human-readable string (locale-dependent)
      expect(label.length).toBeGreaterThan(0)
      expect(label).not.toBe('s1'.slice(0, 8))
    })

    it('returns id slice when created_at is invalid', () => {
      const { formatSessionLabel } = useSessions()
      const label = formatSessionLabel({ id: 'abcdefgh-xyz', agent_id: 'a', state: 'idle', created_at: 'not-a-date', updated_at: '' })
      expect(label).toBe('abcdefgh')
    })
  })

  // ── deleteSession ───────────────────────────────────────────────────────────

  describe('deleteSession', () => {
    it('sends DELETE to /api/v1/sessions/:id, removes session and cleans up messages', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({}), { status: 200 })
      )
      const { sessions, deleteSession, getMessages } = useSessions()
      sessions.value = [{ id: 'sess-del-1', title: 'Test', agent_id: 'a', state: 'idle', created_at: new Date().toISOString(), updated_at: new Date().toISOString() }] as any
      getMessages('sess-del-1').push({ id: 'm1', role: 'user', content: 'hi', streaming: false } as any)
      await deleteSession('sess-del-1')
      expect(sessions.value).toHaveLength(0)
      expect(getMessages('sess-del-1')).toHaveLength(0)
    })

    it('still removes session and messages even when fetch throws', async () => {
      vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
      const { sessions, deleteSession, getMessages } = useSessions()
      sessions.value = [{ id: 'sess-del-2', title: 'Test', agent_id: 'a', state: 'idle', created_at: new Date().toISOString(), updated_at: new Date().toISOString() }] as any
      getMessages('sess-del-2').push({ id: 'm1', role: 'user', content: 'hi', streaming: false } as any)
      await deleteSession('sess-del-2')
      expect(sessions.value).toHaveLength(0)
      expect(getMessages('sess-del-2')).toHaveLength(0)
    })
  })
})
