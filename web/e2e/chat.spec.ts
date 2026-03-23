import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'

/**
 * Set up a WebSocket mock that automatically responds to 'chat' messages.
 * When the browser sends { type: 'chat', ... }, the mock server replies with
 * a sequence of token messages followed by done.
 */
async function setupChatWS(
  page: import('@playwright/test').Page,
  tokens: string[] = ['Hello ', 'from AI!'],
) {
  await page.routeWebSocket('**/ws**', ws => {
    ws.onMessage(msg => {
      try {
        const data = JSON.parse(typeof msg === 'string' ? msg : msg.toString())
        if (data.type === 'chat') {
          for (const token of tokens) {
            ws.send(JSON.stringify({ type: 'token', content: token }))
          }
          ws.send(JSON.stringify({ type: 'done' }))
        }
      } catch { /* ignore non-JSON */ }
    })
  })
}

/**
 * Navigate to a chat session. App.vue provides the WS via provide/inject;
 * the WS connects on mount. Wait for the green status dot to confirm
 * wsConnected = true before interacting with the editor.
 */
async function gotoChatSession(page: import('@playwright/test').Page, sessionId = 'test-session-1') {
  await page.goto(`/#/chat/${sessionId}`)
  // Green dot = WS is connected = wsRef.value is set = sends will work
  await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5000 })
  await page.waitForSelector('.editor-content .ProseMirror', { timeout: 5000 })
}

test.describe('ChatView — streaming', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('editor renders and accepts text input', async ({ page }) => {
    // Simple connected WS — no auto-response needed for this test
    await page.routeWebSocket('**/ws**', _ws => { /* accept connection */ })
    await gotoChatSession(page)

    const editor = page.locator('.editor-content .ProseMirror')
    await expect(editor).toBeVisible()

    // ProseMirror is a contenteditable — click to focus, then type
    await editor.click()
    await page.keyboard.type('Hello world')
    await expect(editor).toContainText('Hello world')
  })

  test('send button is visible and renders on send', async ({ page }) => {
    await page.routeWebSocket('**/ws**', _ws => {})
    await gotoChatSession(page)

    const sendBtn = page.locator('button[title="Send (⏎)"]')
    await expect(sendBtn).toBeVisible()
  })

  test('sends message and user bubble appears immediately', async ({ page }) => {
    await setupChatWS(page)
    await gotoChatSession(page)

    await page.locator('.editor-content .ProseMirror').click()
    await page.keyboard.type('Hello world')
    // @mousedown.prevent on send button — Playwright click fires mousedown
    await page.locator('button[title="Send (⏎)"]').click()

    // User message bubble appears on the right
    await expect(page.locator('.md-content').first()).toContainText('Hello world', { timeout: 3000 })
  })

  test('assistant streaming response appears after send', async ({ page }) => {
    await setupChatWS(page, ['Hello ', 'from AI!'])
    await gotoChatSession(page)

    await page.locator('.editor-content .ProseMirror').click()
    await page.keyboard.type('Tell me something')
    await page.locator('button[title="Send (⏎)"]').click()

    // After streaming completes, assistant bubble contains the joined tokens
    const assistantBubble = page.locator('.md-content').nth(1)
    await expect(assistantBubble).toContainText('Hello from AI!', { timeout: 5000 })
  })

  test('editor clears after message is sent', async ({ page }) => {
    await setupChatWS(page)
    await gotoChatSession(page)

    const editor = page.locator('.editor-content .ProseMirror')
    await editor.click()
    await page.keyboard.type('A message')
    await page.locator('button[title="Send (⏎)"]').click()

    // After send, editor should be empty (ChatEditor resets on send)
    await expect(editor).not.toContainText('A message', { timeout: 3000 })
  })
})
