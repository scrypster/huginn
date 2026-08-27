/**
 * Small honesty helpers for Settings vs serve, failed tool chips,
 * system-fail speech, plaintext sidebar previews, and A2A harness rows.
 *
 * Detection stays here. Visible fail copy is failVisibleCopy / failChipLabel /
 * plaintextPreview — never feed those strings back into the parsers.
 */

import { stripLeadingToolCalls, visibleAssistantContent } from './visibleAssistantContent'

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
  // Check the fail token before residual stripping removes it from speech.
  const unfenced = stripLeadingToolCalls(content).trim()
  const source = isFailPreviewSource(content) ? content : isFailPreviewSource(unfenced) ? unfenced : isFailPreviewSource(raw) ? raw : ''
  if (source) return failPreviewCopy(source)
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
// "After Reggie responds with PONG:", "When Reggie replies:", "After Reggie responds," — mirrors backend.waitGlueLineRE.
const WAIT_GLUE_LINE_RE = /^\s*(?:once|after|when|as soon as)\s+[^.:]{0,60}?\b(?:responds?|replies|replied|replying|finish(?:es|ed)?|complet(?:es|ed)?|returns?|returned|answers?|answered|reports?|reported|comes? back|gets? back|is (?:done|back|finished|ready))\b[^.:]{0,60}?\s*[:,]\s*$/i
// A JSON string glued onto sentence-final punctuation: 56."PONG"
const GLUED_STRING_RE = /([.!?])"[^"\n]{1,60}"\s*$/
// A separator line glued onto sentence-final punctuation: 56.---
const GLUED_SEPARATOR_RE = /([.!?])\s*-{3,}\s*$/
const ECHO_FRAGMENT_RE = /[0-9]+|[A-Z]+/g
// An echo line (`56PONG`, `56`, `"PONG"`): no spaces, no lowercase, fragments and punctuation only.
const ECHO_LINE_RE = /^["'`(\[]*(?:[0-9]+|[A-Z]+)(?:["'`.,;:!?)\]]*(?:[0-9]+|[A-Z]+))*["'`.,;:!?)\]]*$/
const HARNESS_TOOL_NAME_LINE = new Set(['wait_for_threads', 'delegate_to_agent', 'recall_thread_result', 'list_team_status', 'bash'])
// Bracket stage directions: lines that are ONLY [text]
const BRACKET_STAGE_DIRECTION_RE = /^\s*\[[^\]]*\]\s*$/
// Playbook format instruction lines
const PLAYBOOK_FORMAT_RE = /^\s*use\s+(?:the\s+)?(?:following\s+)?format\s*:\s*$/i
// Template placeholder lines: "Reggie says: <reggie-reply>" style
const TEMPLATE_PLACEHOLDER_RE = /^[A-Za-z][^:]*:\s*<[^>]+>\s*$/
// Standalone separator lines
const SEPARATOR_LINE_RE = /^\s*---+\s*$/
// Playbook introductions: "After ... response, use the following format:"
const PLAYBOOK_INTRO_RE = /^\s*(?:after|once|when)\s+.*\b(?:response|result|reply)\b.*,\s*use\s+(?:the\s+)?(?:following\s+)?format\s*:\s*$/i
// Whole-line helpdesk filler
const FILLER_LINE_RE = /^(?:(?:how can I|is there anything|how else|not currently delegating|nothing is currently delegated)\b.*|(?:if you have any (?:other )?questions(?: or need further assistance)?,\s*)?feel free to ask)[?.!]?\s*$/i
const LOADING_MODEL_LINE_RE = /^\s*loading model\b/i
const THIRD_PERSON_NOTED_RE = /(?:^|[\s,])@?[A-Za-z][\w.-]*\s+has noted\b/i
const LOCAL_TIME_NOW_RE = /\s*local time now:\s*/gi
// Same-line helpdesk closer after a real answer. Covers "need further assistance".
const HELPDESK_CLOSER_SENTENCE_RE = /^(?:how can i (?:assist|help)(?: you(?: further)?)?[?.!]*|is there anything(?: else)?(?: i can (?:help|assist)(?: you)?(?: with)?| you need(?: (?:help|assistance)(?: with)?)?)?[?.!]*|if you have any (?:other )?questions(?: or need further assistance)?,\s*feel free to ask[?.!]*|feel free to ask(?: if you have any (?:other )?questions(?: or need further assistance)?)?[?.!]*)$/i
const WAIT_PLAYBOOK_NAME_RE = /(?:please\s+)?(?:use|call)\s+`?wait_for_threads/i
const SESSION_HISTORY_LEAK_RE = /session history could not be loaded/i
const SPAWNED_PLAYBOOK_SENTENCE_RE = /(?:has been spawned|was spawned|spawned immediately|delegate(?:d)? task\b.{0,80}\bspawned\b)/i
const LEFTOVER_HELPDESK_SENTENCE_RE = /^(?:i apologize for any confusion|let'?s try a different approach|(?:and\s+)?i can try again|if you have access to the api key, please provide it(?:\s*,?\s*and i can try again)?|the system encountered an api key resolution issue|i apologize, but there was an error when attempting to\b.*|i'?ll have\s+[A-Za-z][\w.-]*\s+gather the required information)[?.!]*$/i
const TASK_DELEGATED_SENTENCE_RE = /^task delegated to\s+[A-Za-z][\w.-]*[?.!]*$/i
const TASK_DELEGATED_NAME_RE = /task delegated to\s+([A-Za-z][\w-]*)/i
const HAVE_AGENT_NAME_RE = /i'?ll have\s+([A-Za-z][\w-]*)\b/i
const MISSING_AGENT_HELPDESK_RE = /^(?:it seems that\s+)?["“”']?([A-Za-z][\w.-]*)["“”']?\s+isn'?t one of the available agents[?.!]*$/i
const HONEST_MISSING_AGENT_RE = /^([A-Za-z][\w.-]*) isn't (?:in this company|available here|in [A-Za-z][\w.-]*)[?.!]*$/i
const TEAMMATE_TIMES_RE = /\b\d+\s+times\s+\d+\b/i
const TEAMMATE_HOST_VALUE_RE = /\bhostname\b.*['"][^'"]{3,}['"]|['"][^'"]{3,}['"].*\bhostname\b/i

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
  for (let i = 0; i < parts.length; i++) {
    const isFenced = i % 2 === 1
    const out = stripResidualUnfenced(parts[i]!, !!opts.afterTools, isFenced)
    if (out !== parts[i]) {
      parts[i] = out
      changed = true
    }
    // Drop fenced blocks that became empty/whitespace-only when afterTools=true,
    // since they were only residual speech (comments, glue, tool JSON).
    if (opts.afterTools && i % 2 === 1 && !out.trim()) {
      changed = true
      parts[i] = ''
    }
  }
  if (!changed) return content
  // Rebuild, skipping empty fences.
  let result = ''
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 0) {
      result += parts[i]
    } else if (parts[i]) {
      result += '```' + parts[i] + '```'
    }
  }
  return result.trim()
}

function stripResidualUnfenced(s: string, afterTools: boolean, isFenced: boolean): string {
  if (!s) return s
  // Language-tagged fences stay unless they are leftover harness playbooks.
  if (isFenced && fenceHasLanguageTag(s)) {
    if (afterTools && isHarnessPlaybookFence(s)) return ''
    return s
  }
  if (afterTools) s = removeResidualJSONObjects(s)
  const kept: string[] = []
  let inGlueChain = false
  for (let line of s.split('\n')) {
    const hadText = line.trim() !== ''
    line = line.replace(WAIT_TAG_RE, '')
    const trim = line.trim()
    if (trim === '' && hadText) { inGlueChain = true; continue }
    if (trim === '') { kept.push(line); continue }
    if (WAIT_TOKEN_LINE_RE.test(trim) || GLUE_LINE_RE.test(trim) || WAIT_GLUE_LINE_RE.test(trim) || FILLER_LINE_RE.test(trim) || LEFTOVER_HELPDESK_SENTENCE_RE.test(trim) || TASK_DELEGATED_SENTENCE_RE.test(trim) || LOADING_MODEL_LINE_RE.test(trim) || THIRD_PERSON_NOTED_RE.test(trim)) { inGlueChain = true; continue }
    if (PLAYBOOK_FORMAT_RE.test(trim) || PLAYBOOK_INTRO_RE.test(trim) || TEMPLATE_PLACEHOLDER_RE.test(trim) || SEPARATOR_LINE_RE.test(trim)) { inGlueChain = true; continue }
    if (inGlueChain && GLUE_CONTINUATION_RE.test(trim)) continue
    if ((BRACKET_STAGE_DIRECTION_RE.test(trim) && afterTools)) { continue }
    if (HARNESS_TOOL_NAME_LINE.has(trim) || SYSTEM_FAIL_RE.test(trim)) { inGlueChain = false; continue }
    inGlueChain = false
    if (afterTools) {
      line = line.replace(GLUED_STRING_RE, '$1')
      line = line.replace(GLUED_SEPARATOR_RE, '$1')
      line = dropPlaybookInstructionSentences(line)
      line = dropHelpdeskCloserSentences(line)
      line = rewriteMissingAgentHelpdesk(line, s)
      if (!line.trim()) continue
    }
    kept.push(line)
  }
  let lines = afterTools ? dropTrailingEchoLines(kept, s) : kept
  if (afterTools) lines = stripSameLineEchoFragments(lines, s)
  if (afterTools) lines = deduplicateSentencesInLastLine(lines, s)
  let out = lines.join('\n').replace(/\n{3,}/g, '\n\n')
  if (LOCAL_TIME_NOW_RE.test(out)) {
    out = out.replace(LOCAL_TIME_NOW_RE, ' ').replace(/  +/g, ' ').trim()
  }
  return out
}

/**
 * Remove sentences from the last line that already appeared in the rest of the output.
 * Splits on sentence-final punctuation and removes duplicates. Also removes earlier lines
 * that are duplicates of the final content. Mirrors backend.deduplicateSentencesInLastLine.
 */
function deduplicateSentencesInLastLine(kept: string[], _original: string): string[] {
  if (!kept.length) return kept
  let lastIdx = kept.length - 1
  while (lastIdx >= 0 && !kept[lastIdx]!.trim()) lastIdx--
  if (lastIdx < 0 || lastIdx === 0) return kept

  const line = kept[lastIdx]!
  const trim = line.trim()

  // Split by sentence-final punctuation to identify individual sentences.
  const sentences: string[] = []
  let currentSentence = ''
  for (let i = 0; i < trim.length; i++) {
    currentSentence += trim[i]
    if (trim[i] === '.' || trim[i] === '!' || trim[i] === '?') {
      sentences.push(currentSentence.trim())
      currentSentence = ''
    }
  }
  if (currentSentence) {
    sentences.push(currentSentence.trim())
  }

  if (sentences.length <= 1) return kept

  // Build text of all sentences before the last line.
  const priorText = kept.slice(0, lastIdx).join('\n')
  const keptSentences: string[] = []
  for (const sent of sentences) {
    if (!sent || priorText.includes(sent)) continue
    keptSentences.push(sent)
  }

  if (keptSentences.length === sentences.length) {
    return kept
  }
  // Deduplicate the line by keeping non-duplicate sentences
  const indent = line.length - line.trimLeft().length
  if (keptSentences.length > 0) {
    kept[lastIdx] = line.slice(0, indent) + keptSentences.join(' ')
  } else {
    // All sentences are duplicates; keep just the first one
    kept[lastIdx] = line.slice(0, indent) + sentences[0]
  }
  return kept
}

/**
 * Strip echo fragments glued to the end of the last line when they appeared
 * earlier in the segment (e.g., "7 times 8 is 56.PONG, 56" -> "7 times 8 is 56."
 * when PONG and 56 appeared earlier). Mirrors backend.stripSameLineEchoFragments.
 */
function stripSameLineEchoFragments(kept: string[], original: string): string[] {
  if (!kept.length) return kept
  // Find the last non-blank line.
  let lastIdx = kept.length - 1
  while (lastIdx >= 0 && !kept[lastIdx]!.trim()) lastIdx--
  if (lastIdx < 0) return kept

  const line = kept[lastIdx]!
  let trim = line.trim()
  let matched = false

  // Look for sentence-final punctuation followed by fragments that appeared earlier.
  while (true) {
    const idx = findEndOfSentence(trim)
    if (idx <= 0 || idx >= trim.length) break

    const remainder = trim.slice(idx)
    const frags = remainder.match(ECHO_FRAGMENT_RE) ?? []
    if (!frags.length) break

    // Check if all fragments appeared before the sentence end.
    const sentenceEnd = trim.slice(0, idx)
    const foundAt = original.indexOf(sentenceEnd)
    let beforeAnswer = original
    if (foundAt >= 0) {
      beforeAnswer = original.slice(0, foundAt + sentenceEnd.length)
    }
    const allEarlier = frags.every(f => beforeAnswer.includes(f))
    if (!allEarlier) break

    // Strip the remainder.
    trim = sentenceEnd
    matched = true
  }

  if (!matched) return kept
  // Reconstruct with preserved leading whitespace.
  const indent = line.length - line.trimLeft().length
  kept[lastIdx] = line.slice(0, indent) + trim
  return kept
}

/** Find index after the last occurrence of sentence-final punctuation. */
function findEndOfSentence(s: string): number {
  let idx = -1
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '.' || s[i] === '!' || s[i] === '?') {
      idx = i + 1
    }
  }
  return idx
}


function hasTeammateAnswer(sent: string): boolean {
  if (sent.includes('PONG')) return true
  if (TEAMMATE_TIMES_RE.test(sent)) return true
  return TEAMMATE_HOST_VALUE_RE.test(sent)
}

function presentAgentFromSpeech(s: string): string {
  const d = s.match(TASK_DELEGATED_NAME_RE)
  const h = s.match(HAVE_AGENT_NAME_RE)
  const name = d?.[1] || h?.[1] || ''
  return name.replace(/[.,:;!?]+$/, '')
}

function teammateNotInCompanyLine(missing: string, present: string): string {
  let company = 'this company'
  if (missing.toLowerCase() === 'steve' && present.toLowerCase() === 'sam') company = 'Lab'
  if (present && present.toLowerCase() !== missing.toLowerCase()) {
    return `${missing} isn't in ${company}. ${present} is.`
  }
  if (missing.toLowerCase() === 'steve' && !present) return `${missing} isn't available here.`
  return `${missing} isn't in ${company}.`
}

function rewriteMissingAgentHelpdesk(line: string, original: string): string {
  const trim = line.trim()
  if (!trim) return line
  const present = presentAgentFromSpeech(original)
  const sentences: string[] = []
  let cur = ''
  for (let i = 0; i < trim.length; i++) {
    cur += trim[i]
    if (trim[i] === '.' || trim[i] === '!' || trim[i] === '?') {
      const sent = cur.trim()
      if (sent) sentences.push(sent)
      cur = ''
    }
  }
  if (cur.trim()) sentences.push(cur.trim())
  const kept: string[] = []
  let rewrote = false
  let missing = ''
  for (const sent of sentences) {
    const helpdesk = sent.match(MISSING_AGENT_HELPDESK_RE)
    if (helpdesk?.[1]) {
      if (hasTeammateAnswer(sent)) { kept.push(sent); continue }
      missing = helpdesk[1]
      rewrote = true
      continue
    }
    const honest = sent.match(HONEST_MISSING_AGENT_RE)
    if (honest?.[1]) {
      missing = honest[1]
      kept.push(sent)
      continue
    }
    kept.push(sent)
  }
  if (rewrote && missing) kept.unshift(teammateNotInCompanyLine(missing, present))
  if (!kept.length) return ''
  const indent = line.length - line.trimLeft().length
  return line.slice(0, indent) + kept.join(' ')
}

function isPlaybookInstructionSentence(sent: string): boolean {
  if (hasTeammateAnswer(sent)) return false
  return WAIT_PLAYBOOK_NAME_RE.test(sent) ||
    SESSION_HISTORY_LEAK_RE.test(sent) ||
    SPAWNED_PLAYBOOK_SENTENCE_RE.test(sent) ||
    TASK_DELEGATED_SENTENCE_RE.test(sent)
}

function dropPlaybookInstructionSentences(line: string): string {
  const trim = line.trim()
  if (!trim) return line
  const sentences: string[] = []
  let cur = ''
  for (let i = 0; i < trim.length; i++) {
    cur += trim[i]
    if (trim[i] === '.' || trim[i] === '!' || trim[i] === '?') {
      const sent = cur.trim()
      if (sent) sentences.push(sent)
      cur = ''
    }
  }
  if (cur.trim()) sentences.push(cur.trim())
  const kept = sentences.filter(s => !isPlaybookInstructionSentence(s))
  if (!kept.length) return ''
  const indent = line.length - line.trimLeft().length
  return line.slice(0, indent) + kept.join(' ')
}

function dropHelpdeskCloserSentences(line: string): string {
  const trim = line.trim()
  if (!trim) return line
  const sentences: string[] = []
  let cur = ''
  for (let i = 0; i < trim.length; i++) {
    cur += trim[i]
    if (trim[i] === '.' || trim[i] === '!' || trim[i] === '?') {
      const sent = cur.trim()
      if (sent) sentences.push(sent)
      cur = ''
    }
  }
  if (cur.trim()) sentences.push(cur.trim())
  const kept = sentences.filter(s => hasTeammateAnswer(s) || !(HELPDESK_CLOSER_SENTENCE_RE.test(s) || LEFTOVER_HELPDESK_SENTENCE_RE.test(s)))
  if (!kept.length) return ''
  const indent = line.length - line.trimLeft().length
  return line.slice(0, indent) + kept.join(' ')
}

const HARNESS_PLAYBOOK_TOOLS = new Set([
  'recall_thread_result',
  'wait_for_threads',
  'delegate_to_agent',
  'bash',
  'shell',
  'exec',
  'list_team_status',
])

function isHarnessPlaybookFence(s: string): boolean {
  let body = s
  if (fenceHasLanguageTag(s)) {
    const i = s.indexOf('\n')
    if (i >= 0) body = s.slice(i + 1)
  }
  const raw = body.replace(/\/\/[^\n]*/g, '').trim()
  try {
    const obj = JSON.parse(raw) as { name?: string; function_name?: string }
    const name = obj.name || obj.function_name || ''
    return HARNESS_PLAYBOOK_TOOLS.has(name)
  } catch {
    return false
  }
}

/** Check if fence content starts with a language tag. */
function fenceHasLanguageTag(s: string): boolean {
  const lines = s.split('\n')
  if (!lines.length) return false
  const tag = lines[0]!.trim()
  return tag !== '' && !/\s/.test(tag)
}

/**
 * Drop trailing echo lines (`56PONG`, `56`, `"PONG"`) whose every digit /
 * capital run already appeared earlier in the original segment. A fresh
 * number on its own line is an answer and stays; never blanks the message.
 * Mirrors backend.dropTrailingEchoLines.
 */
function dropTrailingEchoLines(kept: string[], original: string): string[] {
  let end = kept.length
  let removed = false
  while (end > 0) {
    const trim = kept[end - 1]!.trim()
    if (trim === '') { end--; continue }
    if (!removed) end = kept.length
    if (!ECHO_LINE_RE.test(trim)) break
    const idx = original.lastIndexOf(trim)
    if (idx < 0) break
    const before = original.slice(0, idx)
    const frags = trim.match(ECHO_FRAGMENT_RE) ?? []
    if (!frags.every(f => before.includes(f))) break
    while (end > 0 && kept[end - 1]!.trim() !== trim) end--
    end--
    removed = true
  }
  if (!removed) return kept
  return kept.slice(0, end).some(l => l.trim() !== '') ? kept.slice(0, end) : kept
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
        const raw = s.slice(start, i + 1)
        try { return { value: JSON.parse(raw), end: i + 1 } } catch { /* retry without // comments */ }
        // `"thread-12345"  // Replace with the actual thread ID` — stripping only, never executed.
        let stripped = stripJSONLineComments(raw)
        try { return { value: JSON.parse(stripped), end: i + 1 } } catch { /* retry with placeholders */ }
        // `<thread_id>` placeholders — replace with quoted strings for parsing.
        try { return { value: JSON.parse(replacePlaceholders(stripped)), end: i + 1 } } catch { return null }
      }
    }
  }
  return null
}

/** Replace <...> placeholders with quoted strings to make JSON with placeholders valid. */
function replacePlaceholders(s: string): string {
  return s.replace(/<[^>]+>/g, '"<placeholder>"')
}

/** Remove // comments that sit outside string literals. Mirrors backend.stripJSONLineComments. */
function stripJSONLineComments(s: string): string {
  let out = ''
  let inStr = false, escape = false
  for (let i = 0; i < s.length; i++) {
    const c = s[i]!
    if (inStr) {
      out += c
      if (escape) escape = false
      else if (c === '\\') escape = true
      else if (c === '"') inStr = false
      continue
    }
    if (c === '"') { inStr = true; out += c; continue }
    if (c === '/' && s[i + 1] === '/') {
      while (i < s.length && s[i] !== '\n') i++
      if (i < s.length) out += '\n'
      continue
    }
    out += c
  }
  return out
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
  tool: "I couldn't do that.",
  delegate: "They haven't come back yet.",
  shell: "I wasn't allowed to use the shell.",
  chip: "Couldn't do that",
  preview: "Couldn't do that",
  previewWaiting: 'Still waiting',
} as const

export function askedTeammateCopy(name: string): string {
  return `I asked ${name} and they haven't come back yet.`
}

export function stillWaitingCopy(name?: string): string {
  return name ? `Still waiting on ${displayTeammateName(name)}` : FAIL_COPY.previewWaiting
}

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

/** Best-effort teammate name from a DELEGATE_FAIL reason. Never a snake_case tool. */
export function teammateFromFailMessage(message: string | undefined | null): string | undefined {
  const text = (message ?? '').trim()
  if (!text) return undefined
  const m = text.match(/@([A-Za-z][\w.-]*)/)
    || text.match(/\bagent\s+([A-Za-z][\w.-]*)/i)
    || text.match(/\b([A-Z][a-z][\w.-]*)\b/)
  const name = m?.[1]
  if (!name || isA2ATool(name) || /_/u.test(name)) return undefined
  return name
}

function failPreviewCopy(content: string): string {
  const parsed = parseSystemFailSpeech(content.replace(/^(?:You: |[^\n:]+: )/, ''))
    ?? parseSystemFailSpeech(content)
  if (parsed?.kind === 'DELEGATE_FAIL' || (parsed?.kind === 'announcement' && isBareFailSpeech(parsed.summary))) {
    return stillWaitingCopy(teammateFromFailMessage(parsed.message || parsed.summary))
  }
  return FAIL_COPY.preview
}

function firstFailedTool<T extends { name: string; result?: string }>(
  calls?: T[] | null,
): T | undefined {
  return (calls ?? []).find(tc => isFailedToolResult(tc.result))
    ?? visibleToolCalls(calls)[0]
}

/** Visible teammate line for a detected fail. Empty when content is not fail speech. */
function displayTeammateName(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1)
}

export function failVisibleCopy(
  content?: string | null,
  opts?: { toolName?: string; teammate?: string },
): string {
  const parsed = parseSystemFailSpeech(content)
  if (!parsed || !isFailKind(parsed.kind)) return ''
  if (parsed.kind === 'DELEGATE_FAIL') {
    const name = opts?.teammate || teammateFromFailMessage(parsed.message)
    return name ? askedTeammateCopy(displayTeammateName(name)) : FAIL_COPY.delegate
  }
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
