/** Map an old array index through a splice-then-insert reorder. */
export function remapIndex(n: number, fromIdx: number, toIdx: number): number | null {
  if (n === fromIdx) return toIdx
  if (fromIdx < toIdx) {
    // Item moved down: indices between (fromIdx, toIdx] shift up by 1
    if (n > fromIdx && n <= toIdx) return n - 1
  } else {
    // Item moved up: indices between [toIdx, fromIdx) shift down by 1
    if (n >= toIdx && n < fromIdx) return n + 1
  }
  return n
}
