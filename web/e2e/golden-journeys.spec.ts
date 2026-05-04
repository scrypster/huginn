import { test, expect, type Page, type WebSocketRoute } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'

const DM_SESSION = 'golden-dm-session'
const CHANNEL_SESSION = 'golden-channel-session'

async function mockSession(page: Page, sessionID: string, options?: { spaceID?: string }) {
  await page.route(`**/api/v1/sessions/${sessionID}/messages*`, route =>
    route.fulfill({ json: [] }),
  )
  await page.route(`**/api/v1/sessions/${sessionID}`, route => {
    if (route.request().method() !== 'GET') return route.fulfill({ json: {} })
    return route.fulfill({
      json: {
        session_id: sessionID,
        agent: 'Coder',
        status: 'active',
        ...(options?.spaceID ? { space_id: options.spaceID } : {}),
      },
    })
  })
}

function createInteractiveWS() {
  let sendToClient: (data: string) => void = () => {}
  let runID = ''
  let runIDResolver: ((id: string) => void) | null = null

  const handler = (ws: WebSocketRoute) => {
    sendToClient = (data: string) => ws.send(data)
    ws.onMessage(raw => {
      try {
        const data = JSON.parse(typeof raw === 'string' ? raw : raw.toString())
        if (data.type === 'chat' && data.run_id) {
          runID = data.run_id
          if (runIDResolver) {
            runIDResolver(runID)
            runIDResolver = null
          }
        }
      } catch {
        // ignore malformed events from test harness
      }
    })
  }

  return {
    handler,
    send: (data: string) => sendToClient(data),
    waitForRunID: async () => {
      if (runID) return runID
      return new Promise<string>(resolve => { runIDResolver = resolve })
    },
  }
}

function wsPayload(type: string, sessionID: string, payload: Record<string, unknown>) {
  return JSON.stringify({ type, session_id: sessionID, payload })
}

async function gotoChat(page: Page, sessionID: string) {
  await page.goto(`/#/chat/${sessionID}`)
  await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5_000 })
  await page.waitForSelector('.editor-content .ProseMirror', { timeout: 5_000 })
}

async function sendMessage(page: Page, text: string) {
  const editor = page.locator('.editor-content .ProseMirror')
  await editor.click()
  await page.keyboard.type(text)
  await page.locator('button[title="Send (⏎)"]').click()
}

test.describe('Golden no-LLM journeys', () => {
  test('DM chat streams and completes naturally', async ({ page }) => {
    await setupApiMocks(page)
    await mockSession(page, DM_SESSION)
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)

    await gotoChat(page, DM_SESSION)
    await sendMessage(page, 'Can you summarize this plan?')

    const runID = await ws.waitForRunID()
    ws.send(JSON.stringify({ type: 'token', session_id: DM_SESSION, content: 'Absolutely — ', run_id: runID }))
    ws.send(JSON.stringify({ type: 'token', session_id: DM_SESSION, content: 'here is a concise summary.', run_id: runID }))
    ws.send(JSON.stringify({ type: 'done', session_id: DM_SESSION, run_id: runID }))

    await expect(page.locator('.md-content').nth(1)).toContainText('Absolutely — here is a concise summary.', { timeout: 5_000 })
    await expect(page.locator('[data-testid="streaming-banner"]')).not.toBeVisible()
  })

  test('channel lead delegates and thread lifecycle stays visible', async ({ page }) => {
    await setupApiMocks(page)
    await mockSession(page, CHANNEL_SESSION, { spaceID: 'space-general' })
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)

    await gotoChat(page, CHANNEL_SESSION)
    await sendMessage(page, 'Investigate this issue with the team.')

    const runID = await ws.waitForRunID()
    ws.send(JSON.stringify({
      type: 'token',
      session_id: CHANNEL_SESSION,
      content: 'I will delegate this to @GitAgent now.',
      run_id: runID,
    }))
    ws.send(wsPayload('delegation_preview', CHANNEL_SESSION, {
      thread_id: 'golden-thread-1',
      agent_id: 'GitAgent',
      task: 'Investigate root cause and propose fix',
    }))
    ws.send(wsPayload('thread_started', CHANNEL_SESSION, {
      thread_id: 'golden-thread-1',
      agent_id: 'GitAgent',
      task: 'Investigate root cause and propose fix',
      parent_message_id: 'msg-golden-parent',
    }))
    ws.send(wsPayload('thread_done', CHANNEL_SESSION, {
      thread_id: 'golden-thread-1',
      summary: 'Root cause identified and fix validated.',
      status: 'completed',
    }))
    ws.send(JSON.stringify({ type: 'done', session_id: CHANNEL_SESSION, run_id: runID }))

    await expect(page.getByText('Investigate root cause and propose fix').first()).toBeVisible({ timeout: 5_000 })
    await expect(page.locator('.md-content').nth(1)).toContainText('delegate this to @GitAgent', { timeout: 5_000 })
    await expect(page.locator('.editor-content .ProseMirror')).toBeVisible()
  })
})
