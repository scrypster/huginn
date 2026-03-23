import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

/**
 * Navigate to the Inbox (notifications) section.
 *
 * App.vue uses watch(activeSection) — navigate from chat first so the watcher
 * detects the transition and calls fetchNotifications().
 */
async function gotoInbox(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Inbox"]')
  // InboxView renders either the empty state or the notification list
  await page.waitForSelector('h1:has-text("Inbox")', { timeout: 5000 })
}

test.describe('InboxView (Notifications)', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('navigates to inbox and shows heading', async ({ page }) => {
    await gotoInbox(page)

    const heading = page.locator('h1:has-text("Inbox")')
    await expect(heading).toBeVisible()
  })

  test('displays notification list with items', async ({ page }) => {
    await gotoInbox(page)

    const list = page.locator('[data-testid="notification-list"]')
    await expect(list).toBeVisible({ timeout: 5000 })

    // Fixture has 1 notification
    const items = page.locator('[data-testid="notification-item"]')
    await expect(items).toHaveCount(1)
  })

  test('notification item contains summary text', async ({ page }) => {
    await gotoInbox(page)

    const item = page.locator('[data-testid="notification-item"]').first()
    await expect(item).toBeVisible({ timeout: 5000 })
    await expect(item).toContainText('Daily Report')
  })

  test('mark all seen button is visible', async ({ page }) => {
    await gotoInbox(page)

    const markAllBtn = page.locator('[data-testid="mark-all-seen-btn"]')
    await expect(markAllBtn).toBeVisible({ timeout: 5000 })
  })

  test('mark all seen triggers API action for pending notifications', async ({ page }) => {
    await gotoInbox(page)

    const requests: string[] = []
    await page.route('**/api/v1/notifications/*/action', async route => {
      requests.push(route.request().url())
      await route.fulfill({ json: {} })
    })

    const btn = page.locator('[data-testid="mark-all-seen-btn"]')
    await expect(btn).toBeVisible({ timeout: 5000 })
    await btn.click()

    // markAllSeen() iterates notifications with status='pending' and calls
    // applyAction(id, 'seen') for each — fixture has 1 pending notification
    await expect.poll(() => requests.length, { timeout: 5000 }).toBeGreaterThan(0)
  })
})
