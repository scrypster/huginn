import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import MsgTimeReveal from '../MsgTimeReveal.vue'

describe('MsgTimeReveal', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-27T15:00:00-04:00'))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('hides the relative label until hover or revealed', async () => {
    const w = mount(MsgTimeReveal, {
      props: { createdAt: '2026-08-27T14:00:00-04:00' },
      slots: { default: '<p>hello</p>' },
    })
    const stamp = w.find('[data-testid="msg-rel-time"]')
    expect(stamp.exists()).toBe(true)
    expect(stamp.text()).toBe('1h')
    expect(w.find('[data-testid="msg-time-row"]').classes()).not.toContain('is-revealed')
    await w.find('[data-testid="msg-time-row"]').trigger('mouseenter')
    expect(w.find('[data-testid="msg-time-row"]').classes()).toContain('is-revealed')
  })

  it('reveals when the list swipe flag is on', () => {
    const w = mount(MsgTimeReveal, {
      props: { createdAt: '2026-08-27T14:58:00-04:00', revealed: true },
    })
    expect(w.find('[data-testid="msg-time-row"]').classes()).toContain('is-revealed')
    expect(w.find('[data-testid="msg-rel-time"]').text()).toBe('2m')
  })

  it('swipe-left on the bubble reveals the stamp', async () => {
    const w = mount(MsgTimeReveal, {
      props: { createdAt: '2026-08-27T14:59:30-04:00' },
    })
    const row = w.find('[data-testid="msg-time-row"]')
    await row.trigger('touchstart', { touches: [{ clientX: 200 }] })
    await row.trigger('touchmove', { touches: [{ clientX: 160 }] })
    expect(row.classes()).toContain('is-revealed')
    expect(w.find('[data-testid="msg-rel-time"]').text()).toBe('just now')
  })
})
