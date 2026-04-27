import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS, setupInteractiveWS } from './helpers/mock-ws'

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

const twoRunsFixture = [
  {
    id: 'run-1',
    workflow_id: 'wf-1',
    status: 'complete',
    steps: [],
    started_at: '2026-04-27T09:00:00Z',
    completed_at: '2026-04-27T09:01:00Z',
  },
  {
    id: 'run-2',
    workflow_id: 'wf-1',
    status: 'complete',
    steps: [],
    started_at: '2026-04-27T10:00:00Z',
    completed_at: '2026-04-27T10:01:00Z',
  },
]

test.describe('WorkflowsView deep', () => {

  // ---------------------------------------------------------------------------
  // Test 1: Deliveries tab shows "All deliveries successful" when no entries
  // ---------------------------------------------------------------------------
  test('deliveries tab shows "All deliveries successful" when no entries', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override runs endpoint to return our fixture (LIFO: registered after setupApiMocks wins)
    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    await gotoWorkflows(page)

    // Open the workflow editor
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Open run history panel
    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    // Expand run-1 by clicking on the run row (locator for the run id text)
    await page.locator('text=run-1').first().click()
    // Wait for the expanded detail section to appear (shows Replay/Fork/Diff buttons)
    await page.waitForSelector('[data-testid="run-replay-btn"]', { timeout: 5000 })

    // Click the Deliveries tab (note: tab buttons now use @click.stop to prevent run collapse)
    await page.click('button:has-text("Deliveries")')

    // Assert the empty message is visible
    await expect(page.locator('text=All deliveries successful')).toBeVisible({ timeout: 5000 })
  })

  // ---------------------------------------------------------------------------
  // Test 2: Cancel button appears while running and calls POST /cancel
  // ---------------------------------------------------------------------------
  test('cancel button appears while running and calls POST /cancel', async ({ page }) => {
    const ws = await setupInteractiveWS(page)
    await setupApiMocks(page)

    await gotoWorkflows(page)

    // Open the workflow editor to select wf-1
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Send workflow_started WS event to set running = true
    ws.send(JSON.stringify({ type: 'workflow_started', workflow_id: 'wf-1', workflow_name: 'Daily Report' }))

    // Cancel button should become visible
    await expect(page.locator('button:has-text("Cancel")')).toBeVisible({ timeout: 3000 })

    // Track POST to cancel endpoint
    let cancelCalled = false
    await page.route('**/api/v1/workflows/wf-1/cancel', route => {
      if (route.request().method() === 'POST') {
        cancelCalled = true
      }
      return route.fulfill({ json: {} })
    })

    // Click the Cancel button
    await page.click('button:has-text("Cancel")')

    await expect.poll(() => cancelCalled, { timeout: 5000 }).toBe(true)
  })

  // ---------------------------------------------------------------------------
  // Test 3: Fork modal submit calls POST /fork
  // ---------------------------------------------------------------------------
  test('fork modal submit calls POST /fork', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override runs endpoint
    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: runsFixture })
    )

    // Mock and track the fork endpoint
    let forkCalled = false
    await page.route('**/api/v1/workflows/wf-1/runs/run-1/fork', route => {
      if (route.request().method() === 'POST') {
        forkCalled = true
      }
      return route.fulfill({ json: {} })
    })

    await gotoWorkflows(page)

    // Open workflow editor
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Open run history
    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    // Expand run-1
    await page.locator('text=run-1').first().click()

    // Click Fork button
    await page.click('[data-testid="run-fork-btn"]')

    // Fork modal should appear
    await expect(page.locator('[data-testid="fork-submit-btn"]')).toBeVisible({ timeout: 5000 })

    // Click submit
    await page.click('[data-testid="fork-submit-btn"]')

    await expect.poll(() => forkCalled, { timeout: 5000 }).toBe(true)
  })

  // ---------------------------------------------------------------------------
  // Test 4: Diff modal opens with two runs
  // ---------------------------------------------------------------------------
  test('diff modal opens with two runs', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override runs endpoint to return two runs
    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: twoRunsFixture })
    )

    await gotoWorkflows(page)

    // Open workflow editor
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Open run history
    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    // Expand run-1
    await page.locator('text=run-1').first().click()

    // Click Diff vs button
    await page.click('[data-testid="run-diff-btn"]')

    // Assert the diff modal UI elements are visible
    await expect(page.locator('[data-testid="diff-compare-btn"]')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('[data-testid="diff-other-run-select"]')).toBeVisible()
  })

  // ---------------------------------------------------------------------------
  // Test 5: Diff compare calls the diff API
  // ---------------------------------------------------------------------------
  test('diff compare calls the diff API', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override runs endpoint to return two runs
    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({ json: twoRunsFixture })
    )

    // Mock and track the diff endpoint (registered after setupApiMocks so it wins)
    let diffCalled = false
    await page.route('**/api/v1/workflows/wf-1/runs/run-1/diff/run-2', route => {
      diffCalled = true
      return route.fulfill({ json: { diff: 'no changes' } })
    })

    await gotoWorkflows(page)

    // Open workflow editor
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Open run history
    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })

    // Expand run-1
    await page.locator('text=run-1').first().click()

    // Click Diff vs button
    await page.click('[data-testid="run-diff-btn"]')

    // Wait for diff modal to open
    await expect(page.locator('[data-testid="diff-compare-btn"]')).toBeVisible({ timeout: 5000 })

    // diffOtherRunId is pre-populated with run-2, click Compare
    await page.click('[data-testid="diff-compare-btn"]')

    await expect.poll(() => diffCalled, { timeout: 5000 }).toBe(true)
  })

  // ---------------------------------------------------------------------------
  // Test 6: Live execution panel appears on workflow_started WS event
  // ---------------------------------------------------------------------------
  test('live execution panel appears on workflow_started WS event', async ({ page }) => {
    const ws = await setupInteractiveWS(page)
    await setupApiMocks(page)

    await gotoWorkflows(page)

    // Open workflow editor for wf-1
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Send workflow_started event
    ws.send(JSON.stringify({ type: 'workflow_started', workflow_id: 'wf-1', workflow_name: 'Daily Report' }))

    // Live Execution panel should appear
    await expect(page.locator('text=Live Execution')).toBeVisible({ timeout: 3000 })

    // Should show the started label
    await expect(page.locator('text=Started: Daily Report')).toBeVisible({ timeout: 3000 })
  })

  // ---------------------------------------------------------------------------
  // Test 7: Live execution panel shows step events
  // ---------------------------------------------------------------------------
  test('live execution panel shows step events', async ({ page }) => {
    const ws = await setupInteractiveWS(page)
    await setupApiMocks(page)

    await gotoWorkflows(page)

    // Open workflow editor for wf-1
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    // Send workflow_started event
    ws.send(JSON.stringify({ type: 'workflow_started', workflow_id: 'wf-1', workflow_name: 'Daily Report' }))

    // Send workflow_step_started event
    ws.send(JSON.stringify({ type: 'workflow_step_started', workflow_id: 'wf-1', position: 0, slug: 'Gather Data' }))

    // Step event label should be visible
    await expect(page.locator('text=Step 0: Gather Data started')).toBeVisible({ timeout: 3000 })
  })

})
