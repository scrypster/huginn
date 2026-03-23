import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

/**
 * Navigate to the Connections section.
 *
 * App.vue uses watch(activeSection) — the watcher only fires on *change*.
 * Navigate from a non-connections route first so the watcher detects the
 * transition and triggers the data load.
 */
async function gotoConnections(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button:has-text("Connections")')
  // Wait for the search input — confirms ConnectionsView mounted and initialized
  await page.waitForSelector('[placeholder="Search connections…"]', { timeout: 5000 })
}

test.describe('ConnectionsView', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('renders catalog with search input and category nav', async ({ page }) => {
    await gotoConnections(page)

    // Search input is visible in the header
    await expect(page.locator('[placeholder="Search connections…"]')).toBeVisible()

    // Category nav has "All" and "My Connections" as top items
    await expect(page.locator('nav button:has-text("All")')).toBeVisible()
    await expect(page.locator('nav button:has-text("My Connections")')).toBeVisible()

    // Category items present in sidebar
    await expect(page.locator('nav button:has-text("Communication")')).toBeVisible()
    await expect(page.locator('nav button:has-text("Dev Tools")')).toBeVisible()
  })

  test('catalog grid shows provider cards', async ({ page }) => {
    await gotoConnections(page)

    // Default view is "All" — catalog grid renders multiple entries
    // GitHub card exists in catalog (dev_tools category)
    await expect(page.locator('text=GitHub').first()).toBeVisible()

    // Slack card exists (communication category)
    await expect(page.locator('text=Slack').first()).toBeVisible()
  })

  test('search input filters catalog entries', async ({ page }) => {
    await gotoConnections(page)

    const search = page.locator('[placeholder="Search connections…"]')

    // Type a search term that matches a specific provider
    await search.fill('datadog')

    // Datadog card should be visible
    await expect(page.locator('text=Datadog').first()).toBeVisible()

    // Slack should not be visible (doesn't match "datadog")
    await expect(page.locator('text=Slack').first()).not.toBeVisible({ timeout: 1000 })
  })

  test('search clears to show all entries again', async ({ page }) => {
    await gotoConnections(page)

    const search = page.locator('[placeholder="Search connections…"]')
    await search.fill('datadog')
    await expect(page.locator('text=Slack').first()).not.toBeVisible({ timeout: 1000 })

    // Clear the search — all entries return
    await search.fill('')
    await expect(page.locator('text=Slack').first()).toBeVisible()
  })

  test('My Connections shows the connected GitHub account from fixture', async ({ page }) => {
    await gotoConnections(page)

    // Click the "My Connections" category
    await page.click('nav button:has-text("My Connections")')

    // The fixture has 1 GitHub OAuth connection (provider: 'github', account_label: 'test-user')
    // ConnectionsView my_connections section shows connected entries
    await expect(page.locator('text=GitHub').first()).toBeVisible()
    await expect(page.locator('text=test-user').first()).toBeVisible()
  })

  test('My Connections shows count badge in sidebar', async ({ page }) => {
    await gotoConnections(page)

    // The fixture has 1 GitHub connection → count badge = 1 in "My Connections" nav button
    const myConnectionsBtn = page.locator('nav button:has-text("My Connections")')
    await expect(myConnectionsBtn).toContainText('1')
  })
})
