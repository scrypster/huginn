/**
 * Small honesty helpers for Settings vs serve, failed tool chips,
 * system-fail speech, and plaintext sidebar previews.
 */

export const TOOLS_ENABLED_SERVE_HINT =
  'TUI/CLI only. huginn serve always registers builtin tools; this switch does not turn them off. Use the deny list to block a tool in the web UI.'

export const DENY_WINS_COPY =
  'Deny wins — a tool listed in both allow and deny stays blocked.'

// Colon is optional: hydrated Steve DMs store the bare token with no reason.
const SYSTEM_FAIL_RE = /^(TOOL_FAIL|DELEGATE_FAIL)(?:\s*:\s*([\s\S]*))?$/i
const FAILED_RESULT_RE = /^(error:|TOOL_FAIL\b|DELEGATE_FAIL\b)|is not available|permission denied/i

export type SystemFailKind = 'TOOL_FAIL' | 'DELEGATE_FAIL'

export interface SystemFailSpeech {
  kind: SystemFailKind
  message: string
}

/** Detect assistant text that is a system failure, not teammate speech. */
export function parseSystemFailSpeech(content: string | undefined | null): SystemFailSpeech | null {
  if (!content) return null
  const m = content.trim().match(SYSTEM_FAIL_RE)
  if (!m) return null
  const kind = m[1]!.toUpperCase() === 'DELEGATE_FAIL' ? 'DELEGATE_FAIL' : 'TOOL_FAIL'
  return { kind, message: (m[2] ?? '').trim() }
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
  return toolCallsFailed(calls) || !!parseSystemFailSpeech(content)
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
