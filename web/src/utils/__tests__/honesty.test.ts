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
  FAIL_COPY,
  failVisibleCopy,
  failDiagnostic,
  failChipLabel,
  failDisplayFor,
  stripResidualSpeech,
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

describe('plaintextPreview display', () => {
  it('humanizes fail tokens instead of leaking TOOL_FAIL', () => {
    expect(plaintextPreview('TOOL_FAIL: The json tool is not available')).toBe(FAIL_COPY.preview)
    expect(plaintextPreview('TOOL_FAIL: The json tool is not available')).not.toContain('TOOL_FAIL')
    expect(plaintextPreview('DELEGATE_FAIL: agent tesla is unavailable')).toBe('Still waiting on Tesla')
    expect(plaintextPreview('Steve: TOOL_FAIL: missing')).toBe(FAIL_COPY.preview)
  })

  it('keeps snake_case in ordinary speech and does not eat underscores', () => {
    expect(plaintextPreview('read_file returned snake_case')).toContain('read_file')
    expect(plaintextPreview('read_file returned snake_case')).toContain('snake_case')
    expect(plaintextPreview('check TOOL_FAIL in the log')).toBe('check TOOL_FAIL in the log')
  })

  it('does not preview leftover tool JSON or wait_for_threads', () => {
    expect(plaintextPreview('{"name":"wait_for_threads","arguments":{}}')).toBe('')
    expect(plaintextPreview('{"name":"wait_for_threads","arguments":{}}')).not.toContain('wait_for_threads')
    expect(plaintextPreview('{"name":"bash","arguments":{"command":"x"}}TOOL_FAIL')).toBe(FAIL_COPY.preview)
  })
})

describe('fail display copy', () => {
  it('speaks in the agent voice and keeps the raw token on the diagnostic', () => {
    expect(failVisibleCopy('TOOL_FAIL: The "json" tool is not available.')).toBe(FAIL_COPY.tool)
    expect(failVisibleCopy('TOOL_FAIL')).toBe(FAIL_COPY.tool)
    expect(failVisibleCopy('DELEGATE_FAIL: agent tesla is unavailable')).toBe(
      'I asked Tesla and they haven\'t come back yet.',
    )
    expect(failVisibleCopy('DELEGATE_FAIL')).toBe(FAIL_COPY.delegate)
    expect(failVisibleCopy('TOOL_FAIL: permission denied', { toolName: 'bash' })).toBe(FAIL_COPY.shell)
    expect(failChipLabel()).toBe(FAIL_COPY.chip)
  })

  it('puts token, tool, and reason on the diagnostic only', () => {
    const diag = failDiagnostic(
      'TOOL_FAIL: The "json" tool is not available.',
      { toolName: 'json' },
    )
    expect(diag).toContain('TOOL_FAIL')
    expect(diag).toContain('json')
    expect(diag).toContain('The "json" tool is not available.')
    expect(failVisibleCopy('TOOL_FAIL: The "json" tool is not available.')).not.toContain('TOOL_FAIL')
  })

  it('bundles display for a failed user-facing tool', () => {
    const d = failDisplayFor(
      'TOOL_FAIL: The "json" tool is not available.',
      [{ name: 'json', result: 'error: tool "json" is not available' }],
    )
    expect(d).not.toBeNull()
    expect(d!.copy).toBe(FAIL_COPY.tool)
    expect(d!.chip).toBe(FAIL_COPY.chip)
    expect(d!.toolName).toBe('json')
    expect(d!.diagnostic).toContain('TOOL_FAIL')
    expect(d!.diagnostic).toContain('json')
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

import { stripResidualSpeech, isToolInvocationObject, isResultShapedObject } from '../honesty'

describe('stripResidualSpeech', () => {
  // 2026-08-26 live 14b CoS agentOutput after delegate + wait already ran.
  const live = '<wait for Reggie to finish>\nOnce Reggie has finished:\nThen calculate:\n7 times 8 is 56.{\n  "pong_response": "PONG",\n  "multiplication_result": "56"\n}'

  it('leaves only human prose after tools ran', () => {
    expect(stripResidualSpeech(live, { afterTools: true })).toBe('7 times 8 is 56.')
  })

  it('strips invented recall_thread_result JSON without touching prose', () => {
    const s = 'Reggie said PONG. 7 times 8 is 56.\n{"name":"recall_thread_result","arguments":{"thread_id":"<thread_id>"}}'
    expect(stripResidualSpeech(s, { afterTools: true })).toBe('Reggie said PONG. 7 times 8 is 56.')
  })

  it('keeps a real go fence', () => {
    const s = 'Here is the helper:\n```go\nfunc add(a, b int) int { return a + b }\n```\n<wait for Reggie to finish>\nThat compiles.'
    expect(stripResidualSpeech(s, { afterTools: true })).toBe(
      'Here is the helper:\n```go\nfunc add(a, b int) int { return a + b }\n```\nThat compiles.',
    )
  })

  it('never shows harness tool names or fail tokens as speech', () => {
    const s = 'wait_for_threads\nTOOL_FAIL: nope\n<wait_for_threads>\nReggie said PONG.\nrecall_thread_result\nbash'
    expect(stripResidualSpeech(s)).toBe('Reggie said PONG.')
  })

  it('keeps in-sentence tool JSON and lone result objects when not afterTools / no prose', () => {
    const sentence = 'Sure, run {"name": "bash", "arguments": {"command": "hostname"}}'
    expect(stripResidualSpeech(sentence)).toBe(sentence)
    expect(stripResidualSpeech('{"pong_response":"PONG"}', { afterTools: true })).toBe('{"pong_response":"PONG"}')
  })

  it('keeps teammate prose and standalone Then-lines', () => {
    for (const s of [
      'Reggie said PONG. 7 times 8 is 56.',
      "I'll wait for your go-ahead before deploying.",
      'Once the migration has finished, the table will have 3 columns.',
      'Then run the tests:\n```sh\ngo test ./...\n```\nAfter that, we ship.',
      'Use this config: {"server": {"port": 8080}} and restart.',
    ]) {
      expect(stripResidualSpeech(s, { afterTools: true })).toBe(s)
    }
  })

  // 2026-08-26 huginn-dev161 S5 speech turn after delegate + wait had run: a
  // // comment inside the tool JSON and echoed result fragments after the answer.
  const liveV2 = 'After Reggie responds with PONG:\n\n{\n  "name": "recall_thread_result",\n  "arguments": {\n    "thread_id": "thread-12345"  // Replace with the actual thread ID\n  }\n}\n\n7 times 8 is 56."PONG"\n56PONG\n56'

  it('leaves only teammate prose for the dev161 S5 leak', () => {
    expect(stripResidualSpeech(liveV2, { afterTools: true })).toBe('7 times 8 is 56.')
  })

  it('treats "After X responds…:" / "When X replies:" as wait-glue', () => {
    for (const glue of ['After Reggie responds with PONG:', 'When Reggie replies:', 'Once Reggie comes back with the result:', 'After Reggie responds,']) {
      expect(stripResidualSpeech(`${glue}\n7 times 8 is 56.`)).toBe('7 times 8 is 56.')
    }
    expect(stripResidualSpeech('After lunch I\'ll pick this up.', { afterTools: true })).toBe('After lunch I\'ll pick this up.')
  })

  it('drops echo fragments only when they already appeared; a fresh answer line stays', () => {
    expect(stripResidualSpeech('Reggie said PONG. 7 times 8 is 56.\n56PONG\n56', { afterTools: true })).toBe('Reggie said PONG. 7 times 8 is 56.')
    expect(stripResidualSpeech('7 times 8 is:\n56', { afterTools: true })).toBe('7 times 8 is:\n56')
    expect(stripResidualSpeech('Reggie said "PONG" back.', { afterTools: true })).toBe('Reggie said "PONG" back.')
  })

  it('strips commented tool JSON but keeps a go fence with // comments', () => {
    const s = 'Here:\n```go\n// add returns a+b\nfunc add(a, b int) int { return a + b }\n```\nAfter Reggie responds with PONG:\n{"name": "bash", "arguments": {"command": "ls" // list\n}}\nThat compiles.'
    expect(stripResidualSpeech(s, { afterTools: true })).toBe('Here:\n```go\n// add returns a+b\nfunc add(a, b int) int { return a + b }\n```\nThat compiles.')
  })

  it('drops exact live bullet5 wait playbook and assistance closer, keeps hostname/PONG/56', () => {
    const spawned =
      "Winston, please note that the delegate task to Steve has been spawned. Please use `wait_for_threads` to block until it finishes and collect the result. Since session history could not be loaded, please ensure to include all necessary context in the task description. The hostname of the machine is 'MJs-MacBook-Pro'. That completes the request. Is there anything else you need assistance with?"
    expect(stripResidualSpeech(spawned, { afterTools: true })).toBe(
      "The hostname of the machine is 'MJs-MacBook-Pro'. That completes the request.",
    )

    const pleaseCall =
      "Please call wait_for_threads, with no additional arguments, to block until Steve's command has finished. Steve ran the 'hostname' command and received the output 'MJs-MacBook-Pro'."
    expect(stripResidualSpeech(pleaseCall, { afterTools: true })).toBe(
      "Steve ran the 'hostname' command and received the output 'MJs-MacBook-Pro'.",
    )

    const closer =
      "The hostname of the system is 'MJs-MacBook-Pro'. If you have any other questions or need further assistance, feel free to ask."
    expect(stripResidualSpeech(closer, { afterTools: true })).toBe(
      "The hostname of the system is 'MJs-MacBook-Pro'.",
    )

    expect(
      stripResidualSpeech(
        "Please call wait_for_threads, with no additional arguments, to block until Steve's command has finished. Reggie said PONG. 7 times 8 is 56.",
        { afterTools: true },
      ),
    ).toBe('Reggie said PONG. 7 times 8 is 56.')
    expect(stripResidualSpeech('Steve spawned a worker to crunch the numbers.', { afterTools: true })).toBe(
      'Steve spawned a worker to crunch the numbers.',
    )
  })

  it('drops the live hallway helpdesk closer and keeps teammate prose', () => {
    expect(stripResidualSpeech('The result of 7 * 8 is 56. If you have any other questions, feel free to ask!', { afterTools: true })).toBe('The result of 7 * 8 is 56.')
    expect(stripResidualSpeech('If you have any other questions about the migration, ping Steve.', { afterTools: true })).toBe('If you have any other questions about the migration, ping Steve.')
    expect(stripResidualSpeech('Feel free to ask Steve for the hostname.', { afterTools: true })).toBe('Feel free to ask Steve for the hostname.')
  })

  it('rewrites the live Lab Winston Steve-deny leftover to teammate speech', () => {
    const liveLabWinstonSteveDeny =
      'I apologize for any confusion. It seems that "Steve" isn\'t one of the available agents. Let\'s try a different approach. I\'ll have Sam gather the required information. Task delegated to Sam. I apologize, but there was an error when attempting to determine the hostname of this machine. The system encountered an API key resolution issue. If you have access to the API key, please provide it, and I can try again.'
    expect(stripResidualSpeech(liveLabWinstonSteveDeny, { afterTools: true })).toBe("Steve isn't in Lab. Sam is.")
    expect(stripResidualSpeech("Steve isn't in this company.", { afterTools: true })).toBe("Steve isn't in this company.")
    expect(stripResidualSpeech("Steve isn't available here.", { afterTools: true })).toBe("Steve isn't available here.")
    const keep =
      "I apologize for any confusion. The hostname of the machine is 'MJs-MacBook-Pro'. Reggie said PONG. 7 times 8 is 56. Let's try a different approach."
    const kept = stripResidualSpeech(keep, { afterTools: true })
    expect(kept).toContain('MJs-MacBook-Pro')
    expect(kept).toContain('PONG')
    expect(kept).toContain('56')
    expect(kept).not.toContain('I apologize')
    expect(kept).not.toContain('different approach')
  })

  it('classifies objects', () => {
    expect(isToolInvocationObject({ name: 'recall_thread_result', arguments: { thread_id: 'x' } })).toBe(true)
    expect(isToolInvocationObject({ function_name: 'bash' })).toBe(true)
    expect(isToolInvocationObject({ pong_response: 'PONG' })).toBe(false)
    expect(isResultShapedObject({ pong_response: 'PONG', multiplication_result: '56' })).toBe(true)
    expect(isResultShapedObject({ server: { port: 8080 } })).toBe(false)
    expect(isResultShapedObject({})).toBe(false)
  })
})

describe('stripResidualSpeech — loading model / third-person / clock', () => {
  it('drops Loading model status', () => {
    expect(stripResidualSpeech('Loading model, please wait...')).toBe('')
  })
  it('drops truncated rail Loading model, pleas', () => {
    expect(stripResidualSpeech('Loading model, pleas')).toBe('')
    expect(stripResidualSpeech('Loading model, pleas…')).toBe('')
  })
  it('drops third-person @Winston has noted', () => {
    const got = stripResidualSpeech('Understood, @Winston has noted your preferences for your dog Odin.')
    expect(got).not.toMatch(/has noted/)
    expect(got).not.toMatch(/@Winston/)
  })
  it('strips Local time now: label', () => {
    const got = stripResidualSpeech('Local time now: Thursday, August 27, 2026, 2:28 PM ET')
    expect(got.toLowerCase()).not.toContain('local time now')
  })
})

describe('plaintextPreview never Loading model', () => {
  it('drops full and truncated status', () => {
    expect(plaintextPreview('Loading model, please wait...')).toBe('')
    expect(plaintextPreview('Loading model, pleas')).toBe('')
    expect(plaintextPreview('Loading model, pleas…')).toBe('')
  })
})
