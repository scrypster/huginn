import { describe, it, expect } from 'vitest'
import { extractLeadMention, hallwayAuthorName, resolveDisplayAgent, type DisplayAgentLike } from '../respondingAgent'

const tess: DisplayAgentLike = { name: 'Tess', icon: 'T', color: '#58a6ff' }
const steve: DisplayAgentLike = { name: 'Steve', icon: 'S', color: '#3fb950' }
const agents = [tess, steve]
const channel = { kind: 'channel', leadAgent: 'Tess', memberAgents: ['Steve'] }

describe('extractLeadMention', () => {
  it('reads a leading @mention', () => {
    expect(extractLeadMention('@Steve say PONG and nothing else')).toBe('Steve')
  })

  it('trims leading whitespace', () => {
    expect(extractLeadMention('  @Steve hello')).toBe('Steve')
  })

  it('accepts hyphen and underscore names', () => {
    expect(extractLeadMention('@Dave-ops deploy')).toBe('Dave-ops')
    expect(extractLeadMention('@Tom_lead plan')).toBe('Tom_lead')
  })

  it('returns empty when the mention is not at the start', () => {
    expect(extractLeadMention('hello @Steve')).toBe('')
  })

  it('returns empty for bare @, digit-leading, or no mention', () => {
    expect(extractLeadMention('@')).toBe('')
    expect(extractLeadMention('@123invalid')).toBe('')
    expect(extractLeadMention('no mention here')).toBe('')
    expect(extractLeadMention('')).toBe('')
  })
})

describe('resolveDisplayAgent', () => {
  it('names the mentioned member while a channel turn is in flight', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: '@Steve say PONG and nothing else',
    })
    expect(agent?.name).toBe('Steve')
  })

  it('keeps the lead when an in-flight channel message has no mention', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: 'what is the status?',
    })
    expect(agent?.name).toBe('Tess')
  })

  it('keeps the lead after the turn (not streaming) even if the last message mentioned a member', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: false,
      inFlightUserContent: '@Steve say PONG',
    })
    expect(agent?.name).toBe('Tess')
  })

  it('keeps the lead when the mention is not a space member', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: '@Unknown do a thing',
    })
    expect(agent?.name).toBe('Tess')
  })

  it('keeps the lead when the mention is mid-message', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: 'please ask @Steve for a PONG',
    })
    expect(agent?.name).toBe('Tess')
  })

  it('matches mention names case-insensitively', () => {
    const agent = resolveDisplayAgent({
      space: channel,
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: '@steve ping',
    })
    expect(agent?.name).toBe('Steve')
  })

  it('does not apply mention override in a DM', () => {
    const agent = resolveDisplayAgent({
      space: { kind: 'dm', leadAgent: 'Tess', memberAgents: [] },
      agents,
      selectedAgent: tess,
      streaming: true,
      inFlightUserContent: '@Steve say PONG',
    })
    expect(agent?.name).toBe('Tess')
  })

  it('falls back to the selected agent when no space is active', () => {
    const agent = resolveDisplayAgent({
      space: null,
      agents,
      selectedAgent: steve,
      streaming: true,
      inFlightUserContent: '@Tess hi',
    })
    expect(agent?.name).toBe('Steve')
  })
})

describe('hallwayAuthorName', () => {
  it('uses the full agent name when present', () => {
    expect(hallwayAuthorName('Winston', 'Steve')).toBe('Winston')
  })

  it('falls back when the live bubble is nameless or a bare initial', () => {
    expect(hallwayAuthorName('', 'Winston')).toBe('Winston')
    expect(hallwayAuthorName('W', 'Winston')).toBe('Winston')
    expect(hallwayAuthorName(undefined, 'Winston')).toBe('Winston')
  })
})
