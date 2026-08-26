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
  classifyHarnessDisplay,
  isA2ATool,
  isBareFailSpeech,
  isDelegationAnnouncement,
  visibleToolCalls,
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

  it('does not treat ordinary speech containing those tokens as fail speech', () => {
    expect(parseSystemFailSpeech('the tool returned TOOL_FAIL unfortunately')).toBeNull()
    expect(parseSystemFailSpeech('hello from Steve')).toBeNull()
  })

  it('classifies auto-approved / delegated / completed / needs-input announcements', () => {
    expect(parseSystemFailSpeech('Delegation to @Steve was auto-approved after 30s.')).toMatchObject({
      kind: 'announcement',
      agent: 'Steve',
    })
    expect(parseSystemFailSpeech('Delegated to @Steve: look up the hostname')).toMatchObject({
      kind: 'announcement',
      agent: 'Steve',
      summary: 'look up the hostname',
    })
    expect(parseSystemFailSpeech('Delegated to @Steve')).toMatchObject({
      kind: 'announcement',
      agent: 'Steve',
    })
    expect(parseSystemFailSpeech('**Steve** completed delegated work: TOOL_FAIL')).toMatchObject({
      kind: 'announcement',
      agent: 'Steve',
      summary: 'TOOL_FAIL',
    })
    expect(parseSystemFailSpeech('@Steve needs input: missing credentials')).toMatchObject({
      kind: 'announcement',
      agent: 'Steve',
      summary: 'missing credentials',
    })
  })

  it('isDelegationAnnouncement is true only for announcement lines', () => {
    expect(isDelegationAnnouncement('Delegation to @Steve was auto-approved after 30s.')).toBe(true)
    expect(isDelegationAnnouncement('TOOL_FAIL')).toBe(false)
    expect(isBareFailSpeech('TOOL_FAIL')).toBe(true)
    expect(isBareFailSpeech('Delegated to @Steve')).toBe(false)
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

  it('does not mark harness announcements as a failed tool chip', () => {
    expect(messageToolChipFailed('Delegated to @Steve: hostname', [])).toBe(false)
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

describe('visibleToolCalls / A2A filter', () => {
  it('omits A2A tools from the chip list', () => {
    const calls = [
      { name: 'delegate_to_agent' },
      { name: 'wait_for_threads' },
      { name: 'list_team_status' },
      { name: 'recall_thread_result' },
      { name: 'read_file' },
    ]
    expect(calls.every(c => c.name === 'read_file' || isA2ATool(c.name))).toBe(true)
    expect(visibleToolCalls(calls).map(c => c.name)).toEqual(['read_file'])
  })

  it('returns empty when only A2A tools are present', () => {
    expect(visibleToolCalls([
      { name: 'delegate_to_agent' },
      { name: 'wait_for_threads' },
    ])).toEqual([])
  })
})

describe('classifyHarnessDisplay', () => {
  it('renders announcement lines as system / completion rows, not teammate voice', () => {
    expect(classifyHarnessDisplay({
      content: 'Delegation to @Steve was auto-approved after 30s.',
      agent: 'Steve',
    } as any)).toEqual({ threadSummary: false, systemLine: true, hideFailSpeech: false })

    expect(classifyHarnessDisplay({
      content: 'Delegated to @Steve: hostname',
    })).toEqual({ threadSummary: false, systemLine: true, hideFailSpeech: false })

    expect(classifyHarnessDisplay({
      content: '**Steve** completed delegated work: TOOL_FAIL',
    })).toEqual({ threadSummary: true, systemLine: false, hideFailSpeech: false })
  })

  it('hides bare TOOL_FAIL speech on a parent that already has A2A chrome', () => {
    expect(classifyHarnessDisplay({
      content: 'TOOL_FAIL',
      toolCalls: [{ name: 'delegate_to_agent' }],
      delegatedThreads: [{ threadId: 't1' }],
    })).toEqual({ threadSummary: false, systemLine: false, hideFailSpeech: true })
  })

  it('keeps a lone fail token in the assistant bubble for the 137 fail chip', () => {
    expect(classifyHarnessDisplay({ content: 'TOOL_FAIL' })).toEqual({
      threadSummary: false,
      systemLine: false,
      hideFailSpeech: false,
    })
  })
})
