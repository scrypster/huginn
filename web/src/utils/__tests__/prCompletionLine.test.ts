import { describe, it, expect } from 'vitest'
import { prCompletionLine } from '../prCompletionLine'
import type { ToolCallRecord } from '../../composables/useSessions'
import type { FileDiff } from '../../composables/useApi'

function makeTC(overrides: Partial<ToolCallRecord> = {}): ToolCallRecord {
  return { id: 't1', name: 'gh_pr_create', args: {}, result: '', done: true, ...overrides }
}

function makeDiff(overrides: Partial<FileDiff> = {}): FileDiff {
  return {
    path: 'internal/tools/mathutil.go',
    unified: '',
    added: 1,
    removed: 1,
    truncated: false,
    is_new: false,
    is_delete: false,
    ...overrides,
  }
}

describe('prCompletionLine', () => {
  it('returns empty string when no PR was opened', () => {
    expect(prCompletionLine(undefined, [makeDiff()])).toBe('')
    expect(prCompletionLine([], [makeDiff()])).toBe('')
  })

  it('returns empty string when a PR was opened but no files changed', () => {
    const calls = [makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' })]
    expect(prCompletionLine(calls, [])).toBe('')
    expect(prCompletionLine(calls, [undefined])).toBe('')
  })

  it('renders a deliverable-handoff line when both a PR and diffs are present', () => {
    const calls = [makeTC({ args: { title: 'Add PR card' }, result: 'https://github.com/scrypster/huginn/pull/123' })]
    const diffs = [makeDiff({ path: 'a.go', added: 5, removed: 2 }), makeDiff({ path: 'b.go', added: 3, removed: 1 })]
    const line = prCompletionLine(calls, diffs)
    expect(line).toBe('Opened PR #123: Add PR card — 2 files changed (+8 −3).')
  })

  it('appends the checks state when a checks call ran', () => {
    const calls = [
      makeTC({ args: { title: 'Add PR card' }, result: 'https://github.com/scrypster/huginn/pull/123' }),
      makeTC({ name: 'gh_pr_checks', result: 'pending: checks still running' }),
    ]
    const line = prCompletionLine(calls, [makeDiff()])
    expect(line).toBe('Opened PR #123: Add PR card — 1 file changed (+1 −1). Checks: pending.')
  })

  it('omits the title segment when the PR has no title', () => {
    const calls = [makeTC({ args: {}, result: 'https://github.com/scrypster/huginn/pull/123' })]
    const line = prCompletionLine(calls, [makeDiff()])
    expect(line).toBe('Opened PR #123 — 1 file changed (+1 −1).')
  })
})
