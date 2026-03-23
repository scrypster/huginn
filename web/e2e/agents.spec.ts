import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS, setupConnectedWS } from './helpers/mock-ws'

/**
 * Navigate to the agents section.
 *
 * App.vue uses watch(activeSection) — the watcher only fires on change, not on
 * initial mount. Navigating directly to /#/agents means activeSection starts as
 * 'agents' with no prior value, so loadAgents() is never called. Work around
 * this by first landing on a non-agents route, then clicking the Agents nav
 * button so the watcher detects a transition and calls loadAgents().
 */
async function gotoAgents(page: import('@playwright/test').Page) {
  // Start somewhere other than agents so activeSection begins as 'chat'
  await page.goto('/#/')
  // Wait for app to initialize (token fetch completes)
  await page.waitForSelector('nav', { timeout: 5000 })
  // Now click the Agents nav button — this changes activeSection and fires the watcher
  await page.click('button:has-text("Agents")')
  // Wait for at least the agent-list container to appear
  await page.waitForSelector('[data-testid="agent-list"]', { timeout: 5000 })
}

test.describe('AgentsView — Toolbelt', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('displays agent list in sidebar', async ({ page }) => {
    await gotoAgents(page)

    const agentList = page.locator('[data-testid="agent-list"]')
    await expect(agentList).toBeVisible()

    // Fixture has 2 agents: Coder and GitAgent
    const items = page.locator('[data-testid="agent-item"]')
    await expect(items).toHaveCount(2)
  })

  test('shows toolbelt entries for agent with connections', async ({ page }) => {
    await gotoAgents(page)
    // Click on GitAgent in the sidebar
    await page.click('[data-testid="agent-item"]:has-text("GitAgent")')

    const toolbeltSection = page.locator('[data-testid="toolbelt-section"]')
    await expect(toolbeltSection).toBeVisible()

    // GitAgent fixture has 1 toolbelt entry (github_cli)
    const entries = page.locator('[data-testid="toolbelt-entry"]')
    await expect(entries).toHaveCount(1)

    // Badge shows the connection account label (e.g. "test-user" from fixture)
    const badge = page.locator('[data-testid="toolbelt-provider-badge"]').first()
    await expect(badge).toBeVisible()
    await expect(badge).not.toBeEmpty()
  })

  test('shows empty toolbelt for agent with no connections', async ({ page }) => {
    await gotoAgents(page)
    // Click on Coder in the sidebar
    await page.click('[data-testid="agent-item"]:has-text("Coder")')

    const toolbeltSection = page.locator('[data-testid="toolbelt-section"]')
    await expect(toolbeltSection).toBeVisible()

    // Coder has no toolbelt entries
    const entries = page.locator('[data-testid="toolbelt-entry"]')
    await expect(entries).toHaveCount(0)
  })

  test('add toolbelt entry button is visible', async ({ page }) => {
    await gotoAgents(page)
    await page.click('[data-testid="agent-item"]:has-text("Coder")')

    // The add-toolbelt-btn container renders when addableConnections.length > 0.
    // Coder has an empty toolbelt so conn-gh-1 (from connectionsFixture) is addable.
    const addBtn = page.locator('[data-testid="add-toolbelt-btn"]')
    await expect(addBtn).toBeVisible()
  })

  test('new agent button navigates to /agents/new and shows editor form', async ({ page }) => {
    await gotoAgents(page)

    // With no agent selected the empty-state panel renders new-agent-btn
    const newBtn = page.locator('[data-testid="new-agent-btn"]')
    await expect(newBtn).toBeVisible()

    await newBtn.click()

    // Must navigate to /agents/new — not stay on /agents
    await expect(page).toHaveURL(/#\/agents\/new/, { timeout: 3000 })

    // The agent name input must be visible (editor opened)
    const nameInput = page.locator('input[placeholder="Agent name"]')
    await expect(nameInput).toBeVisible({ timeout: 3000 })
  })

  test('save button is present on agent editor', async ({ page }) => {
    await gotoAgents(page)
    await page.click('[data-testid="agent-item"]:has-text("Coder")')

    // The save button only renders when dirty=true. Trigger dirty by typing in the name field.
    // The agent name input has placeholder="Agent name".
    const nameInput = page.locator('input[placeholder="Agent name"]')
    await expect(nameInput).toBeVisible({ timeout: 5000 })
    await nameInput.click()
    await nameInput.pressSequentially('x')

    const saveBtn = page.locator('[data-testid="save-agent-btn-sticky"]')
    await expect(saveBtn).toBeVisible()
  })
})

// ── Fresh-install: no agents configured ──────────────────────────────────────

test.describe('AgentsView — fresh install (no agents)', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)

    // Override agents list to empty — simulates brand-new install.
    await page.route('**/api/v1/agents', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: [] })
      return route.continue()
    })
    await page.route('**/api/v1/agents/active', route => {
      if (route.request().method() === 'GET') return route.fulfill({ status: 404, json: { error: 'no active agent' } })
      return route.continue()
    })
  })

  test('sidebar shows blank-canvas empty state with no phantom agents', async ({ page }) => {
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })
    await page.click('button:has-text("Agents")')
    await page.waitForSelector('[data-testid="agent-list"]', { timeout: 5000 })

    const list = page.locator('[data-testid="agent-list"]')
    await expect(list).toContainText('No agents configured')
    await expect(list.locator('[data-testid="agent-item"]')).toHaveCount(0)
  })

  test('New agent button navigates to editor on fresh install', async ({ page }) => {
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })
    await page.click('button:has-text("Agents")')
    await page.waitForSelector('[data-testid="new-agent-btn"]', { timeout: 5000 })

    await page.click('[data-testid="new-agent-btn"]')

    await expect(page).toHaveURL(/#\/agents\/new/, { timeout: 3000 })
    await expect(page.locator('input[placeholder="Agent name"]')).toBeVisible({ timeout: 3000 })
  })

  test('can create and save first agent on fresh install', async ({ page }) => {
    let saveRequestMade = false

    await page.route('**/api/v1/agents/FirstAgent', route => {
      if (route.request().method() === 'PUT') {
        saveRequestMade = true
        return route.fulfill({ json: { name: 'FirstAgent', model: '', icon: 'F', color: '#58a6ff', is_default: false, memory_enabled: false, vault_name: '', toolbelt: [] } })
      }
      return route.continue()
    })
    // After save, list returns the new agent.
    let agentCreated = false
    await page.route('**/api/v1/agents', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({ json: agentCreated ? [{ name: 'FirstAgent', model: '', icon: 'F', color: '#58a6ff', is_default: false, memory_enabled: false, vault_name: '', toolbelt: [] }] : [] })
      }
      return route.continue()
    })

    await page.goto('/#/agents/new')
    await page.waitForSelector('input[placeholder="Agent name"]', { timeout: 5000 })

    await page.fill('input[placeholder="Agent name"]', 'FirstAgent')
    agentCreated = true

    const saveBtn = page.locator('[data-testid="save-agent-btn-sticky"]')
    await expect(saveBtn).toBeVisible({ timeout: 3000 })
    await saveBtn.click()

    expect(saveRequestMade).toBe(true)
    await expect(page).toHaveURL(/#\/agents\/FirstAgent/, { timeout: 3000 })
  })
})

// ── Token initialization race guard ──────────────────────────────────────────

test.describe('AgentsView — token auto-init race', () => {
  test('page loads without 401 errors when token endpoint is slow', async ({ page }) => {
    // Simulate the Vue 3 race: child onMounted fires before parent initApp() completes.
    // The token endpoint takes 400ms to respond — longer than typical component mount.
    let firstCall = true
    await page.route('**/api/v1/token', async route => {
      if (firstCall) {
        firstCall = false
        await new Promise(r => setTimeout(r, 400))
      }
      return route.fulfill({ json: { token: 'slow-token' } })
    })

    await page.route('**/api/v1/**', route => route.fulfill({ status: 200, json: {} }))
    await page.route('**/api/v1/agents', route => route.fulfill({ json: [] }))
    await page.route('**/api/v1/agents/active', route => route.fulfill({ status: 404, json: {} }))
    await blockWS(page)

    await page.goto('/#/agents')

    const agentList = page.locator('[data-testid="agent-list"]')
    await expect(agentList).toBeVisible({ timeout: 8000 })

    // No auth error should surface to the user.
    await expect(page.locator('text=401')).toHaveCount(0)
    await expect(page.locator('text=Unauthorized')).toHaveCount(0)
  })
})
