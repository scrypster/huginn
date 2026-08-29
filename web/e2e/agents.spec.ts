import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS, setupConnectedWS } from './helpers/mock-ws'

/**
 * Navigate to the Agents section.
 *
 * /agents is a top-level section (AgentsView.vue) — App.vue's context-panel
 * aside is chat-only (see utils/navLayout.ts showContextPanel) and never
 * renders for /agents, so there is no separate "sidebar" list to wait on.
 * AgentsView itself renders either the card grid (agents-grid) or the
 * empty state (agents-empty-state) once agents finish loading.
 */
async function gotoAgents(page: import('@playwright/test').Page) {
  await page.goto('/#/')
  await page.waitForSelector('nav', { timeout: 5000 })
  await page.click('button[title="Agents"]')
  await expect(page).toHaveURL(/#\/agents$/, { timeout: 5000 })
  await page.waitForSelector('[data-testid="agents-grid"], [data-testid="agents-empty-state"]', { timeout: 5000 })
}

/**
 * Open an agent's editor from the card grid. Cards open a DM on a plain
 * click — the per-card "Edit" button (data-testid="agent-card-edit") is
 * what navigates to /agents/:name and shows the editor.
 */
async function openAgentEditor(page: import('@playwright/test').Page, name: string) {
  await page.click(`[data-testid="agent-card"]:has-text("${name}") [data-testid="agent-card-edit"]`)
  await expect(page).toHaveURL(new RegExp(`#/agents/${name}$`), { timeout: 5000 })
}

test.describe('AgentsView — Toolbelt', () => {
  test.beforeEach(async ({ page }) => {
    await blockWS(page)
    await setupApiMocks(page)
  })

  test('displays agent grid on the Agents view', async ({ page }) => {
    await gotoAgents(page)

    const grid = page.locator('[data-testid="agents-grid"]')
    await expect(grid).toBeVisible()

    // Fixture has 2 agents: Coder and GitAgent
    const cards = page.locator('[data-testid="agent-card"]')
    await expect(cards).toHaveCount(2)
  })

  test('shows toolbelt entries for agent with connections', async ({ page }) => {
    await gotoAgents(page)
    await openAgentEditor(page, 'GitAgent')

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
    await openAgentEditor(page, 'Coder')

    const toolbeltSection = page.locator('[data-testid="toolbelt-section"]')
    await expect(toolbeltSection).toBeVisible()

    // Coder has no toolbelt entries
    const entries = page.locator('[data-testid="toolbelt-entry"]')
    await expect(entries).toHaveCount(0)
  })

  test('add toolbelt entry button is visible', async ({ page }) => {
    await gotoAgents(page)
    await openAgentEditor(page, 'Coder')

    const addBtn = page.locator('[data-testid="add-toolbelt-btn"]')
    await expect(addBtn).toBeVisible()
  })

  test('new agent button navigates to /agents/new and shows editor form', async ({ page }) => {
    await gotoAgents(page)

    // new-agent-btn is present in both the empty-state panel and the card grid
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
    await openAgentEditor(page, 'Coder')

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

  test('shows blank-canvas empty state with no phantom agents', async ({ page }) => {
    await page.goto('/#/')
    await page.waitForSelector('nav', { timeout: 5000 })
    await page.click('button[title="Agents"]')
    await page.waitForSelector('[data-testid="agents-empty-state"]', { timeout: 5000 })

    const empty = page.locator('[data-testid="agents-empty-state"]')
    await expect(empty).toContainText('No teammates yet')
    await expect(page.locator('[data-testid="agent-card"]')).toHaveCount(0)
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

    // Override models/available so the model picker has at least one model to select.
    await page.route('**/api/v1/models/available', route =>
      route.fulfill({ json: { models: [], provider_models: [{ name: 'claude-sonnet-4-6' }], builtin_models: [] } })
    )

    await page.route('**/api/v1/agents/FirstAgent', route => {
      if (route.request().method() === 'PUT') {
        saveRequestMade = true
        return route.fulfill({ json: { name: 'FirstAgent', model: 'claude-sonnet-4-6', icon: 'F', color: '#58a6ff', is_default: false, memory_enabled: false, vault_name: '', toolbelt: [] } })
      }
      return route.continue()
    })
    // After save, list returns the new agent.
    let agentCreated = false
    await page.route('**/api/v1/agents', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({ json: agentCreated ? [{ name: 'FirstAgent', model: 'claude-sonnet-4-6', icon: 'F', color: '#58a6ff', is_default: false, memory_enabled: false, vault_name: '', toolbelt: [] }] : [] })
      }
      return route.continue()
    })

    await page.goto('/#/agents/new')
    await page.waitForSelector('input[placeholder="Agent name"]', { timeout: 5000 })

    await page.fill('input[placeholder="Agent name"]', 'FirstAgent')

    // Model is now required — open the model picker and select a model.
    await page.click('text=No model selected')
    await page.click('text=claude-sonnet-4-6')

    agentCreated = true

    const saveBtn = page.locator('[data-testid="save-agent-btn-sticky"]')
    await expect(saveBtn).toBeVisible({ timeout: 3000 })
    await saveBtn.click()

    // Save sends PUT, then opens the new agent's DM (fallback: /agents/FirstAgent).
    expect(saveRequestMade).toBe(true)
    await expect(page).toHaveURL(/#\/(space\/.+|agents\/FirstAgent)/, { timeout: 8000 })
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

    // No agents configured in this mock — AgentsView renders the empty state.
    const emptyState = page.locator('[data-testid="agents-empty-state"]')
    await expect(emptyState).toBeVisible({ timeout: 8000 })

    // No auth error should surface to the user.
    await expect(page.locator('text=401')).toHaveCount(0)
    await expect(page.locator('text=Unauthorized')).toHaveCount(0)
  })
})
