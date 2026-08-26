/**
 * Small honesty helpers for Settings vs serve, failed tool chips,
 * system-fail speech, plaintext sidebar previews, and A2A harness rows.
 *
 * Detection stays here. Visible fail copy is failVisibleCopy / failChipLabel /
 * plaintextPreview — never feed those strings back into the parsers.
 */

import { visibleAssistantContent } from './visibleAssistantContent'

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
 * Plaintext sidebar snippet. Ordinary snake_case keeps its underscores.
 * Detected fails render as human copy — never TOOL_FAIL / DELEGATE_FAIL.
 * Leftover leading tool JSON is stripped so harness names never preview.
 */
export function plaintextPreview(content: string, max = 48): string {
  const stripped = visibleAssistantContent(content)
  const raw = stripped.replace(/[`#*>[\]]/g, '').replace(/\s+/g, ' ').trim()
  if (isFailPreviewSource(content) || isFailPreviewSource(raw)) return FAIL_COPY.preview
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

// ── Residual speech ─────────────────────────────────────────────────────
// After a small local model's tool calls have run, its speech channel often
// still carries the playbook: "<wait for Reggie to finish>", "Once Reggie
// has finished:", a re-typed tool object, and the tool result as JSON glued
// to the answer. Same idea as backend.StripResidualSpeech in Go: remove it
// from what the user reads, execute nothing, leave fenced code alone.

const WAIT_TAG_RE = /<\s*wait(?:ing)?(?:[\s_:-][^<>]*)?\s*>/gi
const WAIT_TOKEN_LINE_RE = /^\s*(?:\[|\()?\s*wait(?:ing)?[-_ ]for[-_ ][\w@.-]+(?:[-_ ]to[-_ ]finish)?\s*(?:\]|\))?\s*[:.]?\s*$/i
const GLUE_LINE_RE = /^\s*(?:once|after|when|as soon as)\s+[^.:,]{1,80}?\s+(?:has|have|is|are)?\s*(?:finished|done|complete|completed|replied|responded|answered|returned)\s*[:,]?\s*$/i
const GLUE_CONTINUATION_RE = /^\s*(?:then|next|finally|afterwards|after that)\b[^.]{0,80}:\s*$/i
const HARNESS_TOOL_NAME_LINE = new Set(['wait_for_threads', 'delegate_to_agent', 'recall_thread_result', 'list_team_status', 'bash'])

export interface ResidualSpeechOptions {
  /** Tools already ran this turn: also drop unfenced tool-call JSON (any name) and echoed result objects. */
  afterTools?: boolean
}

/**
 * Remove wait tags, playbook glue lines, harness tool-name lines and fail
 * tokens from assistant speech. With `afterTools`, unfenced tool-invocation
 * JSON (granted or invented — never executed) and flat result-shaped JSON
 * next to prose are dropped too. Fenced blocks are preserved verbatim.
 */
export function stripResidualSpeech(content: string | undefined | null, opts: ResidualSpeechOptions = {}): string {
  if (!content) return ''
  const parts = content.split('```')
  let changed = false
  for (let i = 0; i < parts.length; i += 2) {
    const out = stripResidualUnfenced(parts[i]!, !!opts.afterTools)
    if (out !== parts[i]) {
      parts[i] = out
      changed = true
    }
  }
  return changed ? parts.join('```').trim() : content
}

function stripResidualUnfenced(s: string, afterTools: boolean): string {
  if (!s) return s
  if (afterTools) s = removeResidualJSONObjects(s)
  const kept: string[] = []
  let inGlueChain = false
  for (let line of s.split('\n')) {
    const hadText = line.trim() !== ''
    line = line.replace(WAIT_TAG_RE, '')
    const trim = line.trim()
    if (trim === '' && hadText) { inGlueChain = true; continue }
    if (trim === '') { kept.push(line); continue }
    if (WAIT_TOKEN_LINE_RE.test(trim) || GLUE_LINE_RE.test(trim)) { inGlueChain = true; continue }
    if (inGlueChain && GLUE_CONTINUATION_RE.test(trim)) continue
    if (HARNESS_TOOL_NAME_LINE.has(trim) || SYSTEM_FAIL_RE.test(trim)) { inGlueChain = false; continue }
    inGlueChain = false
    kept.push(line)
  }
  return kept.join('\n').replace(/\n{3,}/g, '\n\n')
}

function removeResidualJSONObjects(s: string): string {
  let out = ''
  let prose = ''
  const results: Array<{ start: number; end: number }> = []
  let i = 0
  while (i < s.length) {
    if (s[i] !== '{') { out += s[i]; prose += s[i]; i++; continue }
    const read = readJSONObjectAt(s, i)
    if (!read) { out += s[i]; prose += s[i]; i++; continue }
    const raw = s.slice(i, read.end)
    if (isToolInvocationObject(read.value)) {
      i = skipOneSeparator(s, read.end)
      continue
    }
    if (isResultShapedObject(read.value)) {
      results.push({ start: out.length, end: out.length + raw.length })
      out += raw
      i = read.end
      continue
    }
    out += raw
    prose += raw
    i = read.end
  }
  if (!results.length || prose.trim() === '') return out
  for (let k = results.length - 1; k >= 0; k--) {
    const sp = results[k]!
    out = out.slice(0, sp.start) + out.slice(skipOneSeparator(out, sp.end))
  }
  return out
}

function skipOneSeparator(s: string, i: number): number {
  if (i < s.length && (s[i] === '\n' || s[i] === ' ' || s[i] === '\t' || s[i] === '\r')) {
    if (s[i] === '\r' && s[i + 1] === '\n') return i + 2
    return i + 1
  }
  return i
}

function readJSONObjectAt(s: string, start: number): { value: unknown; end: number } | null {
  let depth = 0, inStr = false, escape = false
  for (let i = start; i < s.length; i++) {
    const c = s[i]
    if (inStr) {
      if (escape) { escape = false; continue }
      if (c === '\\') { escape = true; continue }
      if (c === '"') inStr = false
      continue
    }
    if (c === '"') { inStr = true; continue }
    if (c === '{') depth++
    else if (c === '}') {
      depth--
      if (depth === 0) {
        try { return { value: JSON.parse(s.slice(start, i + 1)), end: i + 1 } } catch { return null }
      }
    }
  }
  return null
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

/** name / function_name with optional object-or-JSON-string arguments. Mirrors backend.toolCallFromRaw. */
export function isToolInvocationObject(v: unknown): boolean {
  if (!isPlainObject(v)) return false
  const name = typeof v.name === 'string' ? v.name : typeof v.function_name === 'string' ? v.function_name : ''
  if (!name.trim()) return false
  if (!('arguments' in v) || v.arguments == null) return true
  const args = v.arguments
  if (isPlainObject(args)) return true
  if (typeof args === 'string') {
    if (!args.trim()) return true
    try { return isPlainObject(JSON.parse(args)) } catch { return false }
  }
  return false
}

/** Flat object of scalars with no tool name — the shape of an echoed tool result. */
export function isResultShapedObject(v: unknown): boolean {
  if (!isPlainObject(v)) return false
  const keys = Object.keys(v)
  if (!keys.length || 'name' in v || 'function_name' in v) return false
  return keys.every(k => { const x = v[k]; return x === null || typeof x !== 'object' })
}

// ── Display copy (detection stays above; these never feed parsers) ────

export const FAIL_COPY = {
  tool: "I couldn't run that.",
  delegate: "That handoff didn't finish.",
  shell: "I wasn't allowed to use the shell.",
  chip: "Couldn't run",
  preview: "Couldn't finish",
} as const

const PREFIXED_FAIL_RE = /^(?:You: |[^\n:]+: )?(TOOL_FAIL|DELEGATE_FAIL)(?:\s*:[\s\S]*)?$/i

function isFailKind(kind: SystemFailKind | undefined): kind is 'TOOL_FAIL' | 'DELEGATE_FAIL' {
  return kind === 'TOOL_FAIL' || kind === 'DELEGATE_FAIL'
}

function isFailPreviewSource(text: string): boolean {
  const parsed = parseSystemFailSpeech(text)
  if (parsed && isFailKind(parsed.kind)) return true
  if (parsed?.kind === 'announcement' && isBareFailSpeech(parsed.summary)) return true
  return PREFIXED_FAIL_RE.test(text.trim())
}

function isShellDenied(message: string, toolName?: string): boolean {
  const hay = `${toolName ?? ''} ${message}`.toLowerCase()
  return /permission denied|not allowed|denied/.test(hay) && /\bbash\b|\bshell\b/.test(hay)
}

function firstFailedTool<T extends { name: string; result?: string }>(
  calls?: T[] | null,
): T | undefined {
  return (calls ?? []).find(tc => isFailedToolResult(tc.result))
    ?? visibleToolCalls(calls)[0]
}

/** Visible teammate line for a detected fail. Empty when content is not fail speech. */
export function failVisibleCopy(
  content?: string | null,
  opts?: { toolName?: string },
): string {
  const parsed = parseSystemFailSpeech(content)
  if (!parsed || !isFailKind(parsed.kind)) return ''
  if (parsed.kind === 'DELEGATE_FAIL') return FAIL_COPY.delegate
  if (isShellDenied(parsed.message, opts?.toolName)) return FAIL_COPY.shell
  return FAIL_COPY.tool
}

/** Raw token, tool name, and reason — hover / aria / details only. */
export function failDiagnostic(
  content?: string | null,
  opts?: { toolName?: string; result?: string },
): string {
  const parsed = parseSystemFailSpeech(content)
  const parts: string[] = []
  if (parsed && isFailKind(parsed.kind)) parts.push(parsed.kind)
  if (opts?.toolName) parts.push(opts.toolName)
  const reason = (parsed && isFailKind(parsed.kind) ? parsed.message : '') || opts?.result || ''
  if (reason) parts.push(reason)
  return parts.join(' · ')
}

export function failChipLabel(): string {
  return FAIL_COPY.chip
}

export interface FailDisplay {
  copy: string
  diagnostic: string
  chip: string
  toolName?: string
}

/** Bundle visible copy + diagnostic for a message that failed. */
export function failDisplayFor(
  content?: string | null,
  calls?: Array<{ name: string; result?: string }> | null,
): FailDisplay | null {
  const parsed = parseSystemFailSpeech(content)
  const isFailSpeech = !!parsed && isFailKind(parsed.kind)
  const failed = firstFailedTool(calls)
  if (!isFailSpeech && !failed) return null
  const toolName = failed?.name
  return {
    copy: failVisibleCopy(content, { toolName }) || FAIL_COPY.tool,
    diagnostic: failDiagnostic(content, { toolName, result: failed?.result }),
    chip: FAIL_COPY.chip,
    toolName,
  }
}
