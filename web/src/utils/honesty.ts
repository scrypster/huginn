// Harness / A2A speech classification for the Slack-style chat surface.
//
// Persisted thread lifecycle rows and fail tokens are stored as assistant
// messages with an agent name (e.g. Steve). Without this parser they render
// as teammate voice. parseSystemFailSpeech used to be an exact TOOL_FAIL /
// DELEGATE_FAIL token match — it now also recognizes announcement lines.

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

export type SystemSpeechKind = 'tool_fail' | 'delegate_fail' | 'announcement'

export interface SystemSpeech {
  kind: SystemSpeechKind
  agent?: string
  summary?: string
}

const AUTO_APPROVED_RE = /^Delegation to @(\S+) was auto-approved after \d+s\.?$/i
const DELEGATED_TO_RE = /^Delegated to @(\S+)(?::\s*(.*))?$/i
const COMPLETED_WORK_RE = /^\*\*([^*]+)\*\* completed delegated work:\s*(.*)$/i
const NEEDS_INPUT_RE = /^@(\S+) needs input(?::\s*(.*))?$/i

function trimContent(content: string | undefined | null): string {
  return (content ?? '').trim()
}

/**
 * Classify harness fail tokens and delegation announcement lines.
 * Returns null for ordinary agent speech.
 */
export function parseSystemFailSpeech(content: string | undefined | null): SystemSpeech | null {
  const trimmed = trimContent(content)
  if (!trimmed) return null

  if (trimmed === 'TOOL_FAIL') return { kind: 'tool_fail' }
  if (trimmed === 'DELEGATE_FAIL') return { kind: 'delegate_fail' }

  let m = trimmed.match(AUTO_APPROVED_RE)
  if (m) return { kind: 'announcement', agent: m[1] }

  m = trimmed.match(DELEGATED_TO_RE)
  if (m) return { kind: 'announcement', agent: m[1], summary: m[2] || undefined }

  m = trimmed.match(COMPLETED_WORK_RE)
  if (m) return { kind: 'announcement', agent: m[1]?.trim(), summary: m[2] || undefined }

  m = trimmed.match(NEEDS_INPUT_RE)
  if (m) return { kind: 'announcement', agent: m[1], summary: m[2] || undefined }

  return null
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
  return kind === 'tool_fail' || kind === 'delegate_fail'
}

export interface HarnessDisplay {
  threadSummary: boolean
  systemLine: boolean
  hideFailSpeech: boolean
}

/**
 * Decide how a chat row should render given harness-speech classification.
 * Completion announcements reuse the existing threadSummary card. Other
 * announcements and bare fail tokens become muted system lines. A parent
 * row that still owns A2A tools / delegatedThreads keeps its assistant
 * layout so the @delegate strip can attach, but TOOL_FAIL is not spoken.
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
  return { threadSummary: false, systemLine: true, hideFailSpeech: false }
}
