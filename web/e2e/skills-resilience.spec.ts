import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS } from './helpers/mock-ws'

async function gotoSkillsBrowse(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Skills"]')
  await expect(page).toHaveURL(/#\/skills\/browse$/, { timeout: 5000 })
}

test.describe('Skills registry resilience', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await setupConnectedWS(page)
    await page.route('**/api/v1/log-level', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: { level: 'info' } })
      if (route.request().method() === 'PUT') return route.fulfill({ json: { level: 'info' } })
      return route.continue()
    })
  })

  test('Browse recovers after registry index failure via Retry', async ({ page }) => {
    await page.route('**/api/v1/skills', route =>
      route.fulfill({ json: [] }),
    )

    let indexCalls = 0
    await page.route('**/api/v1/skills/registry/index*', route => {
      indexCalls += 1
      if (indexCalls === 1) {
        return route.fulfill({ status: 503, json: { error: 'registry offline' } })
      }
      return route.fulfill({
        json: {
          skills: [
            {
              id: 'code-reviewer',
              name: 'code-reviewer',
              display_name: 'Code Reviewer',
              description: 'Review diffs.',
              author: 'huginn',
              category: 'development',
              tags: ['review'],
              source_url: 'https://example.com/code-reviewer.md',
              collection: '',
              version: '1.2.0',
            },
          ],
          collections: [],
        },
      })
    })

    await gotoSkillsBrowse(page)
    await expect(page.getByText('Registry unavailable')).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect(page.getByText('Code Reviewer')).toBeVisible({ timeout: 5000 })
  })

  test('Install failures surface an actionable error message', async ({ page }) => {
    await page.route('**/api/v1/skills', route =>
      route.fulfill({ json: [] }),
    )
    await page.route('**/api/v1/skills/registry/index*', route =>
      route.fulfill({
        json: {
          skills: [
            {
              id: 'code-reviewer',
              name: 'code-reviewer',
              display_name: 'Code Reviewer',
              description: 'Review diffs.',
              author: 'huginn',
              category: 'development',
              tags: ['review'],
              source_url: 'https://example.com/code-reviewer.md',
              collection: '',
              version: '1.2.0',
            },
          ],
          collections: [],
        },
      }),
    )
    await page.route('**/api/v1/skills/install', route =>
      route.fulfill({ status: 500, json: { error: 'install failed: registry unavailable' } }),
    )

    await gotoSkillsBrowse(page)
    await expect(page.getByText('Skills Marketplace')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('Code Reviewer')).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: 'Install', exact: true }).first().click()
    await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Install', exact: true }).nth(1).click()

    await expect(
      page.getByRole('main').getByText('install failed: registry unavailable').first(),
    ).toBeVisible({ timeout: 5000 })
  })
})
