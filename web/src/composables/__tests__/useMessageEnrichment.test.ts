import { describe, it, expect } from 'vitest'
import {
  adaptSpaceMessages,
  dateLabelFor,
  enrichMessages,
  isSameDay,
} from '../useMessageEnrichment'

describe('useMessageEnrichment utilities', () => {
  it('dateLabelFor returns empty label for invalid input', () => {
    expect(dateLabelFor(undefined)).toBe('')
    expect(dateLabelFor('not-a-date')).toBe('')
  })

  it('isSameDay compares calendar day boundaries', () => {
    expect(isSameDay('2026-05-01T10:00:00Z', '2026-05-01T23:59:59Z')).toBe(true)
    expect(isSameDay('2026-05-01T00:00:00Z', '2026-05-03T00:00:00Z')).toBe(false)
  })

  it('adaptSpaceMessages maps ts→createdAt and infers streaming/tool-call done', () => {
    const out = adaptSpaceMessages([{
      id: 'stream-1',
      role: 'assistant',
      content: 'hello',
      agent: 'Mark',
      ts: '2026-05-01T10:00:00Z',
      toolCalls: [{ id: 'tc-1', name: 'read_file', args: {} } as any],
    } as any])

    expect(out).toHaveLength(1)
    expect(out[0]!.createdAt).toBe('2026-05-01T10:00:00Z')
    expect(out[0]!.streaming).toBe(true)
    expect(out[0]!.toolCalls?.[0]?.done).toBe(true)
  })

  it('enrichMessages suppresses assistant header for close same-agent continuation', () => {
    const msgs = enrichMessages([
      {
        id: 'm1',
        role: 'assistant',
        content: 'part 1',
        agent: 'Mark',
        createdAt: '2026-05-01T10:00:00Z',
      },
      {
        id: 'm2',
        role: 'assistant',
        content: 'part 2',
        agent: 'Mark',
        createdAt: '2026-05-01T10:00:40Z',
      },
    ] as any)

    expect(msgs[0]!.showHeader).toBe(true)
    expect(msgs[1]!.showHeader).toBe(false)
  })

  it('enrichMessages restores header when message gap exceeds 60 seconds', () => {
    const msgs = enrichMessages([
      {
        id: 'm1',
        role: 'assistant',
        content: 'part 1',
        agent: 'Mark',
        createdAt: '2026-05-01T10:00:00Z',
      },
      {
        id: 'm2',
        role: 'assistant',
        content: 'new thought',
        agent: 'Mark',
        createdAt: '2026-05-01T10:01:05Z',
      },
    ] as any)

    expect(msgs[1]!.showHeader).toBe(true)
  })

  it('adaptSpaceMessages treats harness announcements as system/completion rows', () => {
    const out = adaptSpaceMessages([
      {
        id: 'ann-1',
        role: 'assistant',
        content: 'Delegation to @Steve was auto-approved after 30s.',
        agent: 'Steve',
        ts: '2026-05-01T10:00:00Z',
      },
      {
        id: 'ann-2',
        role: 'assistant',
        content: '**Steve** completed delegated work: TOOL_FAIL',
        agent: 'Steve',
        ts: '2026-05-01T10:00:01Z',
      },
    ] as any)

    expect(out[0]!.systemLine).toBe(true)
    expect(out[0]!.threadSummary).toBeFalsy()
    expect(out[1]!.threadSummary).toBe(true)
    expect(out[1]!.systemLine).toBeFalsy()
  })

  it('enrichMessages omits A2A tools from the chip list', () => {
    const msgs = enrichMessages([
      {
        id: 'a1',
        role: 'assistant',
        content: 'delegating',
        agent: 'Tess',
        toolCalls: [
          { id: '1', name: 'delegate_to_agent', args: {}, done: true },
          { id: '2', name: 'wait_for_threads', args: {}, result: 'TOOL_FAIL', done: true },
          { id: '3', name: 'read_file', args: {}, done: true },
        ],
      },
    ] as any)

    expect(msgs[0]!.toolCalls?.map(tc => tc.name)).toEqual(['read_file'])
  })

  it('enrichMessages keeps header for follow-up synthesis', () => {
    const msgs = enrichMessages([
      {
        id: 'm1',
        role: 'assistant',
        content: 'response',
        agent: 'Mark',
        createdAt: '2026-05-01T10:00:00Z',
      },
      {
        id: 'm2',
        role: 'assistant',
        content: 'follow-up',
        agent: 'Mark',
        createdAt: '2026-05-01T10:00:20Z',
        isFollowUp: true,
      },
    ] as any)

    expect(msgs[1]!.showHeader).toBe(true)
  })
})
