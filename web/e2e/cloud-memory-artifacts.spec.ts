import { test, expect, type Page } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

async function gotoWorkflows(page: Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Automation"]')
  await page.waitForSelector('[data-testid="workflow-list"]', { timeout: 5000 })
}

test.describe('Cloud, Memory, and Artifacts journeys', () => {
  test('Cloud view renders connected state and disconnect flow', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    let disconnectCalled = false
    await page.route('**/api/v1/cloud/status', route =>
      route.fulfill({
        json: { registered: true, connected: true, machine_id: 'machine-1', cloud_url: 'https://cloud.example' },
      }),
    )
    await page.route('**/api/v1/cloud/connect', route => {
      if (route.request().method() === 'DELETE') {
        disconnectCalled = true
      }
      return route.fulfill({ json: { status: 'ok' } })
    })

    await page.goto('/#/cloud')
    await expect(page.getByText('Connected to HuginnCloud')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('machine-1')).toBeVisible()

    await page.click('button:has-text("Disconnect")')
    await expect.poll(() => disconnectCalled, { timeout: 5000 }).toBe(true)
  })

  test('Memory view can search and forget a memory', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    let recallCalled = false
    let forgetCalled = false

    await page.route('**/api/v1/muninn/status', route =>
      route.fulfill({ json: { connected: true } }),
    )
    await page.route('**/api/v1/muninn/vaults', route =>
      route.fulfill({ json: { vaults: ['team-alpha'] } }),
    )
    await page.route('**/api/v1/muninn/tool', route => {
      const body = route.request().postDataJSON() as { tool?: string }
      if (body.tool === 'muninn_recall') {
        recallCalled = true
        return route.fulfill({
          json: {
            result: {
              memories: [
                {
                  id: 'mem-1',
                  concept: 'Incident timeline',
                  content: 'Root cause narrowed to retry loop saturation.',
                  entities: ['scheduler', 'retry'],
                  decay_score: 0.12,
                },
              ],
            },
          },
        })
      }
      if (body.tool === 'muninn_forget') {
        forgetCalled = true
        return route.fulfill({ json: { result: { ok: true } } })
      }
      return route.fulfill({ status: 400, json: { error: 'unexpected tool' } })
    })

    await page.goto('/#/memory')
    await expect(page.getByText('Browse and search agent vault memories')).toBeVisible({ timeout: 5000 })

    await page.locator('input[placeholder^="Search memories"]').fill('incident')
    await page.click('button:has-text("Search")')
    await expect.poll(() => recallCalled, { timeout: 5000 }).toBe(true)

    await page.click('button:has-text("Incident timeline")')
    await expect(page.getByText('Decay score: 12%')).toBeVisible()

    await page.evaluate(() => {
      window.confirm = () => true
    })
    await page.click('button:has-text("Forget")')
    await expect.poll(() => forgetCalled, { timeout: 5000 }).toBe(true)
    await expect(page.getByText('Select a memory to view details')).toBeVisible()
  })

  test('Workflow run step artifacts popover lists session artifacts', async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    await page.route('**/api/v1/workflows/**/runs', route =>
      route.fulfill({
        json: [
          {
            id: 'run-art-1',
            workflow_id: 'wf-1',
            status: 'complete',
            steps: [
              { position: 0, slug: 'Gather Data', status: 'success', session_id: 'sess-art-1' },
            ],
            started_at: '2026-05-01T09:00:00Z',
            completed_at: '2026-05-01T09:01:00Z',
          },
        ],
      }),
    )
    await page.route('**/api/v1/sessions/sess-art-1/artifacts', route =>
      route.fulfill({
        json: [
          { id: 'art-1', title: 'Incident Report', kind: 'markdown', status: 'ready' },
        ],
      }),
    )

    await gotoWorkflows(page)
    await page.click('[data-testid="workflow-item"]:has-text("Daily Report")')
    await page.waitForSelector('[data-testid="workflow-name-input"]', { timeout: 5000 })

    await page.click('button:has-text("History")')
    await page.waitForSelector('h2:has-text("Run History")', { timeout: 5000 })
    await page.locator('text=run-art-1').first().click()

    await page.click('[data-testid="step-session-artifacts-btn"]')
    await expect(page.getByText('Incident Report')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('markdown')).toBeVisible()
    await expect(page.getByText('ready')).toBeVisible()
  })
})
