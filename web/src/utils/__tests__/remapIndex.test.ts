import { describe, it, expect } from 'vitest'
import { remapIndex } from '../remapIndex'

describe('remapIndex', () => {
  it('item moved to itself returns same index', () => {
    expect(remapIndex(3, 3, 3)).toBe(3)
  })

  it('item is the moved item — returns toIdx', () => {
    expect(remapIndex(2, 2, 5)).toBe(5)
  })

  it('item in shift range (moved down) — shifts up by 1', () => {
    // fromIdx=1 moved to toIdx=4, n=3 is in (1,4] → returns 2
    expect(remapIndex(3, 1, 4)).toBe(2)
  })

  it('item outside range (moved down) — unchanged', () => {
    // fromIdx=1 moved to toIdx=4, n=0 is outside → returns 0
    expect(remapIndex(0, 1, 4)).toBe(0)
  })

  it('item in shift range (moved up) — shifts down by 1', () => {
    // fromIdx=4 moved to toIdx=1, n=2 is in [1,4) → returns 3
    expect(remapIndex(2, 4, 1)).toBe(3)
  })

  it('item outside range (moved up) — unchanged', () => {
    // fromIdx=4 moved to toIdx=1, n=5 is outside → returns 5
    expect(remapIndex(5, 4, 1)).toBe(5)
  })

  it('single element list — returns 0', () => {
    expect(remapIndex(0, 0, 0)).toBe(0)
  })

  it('fromIdx === toIdx — returns n unchanged', () => {
    expect(remapIndex(7, 3, 3)).toBe(7)
  })
})
