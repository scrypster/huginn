/**
 * Cmd+K global message search helpers.
 *
 * Channel/DM text lives in space timelines (and the space-messages API), not
 * the in-memory session cache. Hits that carry a space_id must open
 * /#/space/:id — leftover /#/chat/:sessionId chrome is session-only.
 */

export interface GlobalSearchHit {
  sessionId: string
  sessionLabel: string
  msgId: string
  agent: string
  snippet: string
  spaceId?: string
}

export interface SearchableSession {
  id: string
  space_id?: string
  title?: string
  created_at?: string
}

export interface SearchableSessionMessage {
  id: string
  content?: string
  role: string
  agent?: string
}

export interface SearchableSpaceMessage {
  id: string
  content?: string
  role: string
  agent?: string
  session_id?: string
}

export interface SpaceMessageGroup {
  spaceId: string
  spaceLabel: string
  messages: SearchableSpaceMessage[]
}

const DEFAULT_LIMIT = 30

/** Route a search hit to space chrome when a space_id exists. */
export function searchResultPath(hit: Pick<GlobalSearchHit, 'spaceId' | 'sessionId'>): string {
  return hit.spaceId ? `/space/${hit.spaceId}` : `/chat/${hit.sessionId}`
}

export function highlightSnippet(content: string, query: string): string | null {
  const q = query.trim()
  if (!q || !content) return null
  const lower = content.toLowerCase()
  const needle = q.toLowerCase()
  const idx = lower.indexOf(needle)
  if (idx === -1) return null
  const start = Math.max(0, idx - 40)
  const end = Math.min(content.length, idx + q.length + 80)
  const raw = (start > 0 ? '…' : '') + content.slice(start, end) + (end < content.length ? '…' : '')
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return raw.replace(new RegExp(`(${escaped})`, 'gi'), '<strong class="text-huginn-blue">$1</strong>')
}

function isSearchableRole(role: string): boolean {
  return role === 'user' || role === 'assistant'
}

export function buildGlobalSearchResults(opts: {
  query: string
  sessions: SearchableSession[]
  getMessages: (sessionId: string) => SearchableSessionMessage[]
  formatSessionLabel: (session: SearchableSession) => string
  spaceMessageGroups?: SpaceMessageGroup[]
  resolveSpaceId?: (sessionId: string) => string | null
  limit?: number
}): GlobalSearchHit[] {
  const q = opts.query.trim().toLowerCase()
  if (!q || q.length < 2) return []

  const limit = opts.limit ?? DEFAULT_LIMIT
  const results: GlobalSearchHit[] = []
  const seen = new Set<string>()

  const pushHit = (hit: GlobalSearchHit) => {
    const key = `${hit.sessionId}:${hit.msgId}`
    if (seen.has(key)) return
    seen.add(key)
    results.push(hit)
  }

  for (const group of opts.spaceMessageGroups ?? []) {
    for (const msg of group.messages) {
      if (!msg.content || !isSearchableRole(msg.role)) continue
      const snippet = highlightSnippet(msg.content, q)
      if (!snippet) continue
      pushHit({
        sessionId: msg.session_id ?? '',
        sessionLabel: group.spaceLabel,
        msgId: msg.id,
        agent: msg.agent ?? '',
        snippet,
        spaceId: group.spaceId,
      })
      if (results.length >= limit) return results
    }
  }

  for (const session of opts.sessions) {
    const spaceId = session.space_id || opts.resolveSpaceId?.(session.id) || undefined
    const msgs = opts.getMessages(session.id)
    for (const msg of msgs) {
      if (!msg.content || !isSearchableRole(msg.role)) continue
      const snippet = highlightSnippet(msg.content, q)
      if (!snippet) continue
      pushHit({
        sessionId: session.id,
        sessionLabel: opts.formatSessionLabel(session),
        msgId: msg.id,
        agent: msg.agent ?? '',
        snippet,
        spaceId,
      })
      if (results.length >= limit) return results
    }
  }

  return results
}

export function formatSpaceSearchLabel(space: { name: string; kind?: string }): string {
  if (space.kind === 'channel') return `#${space.name}`
  return space.name
}

export function mergeSpaceMessageGroups(
  ...groupLists: SpaceMessageGroup[][]
): SpaceMessageGroup[] {
  const bySpace = new Map<string, SpaceMessageGroup>()
  for (const groups of groupLists) {
    for (const group of groups) {
      const existing = bySpace.get(group.spaceId)
      if (!existing) {
        bySpace.set(group.spaceId, {
          spaceId: group.spaceId,
          spaceLabel: group.spaceLabel,
          messages: [...group.messages],
        })
        continue
      }
      const seen = new Set(existing.messages.map(m => m.id))
      existing.messages.push(...group.messages.filter(m => !seen.has(m.id)))
    }
  }
  return [...bySpace.values()]
}
