import type { ToolCallRecord } from '../composables/useSessions'

/**
 * PRInfo is the PR-as-deliverable summary derived from a turn's persisted
 * tool calls: a successful gh_pr_create/glab_mr_create/bitbucket_pr_create result, plus
 * best-effort branch (from a preceding git_push) and checks state (from a
 * gh_pr_checks/glab_mr_checks/bitbucket_pr_checks call, if one ran). UI-side derivation only —
 * mirrors changedFilesLine.ts, no engine text injection.
 */
export interface PRInfo {
  tool: 'gh_pr_create' | 'glab_mr_create' | 'bitbucket_pr_create'
  title: string
  number: string
  url: string
  branch?: string
  checks?: 'pending' | 'pass' | 'fail'
}

// Matches a GitHub PR URL (.../pull/123), a GitLab MR URL
// (.../-/merge_requests/123), and a Bitbucket PR URL
// (.../pull-requests/123) — what the create tools put in
// their tool result on success (see internal/tools/github.go's
// forgeURLPattern, which this mirrors).
const PR_URL_RE = /https?:\/\/\S+\/(?:pull-requests|pull|merge_requests)\/(\d+)\S*/

/**
 * extractPRInfo scans a turn's tool calls for a successful PR/MR creation
 * and, if found, enriches it with the branch that was pushed and the
 * checks state, both read from OTHER tool calls in the same turn. Returns
 * null when no PR/MR was created (or the create call didn't return a
 * parseable URL, e.g. it errored).
 */
export function extractPRInfo(toolCalls?: ToolCallRecord[]): PRInfo | null {
  if (!toolCalls?.length) return null

  const createCall = toolCalls.find(
    tc => (tc.name === 'gh_pr_create' || tc.name === 'glab_mr_create' || tc.name === 'bitbucket_pr_create') && tc.result && PR_URL_RE.test(tc.result)
  )
  if (!createCall) return null

  const match = createCall.result!.match(PR_URL_RE)
  if (!match) return null
  const url = match[0]
  const number = match[1]!
  const title = typeof createCall.args?.title === 'string' ? createCall.args.title : ''

  return {
    tool: createCall.name as 'gh_pr_create' | 'glab_mr_create' | 'bitbucket_pr_create',
    title,
    number,
    url,
    branch: extractPushedBranch(toolCalls),
    checks: extractChecksState(toolCalls),
  }
}

function extractPushedBranch(toolCalls: ToolCallRecord[]): string | undefined {
  const pushCall = [...toolCalls].reverse().find(tc => tc.name === 'git_push' && tc.result)
  if (!pushCall?.result) return undefined
  // Native `git push` prints a "src -> dest" ref-update line; our git_push
  // tool's own fallback message ("pushed <branch> to <remote>") is used
  // when git itself printed nothing (e.g. already up to date).
  const arrow = pushCall.result.match(/\S+\s*->\s*(\S+)/)
  if (arrow) return arrow[1]
  const fallback = pushCall.result.match(/pushed (\S+) to/)
  return fallback ? fallback[1] : undefined
}

function extractChecksState(toolCalls: ToolCallRecord[]): 'pending' | 'pass' | 'fail' | undefined {
  const checksCall = [...toolCalls]
    .reverse()
    .find(tc => tc.name === 'gh_pr_checks' || tc.name === 'glab_mr_checks' || tc.name === 'bitbucket_pr_checks')
  if (!checksCall) return undefined
  // Prefer the tool's AUTHORITATIVE status (from ToolResult.Metadata.status)
  // over a keyword guess — the old text heuristic defaulted to green on an
  // errored/unknown result and could mis-read a check named "*fail*" (Opus
  // vet 2026-08-29). No pill when we can't be sure.
  switch (checksCall.checksStatus) {
    case 'passed': return 'pass'
    case 'pending': return 'pending'
    case 'failed': return 'fail'
  }
  // No authoritative status: only trust an explicit pending signal; never
  // assume pass.
  const text = (checksCall.result || '').toLowerCase()
  if (text.startsWith('pending') || text.includes('still running')) return 'pending'
  return undefined
}
