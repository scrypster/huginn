import { describe, expect, it, vi } from 'vitest'
import {
  buildGlobalSearchResults,
  formatSpaceSearchLabel,
  highlightSnippet,
  mergeSpaceMessageGroups,
  searchResultPath,
} from '../globalSearch'

describe('searchResultPath', () => {
  it('routes a space-backed message hit to /space/:spaceId (not /chat/:sessionId)', () => {
    expect(searchResultPath({
      sessionId: 'sess-channel',
      spaceId: 'space-eng',
    })).toBe('/space/space-eng')
  })

  it('keeps session-only hits on /chat/:sessionId', () => {
    expect(searchResultPath({ sessionId: 'sess-legacy' })).toBe('/chat/sess-legacy')
  })
})

describe('highlightSnippet', () => {
  it('returns null when the query is missing from content', () => {
    expect(highlightSnippet('hello world', 'xyz')).toBeNull()
  })

  it('highlights a case-insensitive match', () => {
    const snippet = highlightSnippet('Hello World', 'world')
    expect(snippet).toContain('<strong class="text-huginn-blue">World</strong>')
  })
})

describe('buildGlobalSearchResults', () => {
  const formatSessionLabel = (s: { id: string }) => `Session ${s.id}`

  it('finds session-cache messages the same way as the original Cmd+K search', () => {
    const formatWithTitle = vi.fn((s: { id: string; title?: string }) => s.title || s.id)
    const results = buildGlobalSearchResults({
      query: 'budget',
      sessions: [{ id: 'sess-1', title: 'Planning' } as { id: string; title?: string; space_id?: string }],
      getMessages: () => [
        { id: 'm1', role: 'user', content: 'Please review the budget numbers', agent: '' },
        { id: 'm2', role: 'tool_call', content: 'budget tool', agent: '' },
      ],
      formatSessionLabel: formatWithTitle,
    })

    expect(results).toHaveLength(1)
    expect(results[0].sessionId).toBe('sess-1')
    expect(results[0].sessionLabel).toBe('Planning')
    expect(results[0].msgId).toBe('m1')
    expect(results[0].spaceId).toBeUndefined()
    expect(searchResultPath(results[0])).toBe('/chat/sess-1')
    expect(formatWithTitle).toHaveBeenCalledWith(expect.objectContaining({ id: 'sess-1', title: 'Planning' }))
  })

  it('finds channel/DM text from space message groups when the session cache is empty', () => {
    const results = buildGlobalSearchResults({
      query: 'deploy',
      sessions: [{ id: 'sess-space', space_id: 'space-eng' }],
      getMessages: () => [],
      formatSessionLabel,
      spaceMessageGroups: [{
        spaceId: 'space-eng',
        spaceLabel: '#engineering',
        messages: [
          { id: 'sm1', role: 'user', content: 'did we deploy the fix?', session_id: 'sess-space', agent: 'atlas' },
        ],
      }],
    })

    expect(results).toHaveLength(1)
    expect(results[0].spaceId).toBe('space-eng')
    expect(results[0].sessionLabel).toBe('#engineering')
    expect(searchResultPath(results[0])).toBe('/space/space-eng')
  })

  it('attaches space_id from the session so a cached session hit still opens the space', () => {
    const results = buildGlobalSearchResults({
      query: 'standup',
      sessions: [{ id: 'sess-dm', space_id: 'space-dm-1' }],
      getMessages: () => [
        { id: 'm1', role: 'assistant', content: 'standup notes are ready', agent: 'atlas' },
      ],
      formatSessionLabel,
    })

    expect(results[0].spaceId).toBe('space-dm-1')
    expect(searchResultPath(results[0])).toBe('/space/space-dm-1')
  })

  it('resolves space_id via resolveSpaceId when the session object has none', () => {
    const results = buildGlobalSearchResults({
      query: 'notes',
      sessions: [{ id: 'sess-mapped' }],
      getMessages: () => [
        { id: 'm1', role: 'user', content: 'meeting notes', agent: '' },
      ],
      formatSessionLabel,
      resolveSpaceId: (id) => id === 'sess-mapped' ? 'space-from-map' : null,
    })

    expect(results[0].spaceId).toBe('space-from-map')
    expect(searchResultPath(results[0])).toBe('/space/space-from-map')
  })

  it('returns no hits for queries shorter than 2 characters', () => {
    expect(buildGlobalSearchResults({
      query: 'a',
      sessions: [{ id: 's' }],
      getMessages: () => [{ id: 'm', role: 'user', content: 'aa' }],
      formatSessionLabel,
    })).toEqual([])
  })

  it('deduplicates the same message when it appears in both session and space sources', () => {
    const results = buildGlobalSearchResults({
      query: 'overlap',
      sessions: [{ id: 'sess-1', space_id: 'space-1' }],
      getMessages: () => [
        { id: 'shared', role: 'user', content: 'overlap text', agent: '' },
      ],
      formatSessionLabel,
      spaceMessageGroups: [{
        spaceId: 'space-1',
        spaceLabel: '#eng',
        messages: [
          { id: 'shared', role: 'user', content: 'overlap text', session_id: 'sess-1' },
        ],
      }],
    })

    expect(results).toHaveLength(1)
    expect(results[0].spaceId).toBe('space-1')
  })

  it('caps results at the limit', () => {
    const getMessages = vi.fn(() =>
      Array.from({ length: 10 }, (_, i) => ({
        id: `m${i}`,
        role: 'user' as const,
        content: 'repeatable needle',
      })),
    )
    const results = buildGlobalSearchResults({
      query: 'needle',
      sessions: [{ id: 's1' }, { id: 's2' }],
      getMessages,
      formatSessionLabel,
      limit: 5,
    })
    expect(results).toHaveLength(5)
  })
})

describe('formatSpaceSearchLabel', () => {
  it('prefixes channels with #', () => {
    expect(formatSpaceSearchLabel({ name: 'eng', kind: 'channel' })).toBe('#eng')
  })

  it('uses the raw name for DMs', () => {
    expect(formatSpaceSearchLabel({ name: 'atlas', kind: 'dm' })).toBe('atlas')
  })
})

describe('mergeSpaceMessageGroups', () => {
  it('merges groups for the same space and dedups by message id', () => {
    const merged = mergeSpaceMessageGroups(
      [{ spaceId: 'sp-1', spaceLabel: '#eng', messages: [{ id: 'a', role: 'user', content: 'one' }] }],
      [{ spaceId: 'sp-1', spaceLabel: '#eng', messages: [{ id: 'a', role: 'user', content: 'one' }, { id: 'b', role: 'user', content: 'two' }] }],
    )
    expect(merged).toHaveLength(1)
    expect(merged[0].messages.map(m => m.id)).toEqual(['a', 'b'])
  })
})
