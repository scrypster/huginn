import { test, expect, type Page } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS, setupConnectedWS } from './helpers/mock-ws'

const WARNING = 'This model is unlikely to use tools or delegate. Grants will not do what you expect.'

const steve = {
  name: 'Steve',
  model: 'qwen2.5-coder:7b',
  icon: 'S',
  color: '#d29922',
  is_default: true,
  system_prompt: '',
  memory_enabled: false,
  vault_name: '',
  local_tools: ['*'],
  toolbelt: [],
}

const chris = {
  name: 'Chris',
  model: 'qwen2.5-coder:14b',
  icon: 'C',
  color: '#3fb950',
  is_default: false,
  system_prompt: '',
  memory_enabled: false,
  vault_name: '',
  local_tools: ['*'],
  toolbelt: [],
  supportsTools: true,
}

async function mockWarningAgents(page: Page) {
  await page.route('**/api/v1/agents', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: [steve, chris] })
    return route.continue()
  })
  await page.route('**/api/v1/agents/Steve', route => route.fulfill({ json: steve }))
  await page.route('**/api/v1/agents/Chris', route => route.fulfill({ json: chris }))
  await page.route('**/api/v1/agents/active', route => {
    if (route.request().method() === 'GET') return route.fulfill({ json: { name: 'Steve' } })
    return route.fulfill({ json: { active_agent: 'Steve' } })
  })
}

async function gotoAgents(page: Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button:has-text("Agents")')
  await page.waitForSelector('[data-testid="agent-card"]', { timeout: 5000 })
}

test.describe('Persistent model tool warning', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
    await mockWarningAgents(page)
  })

  test('7b agent card shows the warning; 14b card does not', async ({ page }) => {
    await gotoAgents(page)
    const cards = page.locator('[data-testid="agent-card"]')
    await expect(cards).toHaveCount(2)
    await expect(cards.filter({ hasText: 'Steve' }).locator('[data-testid="model-tools-warning"]')).toHaveText(WARNING)
    await expect(cards.filter({ hasText: 'Chris' }).locator('[data-testid="model-tools-warning"]')).toHaveCount(0)
    await page.screenshot({ path: '/opt/cursor/artifacts/agent_cards_tool_warning.png', fullPage: true })
  })

  test('editor for a saved 7b agent keeps the warning', async ({ page }) => {
    await gotoAgents(page)
    await page.click('[data-testid="agent-item"]:has-text("Steve")')
    await expect(page.locator('input[placeholder="Agent name"]')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('[data-testid="model-tools-warning"]')).toHaveText(WARNING)
    await page.screenshot({ path: '/opt/cursor/artifacts/agent_editor_tool_warning.png', fullPage: true })
  })
})

test.describe('Chat model tool warning', () => {
  const SESSION = 'steve-dm'

  test.beforeEach(async ({ page }) => {
    await setupConnectedWS(page)
    await setupApiMocks(page)
    await mockWarningAgents(page)
    await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
      route.fulfill({
        json: [
          { id: 'a1', role: 'assistant', content: 'TOOL_FAIL: The "json" tool is not available.', agent: 'Steve' },
        ],
      }),
    )
    await page.route(`**/api/v1/sessions/${SESSION}`, route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({ json: { session_id: SESSION, agent: 'Steve', status: 'active' } })
      }
      return route.fulfill({ json: {} })
    })
    await page.route('**/api/v1/sessions', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          json: [{ session_id: SESSION, title: 'Steve', agent: 'Steve', status: 'active' }],
        })
      }
      return route.continue()
    })
  })

  test('chat header and composer warn for 7b displayAgent; TOOL_FAIL is a chip', async ({ page }) => {
    await page.goto(`/#/chat/${SESSION}`)
    await expect(page.getByTestId('chat-model-tools-warning')).toHaveText(WARNING)
    await expect(page.getByTestId('composer-model-tools-warning')).toHaveText(WARNING)
    await expect(page.getByTestId('tool-fail-chip')).toContainText('The "json" tool is not available.')
    await expect(page.locator('.md-content')).toHaveCount(0)
    await page.screenshot({ path: '/opt/cursor/artifacts/chat_tool_warning_and_fail_chip.png', fullPage: true })
  })
})

test.describe('Persisted space fail chips', () => {
  test.beforeEach(async ({ page }) => {
    await setupConnectedWS(page)
    await setupApiMocks(page)
    await mockWarningAgents(page)
    await page.route('**/api/v1/space-messages/**', route =>
      route.fulfill({
        json: {
          messages: [
            {
              id: 'sm-1',
              session_id: 'dm-steve',
              seq: 1,
              ts: '2026-08-26T00:00:00Z',
              role: 'assistant',
              content: 'TOOL_FAIL: The "json" tool is not available.',
              agent: 'Steve',
            },
            {
              id: 'sm-2',
              session_id: 'dm-steve',
              seq: 2,
              ts: '2026-08-26T00:00:01Z',
              role: 'assistant',
              content: 'DELEGATE_FAIL: Tess is not available.',
              agent: 'Steve',
            },
          ],
          next_cursor: '',
        },
      }),
    )
    await page.route('**/api/v1/spaces/dm-steve', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          json: {
            id: 'dm-steve',
            name: 'Steve',
            kind: 'dm',
            lead_agent: 'Steve',
            member_agents: ['Steve'],
            icon: 'S',
            color: '#d29922',
          },
        })
      }
      return route.continue()
    })
  })

  test('hydrated DM history chips TOOL_FAIL and DELEGATE_FAIL, not prose', async ({ page }) => {
    await page.goto('/#/space/dm-steve')
    const chips = page.getByTestId('tool-fail-chip')
    await expect(chips).toHaveCount(2)
    await expect(chips.nth(0)).toContainText('The "json" tool is not available.')
    await expect(chips.nth(1)).toContainText('Tess is not available.')
    await expect(page.locator('.md-content')).toHaveCount(0)
    await expect(page.getByText('TOOL_FAIL:', { exact: false })).toHaveCount(0)
    await expect(page.getByText('DELEGATE_FAIL:', { exact: false })).toHaveCount(0)
    await page.screenshot({ path: '/opt/cursor/artifacts/space_dm_fail_chips.png', fullPage: true })
  })
})
