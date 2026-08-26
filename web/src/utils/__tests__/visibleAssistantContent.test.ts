import { describe, it, expect } from 'vitest'
import { visibleAssistantContent } from '../visibleAssistantContent'

const liveMixed = '{"name": "bash", "arguments": {"command": "echo PONG"}}PONG'
const pureJSON = '{"name": "bash", "arguments": {"command": "hostname"}}'

describe('visibleAssistantContent', () => {
  it('strips leading tool JSON and keeps leftover prose', () => {
    expect(visibleAssistantContent(liveMixed)).toBe('PONG')
  })

  it('hides a lone tool JSON object', () => {
    expect(visibleAssistantContent(pureJSON)).toBe('')
  })

  it('leaves ordinary prose unchanged', () => {
    expect(visibleAssistantContent('hello')).toBe('hello')
  })

  it('leaves fenced JSON samples visible', () => {
    const sample = 'Here is an example:\n```json\n' + pureJSON + '\n```'
    expect(visibleAssistantContent(sample)).toBe(sample)
  })

  it('leaves JSON embedded after prose visible', () => {
    const sample = 'Sure, run ' + pureJSON
    expect(visibleAssistantContent(sample)).toBe(sample)
  })

  it('does not drop the first leftover character on JSON-then-PONG', () => {
    expect(visibleAssistantContent(liveMixed)).toBe('PONG')
    expect(visibleAssistantContent(liveMixed)).not.toBe('ONG')
  })

  it('leaves plain PONG unchanged', () => {
    expect(visibleAssistantContent('PONG')).toBe('PONG')
    expect(visibleAssistantContent('P')).toBe('P')
    expect(visibleAssistantContent('PONG')).not.toBe('ONG')
  })

  it('strips name-only wait_for_threads JSON and leftover function_name', () => {
    expect(visibleAssistantContent('{"name": "wait_for_threads"}')).toBe('')
    expect(visibleAssistantContent('{"name":"wait_for_threads"} then he said PONG')).toBe(
      'then he said PONG',
    )
    expect(visibleAssistantContent('{"function_name":"bash"} leftover')).toBe('leftover')
  })
})
