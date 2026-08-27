const mentionAllRE = /@[\p{L}\p{N}_.-]+/giu
const hireAskRE = /\b(?:hire|create(?:\s+an)?\s+agent|add(?:\s+a)?\s+teammate|create(?:\s+a)?\s+teammate|create_agent)\b/i
const meshAskRE = /\bmesh\b/i
const wallAskRE = /\b(?:company wall|isn'?t in (?:this )?company|isn'?t in lab)\b/i
const askSteveRE = /\bask\s+steve\b/i
const trivialTimeRE = /\b(?:what time(?: is it)?|current time|time is it|time it is|what day(?: is it)?|current date|what(?:'s| is) the date|date is it)\b/i
const trivialRosterRE = /^(?:who(?:'s| is) here|who(?:'s| is) on the team|who(?:'s| is) on the roster|roster)$/i
const trivialHeadcountRE = /^(?:how many people(?: are(?: in this channel| here)?)?|who(?:'s| is) in this channel)$/i

const ACKS = new Set([
  'thanks', 'thank you', 'thx', 'ty', 'ok', 'okay', 'k', 'got it',
  'cool', 'cheers', 'np', 'no problem', 'sounds good', 'roger', 'ack',
])

export function normalizeTrivialAsk(s: string): string {
  mentionAllRE.lastIndex = 0
  const stripped = s.replace(mentionAllRE, ' ').toLowerCase().trim()
  return stripped.replace(/\s+/g, ' ').replace(/^[ \t.!?…]+|[ \t.!?…]+$/g, '')
}

function isNonTrivialAsk(s: string): boolean {
  if (hireAskRE.test(s) || meshAskRE.test(s) || wallAskRE.test(s) || askSteveRE.test(s)) {
    return true
  }
  mentionAllRE.lastIndex = 0
  const mentions = s.match(mentionAllRE)
  return (mentions?.length ?? 0) >= 2
}

/**
 * Mirror of internal/agent.IsTrivialAsk: short asks that need no hire,
 * no delegate, and no tools. Hide the delegation-plan banner for these.
 */
export function isTrivialAsk(s: string): boolean {
  if (!s.trim()) return false
  if (isNonTrivialAsk(s)) return false
  const norm = normalizeTrivialAsk(s)
  if (!norm) return false
  if (trivialTimeRE.test(s) || trivialTimeRE.test(norm)) return true
  if (norm === 'ping' || norm === 'pong') return true
  if (ACKS.has(norm)) return true
  if (trivialRosterRE.test(norm)) return true
  return trivialHeadcountRE.test(norm)
}
