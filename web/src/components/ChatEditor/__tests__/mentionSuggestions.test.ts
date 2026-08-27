import { describe, it, expect } from 'vitest'
import {
  spaceRosterNames,
  mentionableNames,
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

  it('drops mid-text @Name of a non-member and keeps the rest', () => {
    const result = dropUnknownLeadMention('please ask @Steve about hostname', ['Tess'])
    expect(result.content).toBe('please ask about hostname')
    expect(result.dropped).toBe('Steve')
  })

  it('keeps a mid-text member mention', () => {
    const result = dropUnknownLeadMention('please ask @Steve about hostname', ['Tess', 'Steve'])
    expect(result.content).toBe('please ask @Steve about hostname')
    expect(result.dropped).toBeUndefined()
  })

  it('drops a mid-text non-member while keeping a member mention', () => {
    const result = dropUnknownLeadMention('@Steve please ask @Tess about hostname', ['Steve'])
    expect(result.content).toBe('@Steve please ask about hostname')
    expect(result.dropped).toBe('Tess')
  })
})

describe('mentionableNames company ∩ space', () => {
  const huginn = { kind: 'channel', leadAgent: 'Winston', memberAgents: ['Steve', 'Winston'], companyId: 'co-huginn' }
  const lab = { kind: 'channel', leadAgent: 'Winston', memberAgents: ['Winston', 'Sam'], companyId: 'co-lab' }
  const desk = { kind: 'channel', leadAgent: 'Winston', memberAgents: ['Steve', 'Reggie'], companyId: '' }

  it('Lab space does not offer Reggie', () => {
    const names = mentionableNames(lab, ['Winston', 'Sam'])
    expect(names).not.toContain('Reggie')
    expect(names).not.toContain('Steve')
    const items = filterMentionSuggestions(
      [{ name: 'Reggie' }, { name: 'Winston' }, { name: 'Sam' }, { name: 'Steve' }],
      '',
      names,
    )
    expect(items.map(a => a.name)).toEqual(['Winston', 'Sam'])
    expect(items.map(a => a.name)).not.toContain('Reggie')
  })

  it('Huginn space does not offer Lab-only Sam', () => {
    const names = mentionableNames(huginn, ['Winston', 'Steve'])
    expect(names).toContain('Winston')
    expect(names).toContain('Steve')
    expect(names).not.toContain('Sam')
    const items = filterMentionSuggestions(
      [{ name: 'Sam' }, { name: 'Steve' }, { name: 'Winston' }, { name: 'Reggie' }],
      '',
      names,
    )
    expect(items.map(a => a.name)).toEqual(['Steve', 'Winston'])
    expect(items.map(a => a.name)).not.toContain('Sam')
  })

  it('desk (empty company_id) stays space-member picker', () => {
    const names = mentionableNames(desk, ['Winston'])
    expect(names).toEqual(['Winston', 'Steve', 'Reggie'])
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
