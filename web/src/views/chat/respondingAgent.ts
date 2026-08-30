/**
 * Display-name helpers that mirror channel @mention routing in
 * internal/server/ws.go (extractLeadMention + resolveAgentForMessage 1b).
 * Routing itself is unchanged — this only names the agent the UI shows
 * while a turn is in flight.
 */

export type DisplayAgentLike = {
  name: string
  icon?: string
  model?: string
  description?: string
  vault_name?: string
  color?: string
}

export type DisplaySpaceLike = {
  kind?: string
  leadAgent: string
  memberAgents: string[]
} | null

function isAgentNameStart(c: string): boolean {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

function isAgentNameChar(c: string): boolean {
  return isAgentNameStart(c) || (c >= '0' && c <= '9') || c === '-' || c === '_'
}

/** Leading @Name at the start of content. Empty string when none. */
/** Name on a hallway/drawer bubble. Prefer the message author; never a bare initial. */
export function hallwayAuthorName(msgAgent?: string, fallback?: string): string {
  const a = (msgAgent || '').trim()
  if (a && a.length > 1) return a
  const f = (fallback || '').trim()
  if (f) return f
  return a
}

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

function namesEqual(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase()
}

function isChannelMember(space: NonNullable<DisplaySpaceLike>, name: string): boolean {
  if (namesEqual(space.leadAgent, name)) return true
  return space.memberAgents.some(m => namesEqual(m, name))
}

function findAgent(agents: DisplayAgentLike[], name: string): DisplayAgentLike | undefined {
  return agents.find(a => namesEqual(a.name, name))
}

/**
 * Agent shown in the streaming banner / composer identity chip.
 *
 * In a channel, an in-flight leading @mention of a space member names that
 * agent (same rule as PR 131 routing). Otherwise the space lead. Standalone
 * sessions fall back to the selected primary agent.
 */
export function resolveDisplayAgent(opts: {
  space: DisplaySpaceLike
  agents: DisplayAgentLike[]
  selectedAgent: DisplayAgentLike | null
  streaming: boolean
  inFlightUserContent: string
}): DisplayAgentLike | null {
  const { space, agents, selectedAgent, streaming, inFlightUserContent } = opts
  if (!space) return selectedAgent

  const lead = findAgent(agents, space.leadAgent) ?? null

  if (streaming && space.kind === 'channel') {
    const mentioned = extractLeadMention(inFlightUserContent)
    if (mentioned && isChannelMember(space, mentioned)) {
      const addressed = findAgent(agents, mentioned)
      if (addressed) return addressed
    }
  }

  return lead ?? selectedAgent
}
