import type { FileDiff } from '../composables/useApi'

/**
 * changedFilesLine derives a compact "Changed mathutil.go (+1 −1)" system
 * fallback line from a turn's persisted diffs — UI-side only, no engine
 * text injection (see the hallway completion-line mission). Returns '' when
 * there's nothing to say (no diffs) or when the model's own speech already
 * names every changed file by basename, so a normal "I fixed mathutil.go's
 * Add bug" reply doesn't get a redundant echo underneath it.
 */
export function changedFilesLine(speech: string, diffs: (FileDiff | undefined)[]): string {
  const files = diffs.filter((d): d is FileDiff => !!d)
  if (!files.length) return ''

  const lowerSpeech = (speech || '').toLowerCase()
  const basename = (p: string) => p.split('/').pop() || p
  const allMentioned = files.every(d => lowerSpeech.includes(basename(d.path).toLowerCase()))
  if (allMentioned) return ''

  if (files.length === 1) {
    const d = files[0]!
    return `Changed ${basename(d.path)} (+${d.added} −${d.removed})`
  }

  const totalAdded = files.reduce((n, d) => n + d.added, 0)
  const totalRemoved = files.reduce((n, d) => n + d.removed, 0)
  const names = files.map(d => basename(d.path)).join(', ')
  return `Changed ${files.length} files (+${totalAdded} −${totalRemoved}): ${names}`
}
