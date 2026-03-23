import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// ── Helpers to get a fresh module instance between tests ─────────────────────

async function freshUseThreads() {
  vi.resetModules()
  const mod = await import('../useThreads')
  return { useThreads: mod.useThreads, isRunning: mod.isRunning, TERMINAL_STATUSES: mod.TERMINAL_STATUSES }
}

// ── Sample REST thread data ───────────────────────────────────────────────────

function makeThread(overrides: Partial<import('../useThreads').LiveThread> = {}): import('../useThreads').LiveThread {
  return {
    ID: 'thread-001',
    SessionID: 'sess-abc',
    AgentID: 'agent-1',
    Task: 'Refactor module',
    Status: 'thinking',
    StartedAt: new Date().toISOString(),
    CompletedAt: '',
    TokensUsed: 0,
    TokenBudget: 1000,
    streamingContent: '',
    toolCalls: [],
    elapsedMs: 0,
    ...overrides,
  }
}

// ── Mock API for thread loading ───────────────────────────────────────────────

const mockThreadsList = vi.fn()

vi.mock('../useApi', () => ({
  api: {
    threads: {
      list: (...args: unknown[]) => mockThreadsList(...args),
    },
  },
}))

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('useThreads — isRunning', () => {
  it('returns true for "thinking" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'thinking' }))).toBe(true)
  })

  it('returns true for "tooling" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'tooling' }))).toBe(true)
  })

  it('returns true for "queued" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'queued' }))).toBe(true)
  })

  it('returns false for "done" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'done' }))).toBe(false)
  })

  it('returns false for "cancelled" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'cancelled' }))).toBe(false)
  })

  it('returns false for "error" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'error' }))).toBe(false)
  })

  it('returns false for "completed" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'completed' }))).toBe(false)
  })

  it('returns false for "completed-with-timeout" status', async () => {
    const { isRunning } = await freshUseThreads()
    expect(isRunning(makeThread({ Status: 'completed-with-timeout' }))).toBe(false)
  })
})

describe('useThreads — getSessionThreads / hasThreads / getActiveThreadCount', () => {
  it('getSessionThreads returns empty array for unknown session', async () => {
    const { useThreads } = await freshUseThreads()
    const { getSessionThreads } = useThreads()
    expect(getSessionThreads('sess-unknown')).toEqual([])
  })

  it('hasThreads returns false for unknown session', async () => {
    const { useThreads } = await freshUseThreads()
    const { hasThreads } = useThreads()
    expect(hasThreads('sess-unknown')).toBe(false)
  })

  it('getActiveThreadCount returns 0 for unknown session', async () => {
    const { useThreads } = await freshUseThreads()
    const { getActiveThreadCount } = useThreads()
    expect(getActiveThreadCount('sess-unknown')).toBe(0)
  })
})

describe('useThreads — loadThreads', () => {
  beforeEach(() => {
    mockThreadsList.mockReset()
  })

  it('does nothing if sessionId is empty string', async () => {
    const { useThreads } = await freshUseThreads()
    const { loadThreads } = useThreads()
    await loadThreads('')
    expect(mockThreadsList).not.toHaveBeenCalled()
  })

  it('calls api.threads.list with the sessionId', async () => {
    mockThreadsList.mockResolvedValue([])
    const { useThreads } = await freshUseThreads()
    const { loadThreads } = useThreads()
    await loadThreads('sess-abc')
    expect(mockThreadsList).toHaveBeenCalledWith('sess-abc')
  })

  it('populates session threads after load', async () => {
    const thread = {
      ID: 'thread-001',
      SessionID: 'sess-abc',
      AgentID: 'agent-1',
      Task: 'Do work',
      Status: 'done',
      StartedAt: new Date().toISOString(),
      CompletedAt: new Date().toISOString(),
      TokensUsed: 100,
      TokenBudget: 1000,
    }
    mockThreadsList.mockResolvedValue([thread])
    const { useThreads } = await freshUseThreads()
    const { loadThreads, getSessionThreads } = useThreads()
    await loadThreads('sess-abc')
    expect(getSessionThreads('sess-abc')).toHaveLength(1)
    expect(getSessionThreads('sess-abc')[0]!.ID).toBe('thread-001')
  })

  it('hasThreads returns true after loading threads', async () => {
    const thread = {
      ID: 'thread-001',
      SessionID: 'sess-abc',
      AgentID: 'agent-1',
      Task: 'Task',
      Status: 'done',
      StartedAt: '',
      CompletedAt: '',
      TokensUsed: 0,
      TokenBudget: 0,
    }
    mockThreadsList.mockResolvedValue([thread])
    const { useThreads } = await freshUseThreads()
    const { loadThreads, hasThreads } = useThreads()
    await loadThreads('sess-abc')
    expect(hasThreads('sess-abc')).toBe(true)
  })

  it('silently ignores API errors during loadThreads', async () => {
    mockThreadsList.mockRejectedValue(new Error('threadmgr not configured'))
    const { useThreads } = await freshUseThreads()
    const { loadThreads, getSessionThreads } = useThreads()
    await expect(loadThreads('sess-abc')).resolves.toBeUndefined()
    expect(getSessionThreads('sess-abc')).toEqual([])
  })
})

describe('useThreads — wireWS', () => {
  it('handles thread_started event and sets status to thinking', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-ws')

    handlers['thread_started']!({
      session_id: 'sess-ws',
      payload: { thread_id: 'thread-ws-1', agent_id: 'agent-1', task: 'Do task' },
    })

    const threads = getSessionThreads('sess-ws')
    expect(threads).toHaveLength(1)
    expect(threads[0]!.Status).toBe('thinking')
    expect(threads[0]!.AgentID).toBe('agent-1')
    expect(threads[0]!.Task).toBe('Do task')
  })

  it('handles thread_token event and accumulates streamingContent', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-ws')

    handlers['thread_token']!({ session_id: 'sess-ws', payload: { thread_id: 'thread-t1', token: 'Hello ' } })
    handlers['thread_token']!({ session_id: 'sess-ws', payload: { thread_id: 'thread-t1', token: 'world' } })

    const threads = getSessionThreads('sess-ws')
    expect(threads[0]!.streamingContent).toBe('Hello world')
  })

  it('handles thread_tool_call event and pushes to toolCalls', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-tc')

    handlers['thread_tool_call']!({
      session_id: 'sess-tc',
      payload: { thread_id: 'thread-tc-1', tool: 'bash', args: { cmd: 'ls' } },
    })

    const threads = getSessionThreads('sess-tc')
    expect(threads[0]!.toolCalls).toHaveLength(1)
    expect(threads[0]!.toolCalls[0]!.tool).toBe('bash')
    expect(threads[0]!.toolCalls[0]!.done).toBe(false)
  })

  it('handles thread_tool_done event and marks last matching tool call done', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-td')

    handlers['thread_tool_call']!({
      session_id: 'sess-td',
      payload: { thread_id: 'thread-td-1', tool: 'read_file', args: { path: '/foo' } },
    })
    handlers['thread_tool_done']!({
      session_id: 'sess-td',
      payload: { thread_id: 'thread-td-1', tool: 'read_file', result_summary: '42 lines read' },
    })

    const threads = getSessionThreads('sess-td')
    expect(threads[0]!.toolCalls[0]!.done).toBe(true)
    expect(threads[0]!.toolCalls[0]!.resultSummary).toBe('42 lines read')
  })

  it('handles thread_done event and sets status and clears streamingContent', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-done')

    // First start a thread with some streaming content
    handlers['thread_token']!({ session_id: 'sess-done', payload: { thread_id: 'thread-d1', token: 'thinking...' } })
    handlers['thread_done']!({
      session_id: 'sess-done',
      payload: { thread_id: 'thread-d1', status: 'done', elapsed_ms: 1234, summary: 'Task completed' },
    })

    const threads = getSessionThreads('sess-done')
    expect(threads[0]!.Status).toBe('done')
    expect(threads[0]!.streamingContent).toBe('')
    expect(threads[0]!.elapsedMs).toBe(1234)
  })

  it('handles thread_help event and sets status to blocked', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (event: string, handler: (msg: unknown) => void) => { handlers[event] = handler } }

    wireWS(ws as any, () => 'sess-help')

    handlers['thread_help']!({
      session_id: 'sess-help',
      payload: { thread_id: 'thread-h1', message: 'Need clarification on requirements' },
    })

    const threads = getSessionThreads('sess-help')
    expect(threads[0]!.Status).toBe('blocked')
    expect(threads[0]!.Summary?.Summary).toBe('Need clarification on requirements')
  })
})

describe('useThreads — clearSession', () => {
  it('removes all threads for a session', async () => {
    mockThreadsList.mockResolvedValue([{
      ID: 'thread-001',
      SessionID: 'sess-clear',
      AgentID: 'a',
      Task: 't',
      Status: 'done',
      StartedAt: '',
      CompletedAt: '',
      TokensUsed: 0,
      TokenBudget: 0,
    }])
    const { useThreads } = await freshUseThreads()
    const { loadThreads, clearSession, getSessionThreads } = useThreads()
    await loadThreads('sess-clear')
    expect(getSessionThreads('sess-clear')).toHaveLength(1)
    clearSession('sess-clear')
    expect(getSessionThreads('sess-clear')).toHaveLength(0)
  })

  it('does nothing for a session that has no threads', async () => {
    const { useThreads } = await freshUseThreads()
    const { clearSession, getSessionThreads } = useThreads()
    expect(() => clearSession('sess-nonexistent')).not.toThrow()
    expect(getSessionThreads('sess-nonexistent')).toEqual([])
  })
})

// ── parentMessageId in WS events ─────────────────────────────────────────────

describe('useThreads — parentMessageId in WS events', () => {
  it('thread_started with parent_message_id sets parentMessageId on thread', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-pm')

    handlers['thread_started']!({
      session_id: 'sess-pm',
      payload: { thread_id: 'thread-pm1', agent_id: 'a', task: 't', parent_message_id: 'msg-42' },
    })

    const threads = getSessionThreads('sess-pm')
    expect(threads[0]!.parentMessageId).toBe('msg-42')
  })

  it('thread_started without parent_message_id leaves parentMessageId undefined', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-npm')

    handlers['thread_started']!({
      session_id: 'sess-npm',
      payload: { thread_id: 'thread-npm1', agent_id: 'a', task: 't' },
    })

    const threads = getSessionThreads('sess-npm')
    expect(threads[0]!.parentMessageId).toBeUndefined()
  })

  it('delegation_preview with parent_message_id sets parentMessageId on preview', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionPreviews } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-dp')

    handlers['delegation_preview']!({
      session_id: 'sess-dp',
      payload: { thread_id: 'thread-dp1', agent_id: 'agent-z', task: 'review', parent_message_id: 'msg-origin' },
    })

    const previews = getSessionPreviews('sess-dp')
    expect(previews).toHaveLength(1)
    expect(previews[0]!.parentMessageId).toBe('msg-origin')
  })
})

// ── LRU session eviction ──────────────────────────────────────────────────────

describe('useThreads — LRU session eviction', () => {
  it('allows up to 5 sessions without eviction', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'unused')

    for (let i = 1; i <= 5; i++) {
      handlers['thread_started']!({
        session_id: `sess-lru-${i}`,
        payload: { thread_id: `t-lru-${i}`, agent_id: 'a', task: 't' },
      })
    }

    for (let i = 1; i <= 5; i++) {
      expect(getSessionThreads(`sess-lru-${i}`)).toHaveLength(1)
    }
  })

  it('evicts the oldest session when a 6th is added', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'unused')

    // Add sessions 1-5
    for (let i = 1; i <= 5; i++) {
      handlers['thread_started']!({
        session_id: `sess-evict-${i}`,
        payload: { thread_id: `t-evict-${i}`, agent_id: 'a', task: 't' },
      })
    }
    // Add 6th — should evict sess-evict-1
    handlers['thread_started']!({
      session_id: 'sess-evict-6',
      payload: { thread_id: 't-evict-6', agent_id: 'a', task: 't' },
    })

    expect(getSessionThreads('sess-evict-1')).toHaveLength(0)
    expect(getSessionThreads('sess-evict-6')).toHaveLength(1)
  })

  it('clearSession removes session from LRU so it does not count toward limit', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads, clearSession } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'unused')

    // Fill 5 sessions
    for (let i = 1; i <= 5; i++) {
      handlers['thread_started']!({
        session_id: `sess-clr-${i}`,
        payload: { thread_id: `t-clr-${i}`, agent_id: 'a', task: 't' },
      })
    }
    // Clear sess-clr-1
    clearSession('sess-clr-1')

    // Adding a new session should NOT evict sess-clr-2 (we cleared slot 1)
    handlers['thread_started']!({
      session_id: 'sess-clr-new',
      payload: { thread_id: 't-clr-new', agent_id: 'a', task: 't' },
    })

    expect(getSessionThreads('sess-clr-1')).toHaveLength(0)  // already cleared
    expect(getSessionThreads('sess-clr-2')).toHaveLength(1)  // not evicted
    expect(getSessionThreads('sess-clr-new')).toHaveLength(1)
  })
})

// ── isAgentActive ─────────────────────────────────────────────────────────────

describe('useThreads — isAgentActive', () => {
  it('returns true when agent has a running thread', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, isAgentActive } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-aa')

    handlers['thread_started']!({
      session_id: 'sess-aa',
      payload: { thread_id: 'thread-aa1', agent_id: 'builder', task: 'build' },
    })

    expect(isAgentActive('builder')).toBe(true)
  })

  it('returns false when agent only has terminal threads', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, isAgentActive } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-aa2')

    handlers['thread_started']!({ session_id: 'sess-aa2', payload: { thread_id: 't-aa2', agent_id: 'reviewer', task: 't' } })
    handlers['thread_done']!({ session_id: 'sess-aa2', payload: { thread_id: 't-aa2', status: 'done' } })

    expect(isAgentActive('reviewer')).toBe(false)
  })

  it('returns false when no threads exist', async () => {
    const { useThreads } = await freshUseThreads()
    const { isAgentActive } = useThreads()
    expect(isAgentActive('nobody')).toBe(false)
  })

  it('is case-insensitive', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, isAgentActive } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-ci')

    handlers['thread_started']!({ session_id: 'sess-ci', payload: { thread_id: 't-ci', agent_id: 'DeployBot', task: 't' } })

    expect(isAgentActive('deploybot')).toBe(true)
    expect(isAgentActive('DEPLOYBOT')).toBe(true)
  })
})

// ── getSessionPreviews / ackPreview ───────────────────────────────────────────

describe('useThreads — getSessionPreviews / ackPreview', () => {
  it('delegation_preview event adds to getSessionPreviews', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionPreviews } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-prev')

    handlers['delegation_preview']!({
      session_id: 'sess-prev',
      payload: { thread_id: 'thread-p1', agent_id: 'qa', task: 'run tests' },
    })

    const previews = getSessionPreviews('sess-prev')
    expect(previews).toHaveLength(1)
    expect(previews[0]!.agentId).toBe('qa')
    expect(previews[0]!.task).toBe('run tests')
  })

  it('duplicate delegation_preview is deduplicated', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionPreviews } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-dup')

    const evt = { session_id: 'sess-dup', payload: { thread_id: 'thread-dup1', agent_id: 'qa', task: 'task' } }
    handlers['delegation_preview']!(evt)
    handlers['delegation_preview']!(evt)

    expect(getSessionPreviews('sess-dup')).toHaveLength(1)
  })

  it('ackPreview sends WS message and removes preview', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionPreviews, ackPreview } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const sent: unknown[] = []
    const ws = {
      on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn },
      send: (msg: unknown) => { sent.push(msg) },
    }
    wireWS(ws as any, () => 'sess-ack')

    handlers['delegation_preview']!({
      session_id: 'sess-ack',
      payload: { thread_id: 'thread-ack1', agent_id: 'coder', task: 'code it' },
    })
    const preview = getSessionPreviews('sess-ack')[0]!
    ackPreview(ws as any, preview, true)

    expect(sent).toHaveLength(1)
    const sentMsg = sent[0] as any
    expect(sentMsg.type).toBe('delegation_preview_ack')
    expect(sentMsg.payload.approved).toBe(true)
    expect(getSessionPreviews('sess-ack')).toHaveLength(0)
  })

  it('ackPreview with approved=false sends denial and removes preview', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionPreviews, ackPreview } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const sent: unknown[] = []
    const ws = {
      on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn },
      send: (msg: unknown) => { sent.push(msg) },
    }
    wireWS(ws as any, () => 'sess-deny')

    handlers['delegation_preview']!({
      session_id: 'sess-deny',
      payload: { thread_id: 'thread-deny1', agent_id: 'qa', task: 'run' },
    })
    const preview = getSessionPreviews('sess-deny')[0]!
    ackPreview(ws as any, preview, false)

    const sentMsg = sent[0] as any
    expect(sentMsg.payload.approved).toBe(false)
    expect(getSessionPreviews('sess-deny')).toHaveLength(0)
  })
})

// ── thread_help_resolving / thread_help_resolved ──────────────────────────────

describe('useThreads — thread_help_resolving / thread_help_resolved', () => {
  it('thread_help_resolving sets status to resolving', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-res')

    handlers['thread_started']!({ session_id: 'sess-res', payload: { thread_id: 'thread-res1', agent_id: 'a', task: 't' } })
    handlers['thread_help_resolving']!({ session_id: 'sess-res', payload: { thread_id: 'thread-res1' } })

    expect(getSessionThreads('sess-res')[0]!.Status).toBe('resolving')
  })

  it('thread_help_resolved sets status to thinking', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-rsd')

    handlers['thread_started']!({ session_id: 'sess-rsd', payload: { thread_id: 'thread-rsd1', agent_id: 'a', task: 't' } })
    handlers['thread_help_resolved']!({ session_id: 'sess-rsd', payload: { thread_id: 'thread-rsd1' } })

    expect(getSessionThreads('sess-rsd')[0]!.Status).toBe('thinking')
  })
})

// ── streaming content 600-char cap ────────────────────────────────────────────

describe('useThreads — streaming content cap', () => {
  it('trims streamingContent to last 600 chars', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-cap')

    // Send a token that by itself exceeds 600 chars
    const bigToken = 'x'.repeat(700)
    handlers['thread_token']!({ session_id: 'sess-cap', payload: { thread_id: 'thread-cap1', token: bigToken } })

    const content = getSessionThreads('sess-cap')[0]!.streamingContent
    expect(content.length).toBeLessThanOrEqual(600)
    // The last 600 chars of a 700-char string of 'x' is still all 'x'
    expect(content).toBe('x'.repeat(600))
  })
})

// ── thread_status WS event ────────────────────────────────────────────────────

describe('useThreads — thread_status WS event', () => {
  it('updates thread status when thread_status event is received for a known thread', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-ts1')

    handlers['thread_started']!({
      session_id: 'sess-ts1',
      payload: { thread_id: 'thread-ts1', agent_id: 'a', task: 't' },
    })
    expect(getSessionThreads('sess-ts1')[0]!.Status).toBe('thinking')

    handlers['thread_status']!({
      session_id: 'sess-ts1',
      payload: { thread_id: 'thread-ts1', status: 'tooling' },
    })

    expect(getSessionThreads('sess-ts1')[0]!.Status).toBe('tooling')
  })

  it('thread_status for an unknown thread ID is a no-op (no crash)', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-ts2')

    // Emit thread_status for a thread that was never started in this session
    expect(() => {
      handlers['thread_status']!({
        session_id: 'sess-ts2',
        payload: { thread_id: 'nonexistent-thread', status: 'done' },
      })
    }).not.toThrow()
  })

  it('thread_status correctly updates status regardless of previous status value', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-ts3')

    handlers['thread_started']!({
      session_id: 'sess-ts3',
      payload: { thread_id: 'thread-ts3', agent_id: 'a', task: 't' },
    })
    // Transition through multiple statuses
    handlers['thread_status']!({ session_id: 'sess-ts3', payload: { thread_id: 'thread-ts3', status: 'blocked' } })
    expect(getSessionThreads('sess-ts3')[0]!.Status).toBe('blocked')

    handlers['thread_status']!({ session_id: 'sess-ts3', payload: { thread_id: 'thread-ts3', status: 'thinking' } })
    expect(getSessionThreads('sess-ts3')[0]!.Status).toBe('thinking')

    handlers['thread_status']!({ session_id: 'sess-ts3', payload: { thread_id: 'thread-ts3', status: 'done' } })
    expect(getSessionThreads('sess-ts3')[0]!.Status).toBe('done')
  })

  it('thread_status with missing payload fields does not crash', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-ts4')

    // Malformed payload: missing thread_id and status
    expect(() => {
      handlers['thread_status']!({ session_id: 'sess-ts4', payload: {} })
    }).not.toThrow()
  })
})

// ── Ticker management ─────────────────────────────────────────────────────────

describe('useThreads — ticker management', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('elapsed increments while thread is running', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-tick1')

    handlers['thread_started']!({
      session_id: 'sess-tick1',
      payload: { thread_id: 'thread-tick1', agent_id: 'a', task: 't' },
    })

    const beforeMs = getSessionThreads('sess-tick1')[0]!.elapsedMs
    vi.advanceTimersByTime(1500)
    const afterMs = getSessionThreads('sess-tick1')[0]!.elapsedMs
    expect(afterMs).toBeGreaterThan(beforeMs)
  })

  it('elapsed stops incrementing after thread_done event', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-tick2')

    handlers['thread_started']!({
      session_id: 'sess-tick2',
      payload: { thread_id: 'thread-tick2', agent_id: 'a', task: 't' },
    })
    vi.advanceTimersByTime(500)

    handlers['thread_done']!({
      session_id: 'sess-tick2',
      payload: { thread_id: 'thread-tick2', status: 'done', elapsed_ms: 500 },
    })

    const elapsedAtDone = getSessionThreads('sess-tick2')[0]!.elapsedMs
    vi.advanceTimersByTime(2000)
    const elapsedAfterWait = getSessionThreads('sess-tick2')[0]!.elapsedMs
    // After done, ticker is stopped — elapsed should not have increased
    expect(elapsedAfterWait).toBe(elapsedAtDone)
  })
})

// ── Concurrent / edge cases ───────────────────────────────────────────────────

describe('useThreads — concurrent and edge cases', () => {
  beforeEach(() => { mockThreadsList.mockReset() })

  it('loadThreads then thread_started adds new thread alongside existing ones', async () => {
    mockThreadsList.mockResolvedValue([{
      ID: 'existing-thread',
      SessionID: 'sess-mix',
      AgentID: 'a',
      Task: 'existing',
      Status: 'done',
      StartedAt: '',
      CompletedAt: '',
      TokensUsed: 0,
      TokenBudget: 0,
    }])
    const { useThreads } = await freshUseThreads()
    const { loadThreads, wireWS, getSessionThreads } = useThreads()
    await loadThreads('sess-mix')

    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-mix')

    handlers['thread_started']!({
      session_id: 'sess-mix',
      payload: { thread_id: 'new-thread', agent_id: 'b', task: 'new task' },
    })

    const threads = getSessionThreads('sess-mix')
    expect(threads).toHaveLength(2)
    const ids = threads.map(t => t.ID)
    expect(ids).toContain('existing-thread')
    expect(ids).toContain('new-thread')
  })

  it('thread_tool_done for a tool_name that has no pending call is a no-op', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getSessionThreads } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-noop')

    handlers['thread_started']!({
      session_id: 'sess-noop',
      payload: { thread_id: 'thread-noop1', agent_id: 'a', task: 't' },
    })

    // Emit tool_done for a tool that was never called
    expect(() => {
      handlers['thread_tool_done']!({
        session_id: 'sess-noop',
        payload: { thread_id: 'thread-noop1', tool: 'nonexistent_tool', result_summary: 'n/a' },
      })
    }).not.toThrow()

    // toolCalls remains empty
    expect(getSessionThreads('sess-noop')[0]!.toolCalls).toHaveLength(0)
  })

  it('getActiveThreadCount counts only non-terminal threads', async () => {
    const { useThreads } = await freshUseThreads()
    const { wireWS, getActiveThreadCount } = useThreads()
    const handlers: Record<string, (msg: unknown) => void> = {}
    const ws = { on: (e: string, fn: (msg: unknown) => void) => { handlers[e] = fn } }
    wireWS(ws as any, () => 'sess-cnt')

    handlers['thread_started']!({ session_id: 'sess-cnt', payload: { thread_id: 'th-running', agent_id: 'a', task: 't' } })
    handlers['thread_started']!({ session_id: 'sess-cnt', payload: { thread_id: 'th-done', agent_id: 'b', task: 't' } })
    handlers['thread_done']!({ session_id: 'sess-cnt', payload: { thread_id: 'th-done', status: 'done' } })

    expect(getActiveThreadCount('sess-cnt')).toBe(1)
  })
})
