import { describe, it, expect } from 'vitest'
import { extractPRInfo } from '../prInfo'
import type { ToolCallRecord } from '../../composables/useSessions'

function makeTC(overrides: Partial<ToolCallRecord> = {}): ToolCallRecord {
  return {
    id: 't1',
    name: 'gh_pr_create',
    args: {},
    result: '',
    done: true,
    ...overrides,
  }
}

describe('extractPRInfo', () => {
  it('returns null when there are no tool calls', () => {
    expect(extractPRInfo(undefined)).toBeNull()
    expect(extractPRInfo([])).toBeNull()
  })

  it('returns null when no gh_pr_create/glab_mr_create ran', () => {
    const calls = [makeTC({ name: 'read_file', result: 'contents' })]
    expect(extractPRInfo(calls)).toBeNull()
  })

  it('returns null when gh_pr_create errored (no URL in result)', () => {
    const calls = [makeTC({ result: 'gh pr create: exit status 1\nno commits between main and feature' })]
    expect(extractPRInfo(calls)).toBeNull()
  })

  it('extracts title/number/url from a successful gh_pr_create', () => {
    const calls = [
      makeTC({
        args: { title: 'Add PR card', body: 'x' },
        result: 'https://github.com/scrypster/huginn/pull/123',
      }),
    ]
    const pr = extractPRInfo(calls)
    expect(pr).toEqual({
      tool: 'gh_pr_create',
      title: 'Add PR card',
      number: '123',
      url: 'https://github.com/scrypster/huginn/pull/123',
      branch: undefined,
      checks: undefined,
    })
  })

  it('extracts a glab_mr_create URL shape', () => {
    const calls = [
      makeTC({
        name: 'glab_mr_create',
        args: { title: 'Add MR card' },
        result: 'https://gitlab.com/scrypster/huginn/-/merge_requests/456',
      }),
    ]
    const pr = extractPRInfo(calls)
    expect(pr?.tool).toBe('glab_mr_create')
    expect(pr?.number).toBe('456')
  })

  it('derives the branch from a preceding git_push tool call (arrow form)', () => {
    const calls = [
      makeTC({ name: 'git_push', result: ' * [new branch]      feature-x -> feature-x' }),
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
    ]
    expect(extractPRInfo(calls)?.branch).toBe('feature-x')
  })

  it('derives the branch from the git_push fallback message', () => {
    const calls = [
      makeTC({ name: 'git_push', result: 'pushed feature-y to origin' }),
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
    ]
    expect(extractPRInfo(calls)?.branch).toBe('feature-y')
  })

  it('reads checks state pending from a gh_pr_checks call', () => {
    const calls = [
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
      makeTC({ name: 'gh_pr_checks', result: 'pending: checks still running', checksStatus: 'pending' }),
    ]
    expect(extractPRInfo(calls)?.checks).toBe('pending')
  })

  it('reads checks state fail from a gh_pr_checks call', () => {
    const calls = [
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
      makeTC({ name: 'gh_pr_checks', result: 'gh pr checks: exit status 1', checksStatus: 'failed' }),
    ]
    expect(extractPRInfo(calls)?.checks).toBe('fail')
  })

  it('shows NO pill when checks status is unknown (never defaults to green)', () => {
    const calls = [
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
      makeTC({ name: 'gh_pr_checks', result: 'weird-check named fail-fast passed' }),
    ]
    expect(extractPRInfo(calls)?.checks).toBeUndefined()
  })

  it('reads checks state pass from a gh_pr_checks call', () => {
    const calls = [
      makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' }),
      makeTC({ name: 'gh_pr_checks', result: 'lint  pass  2m\ntest  pass  1m', checksStatus: 'passed' }),
    ]
    expect(extractPRInfo(calls)?.checks).toBe('pass')
  })

  it('leaves checks undefined when no checks call ran', () => {
    const calls = [makeTC({ result: 'https://github.com/scrypster/huginn/pull/1' })]
    expect(extractPRInfo(calls)?.checks).toBeUndefined()
  })
})
