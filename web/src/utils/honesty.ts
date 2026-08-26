/**
 * Small honesty helpers for Settings vs serve, failed tool chips,
 * system-fail speech, plaintext sidebar previews, and A2A harness rows.
 */

export const TOOLS_ENABLED_SERVE_HINT =
  'TUI/CLI only. huginn serve always registers builtin tools; this switch does not turn them off. Use the deny list to block a tool in the web UI.'

export const DENY_WINS_COPY =
  'Deny wins — a tool listed in both allow and deny stays blocked.'

// Colon is optional: hydrated Steve DMs store the bare token with no reason.
const SYSTEM_FAIL_RE = /^(TOOL_FAIL|DELEGATE_FAIL)(?:\s*:\s*([\s\S]*))?$/i
const FAILED_RESULT_RE = /^(error:|TOOL_FAIL\b|DELEGATE_FAIL\b)|is not available|permission denied/i

export const A2A_TOOL_NAMES = new Set([
  'delegate_to_agent',
  'wait_for_threads',
  'list_team_status',
  'recall_thread_result',
])

export function isA2ATool(name: string | undefined | null): boolean {
  return !!name && A2A_TOOL_NAMES.has(name)
}

export function visibleToolCalls<T extends { name: string }>(calls?: T[] | null): T[] {
  return (calls ?? []).filter(tc => !isA2ATool(tc.name))
}

export type SystemFailKind = 'TOOL_FAIL' | 'DELEGATE_FAIL' | 'announcement'

export interface SystemFailSpeech {
  kind: SystemFailKind
  message: string
  agent?: string
  summary?: string
}

/** @deprecated alias — 142 harness helpers used SystemSpeech / SystemSpeechKind */
export type SystemSpeechKind = SystemFailKind
export type SystemSpeech = SystemFailSpeech

const AUTO_APPROVED_RE = /^Delegation to @(\S+) was auto-approved after \d+s\.?$/i
const DELEGATED_TO_RE = /^Delegated to @(\S+)(?::\s*(.*))?$/i
const COMPLETED_WORK_RE = /^\*\*([^*]+)\*\* completed delegated work:\s*(.*)$/i
const NEEDS_INPUT_RE = /^@(\S+) needs input(?::\s*(.*))?$/i

function trimContent(content: string | undefined | null): string {
  return (content ?? '').trim()
}

/** Detect assistant text that is a system failure or harness announcement, not teammate speech. */
export function parseSystemFailSpeech(content: string | undefined | null): SystemFailSpeech | null {
  const trimmed = trimContent(content)
  if (!trimmed) return null

  const m = trimmed.match(SYSTEM_FAIL_RE)
  if (m) {
    const kind = m[1]!.toUpperCase() === 'DELEGATE_FAIL' ? 'DELEGATE_FAIL' : 'TOOL_FAIL'
    return { kind, message: (m[2] ?? '').trim() }
  }

  let am = trimmed.match(AUTO_APPROVED_RE)
  if (am) return { kind: 'announcement', message: '', agent: am[1] }

  am = trimmed.match(DELEGATED_TO_RE)
  if (am) return { kind: 'announcement', message: '', agent: am[1], summary: am[2] || undefined }

  am = trimmed.match(COMPLETED_WORK_RE)
  if (am) return { kind: 'announcement', message: '', agent: am[1]?.trim(), summary: am[2] || undefined }

  am = trimmed.match(NEEDS_INPUT_RE)
  if (am) return { kind: 'announcement', message: '', agent: am[1], summary: am[2] || undefined }

  return null
}

export function isFailedToolResult(result?: string): boolean {
  const text = (result ?? '').trim()
  if (!text) return false
  return FAILED_RESULT_RE.test(text)
}

export function toolCallsFailed(calls?: Array<{ result?: string }>): boolean {
  return !!calls?.some(tc => isFailedToolResult(tc.result))
}

/** Chip is failed if any call failed or the message itself is system-fail speech. */
export function messageToolChipFailed(
  content?: string,
  calls?: Array<{ result?: string }>,
): boolean {
  const parsed = parseSystemFailSpeech(content)
  const isFail = !!parsed && parsed.kind !== 'announcement'
  return toolCallsFailed(calls) || isFail
}

/** Intersection of allow and deny lists (case-sensitive tool names). */
export function conflictingTools(allowed: string[], disallowed: string[]): string[] {
  if (!allowed.length || !disallowed.length) return []
  const deny = new Set(disallowed)
  const seen = new Set<string>()
  const out: string[] = []
  for (const name of allowed) {
    if (deny.has(name) && !seen.has(name)) {
      seen.add(name)
      out.push(name)
    }
  }
  return out
}

/**
 * Plaintext sidebar snippet. Underscores (snake_case, TOOL_FAIL) must survive.
 * Does not interpret markdown italics.
 */
export function plaintextPreview(content: string, max = 48): string {
  const raw = content.replace(/[`#*>[\]]/g, '').replace(/\s+/g, ' ').trim()
  return raw.length > max ? raw.slice(0, max) + '…' : raw
}

/** Collapse a doubled leading "v" so vv0.4.0-try-all becomes v0.4.0-try-all. */
export function formatVersionLabel(raw: string): string {
  const s = raw.trim()
  if (!s) return '…'
  return s.replace(/^v{2,}(?=\d)/i, 'v')
}

export function isDelegationAnnouncement(content: string | undefined | null): boolean {
  return parseSystemFailSpeech(content)?.kind === 'announcement'
}

export function isCompletedDelegationAnnouncement(content: string | undefined | null): boolean {
  return COMPLETED_WORK_RE.test(trimContent(content))
}

/** True when the whole message is a fail token (not an announcement wrapping one). */
export function isBareFailSpeech(content: string | undefined | null): boolean {
  const kind = parseSystemFailSpeech(content)?.kind
  return kind === 'TOOL_FAIL' || kind === 'DELEGATE_FAIL'
}

export interface HarnessDisplay {
  threadSummary: boolean
  systemLine: boolean
  hideFailSpeech: boolean
}

/**
 * Decide how a chat row should render given harness-speech classification.
 * Completion announcements reuse the existing threadSummary card. Other
 * announcements become muted system lines. Fail tokens stay in the assistant
 * bubble so the 137 fail chip can render; a parent row that still owns A2A
 * tools / delegatedThreads keeps its assistant layout and hides raw TOOL_FAIL.
 */
export function classifyHarnessDisplay(msg: {
  content?: string
  threadSummary?: boolean
  toolCalls?: Array<{ name: string }>
  delegatedThreads?: unknown[]
}): HarnessDisplay {
  if (msg.threadSummary) {
    return { threadSummary: true, systemLine: false, hideFailSpeech: false }
  }
  const parsed = parseSystemFailSpeech(msg.content)
  if (!parsed) {
    return { threadSummary: false, systemLine: false, hideFailSpeech: false }
  }
  if (parsed.kind === 'announcement') {
    const completion = isCompletedDelegationAnnouncement(msg.content)
    return {
      threadSummary: completion,
      systemLine: !completion,
      hideFailSpeech: false,
    }
  }
  const hasDelegationChrome = !!(msg.delegatedThreads?.length || msg.toolCalls?.length)
  if (hasDelegationChrome) {
    return { threadSummary: false, systemLine: false, hideFailSpeech: true }
  }
  // Lone fail token: keep the 137 red system-fail chip in the assistant bubble.
  return { threadSummary: false, systemLine: false, hideFailSpeech: false }
}
