const SYSTEM_FAIL_PREFIX = /^(TOOL_FAIL|DELEGATE_FAIL):\s*([\s\S]*)$/

// Raw TOOL_FAIL / DELEGATE_FAIL lines are system errors, not teammate voice.
export function parseToolFailContent(content: string | undefined | null): string | null {
  if (!content) return null
  const trimmed = content.trim()
  const match = trimmed.match(SYSTEM_FAIL_PREFIX)
  if (!match) return null
  return match[2]?.trim() || trimmed
}
