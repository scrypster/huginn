import { describe, it, expect } from 'vitest'
import {
  classifyHarnessDisplay,
  isA2ATool,
  isBareFailSpeech,
  isDelegationAnnouncement,
  parseSystemFailSpeech,
  visibleToolCalls,
} from '../honesty'

describe('parseSystemFailSpeech', () => {
  it('matches exact TOOL_FAIL / DELEGATE_FAIL tokens', () => {
    expect(parseSystemFailSpeech('TOOL_FAIL')).toEqual({ kind: 'tool_fail' })
    expect(parseSystemFailSpeech('DELEGATE_FAIL')).toEqual({ kind: 'delegate_fail' })
    expect(parseSystemFailSpeech('  TOOL_FAIL  ')).toEqual({ kind: 'tool_fail' })
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

  it('renders a lone fail token as a system line', () => {
    expect(classifyHarnessDisplay({ content: 'TOOL_FAIL' })).toEqual({
      threadSummary: false,
      systemLine: true,
      hideFailSpeech: false,
    })
  })
})
