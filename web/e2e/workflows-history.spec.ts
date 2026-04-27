import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS, setupInteractiveWS } from './helpers/mock-ws'

/**
 * Navigate to the workflows (Automation) section.
 * Same pattern as workflows.spec.ts — go home first so the watcher fires.
 */
async function gotoWorkflows(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Automation"]')
  await page.waitForSelector('[data-testid="workflow-list"]', { timeout: 5000 })
}

const runsFixture = [
  {
    id: 'run-1',
    workflow_id: 'wf-1',
    status: 'complete',
    steps: [
      { position: 0, slug: 'Gather Data', status: 'success' },
      { position: 1, slug: 'Send Summary', status: 'success' },
    ],
    started_at: '2026-04-27T09:00:00Z',
    completed_at: '2026-04-27T09:01:00Z',
  },
]

test.describe('WorkflowsView — run history and live events', () => {
  test('Run Now button calls POST /run', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    let postCalled = false
    await page.route('**/api/v1/workflows/wf-1/run', async route => {
      if (route.request().method() === 'POST') {
        postCalled = true
        await route.fulfill({ json: {} })
      } else {
        await route.continue()
      }
    })

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("Run Now")', { timeout: 5000 })

    await page.click('button:has-text("Run Now")')

    await expect.poll(() => postCalled, { timeout: 5000 }).toBe(true)
  })

  test('History button opens run history panel', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("History")', { timeout: 5000 })

    await page.click('button:has-text("History")')

    await expect(page.locator('h2:has-text("Run History")')).toBeVisible({ timeout: 5000 })
  })

  test('Run history panel shows run list with status and step pills', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override the runs mock AFTER setupApiMocks so LIFO makes this handler win
    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("History")', { timeout: 5000 })

    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    await expect(page.locator('span:has-text("complete")').first()).toBeVisible({ timeout: 5000 })
    await expect(page.locator('text=Gather Data').first()).toBeVisible({ timeout: 5000 })
  })

  test('Expanding a run row shows action buttons', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("History")', { timeout: 5000 })

    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    // Click the run row — span inside shows 'run-1', click bubbles up to the row div
    await page.locator('text=run-1').first().click()

    await expect(page.locator('[data-testid="run-replay-btn"]')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('[data-testid="run-fork-btn"]')).toBeVisible({ timeout: 5000 })
  })

  test('Replay button calls POST /replay', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    let replayCalled = false
    await page.route('**/api/v1/workflows/wf-1/runs/run-1/replay', async route => {
      if (route.request().method() === 'POST') {
        replayCalled = true
        await route.fulfill({ json: {} })
      } else {
        await route.continue()
      }
    })

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("History")', { timeout: 5000 })

    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    await page.locator('text=run-1').first().click()
    await page.waitForSelector('[data-testid="run-replay-btn"]', { timeout: 5000 })

    await page.click('[data-testid="run-replay-btn"]')

    await expect.poll(() => replayCalled, { timeout: 5000 }).toBe(true)
  })

  test('Fork button opens modal', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("History")', { timeout: 5000 })

    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    await page.locator('text=run-1').first().click()
    await page.waitForSelector('[data-testid="run-fork-btn"]', { timeout: 5000 })

    await page.click('[data-testid="run-fork-btn"]')

    await expect(page.locator('[data-testid="fork-submit-btn"]')).toBeVisible({ timeout: 5000 })
  })

  test('WS workflow_started shows Running… then workflow_complete reverts to Run Now', async ({ page }) => {
    const ws = await setupInteractiveWS(page)
    await setupApiMocks(page)

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('button:has-text("Run Now")', { timeout: 5000 })

    ws.send(JSON.stringify({ type: 'workflow_started', workflow_id: 'wf-1', workflow_name: 'Daily Report' }))

    await expect(page.locator('button:has-text("Running…")')).toBeVisible({ timeout: 3000 })

    ws.send(JSON.stringify({ type: 'workflow_complete', workflow_id: 'wf-1' }))

    await expect(page.locator('button:has-text("Run Now")')).toBeVisible({ timeout: 3000 })
  })
})
