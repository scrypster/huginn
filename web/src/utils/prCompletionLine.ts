import type { FileDiff } from '../composables/useApi'
import type { ToolCallRecord } from '../composables/useSessions'
import { extractPRInfo } from './prInfo'

/**
 * prCompletionLine derives a deliverable-handoff completion line —
 * "Opened PR #123: <title> — 2 files changed (+8 −3). Checks: pending." —
 * for a turn that both changed files AND opened a PR. UI-side only, derived
 * from persisted tool calls (mirrors changedFilesLine.ts) — no engine text
 * injection. Returns '' when the turn didn't open a PR, or opened one
 * without changing any files (a PR with no diffs isn't a "deliverable"
 * line — changedFilesLine/the tool-call chip cover that case instead).
 */
export function prCompletionLine(toolCalls: ToolCallRecord[] | undefined, diffs: (FileDiff | undefined)[]): string {
  const pr = extractPRInfo(toolCalls)
  const files = diffs.filter((d): d is FileDiff => !!d)
  if (!pr || !files.length) return ''

  const totalAdded = files.reduce((n, d) => n + d.added, 0)
  const totalRemoved = files.reduce((n, d) => n + d.removed, 0)
  const filesPart = `${files.length} file${files.length === 1 ? '' : 's'} changed (+${totalAdded} −${totalRemoved})`
  const checksPart = pr.checks ? ` Checks: ${pr.checks}.` : ''
  const titlePart = pr.title ? `: ${pr.title}` : ''

  return `Opened PR #${pr.number}${titlePart} — ${filesPart}.${checksPart}`
}
