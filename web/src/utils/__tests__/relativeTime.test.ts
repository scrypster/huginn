import { describe, it, expect } from 'vitest'
import { calendarDayKey, formatClockTime, formatRelativeTime, messageCreatedAt } from '../relativeTime'

describe('formatRelativeTime', () => {
  const now = Date.parse('2026-08-27T15:00:00-04:00')

  it('returns empty for missing or invalid input', () => {
    expect(formatRelativeTime(undefined, now)).toBe('')
    expect(formatRelativeTime('', now)).toBe('')
    expect(formatRelativeTime('not-a-date', now)).toBe('')
  })

  it('uses just now / Nm / Nh within a day', () => {
    expect(formatRelativeTime('2026-08-27T14:59:20-04:00', now)).toBe('just now')
    expect(formatRelativeTime('2026-08-27T14:58:00-04:00', now)).toBe('2m')
    expect(formatRelativeTime('2026-08-27T14:00:00-04:00', now)).toBe('1h')
    expect(formatRelativeTime('2026-08-27T03:00:00-04:00', now)).toBe('12h')
  })

  it('uses yesterday and weekday across calendar days', () => {
    expect(formatRelativeTime('2026-08-26T18:00:00-04:00', now)).toBe('yesterday')
    const weekday = formatRelativeTime('2026-08-24T15:00:00-04:00', now)
    expect(weekday.toLowerCase()).toBe('monday')
  })

  it('classifies 02:15Z as yesterday in America/New_York', () => {
    expect(formatRelativeTime('2026-08-27T02:15:23Z', now)).toBe('yesterday')
    expect(calendarDayKey('2026-08-27T02:15:23Z')).toBe('2026-08-26')
    expect(calendarDayKey('2026-08-27T16:00:00Z')).toBe('2026-08-27')
  })

  it('falls back to a short date after a week', () => {
    const label = formatRelativeTime('2026-08-10T15:00:00-04:00', now)
    expect(label.toLowerCase()).toMatch(/aug/)
    expect(label).toMatch(/10/)
  })
})

describe('formatClockTime', () => {
  it('returns a clock string for a valid timestamp', () => {
    const clock = formatClockTime('2026-08-27T15:04:00-04:00')
    expect(clock).toMatch(/\d/)
    expect(clock.toLowerCase()).toMatch(/[ap]/)
  })

  it('returns empty for invalid input', () => {
    expect(formatClockTime(undefined)).toBe('')
    expect(formatClockTime('nope')).toBe('')
  })
})

describe('messageCreatedAt', () => {
  it('prefers created_at over ts / createdAt', () => {
    expect(messageCreatedAt({ created_at: 'a', ts: 'b', createdAt: 'c' })).toBe('a')
    expect(messageCreatedAt({ ts: 'b', createdAt: 'c' })).toBe('c')
    expect(messageCreatedAt({ ts: 'b' })).toBe('b')
    expect(messageCreatedAt({})).toBe('')
  })
})
