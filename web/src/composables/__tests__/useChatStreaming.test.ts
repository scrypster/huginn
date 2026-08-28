import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useChatStreaming } from '../useChatStreaming'

describe('useChatStreaming watchdog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not reset a live turn at 60s when thinking is flowing', () => {
    const s = useChatStreaming()
    s.streaming.value = true
    s.startStreamingWatchdog(() => true)
    vi.advanceTimersByTime(60_000)
    expect(s.streaming.value).toBe(true)
    vi.advanceTimersByTime(60_000)
    expect(s.streaming.value).toBe(true)
  })

  it('does not reset a live turn at 60s when tokens keep arriving', () => {
    const s = useChatStreaming()
    s.streaming.value = true
    s.startStreamingWatchdog()
    vi.advanceTimersByTime(50_000)
    s.startStreamingWatchdog()
    vi.advanceTimersByTime(50_000)
    expect(s.streaming.value).toBe(true)
  })

  it('resets after 60s of true inactivity', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const s = useChatStreaming()
    s.streaming.value = true
    s.startStreamingWatchdog()
    vi.advanceTimersByTime(60_000)
    expect(s.streaming.value).toBe(false)
    warn.mockRestore()
  })
})
