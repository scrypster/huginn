import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

const failedEntry = {
  id: 'dq-1',
  workflow_id: 'wf-1',
  run_id: 'run-1',
  endpoint: 'https://hooks.example.com/webhook',
  channel: 'webhook',
  status: 'failed',
  attempt_count: 5,
  max_attempts: 5,
  retry_window_s: 480,
  next_retry_at: '2026-04-27T10:00:00Z',
  created_at: '2026-04-27T09:00:00Z',
  last_error: 'connection refused',
}

async function gotoApp(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
}

test.describe('Delivery Queue UI', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('no badge shown when delivery queue is empty', async ({ page }) => {
    // Default mocks return count: 0 — badge should not appear
    await gotoApp(page)

    // The automation nav button should have no visible badge
    const automationBtn = page.locator('button[title="Automation"]')
    await expect(automationBtn).toBeVisible()

    // Badge span is conditionally rendered only when hasIssues (count > 0)
    // It sits inside the automation button — should not be present
    const badge = automationBtn.locator('span').filter({ hasText: /\d/ })
    await expect(badge).not.toBeVisible()
  })

  test('badge appears on Automation nav when there are delivery issues', async ({ page }) => {
    // Override badge endpoint to return count: 1 (registered after setupApiMocks = higher priority)
    await page.route('**/api/v1/delivery-queue/badge', route =>
      route.fulfill({ json: { count: 1 } })
    )
    await page.route('**/api/v1/delivery-queue', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: [failedEntry] })
      return route.continue()
    })

    await gotoApp(page)

    // Badge should be visible on the Automation nav button
    const automationBtn = page.locator('button[title="Automation"]')
    await expect(automationBtn).toBeVisible()
    const badge = automationBtn.locator('span')
    await expect(badge).toBeVisible({ timeout: 3000 })
    await expect(badge).toContainText('1')
  })

  test('clicking delivery badge opens drawer with failed entry', async ({ page }) => {
    await page.route('**/api/v1/delivery-queue/badge', route =>
      route.fulfill({ json: { count: 1 } })
    )
    await page.route('**/api/v1/delivery-queue', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: [failedEntry] })
      return route.continue()
    })

    await gotoApp(page)

    // Wait for badge to appear and click it
    const badge = page.locator('button[title="Automation"] span')
    await expect(badge).toBeVisible({ timeout: 3000 })
    await badge.click({ force: true })

    // Drawer should open with the entry
    await expect(page.locator('text=Delivery Issues')).toBeVisible({ timeout: 3000 })
    await expect(page.locator('text=https://hooks.example.com/webhook')).toBeVisible()
    await expect(page.locator('text=Failed after 5 attempts')).toBeVisible()
    await expect(page.locator('text=connection refused')).toBeVisible()
  })

  test('dismiss removes entry from drawer', async ({ page }) => {
    // Badge override — must be registered AFTER setupApiMocks (LIFO = higher priority)
    await page.route('**/api/v1/delivery-queue/badge', route =>
      route.fulfill({ json: { count: 1 } })
    )

    let dismissed = false
    // Catch all remaining delivery-queue routes (list + dismiss DELETE)
    await page.route('**/api/v1/delivery-queue', route => {
      const method = route.request().method()
      if (method === 'GET') {
        return route.fulfill({ json: dismissed ? [] : [failedEntry] })
      }
      return route.continue()
    })
    await page.route('**/api/v1/delivery-queue/dq-1', route => {
      if (route.request().method() === 'DELETE') {
        dismissed = true
        return route.fulfill({ json: { status: 'dismissed', id: 'dq-1' } })
      }
      return route.continue()
    })

    await gotoApp(page)

    // Open drawer
    const badge = page.locator('button[title="Automation"] span')
    await expect(badge).toBeVisible({ timeout: 3000 })
    await badge.click({ force: true })

    await expect(page.locator('text=Delivery Issues')).toBeVisible({ timeout: 3000 })
    await expect(page.locator('text=https://hooks.example.com/webhook')).toBeVisible()

    // Click Dismiss
    await page.locator('button', { hasText: 'Dismiss' }).first().click()

    // Entry should be gone from the drawer
    await expect(page.locator('text=https://hooks.example.com/webhook')).not.toBeVisible({ timeout: 3000 })
    await expect(page.locator('text=No delivery issues')).toBeVisible()
  })
})
