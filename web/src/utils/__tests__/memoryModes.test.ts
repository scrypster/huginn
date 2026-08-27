import { describe, expect, it } from 'vitest'
import { MEMORY_MODES, normalizeMemoryMode } from '../memoryModes'

describe('MEMORY_MODES', () => {
  it('keeps the agent-settings taxonomy only', () => {
    expect(MEMORY_MODES.map(m => m.value)).toEqual(['passive', 'conversational', 'immersive'])
    expect(MEMORY_MODES.map(m => m.label)).toEqual(['Passive', 'Conversational', 'Immersive'])
  })

  it('normalizes unknown values to conversational', () => {
    expect(normalizeMemoryMode('immersive')).toBe('immersive')
    expect(normalizeMemoryMode('')).toBe('conversational')
    expect(normalizeMemoryMode('aggressive')).toBe('conversational')
  })
})
