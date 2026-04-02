import { test, expect, type Page, type WebSocketRoute } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'

const SESSION = 'test-chat-session'

// ── Helpers ────────────────────────────────────────────────────────────────────

/**
 * Mock the session-specific endpoints needed for a DM chat:
 * - GET /api/v1/sessions/{id} — session details
 * - GET /api/v1/sessions/{id}/messages — message history (empty by default)
 *
 * Registered AFTER setupApiMocks so they win via LIFO priority.
 */
async function mockSessionEndpoints(
  page: Page,
  messages: unknown[] = [],
) {
  await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
    route.fulfill({ json: messages }),
  )
  await page.route(`**/api/v1/sessions/${SESSION}`, route => {
    if (route.request().method() === 'GET') {
      return route.fulfill({
        json: { session_id: SESSION, agent: 'Coder', status: 'active' },
      })
    }
    return route.fulfill({ json: {} })
  })
}

/**
 * Sets up a WebSocket that intercepts client messages and exposes a `send()`
 * handle. Returns the server handle AND a promise-based helper to capture the
 * client's run_id from outgoing 'chat' messages.
 */
function createInteractiveWS() {
  let serverSend: (data: string) => void = () => {}
  let runIdResolve: ((id: string) => void) | null = null
  let capturedRunId = ''

  const handler = (ws: WebSocketRoute) => {
    serverSend = (data: string) => ws.send(data)

    ws.onMessage(msg => {
      try {
        const data = JSON.parse(typeof msg === 'string' ? msg : msg.toString())
        if (data.type === 'chat' && data.run_id) {
          capturedRunId = data.run_id
          if (runIdResolve) {
            runIdResolve(data.run_id)
            runIdResolve = null
          }
        }
      } catch { /* ignore non-JSON */ }
    })
  }

  return {
    handler,
    send: (data: string) => serverSend(data),
    get capturedRunId() { return capturedRunId },
    /** Wait for the next chat message from the client and return its run_id. */
    waitForRunId(): Promise<string> {
      if (capturedRunId) return Promise.resolve(capturedRunId)
      return new Promise(resolve => { runIdResolve = resolve })
    },
  }
}

/**
 * Navigate to the chat session and wait for WS connection + editor ready.
 */
async function gotoChatSession(page: Page, sessionId = SESSION) {
  await page.goto(`/#/chat/${sessionId}`)
  await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5000 })
  await page.waitForSelector('.editor-content .ProseMirror', { timeout: 5000 })
}

/**
 * Type a message into the ProseMirror editor and click Send.
 */
async function typeAndSend(page: Page, text: string) {
  const editor = page.locator('.editor-content .ProseMirror')
  await editor.click()
  await page.keyboard.type(text)
  await page.locator('button[title="Send (⏎)"]').click()
}

// ── Test group: Chat — basic text response ─────────────────────────────────────

test.describe('Chat — basic text response', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await mockSessionEndpoints(page)
  })

  test('sends a message and renders streamed text tokens', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'Hello world')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Hello ', run_id: runId }))
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'from AI!', run_id: runId }))
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    // The assistant message bubble should contain the streamed text
    const assistantBubbles = page.locator('[class*="role"] .md-content, .md-content')
    // Find the assistant bubble (second .md-content — first is the user message)
    await expect(page.locator('.md-content').nth(1)).toContainText('Hello from AI!', { timeout: 5000 })
  })

  test('shows streaming indicator while tokens arrive, hides it when done', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'test cursor')

    const runId = await ws.waitForRunId()
    // Send one token — streaming indicator should be visible during streaming
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Partial ', run_id: runId }))

    // The streaming indicator now uses data-testid="streaming-thinking" with bouncing dots
    const indicator = page.locator('[data-testid="streaming-thinking"]')
    await expect(indicator).toBeVisible({ timeout: 3000 })

    // Send done — indicator should disappear
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))
    await expect(indicator).not.toBeVisible({ timeout: 3000 })
  })

  test('persists and reloads text content from history', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'hello')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Persisted response', run_id: runId }))
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    await expect(page.locator('.md-content').nth(1)).toContainText('Persisted response', { timeout: 5000 })

    // Now mock the messages API to return this message in history, and reload
    await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
      route.fulfill({
        json: [
          { id: 'msg-1', role: 'user', content: 'hello', ts: '2026-03-15T10:00:00Z' },
          { id: 'msg-2', role: 'assistant', content: 'Persisted response', agent: 'Coder', ts: '2026-03-15T10:00:01Z' },
        ],
      }),
    )

    await page.reload()
    await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5000 })
    await page.waitForSelector('.editor-content .ProseMirror', { timeout: 5000 })

    // Verify content is visible after reload
    await expect(page.locator('.md-content').filter({ hasText: 'Persisted response' })).toBeVisible({ timeout: 5000 })
  })
})

// ── Test group: Chat — tool calls ──────────────────────────────────────────────

test.describe('Chat — tool calls', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await mockSessionEndpoints(page)
  })

  test('shows running chip inside message bubble while tool is executing', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'run a tool')

    const runId = await ws.waitForRunId()

    // Send a token so assistant message exists
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Let me run that...', run_id: runId }))

    // Send tool_call event
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' } },
      run_id: runId,
    }))

    // "running" chip should appear inside the message
    const runningChip = page.locator('text=· running')
    await expect(runningChip).toBeVisible({ timeout: 3000 })

    // Verify the chip text includes the tool call count
    await expect(page.locator('text=1 tool call')).toBeVisible({ timeout: 3000 })
  })

  test('running chip disappears and done chip appears after tool_result', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'run a tool')

    const runId = await ws.waitForRunId()

    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Running tool...', run_id: runId }))
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' } },
      run_id: runId,
    }))

    await expect(page.locator('text=· running')).toBeVisible({ timeout: 3000 })

    // Send tool_result
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' }, result: 'file.txt' },
      run_id: runId,
    }))

    // "running" chip should disappear, "done" chip should appear
    await expect(page.locator('text=· running')).not.toBeVisible({ timeout: 3000 })
    await expect(page.locator('text=· done')).toBeVisible({ timeout: 3000 })
    await expect(page.locator('text=1 tool call')).toBeVisible({ timeout: 3000 })
  })

  test('text after tool call renders below the done chip', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'run and explain')

    const runId = await ws.waitForRunId()

    // Token before tool
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Running...', run_id: runId }))

    // Tool call + result
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' } },
      run_id: runId,
    }))
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' }, result: 'output' },
      run_id: runId,
    }))

    // More text after tool completes
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: ' Here is the result.', run_id: runId }))
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    // Both the tool chip and post-tool text should be visible
    await expect(page.locator('text=· done')).toBeVisible({ timeout: 3000 })
    await expect(page.locator('.md-content').filter({ hasText: 'Here is the result' })).toBeVisible({ timeout: 5000 })
  })

  test('done chip persists after streaming ends', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'tool test')

    const runId = await ws.waitForRunId()

    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Using tool...', run_id: runId }))
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: {} },
      run_id: runId,
    }))
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: {}, result: 'ok' },
      run_id: runId,
    }))
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    // After done, the "done" chip must persist
    await expect(page.locator('text=· done')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('text=1 tool call')).toBeVisible({ timeout: 5000 })
  })

  test('multiple sequential tool calls show correct count', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'run two tools')

    const runId = await ws.waitForRunId()

    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Working...', run_id: runId }))

    // First tool call + result
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' } },
      run_id: runId,
    }))
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' }, result: 'file1' },
      run_id: runId,
    }))

    // Second tool call + result
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-2', tool: 'read_file', args: { path: 'file1' } },
      run_id: runId,
    }))
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-2', tool: 'read_file', args: { path: 'file1' }, result: 'content' },
      run_id: runId,
    }))

    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    // Chip should say "2 tool calls"
    await expect(page.locator('text=2 tool calls')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('text=· done')).toBeVisible({ timeout: 5000 })
  })
})

// ── Test group: Chat — agent icon ──────────────────────────────────────────────

test.describe('Chat — agent icon', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await mockSessionEndpoints(page)
  })

  test('assistant message shows correct agent icon letter', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'hi')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Hello!', run_id: runId }))
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))

    await expect(page.locator('.md-content').nth(1)).toContainText('Hello!', { timeout: 5000 })

    // The Coder agent has icon 'C'. Look for the 7x7 avatar with letter 'C'.
    // The avatar is a w-7 h-7 rounded-lg div containing a span with the letter.
    const avatar = page.locator('.w-7.h-7.rounded-lg span.font-bold')
    await expect(avatar.first()).toHaveText('C', { timeout: 3000 })
  })
})

// ── Test group: Chat — history reload ──────────────────────────────────────────

test.describe('Chat — history reload', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('tool_calls from history are visible after page reload', async ({ page }) => {
    // Mock messages endpoint with tool_calls in the response
    await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
      route.fulfill({
        json: [
          { id: 'msg-1', role: 'user', content: 'list files', ts: '2026-03-15T10:00:00Z' },
          {
            id: 'msg-2',
            role: 'assistant',
            content: 'Here are the files:',
            agent: 'Coder',
            ts: '2026-03-15T10:00:01Z',
            tool_calls: [
              { id: 'tc-1', name: 'bash', args: { command: 'ls' }, result: 'file.txt' },
              { id: 'tc-2', name: 'read_file', args: { path: 'file.txt' }, result: 'hello' },
            ],
          },
        ],
      }),
    )
    await page.route(`**/api/v1/sessions/${SESSION}`, route =>
      route.fulfill({ json: { session_id: SESSION, agent: 'Coder', status: 'active' } }),
    )

    await page.routeWebSocket('**/ws**', _ws => {})
    await gotoChatSession(page)

    // The done chip should render from persisted tool_calls
    await expect(page.locator('text=2 tool calls')).toBeVisible({ timeout: 5000 })
    await expect(page.locator('text=· done')).toBeVisible({ timeout: 5000 })
  })

  test('text content from history renders on reload', async ({ page }) => {
    await page.route(`**/api/v1/sessions/${SESSION}/messages*`, route =>
      route.fulfill({
        json: [
          { id: 'msg-1', role: 'user', content: 'what is 2+2?', ts: '2026-03-15T10:00:00Z' },
          { id: 'msg-2', role: 'assistant', content: 'The answer is 4.', agent: 'Coder', ts: '2026-03-15T10:00:01Z' },
        ],
      }),
    )
    await page.route(`**/api/v1/sessions/${SESSION}`, route =>
      route.fulfill({ json: { session_id: SESSION, agent: 'Coder', status: 'active' } }),
    )

    await page.routeWebSocket('**/ws**', _ws => {})
    await gotoChatSession(page)

    // Both user and assistant messages should render
    await expect(page.locator('.md-content').filter({ hasText: 'what is 2+2?' })).toBeVisible({ timeout: 5000 })
    await expect(page.locator('.md-content').filter({ hasText: 'The answer is 4.' })).toBeVisible({ timeout: 5000 })
  })
})

// ── Test group: Chat — error handling ──────────────────────────────────────────

test.describe('Chat — error handling', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await mockSessionEndpoints(page)
  })

  test('error WS message shows error text in assistant bubble', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'cause an error')

    const runId = await ws.waitForRunId()

    // Send a token first so the assistant message exists
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: '', run_id: runId }))

    // Send error
    ws.send(JSON.stringify({
      type: 'error',
      session_id: SESSION,
      content: 'something went wrong',
      run_id: runId,
    }))

    // Error text should be visible in the assistant bubble
    await expect(page.locator('.md-content').filter({ hasText: 'something went wrong' })).toBeVisible({ timeout: 5000 })
  })
})

// ── Test group: Chat — streaming indicator ──────────────────────────────────────

test.describe('Chat — streaming indicator', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
    await mockSessionEndpoints(page)
  })

  test('streaming indicator visible between tool result and next token', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'run and continue')

    const runId = await ws.waitForRunId()

    // Send text token, tool call, tool result — then pause (no done yet)
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Checking...', run_id: runId }))
    ws.send(JSON.stringify({
      type: 'tool_call',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' } },
      run_id: runId,
    }))
    ws.send(JSON.stringify({
      type: 'tool_result',
      session_id: SESSION,
      payload: { id: 'tc-1', tool: 'bash', args: { command: 'ls' }, result: 'files' },
      run_id: runId,
    }))

    // After tool_result and before next token, the streaming-thinking indicator should be visible
    const indicator = page.locator('[data-testid="streaming-thinking"]')
    await expect(indicator).toBeVisible({ timeout: 5000 })
  })

  test('streaming indicator gone after done', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'quick response')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Done!', run_id: runId }))

    // Indicator should be visible while streaming
    const indicator = page.locator('[data-testid="streaming-thinking"]')
    await expect(indicator).toBeVisible({ timeout: 3000 })

    // Send done — indicator should disappear
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))
    await expect(indicator).not.toBeVisible({ timeout: 3000 })
  })

  test('streaming banner visible while streaming, hidden after done', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'test banner')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Working...', run_id: runId }))

    // Banner should be visible while streaming
    const banner = page.locator('[data-testid="streaming-banner"]')
    await expect(banner).toBeVisible({ timeout: 3000 })
    await expect(banner).toContainText('responding', { timeout: 3000 })

    // Send done — banner should disappear
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))
    await expect(banner).not.toBeVisible({ timeout: 3000 })
  })

  test('send button disabled while streaming', async ({ page }) => {
    const ws = createInteractiveWS()
    await page.routeWebSocket('**/ws**', ws.handler)
    await gotoChatSession(page)

    await typeAndSend(page, 'test disabled')

    const runId = await ws.waitForRunId()
    ws.send(JSON.stringify({ type: 'token', session_id: SESSION, content: 'Streaming...', run_id: runId }))

    // The ProseMirror editor should be non-editable while streaming
    // (ChatEditor :disabled="streaming" sets editable:false on the editor)
    const editor = page.locator('.editor-content .ProseMirror')
    await expect(editor).toHaveAttribute('contenteditable', 'false', { timeout: 3000 })

    // Send done — editor should become editable again
    ws.send(JSON.stringify({ type: 'done', session_id: SESSION, run_id: runId }))
    await expect(editor).toHaveAttribute('contenteditable', 'true', { timeout: 3000 })
  })
})
