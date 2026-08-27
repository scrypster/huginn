/** Relative age labels for hallway / thread timestamps. */

/** Hallway / thread calendar days are America/New_York, matching Slack-local. */
export const HALLWAY_TZ = 'America/New_York'

/** YYYY-MM-DD in the hallway timezone (default America/New_York). */
export function calendarDayKey(
  input: string | Date | undefined | null,
  timeZone = HALLWAY_TZ,
): string {
  if (input == null || input === '') return ''
  const d = input instanceof Date ? input : new Date(input)
  if (Number.isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(d)
}

/** Whole calendar days between two instants in the hallway timezone (later − earlier). */
export function calendarDaysBetween(
  later: string | Date,
  earlier: string | Date,
  timeZone = HALLWAY_TZ,
): number {
  const a = calendarDayKey(later, timeZone)
  const b = calendarDayKey(earlier, timeZone)
  if (!a || !b) return NaN
  const [ay, am, ad] = a.split('-').map(Number)
  const [by, bm, bd] = b.split('-').map(Number)
  return Math.round((Date.UTC(ay!, am! - 1, ad!) - Date.UTC(by!, bm! - 1, bd!)) / 86_400_000)
}

export function formatRelativeTime(ts: string | undefined | null, nowMs = Date.now()): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date(nowMs)
  const diffDays = calendarDaysBetween(now, d)
  if (diffDays === 1) return 'yesterday'
  if (diffDays > 1 && diffDays < 7) {
    return d.toLocaleDateString('en-US', { timeZone: HALLWAY_TZ, weekday: 'long' })
  }
  if (diffDays >= 7) {
    return d.toLocaleDateString('en-US', { timeZone: HALLWAY_TZ, weekday: 'short', month: 'short', day: 'numeric' })
  }

  const diffMs = nowMs - d.getTime()
  if (diffMs < 60_000) return 'just now'
  const diffMin = Math.floor(diffMs / 60_000)
  if (diffMin < 60) return `${diffMin}m`
  const diffHr = Math.floor(diffMin / 60)
  return `${Math.max(diffHr, 1)}h`
}

/** Clock time shown as a tooltip when the relative label is revealed. */
export function formatClockTime(ts: string | undefined | null): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('en-US', { timeZone: HALLWAY_TZ, hour: 'numeric', minute: '2-digit' })
}

export function messageCreatedAt(msg: { created_at?: string; ts?: string; createdAt?: string } | null | undefined): string {
  if (!msg) return ''
  return msg.created_at || msg.createdAt || msg.ts || ''
}

export function messageTimeMs(msg: { created_at?: string; ts?: string; createdAt?: string } | null | undefined): number {
  const raw = messageCreatedAt(msg)
  if (!raw) return 0
  const t = new Date(raw).getTime()
  return Number.isFinite(t) ? t : 0
}
