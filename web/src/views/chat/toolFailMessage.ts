// Raw TOOL_FAIL: lines are system errors, not teammate voice.
export function parseToolFailContent(content: string | undefined | null): string | null {
  if (!content) return null
  const trimmed = content.trim()
  const match = trimmed.match(/^TOOL_FAIL:\s*([\s\S]*)$/)
  if (!match) return null
  return match[1]?.trim() || trimmed
}
