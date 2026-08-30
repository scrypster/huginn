import { isA2ATool, isBareFailSpeech, parseSystemFailSpeech } from '../utils/honesty'
import { visibleAssistantContent } from '../utils/visibleAssistantContent'

export type ReplySpeechKind = 'speech' | 'fail' | 'hidden'

export function classifyReplySpeech(content: string | undefined | null): { kind: ReplySpeechKind; text: string } {
  const raw = content ?? ''
  const visible = visibleAssistantContent(raw, { afterTools: true })
  if (isBareFailSpeech(raw) || isBareFailSpeech(visible)) return { kind: 'fail', text: raw }
  if (parseSystemFailSpeech(raw) || parseSystemFailSpeech(visible)) return { kind: 'hidden', text: '' }
  const trimmed = visible.trim()
  if (!trimmed || isA2ATool(trimmed) || isA2ATool(raw.trim())) return { kind: 'hidden', text: '' }
  return { kind: 'speech', text: visible }
}

export function lastReplyPreview(content: string | undefined | null): string {
  const c = classifyReplySpeech(content)
  if (c.kind !== 'speech') return ''
  const t = c.text.trim()
  if (!t || /^delegated to @/i.test(t)) return ''
  return t
}
