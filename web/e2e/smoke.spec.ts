import { test, expect } from '@playwright/test'

test('app loads without crashing', async ({ page }) => {
  // Mock token endpoint first
  await page.route('**/api/v1/token', route =>
    route.fulfill({ json: { token: 'test-smoke-token' } })
  )
  // Catch-all for all other API calls
  await page.route('**/api/v1/**', route =>
    route.fulfill({ status: 200, json: {} })
  )
  // Silence WebSocket connection attempts
  await page.routeWebSocket('**/ws**', _ws => { /* swallow */ })

  // Register pageerror listener BEFORE navigation so errors during page load
  // are captured. Any listener registered after goto() misses errors that fire
  // during or immediately after the initial script evaluation.
  const errors: string[] = []
  page.on('pageerror', err => errors.push(err.message))

  await page.goto('/#/')
  await expect(page.locator('body')).toBeVisible()
  await page.waitForTimeout(500)
  // Filter out network errors from unmocked endpoints (expected in smoke test)
  const realErrors = errors.filter(e => !e.includes('Failed to fetch'))
  expect(realErrors).toHaveLength(0)
})
