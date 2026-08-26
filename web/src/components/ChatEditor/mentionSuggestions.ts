/**
 * Composer @ picker + leftover-mention helpers.
 *
 * Server routing (extractLeadMention + space-member check in
 * internal/server/ws.go) only addresses space members. The picker must
 * list the same roster; leftover typed @Name of a non-member is dropped
 * so it cannot silently retarget the lead.
 */

export type MentionAgent = Record<string, unknown>

export type MentionSpace = {
  kind?: string
  leadAgent?: string
  memberAgents?: string[]
} | null | undefined

function namesEqual(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase()
}

/** Active-space roster for the @ picker. Undefined = standalone (no filter). */
export function spaceRosterNames(space: MentionSpace): string[] | undefined {
  if (!space) return undefined
  if (space.kind === 'dm') {
    return space.leadAgent ? [space.leadAgent] : []
  }
  const names: string[] = []
  const seen = new Set<string>()
  for (const n of [space.leadAgent, ...(space.memberAgents ?? [])]) {
    if (!n) continue
    const key = n.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    names.push(n)
  }
  return names
}

export function isSpaceMember(memberNames: string[] | undefined, name: string): boolean {
  if (!memberNames) return true
  return memberNames.some(m => namesEqual(m, name))
}

export function filterMentionSuggestions(
  agents: MentionAgent[],
  query: string,
  memberNames?: string[] | undefined,
): MentionAgent[] {
  const q = query.toLowerCase()
  const rostered = memberNames
    ? agents.filter(a => isSpaceMember(memberNames, String(a.name ?? '')))
    : agents
  return rostered
    .filter(a => String(a.name ?? '').toLowerCase().startsWith(q))
    .slice(0, 6)
}

function isAgentNameStart(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

function isAgentNameChar(c: string): boolean {
  return isAgentNameStart(c) || (c >= '0' && c <= '9') || c === '-' || c === '_'
}

/** Leading @Name at the start of content. Empty string when none. Mirrors ws.go. */
export function extractLeadMention(content: string): string {
  const trimmed = content.trim()
  if (!trimmed.startsWith('@')) return ''
  const rest = trimmed.slice(1)
  if (!rest || !isAgentNameStart(rest[0]!)) return ''
  let end = 1
  while (end < rest.length && isAgentNameChar(rest[end]!)) end++
  if (end > 64) return ''
  return rest.slice(0, end)
}

/**
 * If a leftover typed @Name is not on the roster, strip it so send cannot
 * silently hit the lead. Standalone sessions (memberNames undefined) are
 * unchanged. Returns the dropped name when a mention was removed.
 */
export function dropUnknownLeadMention(
  content: string,
  memberNames?: string[] | undefined,
): { content: string; dropped?: string } {
  if (!memberNames) return { content }
  const mentioned = extractLeadMention(content)
  if (!mentioned || isSpaceMember(memberNames, mentioned)) return { content }

  const trimmed = content.trimStart()
  const after = trimmed.slice(1 + mentioned.length)
  const rest = after.replace(/^[ \t]+/, '')
  return { content: rest, dropped: mentioned }
}
