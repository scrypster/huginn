import { describe, it, expect } from 'vitest'
import {
  parseSystemFailSpeech,
  isFailedToolResult,
  toolCallsFailed,
  messageToolChipFailed,
  conflictingTools,
  plaintextPreview,
  formatVersionLabel,
  TOOLS_ENABLED_SERVE_HINT,
  DENY_WINS_COPY,
} from '../honesty'

describe('parseSystemFailSpeech', () => {
  it('does not treat normal assistant prose as a system fail', () => {
    expect(parseSystemFailSpeech('PONG')).toBeNull()
    expect(parseSystemFailSpeech('The tool failed, sorry.')).toBeNull()
  })

  it('parses TOOL_FAIL as a system fail, not speech', () => {
    const fail = parseSystemFailSpeech(
      'TOOL_FAIL: The "json" tool is not available. Please use a different method.',
    )
    expect(fail).not.toBeNull()
    expect(fail!.kind).toBe('TOOL_FAIL')
    expect(fail!.message).toContain('json')
    expect(fail!.message).not.toMatch(/^TOOL_FAIL/)
  })

  it('parses DELEGATE_FAIL as a system fail', () => {
    const fail = parseSystemFailSpeech('DELEGATE_FAIL: agent tesla is unavailable')
    expect(fail).not.toBeNull()
    expect(fail!.kind).toBe('DELEGATE_FAIL')
    expect(fail!.message).toContain('tesla')
  })

  it('accepts leading whitespace', () => {
    expect(parseSystemFailSpeech('  TOOL_FAIL: missing')).not.toBeNull()
  })

  it('parses a bare TOOL_FAIL token with an empty message', () => {
    const fail = parseSystemFailSpeech('TOOL_FAIL')
    expect(fail).not.toBeNull()
    expect(fail!.kind).toBe('TOOL_FAIL')
    expect(fail!.message).toBe('')
  })

  it('parses a bare DELEGATE_FAIL token with an empty message', () => {
    const fail = parseSystemFailSpeech('DELEGATE_FAIL')
    expect(fail).not.toBeNull()
    expect(fail!.kind).toBe('DELEGATE_FAIL')
    expect(fail!.message).toBe('')
  })

  it('still returns the reason for the colon form', () => {
    const fail = parseSystemFailSpeech(
      'TOOL_FAIL: The "json" tool is not available. Please use a different method to format the response.',
    )
    expect(fail).not.toBeNull()
    expect(fail!.kind).toBe('TOOL_FAIL')
    expect(fail!.message).toBe(
      'The "json" tool is not available. Please use a different method to format the response.',
    )
  })
})

describe('tool chip failure', () => {
  it('does not mark a successful result as failed', () => {
    expect(isFailedToolResult('hi')).toBe(false)
    expect(isFailedToolResult(undefined)).toBe(false)
    expect(toolCallsFailed([{ result: 'ok' }])).toBe(false)
  })

  it('marks error / not-available / denied results as failed', () => {
    expect(isFailedToolResult('error: tool "json" is not available')).toBe(true)
    expect(isFailedToolResult('error: permission denied')).toBe(true)
    expect(isFailedToolResult('TOOL_FAIL: missing')).toBe(true)
  })

  it('marks the chip failed when the bubble is system-fail speech even if the result looks done', () => {
    expect(messageToolChipFailed(
      'TOOL_FAIL: The "json" tool is not available.',
      [{ result: 'ok' }],
    )).toBe(true)
    expect(messageToolChipFailed('TOOL_FAIL', [])).toBe(true)
    expect(messageToolChipFailed('PONG', [{ result: 'hi' }])).toBe(false)
  })
})

describe('conflictingTools — deny wins', () => {
  it('returns names listed in both allow and deny', () => {
    expect(conflictingTools(
      ['read_file', 'write_file', 'bash'],
      ['bash', 'web_search'],
    )).toEqual(['bash'])
  })

  it('is empty when lists do not overlap', () => {
    expect(conflictingTools(['read_file'], ['bash'])).toEqual([])
  })
})

describe('plaintextPreview keeps underscores', () => {
  it('keeps snake_case and TOOL_FAIL underscores', () => {
    expect(plaintextPreview('TOOL_FAIL: The json tool is not available')).toContain('TOOL_FAIL')
    expect(plaintextPreview('read_file returned snake_case')).toContain('read_file')
    expect(plaintextPreview('read_file returned snake_case')).toContain('snake_case')
  })

  it('does not interpret markdown italics', () => {
    const text = plaintextPreview('Steve: TOOL_FAIL: missing')
    expect(text).toBe('Steve: TOOL_FAIL: missing')
    expect(text).not.toBe('Steve: TOOLFAIL: missing')
  })
})

describe('formatVersionLabel', () => {
  it('collapses vv0.4.0-try-all to v0.4.0-try-all', () => {
    expect(formatVersionLabel('vv0.4.0-try-all')).toBe('v0.4.0-try-all')
  })

  it('leaves a single leading v and non-semver labels alone', () => {
    expect(formatVersionLabel('v0.4.0-try-all')).toBe('v0.4.0-try-all')
    expect(formatVersionLabel('dev')).toBe('dev')
  })
})

describe('tools_enabled honesty copy', () => {
  it('states that serve ignores the master switch', () => {
    expect(TOOLS_ENABLED_SERVE_HINT.toLowerCase()).toContain('serve')
    expect(TOOLS_ENABLED_SERVE_HINT.toLowerCase()).toMatch(/tui|cli/)
    expect(TOOLS_ENABLED_SERVE_HINT.toLowerCase()).toContain('does not turn them off')
    expect(DENY_WINS_COPY.toLowerCase()).toContain('deny wins')
  })
})
