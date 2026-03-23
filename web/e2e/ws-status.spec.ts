import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS, setupDisconnectedWS } from './helpers/mock-ws'

test.describe('WebSocket Status Indicator', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('status dot is red when WebSocket is not connected', async ({ page }) => {
    // setupDisconnectedWS accepts the connection then immediately closes it,
    // which triggers ws.onclose in the browser and sets connected = false.
    await setupDisconnectedWS(page)
    await page.goto('/#/')

    const dot = page.locator('[data-testid="ws-status-dot"]')
    await expect(dot).toBeVisible()
    // Dot must be red when disconnected
    await expect(dot).toHaveClass(/bg-huginn-red/, { timeout: 5000 })
  })

  test('status dot is green when WebSocket is connected (local, no cloud)', async ({ page }) => {
    // setupConnectedWS accepts the connection without closing it.
    // useHuginnWS.ts sets connected.value = true in ws.onopen — no message needed.
    // The cloud/status mock returns { connected: false }, so cloudConnected = false.
    // The class logic: !wsConnected ? red : cloudConnected ? blue : green
    // => wsConnected=true, cloudConnected=false => bg-huginn-green
    await setupConnectedWS(page)
    await page.goto('/#/')

    const dot = page.locator('[data-testid="ws-status-dot"]')
    await expect(dot).toBeVisible()
    // When connected locally with no cloud, dot should be green
    await expect(dot).toHaveClass(/bg-huginn-green/, { timeout: 5000 })
  })
})
