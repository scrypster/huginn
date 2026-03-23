import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'

test.describe('WebSocket reconnect backoff', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('reconnects with backoff after server closes connection', async ({ page }) => {
    let connectionCount = 0

    await page.routeWebSocket('**/ws**', ws => {
      connectionCount++
      if (connectionCount === 1) {
        // First connection succeeds then closes after 200ms
        setTimeout(() => ws.close(), 200)
      }
      // Second connection stays open
    })

    await page.goto('/#/chat/test-session-1')

    // Wait for initial connection then disconnection
    await page.waitForTimeout(500)

    // The backoff should reconnect within ~1-2s (first attempt)
    await page.waitForTimeout(2000)

    // Should have reconnected at least once
    expect(connectionCount).toBeGreaterThanOrEqual(2)
  })

  test('status dot reflects connection state', async ({ page }) => {
    let wsHandle: { close: () => void } | null = null

    await page.routeWebSocket('**/ws**', ws => {
      wsHandle = ws
    })

    await page.goto('/#/chat/test-session-1')

    // Wait for connected state
    const dot = page.locator('[data-testid="ws-status-dot"]')
    await expect(dot).toHaveClass(/bg-huginn-green/, { timeout: 5000 })

    // Close the connection
    wsHandle?.close()

    // Dot should lose green class (disconnected)
    await expect(dot).not.toHaveClass(/bg-huginn-green/, { timeout: 5000 })
  })
})
