import { describe, it, expect } from 'vitest'
import {
  adaptSpaceMessages,
  dateLabelFor,
  enrichMessages,
  isSameDay,
  sortMessagesChronological,
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

  it('adaptSpaceMessages maps created_at/ts→createdAt and infers streaming/tool-call done', () => {
    const out = adaptSpaceMessages([{
      id: 'stream-1',
      role: 'assistant',
      content: 'hello',
      agent: 'Mark',
      ts: '2026-05-01T09:00:00Z',
      created_at: '2026-05-01T10:00:00Z',
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

describe('hallway chronology (Slack oldest→newest)', () => {
  const now = new Date('2026-08-27T16:00:00Z') // noon ET

  it('treats 02:15Z as yesterday in America/New_York', () => {
    // 2026-08-27T02:15:23Z = 10:15 PM ET on Aug 26
    expect(dateLabelFor('2026-08-27T02:15:23Z', now)).toBe('Yesterday')
    expect(dateLabelFor('2026-08-27T16:00:11Z', now)).toBe('Today')
    expect(isSameDay('2026-08-27T02:15:23Z', '2026-08-26T22:00:00-04:00')).toBe(true)
    expect(isSameDay('2026-08-27T02:15:23Z', '2026-08-27T16:00:00Z')).toBe(false)
  })

  it('sorts newest-first leftovers so Today never sits above Yesterday', () => {
    const msgs = enrichMessages([
      { id: 'today-user', role: 'user', content: '@Winston what time is it?', createdAt: '2026-08-27T16:00:11Z' },
      { id: 'today-win', role: 'assistant', content: '12:00 PM ET', agent: 'Winston', createdAt: '2026-08-27T16:01:38Z' },
      { id: 'pong', role: 'assistant', content: 'PONG', agent: 'Steve', createdAt: '2026-08-27T02:15:23Z' },
    ] as any, now)

    expect(msgs.map(m => m.id)).toEqual(['pong', 'today-user', 'today-win'])
    expect(msgs.map(m => m.dateLabel).filter(Boolean)).toEqual(['Yesterday', 'Today'])
    const yesterdayAt = msgs.findIndex(m => m.dateLabel === 'Yesterday')
    const todayAt = msgs.findIndex(m => m.dateLabel === 'Today')
    expect(yesterdayAt).toBeGreaterThanOrEqual(0)
    expect(todayAt).toBeGreaterThan(yesterdayAt)
    expect(msgs[0]!.content).toBe('PONG')
    expect(msgs[todayAt]!.content).toContain('@Winston')
  })

  it('sortMessagesChronological keeps streaming placeholders last when ts ties', () => {
    const ts = '2026-08-27T16:00:11Z'
    const out = sortMessagesChronological([
      { id: 'stream-1', seq: -1, ts },
      { id: 'real-2', seq: 2, ts },
      { id: 'real-1', seq: 1, ts },
    ])
    expect(out.map(m => m.id)).toEqual(['real-1', 'real-2', 'stream-1'])
  })
})
