import { describe, it, expect } from 'vitest'
import { agentDisplayDescription, DEFAULT_AGENT_DESCRIPTION } from '../agentDescription'

describe('agentDisplayDescription', () => {
  it('prefers an explicit description', () => {
    expect(agentDisplayDescription({
      description: 'Codes and reviews PRs',
      system_prompt: 'You are Steve, a coder.',
    })).toBe('Codes and reviews PRs')
  })

  it('derives from the first sentence of the system prompt', () => {
    expect(agentDisplayDescription({
      system_prompt: 'You are Steve, a coder. Use tools.',
    })).toBe('a coder')
  })

  it('uses a prompt without a name prefix as-is (first sentence)', () => {
    expect(agentDisplayDescription({
      system_prompt: 'Reviews pull requests for regressions.',
    })).toBe('Reviews pull requests for regressions')
  })

  it('never returns the raw "No description" placeholder', () => {
    expect(agentDisplayDescription({})).not.toBe('No description')
    expect(agentDisplayDescription({ description: '', system_prompt: '' })).not.toBe('No description')
    expect(agentDisplayDescription(null)).not.toBe('No description')
  })

  it('falls back to a sensible default when nothing can be derived', () => {
    expect(agentDisplayDescription({})).toBe(DEFAULT_AGENT_DESCRIPTION)
    expect(agentDisplayDescription({ name: 'Steve' })).toBe(DEFAULT_AGENT_DESCRIPTION)
  })

  it('trims whitespace-only descriptions before falling back', () => {
    expect(agentDisplayDescription({
      description: '   ',
      system_prompt: 'You are Nova, the scheduler.',
    })).toBe('the scheduler')
  })
})
