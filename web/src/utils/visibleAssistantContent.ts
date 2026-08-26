/**
 * Strip a leading tool-call JSON object from assistant text so local-model
 * harness leakage (`{"name":"bash",...}PONG`) never renders in the bubble.
 *
 * Does not execute anything. Fenced samples and JSON that appears after
 * leading prose stay visible.
 */
export function visibleAssistantContent(content: string): string {
  if (!content) return content
  const trimmed = content.trimStart()
  if (!trimmed.startsWith('{')) return content
  let rest = trimmed
  let stripped = false
  while (rest.startsWith('{')) {
    const read = readJSONObject(rest)
    if (!read || !isToolCallObject(read.value)) break
    stripped = true
    rest = read.after.replace(/^\s+/, '')
  }
  return stripped ? rest : content
}

function readJSONObject(s: string): { value: unknown; after: string } | null {
  if (!s.startsWith('{')) return null
  let depth = 0
  let inStr = false
  let escape = false
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (inStr) {
      if (escape) {
        escape = false
        continue
      }
      if (c === '\\') {
        escape = true
        continue
      }
      if (c === '"') inStr = false
      continue
    }
    if (c === '"') {
      inStr = true
      continue
    }
    if (c === '{') depth++
    else if (c === '}') {
      depth--
      if (depth === 0) {
        const raw = s.slice(0, i + 1)
        try {
          return { value: JSON.parse(raw), after: s.slice(i + 1) }
        } catch {
          return null
        }
      }
    }
  }
  return null
}

function isToolCallObject(v: unknown): boolean {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return false
  const o = v as Record<string, unknown>
  const name = typeof o.name === 'string' ? o.name : typeof o.function_name === 'string' ? o.function_name : ''
  if (!name.trim()) return false
  // Missing arguments still counts as a tool invocation (wait_for_threads).
  if (!('arguments' in o)) return true
  const args = o.arguments
  if (args == null) return true
  if (typeof args === 'object' && !Array.isArray(args)) return true
  if (typeof args === 'string') {
    const inner = args.trim()
    if (!inner) return true
    try {
      const parsed = JSON.parse(inner)
      return !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    } catch {
      return false
    }
  }
  return false
}
