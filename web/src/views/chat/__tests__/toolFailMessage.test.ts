import { describe, it, expect } from 'vitest'
import { parseToolFailContent } from '../toolFailMessage'

describe('parseToolFailContent', () => {
  it('extracts the error after TOOL_FAIL:', () => {
    expect(parseToolFailContent('TOOL_FAIL: The "json" tool is not available.')).toBe(
      'The "json" tool is not available.',
    )
  })

  it('returns null for normal assistant text', () => {
    expect(parseToolFailContent('I will look that up for you.')).toBeNull()
    expect(parseToolFailContent('')).toBeNull()
    expect(parseToolFailContent(undefined)).toBeNull()
  })

  it('does not treat mid-paragraph TOOL_FAIL as a system error', () => {
    expect(parseToolFailContent('Sorry — TOOL_FAIL: The "json" tool is not available.')).toBeNull()
  })
})
