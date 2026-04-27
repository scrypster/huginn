import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

/**
 * Navigate to the workflows (Automation) section.
 *
 * App.vue uses watch(activeSection) — the watcher only fires on change, not on
 * initial mount. Navigating directly to /#/workflows means activeSection starts as
 * 'automation' with no prior value, so loadWorkflows() is never called. Work around
 * this by first landing on a non-automation route, then clicking the Automation nav
 * button so the watcher detects a transition and calls loadWorkflows().
 */
async function gotoWorkflows(page: import('@playwright/test').Page) {
  // Start somewhere other than automation so activeSection begins as 'chat'
  await page.goto('/#/')
  // Wait for app to initialize (token fetch completes)
  await page.waitForSelector('nav', { timeout: 5000 })
  // Now click the Automation nav button — this changes activeSection and fires the watcher
  await page.click('button[title="Automation"]')
  // Wait for the workflow list container to appear
  await page.waitForSelector('[data-testid="workflow-list"]', { timeout: 5000 })
}

test.describe('WorkflowsView', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('displays workflow list', async ({ page }) => {
    await gotoWorkflows(page)

    const list = page.locator('[data-testid="workflow-list"]')
    await expect(list).toBeVisible()

    // Fixture has 2 workflows (Daily Report + Downstream for chain picker)
    const items = page.locator('[data-testid="workflow-item"]')
    await expect(items).toHaveCount(2)
    await expect(items.filter({ hasText: 'Daily Report' })).toHaveCount(1)
  })

  test('opens workflow editor on click', async ({ page }) => {
    await gotoWorkflows(page)

    // Click the workflow item
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')

    // Editor should render with the workflow name
    const nameInput = page.locator('[data-testid="workflow-name-input"]')
    await expect(nameInput).toBeVisible({ timeout: 5000 })
    await expect(nameInput).toHaveValue('Daily Report')
  })

  test('shows workflow steps', async ({ page }) => {
    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')

    // Fixture workflow has 2 steps
    const steps = page.locator('[data-testid="workflow-step"]')
    await expect(steps).toHaveCount(2, { timeout: 5000 })
  })

  test('can add a new step', async ({ page }) => {
    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')

    // Wait for steps to render
    await page.waitForSelector('[data-testid="workflow-step"]', { timeout: 5000 })

    // Click Add Step
    await page.click('[data-testid="add-step-btn"]')

    // Should now have 3 steps
    const steps = page.locator('[data-testid="workflow-step"]')
    await expect(steps).toHaveCount(3)
  })

  test('create workflow button opens modal', async ({ page }) => {
    await gotoWorkflows(page)

    await page.click('[data-testid="new-workflow-btn"]')

    // Modal should appear with "New Workflow" heading and a blank workflow option
    await expect(page.locator('text=New Workflow').first()).toBeVisible({ timeout: 5000 })
    await expect(page.locator('text=Blank Workflow')).toBeVisible()
  })

  test('save workflow calls PUT', async ({ page }) => {
    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')

    const nameInput = page.locator('[data-testid="workflow-name-input"]')
    await expect(nameInput).toBeVisible({ timeout: 5000 })

    // Track whether PUT was called
    let putCalled = false
    await page.route('**/api/v1/workflows/**', async route => {
      if (route.request().method() === 'PUT') {
        putCalled = true
        await route.fulfill({ json: { id: 'wf-1', name: 'Updated Report', enabled: true, schedule: '0 9 * * 1-5', steps: [], notification: { on_success: true, on_failure: true, severity: 'info', deliver_to: [] } } })
      } else {
        await route.continue()
      }
    })

    // Modify the name
    await nameInput.fill('Updated Report')

    // Click Save
    await page.click('[data-testid="save-workflow-btn"]')

    await expect.poll(() => putCalled, { timeout: 5000 }).toBe(true)
  })

  test('save workflow PUT body includes retry, chain, and step model_override', async ({ page }) => {
    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    let putBody: Record<string, unknown> | null = null
    await page.route('**/api/v1/workflows/wf-1', async route => {
      if (route.request().method() === 'PUT') {
        putBody = route.request().postDataJSON() as Record<string, unknown>
        await route.fulfill({
          json: {
            id: 'wf-1',
            name: 'Daily Report',
            enabled: true,
            schedule: '0 9 * * 1-5',
            version: 2,
            steps: putBody?.steps as unknown[],
            retry: putBody?.retry,
            chain: putBody?.chain,
            notification: { on_success: true, on_failure: true, severity: 'info', deliver_to: [] },
          },
        })
      } else {
        await route.continue()
      }
    })

    await page.click('[data-testid="workflow-advanced-toggle"]')
    await page.locator('[data-testid="workflow-retry-max-input"]').fill('2')
    await page.locator('[data-testid="workflow-retry-delay-input"]').fill('15s')
    await page.locator('[data-testid="workflow-chain-next-input"]').selectOption('wf-2')

    await page.locator('[data-testid="workflow-step"]').first().click()
    await page.locator('[data-testid="step-model-override-input"]').fill('gpt-4o-mini')

    await page.click('[data-testid="save-workflow-btn"]')
    await expect.poll(() => putBody !== null, { timeout: 5000 }).toBe(true)
    expect(putBody!.retry).toMatchObject({ max_retries: 2, delay: '15s' })
    expect(putBody!.chain).toMatchObject({ next: 'wf-2' })
    const steps = putBody!.steps as Array<Record<string, unknown>>
    expect(steps[0]).toMatchObject({ model_override: 'gpt-4o-mini' })
  })

  test('delete workflow button calls DELETE', async ({ page }) => {
    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')

    await page.waitForSelector('[data-testid="delete-workflow-btn"]', { timeout: 5000 })

    // Track DELETE call
    let deleteCalled = false
    await page.route('**/api/v1/workflows/**', async route => {
      if (route.request().method() === 'DELETE') {
        deleteCalled = true
        await route.fulfill({ status: 204, body: '' })
      } else {
        await route.continue()
      }
    })

    await page.click('[data-testid="delete-workflow-btn"]')

    // Inline confirmation UI: click the Confirm button that appears
    await page.click('button:has-text("Confirm")')

    await expect.poll(() => deleteCalled, { timeout: 5000 }).toBe(true)
  })
})
