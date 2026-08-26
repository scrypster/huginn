/**
 * Composer @ picker + leftover-mention helpers.
 *
 * When the space has a roster, only roster names may be the addressee or
 * extra-spawn (server resolveMentionAddressee / additionalMentionNames).
 * The picker lists that same roster. Leftover typed @Name of a non-member
 * — leading or mid-text — is dropped with a visible hint so a 1:1 or
 * channel turn cannot silently go to someone not in the room.
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

/** Leading @Name at the start of content. Empty string when none. */
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

type MentionHit = { name: string; start: number; end: number }

/** Every @Name token in content, in order. Skips email-style alice@Bob. */
export function extractMentionHits(content: string): MentionHit[] {
  const hits: MentionHit[] = []
  for (let i = 0; i < content.length; i++) {
    if (content[i] !== '@') continue
    if (i > 0 && isAgentNameChar(content[i - 1]!)) continue
    if (i + 1 >= content.length || !isAgentNameStart(content[i + 1]!)) continue
    let end = i + 2
    while (end < content.length && isAgentNameChar(content[end]!)) end++
    if (end - (i + 1) > 64) continue
    hits.push({ name: content.slice(i + 1, end), start: i, end })
  }
  return hits
}

/**
 * If a leftover typed @Name is not on the roster, strip it so send cannot
 * silently address someone not in the room. Leading leftovers keep the
 * original trim (drop `@Name` plus following spaces). Mid-text leftovers
 * drop the token and collapse a leftover space. Standalone sessions
 * (memberNames undefined) are unchanged. Returns the first dropped name.
 */
export function dropUnknownLeadMention(
  content: string,
  memberNames?: string[] | undefined,
): { content: string; dropped?: string } {
  if (!memberNames) return { content }

  let result = content
  let dropped: string | undefined

  const leading = extractLeadMention(result)
  if (leading && !isSpaceMember(memberNames, leading)) {
    const trimmed = result.trimStart()
    result = trimmed.slice(1 + leading.length).replace(/^[ \t]+/, '')
    dropped = leading
  }

  const unknown = extractMentionHits(result).filter(h => !isSpaceMember(memberNames, h.name))
  if (unknown.length === 0) return { content: result, dropped }

  if (!dropped) dropped = unknown[0]!.name
  for (let i = unknown.length - 1; i >= 0; i--) {
    const hit = unknown[i]!
    const before = result.slice(0, hit.start)
    let after = result.slice(hit.end)
    if (before.endsWith(' ') && after.startsWith(' ')) {
      after = after.slice(1)
    }
    result = before + after
  }
  return { content: result, dropped }
}
