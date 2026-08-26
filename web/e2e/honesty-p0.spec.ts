import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupConnectedWS } from './helpers/mock-ws'

const ARTIFACTS = '/opt/cursor/artifacts'

const liveConfig = {
  theme: 'dark',
  tools_enabled: false,
  allowed_tools: ['read_file', 'write_file', 'bash'],
  disallowed_tools: ['bash', 'web_search'],
  brave_api_key: '',
  web_ui: { enabled: true, port: 0, auto_open: false, bind: '127.0.0.1' },
  integrations: {},
  mcp_servers: [],
}

const failMessages = {
  messages: [
    {
      id: 'u1',
      session_id: 'sess-mention',
      seq: 1,
      ts: new Date().toISOString(),
      role: 'user',
      agent: '',
      content: '@Steve say PONG and nothing else',
    },
    {
      id: 'a1',
      session_id: 'sess-mention',
      seq: 2,
      ts: new Date().toISOString(),
      role: 'assistant',
      agent: 'Steve',
      content: 'TOOL_FAIL: The "json" tool is not available. Please use a different method to format the response.',
      toolCalls: [
        {
          id: 'tc-json',
          name: 'json',
          args: {},
          result: 'error: tool "json" is not available',
          done: true,
        },
      ],
    },
  ],
  next_cursor: '',
}

test.describe('P0 honesty', () => {
  test('Settings tools: serve copy + allow/deny conflict', async ({ page }) => {
    await setupApiMocks(page)
    await page.route('**/api/v1/config', route => {
      if (route.request().method() === 'GET') return route.fulfill({ json: liveConfig })
      return route.fulfill({ json: { saved: true, requires_restart: false } })
    })
    await page.route('**/api/v1/health', route =>
      route.fulfill({ json: { status: 'ok', version: 'vv0.4.0-try-all', stale: false } }),
    )
    await setupConnectedWS(page)

    await page.goto('/#/settings')
    await page.getByRole('button', { name: 'Tools', exact: true }).click()
    await expect(page.getByTestId('tools-enabled-serve-note')).toBeVisible()
    await expect(page.getByTestId('tools-enabled-serve-note')).toContainText('serve')
    await expect(page.getByTestId('tool-list-conflict')).toBeVisible()
    await expect(page.getByTestId('tool-list-conflict')).toContainText('bash')
    await expect(page.getByTestId('tool-list-conflict')).toContainText('Deny wins')

    await page.screenshot({ path: `${ARTIFACTS}/honesty_settings_tools_conflict.png`, fullPage: true })
  })

  test('Chat: fail line and chip are human; preview is Couldn\'t finish', async ({ page }) => {
    await setupApiMocks(page)
    await page.route('**/api/v1/space-messages/space-general**', route =>
      route.fulfill({ json: failMessages }),
    )
    await page.route('**/api/v1/space-sessions/space-general**', route =>
      route.fulfill({ json: [{ id: 'sess-mention', updated_at: new Date().toISOString() }] }),
    )
    await page.route('**/api/v1/memory/replication-status', route =>
      route.fulfill({ json: { pending: 0, failed: 0, dead: 0, connected: true } }),
    )
    await setupConnectedWS(page)

    await page.goto('/#/space/space-general')
    await expect(page.getByTestId('system-fail-line')).toBeVisible({ timeout: 8000 })
    await expect(page.getByTestId('system-fail-copy')).toHaveText("I couldn't run that.")
    await expect(page.getByTestId('system-fail-line')).not.toContainText('TOOL_FAIL')
    await expect(page.getByTestId('system-fail-line')).toHaveAttribute('title', /TOOL_FAIL/)
    await expect(page.getByRole('button', { name: "Couldn't run" })).toBeVisible()
    await expect(page.locator('text=· done')).toHaveCount(0)

    const preview = page.locator('[data-testid="channel-item-space-general"]')
    await expect(preview).toContainText("Couldn't finish")
    await expect(preview).not.toContainText('TOOL_FAIL')
    await expect(preview).not.toContainText('wait_for_threads')

    await page.getByRole('button', { name: 'Details' }).click()
    await expect(page.getByTestId('system-fail-details')).toContainText('TOOL_FAIL')
    await expect(page.getByTestId('system-fail-details')).toContainText('json')

    await page.screenshot({ path: `${ARTIFACTS}/honesty_toolfail_system_line.png`, fullPage: true })
  })

  test('Version badge collapses vv prefix; new agent is not unsaved', async ({ page }) => {
    await setupApiMocks(page)
    await page.route('**/api/v1/health', route =>
      route.fulfill({ json: { status: 'ok', version: 'vv0.4.0-try-all', stale: false } }),
    )
    await setupConnectedWS(page)

    await page.goto('/#/chat')
    await page.locator('[data-testid="ws-status-dot"]').click()
    const versionRow = page.getByTestId('popover-version-row')
    await expect(versionRow).toContainText('v0.4.0-try-all')
    await expect(versionRow).not.toContainText('vv0.4.0')
    await page.screenshot({ path: `${ARTIFACTS}/honesty_version_badge.png` })

    await page.goto('/#/stats')
    const statsVersion = page.getByTestId('stats-server-version')
    await expect(statsVersion).toHaveText('v0.4.0-try-all')
    await expect(statsVersion).not.toContainText('vv')
    await page.screenshot({ path: `${ARTIFACTS}/honesty_stats_server_version.png`, fullPage: true })

    await page.goto('/#/agents/new')
    await expect(page.getByText('Unsaved changes')).toHaveCount(0)
    await expect(page.getByTestId('delete-agent-btn')).toHaveCount(0)
    await page.screenshot({ path: `${ARTIFACTS}/honesty_new_agent_clean.png`, fullPage: true })
  })
})
