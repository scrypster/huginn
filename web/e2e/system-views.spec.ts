import { test, expect, type Page } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS } from './helpers/mock-ws'

async function gotoSection(page: Page, title: string, pathPattern: RegExp) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click(`button[title="${title}"]`)
  await expect(page).toHaveURL(pathPattern, { timeout: 5000 })
}

test.describe('System views coverage', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await page.route('**/api/v1/log-level', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: { level: 'info' } })
      if (route.request().method() === 'PUT') return route.fulfill({ json: { level: 'info' } })
      return route.continue()
    })
    await setupConnectedWS(page)
  })

  test('Stats view renders core cards', async ({ page }) => {
    await page.route('**/api/v1/stats', route =>
      route.fulfill({ json: { last_prompt_tokens: 42, last_completion_tokens: 314 } }),
    )
    await page.route('**/api/v1/cost', route =>
      route.fulfill({ json: { session_total_usd: 1.2345 } }),
    )
    await page.route('**/api/v1/sessions', route => {
      if (route.request().method() !== 'GET') return route.continue()
      return route.fulfill({
        json: [
          { session_id: 's-1', status: 'active', message_count: 12, agent: 'Coder', model: 'claude-sonnet-4-6' },
          { session_id: 's-2', status: 'idle', message_count: 7, agent: 'GitAgent', model: 'claude-haiku-4' },
        ],
      })
    })
    await page.route('**/api/v1/stats/history*', route =>
      route.fulfill({ json: { stats: [], cost: [] } }),
    )

    await gotoSection(page, 'Stats', /#\/stats$/)
    await expect(page.getByText('Overview')).toBeVisible()
    await expect(page.getByText('Top Agents')).toBeVisible()
    await expect(page.getByText('Top Models')).toBeVisible()
    await expect(page.getByText('$1.2345')).toBeVisible()
  })

  test('Settings view can switch tabs to About', async ({ page }) => {
    await gotoSection(page, 'Settings', /#\/settings$/)
    await expect(page.getByRole('main').getByText('Settings')).toBeVisible()

    await page.getByRole('button', { name: 'About', exact: true }).click()
    await expect(page.locator('[data-testid="settings-about-panel"]')).toBeVisible()
    await expect(page.locator('[data-testid="settings-version-value"]')).not.toBeEmpty()
  })

  test('Models view shows provider catalog from API', async ({ page }) => {
    await page.route('**/api/v1/config', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          json: {
            theme: 'dark',
            backend: {
              provider: 'anthropic',
              endpoint: 'https://api.anthropic.com',
              api_key: '[REDACTED]',
            },
            integrations: {},
            web_ui: { enabled: true, port: 0, auto_open: false, bind: '127.0.0.1' },
            allowed_tools: [],
            disallowed_tools: [],
            mcp_servers: [],
          },
        })
      }
      if (route.request().method() === 'PUT') {
        return route.fulfill({ json: { saved: true, requires_restart: false } })
      }
      return route.continue()
    })
    await page.route('**/api/v1/providers/anthropic/models', route =>
      route.fulfill({
        json: [
          {
            id: 'claude-sonnet-4-6',
            name: 'Claude Sonnet 4.6',
            description: 'High-capability reasoning model',
            context_length: 200000,
            pricing_prompt: 3.0,
            pricing_completion: 15.0,
          },
        ],
      }),
    )

    await gotoSection(page, 'Models', /#\/models(\/anthropic)?$/)
    await expect(page.getByRole('button', { name: 'Anthropic', exact: true })).toBeVisible()
    await expect(page.getByText('Claude Sonnet 4.6')).toBeVisible()
  })

  test('Logs view renders and filters entries', async ({ page }) => {
    await page.route('**/api/v1/logs*', route =>
      route.fulfill({
        json: {
          lines: [
            '{"time":"2026-05-01T10:00:00Z","level":"INFO","msg":"scheduler tick","workflow_id":"wf-1"}',
            '{"time":"2026-05-01T10:00:05Z","level":"ERROR","msg":"database unavailable","component":"sqlite"}',
          ],
        },
      }),
    )

    await gotoSection(page, 'Logs', /#\/logs$/)
    await expect(page.getByText('scheduler tick')).toBeVisible()
    await expect(page.getByText('database unavailable')).toBeVisible()

    await page.fill('input[placeholder="search..."]', 'database')
    await expect(page.getByText('database unavailable')).toBeVisible()
    await expect(page.getByText('scheduler tick')).toHaveCount(0)
  })

  test('Skills view covers browse and installed tabs', async ({ page }) => {
    await page.route('**/api/v1/skills', route =>
      route.fulfill({
        json: [
          {
            name: 'git-assistant',
            author: 'huginn',
            source: 'registry',
            enabled: true,
            tool_count: 2,
            version: '1.0.0',
          },
        ],
      }),
    )
    await page.route('**/api/v1/skills/registry/index*', route =>
      route.fulfill({
        json: {
          skills: [
            {
              id: 'code-reviewer',
              name: 'code-reviewer',
              display_name: 'Code Reviewer',
              description: 'Review diffs for bugs and regressions.',
              author: 'huginn',
              category: 'development',
              tags: ['review', 'quality'],
              source_url: 'https://example.com/code-reviewer.md',
              collection: 'engineering',
              version: '1.2.0',
            },
          ],
          collections: [],
        },
      }),
    )

    await gotoSection(page, 'Skills', /#\/skills\/browse$/)
    await expect(page.getByText('Skills Marketplace')).toBeVisible()
    await expect(page.getByText('Code Reviewer')).toBeVisible()

    await page.getByRole('button', { name: 'Installed', exact: true }).click()
    await expect(page).toHaveURL(/#\/skills\/installed$/, { timeout: 5000 })
    await expect(page.getByText('Installed Skills')).toBeVisible()
    await expect(page.getByText('git-assistant')).toBeVisible()
  })
})
