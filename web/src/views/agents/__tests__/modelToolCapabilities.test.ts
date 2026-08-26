import { describe, it, expect } from 'vitest'
import {
  MODEL_TOOL_WARNING,
  isLowTierToolClass,
  modelUnreliableForTools,
} from '../modelToolCapabilities'

describe('model tool capability warning', () => {
  it('treats 7b / 3b / tiny names as unreliable even if Ollama says tools', () => {
    expect(isLowTierToolClass('qwen2.5-coder:7b')).toBe(true)
    expect(isLowTierToolClass('llama3:3b')).toBe(true)
    expect(isLowTierToolClass('phi:tiny')).toBe(true)
    expect(modelUnreliableForTools({ name: 'qwen2.5-coder:7b', supportsTools: true })).toBe(true)
  })

  it('warns when supportsTools is explicitly false', () => {
    expect(modelUnreliableForTools({ name: 'custom-coder', supportsTools: false })).toBe(true)
  })

  it('stays quiet for 14b+ or tools without a low-tier name', () => {
    expect(isLowTierToolClass('qwen2.5-coder:14b')).toBe(false)
    expect(isLowTierToolClass('llama2:13b')).toBe(false)
    expect(isLowTierToolClass('llama3.3:70b')).toBe(false)
    expect(modelUnreliableForTools({ name: 'qwen2.5-coder:14b', supportsTools: true })).toBe(false)
    expect(modelUnreliableForTools({ name: 'claude-sonnet-4-6' })).toBe(false)
  })

  it('exports the picker warning copy', () => {
    expect(MODEL_TOOL_WARNING).toBe(
      'This model is unlikely to use tools or delegate. Grants will not do what you expect.',
    )
  })
})
