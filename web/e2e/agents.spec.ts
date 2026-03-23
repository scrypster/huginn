import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { blockWS } from './helpers/mock-ws'

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

  test('new agent button is present in empty state', async ({ page }) => {
    await gotoAgents(page)

    // With no agent selected the empty-state panel renders new-agent-btn
    const newBtn = page.locator('[data-testid="new-agent-btn"]')
    await expect(newBtn).toBeVisible()

    // Clicking should not throw or navigate away — agent list stays visible
    await newBtn.click()
    await expect(page.locator('[data-testid="agent-list"]')).toBeVisible()
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
