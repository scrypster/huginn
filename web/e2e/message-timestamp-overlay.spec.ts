import { test, expect, type Page } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'

// Regression coverage for the hover-timestamp layout-shift bug: hovering a
// chat message used to push the text sideways and shrink the space
// available to it (a flex sibling growing from max-width: 0), which could
// force the text to rewrap onto new lines. The fix makes the timestamp an
// absolutely-positioned overlay driven purely by an opacity transition, so
// the message text's box must be pixel-identical whether or not it's
// hovered.

const SESSION = 'ts-overlay-session'

async function mockSessionEndpoints(page: Page, messages: unknown[]) {
  await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
    route.fulfill({ json: messages }),
  )
  await page.route(`**/api/v1/sessions/${SESSION}`, route => {
    if (route.request().method() === 'GET') {
      return route.fulfill({
        json: { session_id: SESSION, agent: 'Coder', status: 'active' },
      })
    }
    return route.fulfill({ json: {} })
  })
}

test.describe('Chat message hover timestamp — zero-reflow overlay', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('hovering a message reveals the timestamp without moving or rewrapping the message text', async ({ page }) => {
    // A long-ish line, close to the bubble's wrap width, so any shrink in
    // available width from the old push implementation would force a
    // visible rewrap onto an extra line.
    const userText = 'This is a moderately long chat message meant to sit close to the edge of the bubble width so a layout shift would visibly rewrap it onto a new line.'
    const assistantText = 'Here is a similarly long assistant reply that also runs close to the available width of its container, to catch any reflow on hover as well.'

    await mockSessionEndpoints(page, [
      { id: 'msg-1', role: 'user', content: userText, ts: '2026-03-15T10:00:00Z' },
      { id: 'msg-2', role: 'assistant', content: assistantText, agent: 'Coder', ts: '2026-03-15T10:00:01Z' },
    ])
    await page.routeWebSocket('**/ws**', _ws => { /* swallow */ })

    await page.goto(`/#/chat/${SESSION}`)
    await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5000 })

    const userBubble = page.locator('[data-testid="space-root-bubble"]').filter({ hasText: 'moderately long chat message' })
    await expect(userBubble).toBeVisible({ timeout: 5000 })
    const assistantBubble = page.locator('[data-testid="space-root-bubble"]').filter({ hasText: 'similarly long assistant reply' })
    await expect(assistantBubble).toBeVisible({ timeout: 5000 })

    for (const bubble of [userBubble, assistantBubble]) {
      const row = bubble.locator('xpath=ancestor::*[@data-testid="msg-time-row"][1]')
      const stamp = row.locator('[data-testid="msg-rel-time"]')

      // Timestamp is present but not (visually) revealed before hover.
      await expect(stamp).toHaveCount(1)
      await expect(stamp).toHaveCSS('opacity', '0')

      const boxBefore = await bubble.boundingBox()
      expect(boxBefore).not.toBeNull()

      await row.hover()

      // (a) the timestamp becomes visible on hover
      await expect(stamp).toHaveCSS('opacity', '1')

      // (b) the message text's bounding box is IDENTICAL before and during hover
      const boxDuring = await bubble.boundingBox()
      expect(boxDuring).not.toBeNull()
      expect(boxDuring).toEqual(boxBefore)

      // Move off to reset hover state for the next bubble/assertion.
      await page.mouse.move(0, 0)
    }
  })
})
