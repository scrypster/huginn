import { describe, it, expect } from 'vitest'
import { isFreshInstall, pickDefaultAgent, isModelConfigured } from '../firstRun'

describe('isFreshInstall', () => {
  it('is true with zero spaces and zero sessions', () => {
    expect(isFreshInstall(0, 0)).toBe(true)
  })

  it('is false when a space exists', () => {
    expect(isFreshInstall(1, 0)).toBe(false)
  })

  it('is false when a session exists', () => {
    expect(isFreshInstall(0, 1)).toBe(false)
  })

  it('is false when both exist', () => {
    expect(isFreshInstall(3, 5)).toBe(false)
  })
})

describe('pickDefaultAgent', () => {
  it('returns null for an empty roster', () => {
    expect(pickDefaultAgent([])).toBeNull()
  })

  it('prefers the agent flagged is_default', () => {
    const agents = [{ name: 'atlas' }, { name: 'cos', is_default: true }, { name: 'hermes' }]
    expect(pickDefaultAgent(agents)?.name).toBe('cos')
  })

  it('falls back to the first agent when none is flagged default', () => {
    const agents = [{ name: 'atlas' }, { name: 'hermes' }]
    expect(pickDefaultAgent(agents)?.name).toBe('atlas')
  })
})

describe('isModelConfigured', () => {
  it('is false for a null config', () => {
    expect(isModelConfigured(null)).toBe(false)
  })

  it('is false when no backend type is set', () => {
    expect(isModelConfigured({ backend: {} })).toBe(false)
  })

  it('is true for ollama with a base url', () => {
    expect(isModelConfigured({ backend: { type: 'ollama' }, ollama_base_url: 'http://localhost:11434' })).toBe(true)
  })

  it('is true for ollama with a backend endpoint', () => {
    expect(isModelConfigured({ backend: { type: 'ollama', endpoint: 'http://localhost:11434' } })).toBe(true)
  })

  it('is false for ollama with no url anywhere', () => {
    expect(isModelConfigured({ backend: { type: 'ollama' } })).toBe(false)
  })

  it('is true for a hosted provider with an api key', () => {
    expect(isModelConfigured({ backend: { type: 'anthropic', api_key: 'sk-xxx' } })).toBe(true)
  })

  it('is false for a hosted provider with no api key', () => {
    expect(isModelConfigured({ backend: { type: 'anthropic', api_key: '' } })).toBe(false)
  })
})
