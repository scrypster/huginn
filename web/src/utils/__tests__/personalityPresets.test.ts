import { describe, it, expect } from 'vitest'
import { PERSONALITY_PRESETS, normalizePersonality, personalityLabel, DEFAULT_PERSONALITY } from '../personalityPresets'

describe('personalityPresets', () => {
  it('lists exactly the 5 presets from the design brief', () => {
    const values = PERSONALITY_PRESETS.map(p => p.value)
    expect(values).toEqual(['default', 'strict-reviewer', 'fast-builder', 'skeptical-architect', 'terse-operator'])
  })

  it('every preset has a non-empty label and description', () => {
    for (const p of PERSONALITY_PRESETS) {
      expect(p.label.length).toBeGreaterThan(0)
      expect(p.description.length).toBeGreaterThan(0)
    }
  })

  it('normalizePersonality falls back to default for unknown values', () => {
    expect(normalizePersonality('not-a-real-preset')).toBe(DEFAULT_PERSONALITY)
    expect(normalizePersonality(undefined)).toBe(DEFAULT_PERSONALITY)
  })

  it('normalizePersonality passes through a known value', () => {
    expect(normalizePersonality('strict-reviewer')).toBe('strict-reviewer')
  })

  it('personalityLabel resolves the display label', () => {
    expect(personalityLabel('strict-reviewer')).toBe('Strict Reviewer')
    expect(personalityLabel(undefined)).toBe('Default')
  })
})
