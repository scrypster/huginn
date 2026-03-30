import { describe, it, expect } from 'vitest'
import { pruneOrphanedUnseenIds } from '../unseenSessions'

describe('pruneOrphanedUnseenIds', () => {
  it('keeps regular session IDs that are in the known sessions set', () => {
    const result = pruneOrphanedUnseenIds(
      ['sess-1', 'sess-2'],
      id => ['sess-1', 'sess-2'].includes(id),
      () => null,
    )
    expect(result).toEqual(['sess-1', 'sess-2'])
  })

  it('keeps space session IDs that have a space mapping', () => {
    const result = pruneOrphanedUnseenIds(
      ['space-sess-1'],
      () => false,
      id => id === 'space-sess-1' ? 'space-abc' : null,
    )
    expect(result).toEqual(['space-sess-1'])
  })

  it('removes orphaned IDs with no session or space mapping', () => {
    const result = pruneOrphanedUnseenIds(
      ['orphan-1', 'orphan-2'],
      () => false,
      () => null,
    )
    expect(result).toEqual([])
  })

  it('removes only the orphaned entries while keeping valid ones', () => {
    // This is the core regression test: 3 orphaned IDs + 1 valid space session
    const result = pruneOrphanedUnseenIds(
      ['orphan-1', 'orphan-2', 'orphan-3', 'space-sess-valid'],
      () => false,
      id => id === 'space-sess-valid' ? 'space-xyz' : null,
    )
    expect(result).toEqual(['space-sess-valid'])
  })

  it('returns empty array when input is empty', () => {
    expect(pruneOrphanedUnseenIds([], () => false, () => null)).toEqual([])
  })
})
