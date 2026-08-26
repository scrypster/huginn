import { parseSystemFailSpeech } from '../../utils/honesty'

// Thin wrapper over 137 honesty helpers — never a second fail-chip parser.
export function parseToolFailContent(content: string | undefined | null): string | null {
  const parsed = parseSystemFailSpeech(content)
  if (!parsed || parsed.kind === 'announcement') return null
  return parsed.message || parsed.kind
}
