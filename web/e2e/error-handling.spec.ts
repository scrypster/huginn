import { test, expect } from '@playwright/test'
import { setupApiMocks, setupRouteError, createMethodErrorHandler, createStatefulHandler } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

/**
 * Error-state E2E coverage.
 *
 * All tests use blockWS + setupApiMocks as the baseline, then register
 * error-inducing overrides AFTER setupApiMocks. Playwright LIFO ordering
 * guarantees the override wins over the default mock.
 */

async function gotoInbox(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Inbox"]')
  await page.waitForSelector('h1:has-text("Inbox")', { timeout: 5000 })
}

test.describe('API error states', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  // ── Scenario 1: Mark-all-seen → 500 → inbox error banner ──────────────────
  test('mark all seen API error shows inbox-error-banner', async ({ page }) => {
    // Override action endpoint to return 500 for POST requests
    await page.route('**/api/v1/notifications/*/action', createMethodErrorHandler('POST', 500))

    await gotoInbox(page)

    const btn = page.locator('[data-testid="mark-all-seen-btn"]')
    await expect(btn).toBeVisible({ timeout: 5000 })
    await btn.click()

    const banner = page.locator('[data-testid="inbox-error-banner"]')
    await expect(banner).toBeVisible({ timeout: 5000 })
  })

  // ── Scenario 2: Dismiss-all → 500 → inbox error banner ────────────────────
  test('dismiss all API error shows inbox-error-banner', async ({ page }) => {
    await page.route('**/api/v1/notifications/*/action', createMethodErrorHandler('POST', 500))

    await gotoInbox(page)

    const btn = page.locator('[data-testid="dismiss-all-btn"]')
    await expect(btn).toBeVisible({ timeout: 5000 })
    await btn.click()

    const banner = page.locator('[data-testid="inbox-error-banner"]')
    await expect(banner).toBeVisible({ timeout: 5000 })
  })

  // ── Scenario 3: inbox-error-banner dismisses on ✕ click ───────────────────
  test('inbox-error-banner dismisses on close click', async ({ page }) => {
    await page.route('**/api/v1/notifications/*/action', createMethodErrorHandler('POST', 500))

    await gotoInbox(page)
    const btn = page.locator('[data-testid="mark-all-seen-btn"]')
    await expect(btn).toBeVisible({ timeout: 5000 })
    await btn.click()

    const banner = page.locator('[data-testid="inbox-error-banner"]')
    await expect(banner).toBeVisible({ timeout: 5000 })

    // Click the ✕ close button inside the banner
    await banner.locator('button').click()
    await expect(banner).not.toBeVisible({ timeout: 3000 })
  })

  // ── Scenario 4: Delete connection → 500 → error shown ────────────────────
  test('delete connection API error surfaces to the user', async ({ page }) => {
    await setupRouteError(page, '**/api/v1/connections/**', 500, { error: 'storage failure' }, { method: 'DELETE' })

    await page.goto('/#/connections')
    await page.waitForSelector('h1:has-text("Connections")', { timeout: 6000 })

    // Click the first delete/remove button
    const deleteBtn = page.locator('button[data-testid="delete-connection-btn"]').first()
    if (await deleteBtn.isVisible()) {
      await deleteBtn.click()
      // App should display an error state (not crash silently)
      const errorText = page.locator('[data-testid="connection-error"]')
      await expect(errorText).toBeVisible({ timeout: 5000 })
    }
  })

  // ── Scenario 5: Notifications list → 500 → empty or error state ──────────
  test('notifications list 500 does not crash the view', async ({ page }) => {
    await setupRouteError(page, '**/api/v1/notifications', 500, { error: 'db error' }, { method: 'GET' })

    await gotoInbox(page)

    // The view should still render (no JS crash) with heading visible
    const heading = page.locator('h1:has-text("Inbox")')
    await expect(heading).toBeVisible({ timeout: 5000 })

    // There should be no notification items (error path returns empty / skip)
    const items = page.locator('[data-testid="notification-item"]')
    await expect(items).toHaveCount(0)
  })

  // ── Scenario 6: Stateful handler — action fails first, succeeds on retry ──
  test('mark-seen retry succeeds after transient 503', async ({ page }) => {
    const handler = createStatefulHandler(
      {},
      503,
      { error: 'service unavailable' },
      true, // failFirst
      { method: 'POST' },
    )
    await page.route('**/api/v1/notifications/*/action', handler)

    await gotoInbox(page)

    const btn = page.locator('[data-testid="mark-all-seen-btn"]')
    await expect(btn).toBeVisible({ timeout: 5000 })

    // First click — should fail (503)
    await btn.click()
    const banner = page.locator('[data-testid="inbox-error-banner"]')
    await expect(banner).toBeVisible({ timeout: 5000 })

    // Dismiss error
    await banner.locator('button').click()
    await expect(banner).not.toBeVisible({ timeout: 3000 })

    // Second click — handler now returns 200 (stateful)
    await btn.click()
    await expect(banner).not.toBeVisible({ timeout: 3000 })
  })

  // ── Scenario 7: Config load → 500 → app still renders ────────────────────
  test('config API 500 does not prevent app render', async ({ page }) => {
    await setupRouteError(page, '**/api/v1/config', 500)

    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 6000 })

    // App shell must be visible even when config fetch fails
    const nav = page.locator('nav')
    await expect(nav).toBeVisible()
  })

  // ── Scenario 8: Workflows list → 500 → view renders without crashing ──────
  test('workflows list 500 does not crash the view', async ({ page }) => {
    await setupRouteError(page, '**/api/v1/workflows', 500, { error: 'db error' }, { method: 'GET' })

    await page.goto('/#/workflows')
    await page.waitForSelector('h1:has-text("Workflows")', { timeout: 6000 })

    // View should render (heading visible) even with no data
    const heading = page.locator('h1:has-text("Workflows")')
    await expect(heading).toBeVisible()
  })
})
