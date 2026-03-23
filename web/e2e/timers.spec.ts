import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS, setupDisconnectedWS } from './helpers/mock-ws'

/**
 * WS degradation banner timing tests.
 *
 * Uses Playwright's fake-clock API (page.clock) to precisely control
 * setTimeout / setInterval without waiting real wall-clock seconds.
 * This makes the suite deterministic and fast.
 *
 * App.vue sets showDegradedBanner = true after 4 000 ms of continuous
 * WS disconnection (debounced via setTimeout).
 */

test.describe('WS degradation banner — debounce timing', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  // ── 1. Banner absent immediately after disconnect ──────────────────────────
  test('banner is NOT shown immediately after WS closes', async ({ page }) => {
    // Install fake clock BEFORE page load so the app's timers are captured.
    await page.clock.install({ time: 0 })
    await setupDisconnectedWS(page)
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })

    // Do NOT advance time — banner should still be hidden (debounce pending)
    const banner = page.locator('[data-testid="ws-degraded-banner"]')
    await expect(banner).not.toBeVisible()
  })

  // ── 2. Banner appears after 4 s fake-clock advance ────────────────────────
  test('banner appears after 4 s of disconnection', async ({ page }) => {
    await page.clock.install({ time: 0 })
    await setupDisconnectedWS(page)
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })

    // Advance fake clock by exactly 4 seconds to trigger the debounce
    await page.clock.runFor(4000)

    const banner = page.locator('[data-testid="ws-degraded-banner"]')
    await expect(banner).toBeVisible({ timeout: 3000 })
    await expect(banner).toContainText('reconnecting')
  })

  // ── 3. Banner absent when connection stays up past 4 s ────────────────────
  test('banner does NOT appear when WS stays connected past 4 s', async ({ page }) => {
    await page.clock.install({ time: 0 })
    await setupConnectedWS(page)
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })

    // Advance past the debounce window
    await page.clock.runFor(5000)

    const banner = page.locator('[data-testid="ws-degraded-banner"]')
    await expect(banner).not.toBeVisible()
  })

  // ── 4. Banner dismisses via ✕ button ──────────────────────────────────────
  test('banner dismisses when ✕ is clicked', async ({ page }) => {
    await page.clock.install({ time: 0 })
    await setupDisconnectedWS(page)
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })

    await page.clock.runFor(4000)

    const banner = page.locator('[data-testid="ws-degraded-banner"]')
    await expect(banner).toBeVisible({ timeout: 3000 })

    await banner.locator('button').click()
    await expect(banner).not.toBeVisible({ timeout: 3000 })
  })

  // ── 5. Partial advance (< 4 s) keeps banner hidden ────────────────────────
  test('banner stays hidden when only 3.9 s elapse', async ({ page }) => {
    await page.clock.install({ time: 0 })
    await setupDisconnectedWS(page)
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })

    // Just under the debounce threshold
    await page.clock.runFor(3900)

    const banner = page.locator('[data-testid="ws-degraded-banner"]')
    await expect(banner).not.toBeVisible()
  })
})
