import { describe, it, expect } from 'vitest'
import { changedFilesLine } from '../changedFilesLine'
import type { FileDiff } from '../../composables/useApi'

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

describe('changedFilesLine', () => {
  it('returns empty string when there are no diffs', () => {
    expect(changedFilesLine('done!', [undefined, undefined])).toBe('')
  })

  it('returns empty string when there are no tool calls at all', () => {
    expect(changedFilesLine('done!', [])).toBe('')
  })

  it('renders a compact line for a single changed file', () => {
    const line = changedFilesLine('Fixed the bug.', [makeDiff()])
    expect(line).toBe('Changed mathutil.go (+1 −1)')
  })

  it('suppresses the line when the speech already names the file', () => {
    const line = changedFilesLine('I fixed mathutil.go\'s Add function.', [makeDiff()])
    expect(line).toBe('')
  })

  it('is case-insensitive when checking whether speech already mentions the file', () => {
    const line = changedFilesLine('Fixed MATHUTIL.GO', [makeDiff()])
    expect(line).toBe('')
  })

  it('aggregates multiple changed files into one line', () => {
    const line = changedFilesLine('Done.', [
      makeDiff({ path: 'a.go', added: 2, removed: 0 }),
      makeDiff({ path: 'b.go', added: 1, removed: 3 }),
    ])
    expect(line).toBe('Changed 2 files (+3 −3): a.go, b.go')
  })

  it('does not suppress the multi-file line unless every file is mentioned', () => {
    const line = changedFilesLine('Fixed a.go', [
      makeDiff({ path: 'a.go' }),
      makeDiff({ path: 'b.go' }),
    ])
    expect(line).not.toBe('')
    expect(line).toContain('a.go, b.go')
  })

  it('ignores tool calls that did not produce a diff (non-file tools)', () => {
    const line = changedFilesLine('Checked the weather.', [undefined, makeDiff({ path: 'notes.md' })])
    expect(line).toBe('Changed notes.md (+1 −1)')
  })
})
