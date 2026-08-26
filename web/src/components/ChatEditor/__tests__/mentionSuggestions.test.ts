import { describe, it, expect } from 'vitest'
import {
  spaceRosterNames,
  filterMentionSuggestions,
  dropUnknownLeadMention,
  extractLeadMention,
} from '../mentionSuggestions'

const steve = { name: 'Steve', color: '#58a6ff' }
const tess = { name: 'Tess', color: '#3fb950' }
const chris = { name: 'Chris', color: '#d2a8ff' }
const allAgents = [steve, tess, chris]

describe('spaceRosterNames', () => {
  it('returns undefined for standalone chat (no space)', () => {
    expect(spaceRosterNames(null)).toBeUndefined()
    expect(spaceRosterNames(undefined)).toBeUndefined()
  })

  it('DM roster is that one agent', () => {
    expect(spaceRosterNames({
      kind: 'dm',
      leadAgent: 'Steve',
      memberAgents: [],
    })).toEqual(['Steve'])
  })

  it('DM ignores extra memberAgents — still only the DM agent', () => {
    expect(spaceRosterNames({
      kind: 'dm',
      leadAgent: 'Steve',
      memberAgents: ['Steve', 'Tess'],
    })).toEqual(['Steve'])
  })

  it('channel roster is lead + members, de-duplicated', () => {
    expect(spaceRosterNames({
      kind: 'channel',
      leadAgent: 'Steve',
      memberAgents: ['Steve', 'Chris'],
    })).toEqual(['Steve', 'Chris'])
  })
})

describe('filterMentionSuggestions', () => {
  it('channel picker lists only members', () => {
    const roster = spaceRosterNames({
      kind: 'channel',
      leadAgent: 'Steve',
      memberAgents: ['Steve', 'Chris'],
    })
    const items = filterMentionSuggestions(allAgents, '', roster)
    expect(items.map(a => a.name)).toEqual(['Steve', 'Chris'])
  })

  it('DM picker lists only that agent', () => {
    const roster = spaceRosterNames({
      kind: 'dm',
      leadAgent: 'Steve',
      memberAgents: [],
    })
    const items = filterMentionSuggestions(allAgents, '', roster)
    expect(items.map(a => a.name)).toEqual(['Steve'])
  })

  it('a non-member is not suggested', () => {
    const roster = spaceRosterNames({
      kind: 'channel',
      leadAgent: 'Steve',
      memberAgents: ['Steve', 'Chris'],
    })
    const items = filterMentionSuggestions(allAgents, 'T', roster)
    expect(items.map(a => a.name)).not.toContain('Tess')
    expect(items).toEqual([])
  })

  it('does not suggest a non-member even when the query matches their name', () => {
    const roster = ['Steve']
    const items = filterMentionSuggestions(allAgents, 'Tess', roster)
    expect(items).toEqual([])
  })

  it('standalone (no roster) still lists every agent', () => {
    const items = filterMentionSuggestions(allAgents, '', undefined)
    expect(items.map(a => a.name)).toEqual(['Steve', 'Tess', 'Chris'])
  })

  it('filters roster members by query prefix, case-insensitive', () => {
    const items = filterMentionSuggestions(allAgents, 'ch', ['Steve', 'Chris'])
    expect(items.map(a => a.name)).toEqual(['Chris'])
  })
})

describe('dropUnknownLeadMention', () => {
  it('drops leftover @Name that is not a member', () => {
    const result = dropUnknownLeadMention('@Tess say hello', ['Steve'])
    expect(result.content).toBe('say hello')
    expect(result.dropped).toBe('Tess')
  })

  it('keeps a member leading mention', () => {
    const result = dropUnknownLeadMention('@Steve say hello', ['Steve', 'Chris'])
    expect(result.content).toBe('@Steve say hello')
    expect(result.dropped).toBeUndefined()
  })

  it('leaves standalone mentions unchanged', () => {
    const result = dropUnknownLeadMention('@Tess hello', undefined)
    expect(result.content).toBe('@Tess hello')
    expect(result.dropped).toBeUndefined()
  })

  it('does not send an empty leftover mention to the lead', () => {
    const result = dropUnknownLeadMention('@Tess', ['Steve'])
    expect(result.content.trim()).toBe('')
    expect(result.dropped).toBe('Tess')
  })
})

describe('extractLeadMention', () => {
  it('reads a leading @Name', () => {
    expect(extractLeadMention('@Steve do X')).toBe('Steve')
  })

  it('ignores a mid-message mention', () => {
    expect(extractLeadMention('hey @Steve')).toBe('')
  })
})
