import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import MsgTimeReveal from '../MsgTimeReveal.vue'

const componentSource = readFileSync(
  join(dirname(__dirname), 'MsgTimeReveal.vue'),
  'utf-8',
)

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

  // Regression coverage for the layout-shift bug: hovering used to push the
  // message text sideways (transform + growing max-width in the flex flow),
  // causing it to rewrap. The stamp must now be an absolutely-positioned
  // overlay that only fades in/out — it can never touch the text
  // container's box.
  it('positions the timestamp as a non-flow overlay, not a flex sibling that pushes text', () => {
    const w = mount(MsgTimeReveal, {
      props: { createdAt: '2026-08-27T14:00:00-04:00' },
      slots: { default: '<p>hello</p>' },
    })
    const stamp = w.find('[data-testid="msg-rel-time"]')
    expect(stamp.exists()).toBe(true)
    expect(stamp.classes()).toContain('msg-time-stamp')

    // The stamp's CSS rule must take it out of flow entirely (position:
    // absolute) — a `max-width`/`margin` push transition on `.msg-time-stamp`
    // (the old implementation) is exactly the layout-shift bug being fixed.
    const stampRule = componentSource.match(/\.msg-time-stamp\s*\{[^}]*\}/)?.[0] ?? ''
    expect(stampRule).toMatch(/position:\s*absolute/)
    expect(stampRule).not.toMatch(/max-width/)
    expect(stampRule).not.toMatch(/\bmargin(-left|-right)?:/)

    // Only opacity may transition on reveal for the stamp — no transform/width/margin motion.
    const transitionMatch = stampRule.match(/transition:\s*([^;]+);/)?.[1] ?? ''
    expect(transitionMatch).toMatch(/opacity/)
    expect(transitionMatch).not.toMatch(/transform|max-width|margin/)

    // The message body itself must carry no transform (the old slide effect).
    const bodyRule = componentSource.match(/\.msg-time-body\s*\{[^}]*\}/)?.[0] ?? ''
    expect(bodyRule).not.toMatch(/transform/)
    const revealedBodyRule = componentSource.match(/\.msg-time-row\.is-revealed \.msg-time-body\s*\{[^}]*\}/)
    expect(revealedBodyRule).toBeNull()
  })

  it('never changes the width-affecting classes of the message body between hover and no-hover', async () => {
    const w = mount(MsgTimeReveal, {
      props: { createdAt: '2026-08-27T14:00:00-04:00' },
      slots: { default: '<p>hello</p>' },
    })
    const body = w.find('[data-testid="msg-time-body"]')
    const before = body.classes().slice().sort()
    await w.find('[data-testid="msg-time-row"]').trigger('mouseenter')
    const after = body.classes().slice().sort()
    expect(after).toEqual(before)
  })

})
