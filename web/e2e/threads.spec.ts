import { test, expect } from '@playwright/test'
import { setupApiMocks } from './helpers/mock-api'
import { setupInteractiveWS } from './helpers/mock-ws'

// ── helpers ─────────────────────────────────────────────────────────────────

const SESSION = 'test-session-1'

async function gotoChatSession(page: import('@playwright/test').Page) {
  await page.goto(`/#/chat/${SESSION}`)
  await expect(page.locator('[data-testid="ws-status-dot"]')).toHaveClass(/bg-huginn-green/, { timeout: 5_000 })
}

// Build a WSMessage that matches the backend format:
//   { type, session_id, payload: { ... } }
// session_id is top-level, not inside payload.
function wsMsg(type: string, payload: Record<string, unknown> = {}) {
  return JSON.stringify({ type, session_id: SESSION, payload })
}

// ── Delegation preview banner ────────────────────────────────────────────────

test.describe('Delegation preview', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('banner appears when delegation_preview WS message received', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('delegation_preview', {
      thread_id: 'thread-dp-1',
      agent_id:  'Dave',
      task:      'Write the unit tests',
    }))

    await expect(page.locator('[data-testid="delegation-preview-list"]')).toBeVisible()
    await expect(page.locator('[data-testid="delegation-preview-agent"]')).toContainText('Dave')
    await expect(page.locator('[data-testid="delegation-preview-task"]')).toContainText('Write the unit tests')
  })

  test('Allow button sends delegation_preview_ack with approved=true and removes banner', async ({ page }) => {
    const outbound: unknown[] = []

    await page.routeWebSocket('**/ws**', ws => {
      ws.onMessage(raw => {
        try { outbound.push(JSON.parse(typeof raw === 'string' ? raw : raw.toString())) } catch { /* ignore */ }
      })
      ws.send(wsMsg('delegation_preview', {
        thread_id: 'thread-allow-1',
        agent_id:  'Sam',
        task:      'Build API',
      }))
    })

    await gotoChatSession(page)
    await expect(page.locator('[data-testid="delegation-preview-allow"]')).toBeVisible()

    await page.locator('[data-testid="delegation-preview-allow"]').click()

    // Banner disappears
    await expect(page.locator('[data-testid="delegation-preview-list"]')).not.toBeVisible()

    // Outbound includes delegation_preview_ack with approved=true
    await expect.poll(() => outbound, { timeout: 2_000 }).toContainEqual(
      expect.objectContaining({ type: 'delegation_preview_ack', payload: expect.objectContaining({ approved: true }) })
    )
  })

  test('Deny button sends delegation_preview_ack with approved=false and removes banner', async ({ page }) => {
    const outbound: unknown[] = []

    await page.routeWebSocket('**/ws**', ws => {
      ws.onMessage(raw => {
        try { outbound.push(JSON.parse(typeof raw === 'string' ? raw : raw.toString())) } catch { /* ignore */ }
      })
      ws.send(wsMsg('delegation_preview', {
        thread_id: 'thread-deny-1',
        agent_id:  'Tom',
        task:      'Write docs',
      }))
    })

    await gotoChatSession(page)
    await expect(page.locator('[data-testid="delegation-preview-deny"]')).toBeVisible()

    await page.locator('[data-testid="delegation-preview-deny"]').click()

    await expect(page.locator('[data-testid="delegation-preview-list"]')).not.toBeVisible()

    await expect.poll(() => outbound, { timeout: 2_000 }).toContainEqual(
      expect.objectContaining({ type: 'delegation_preview_ack', payload: expect.objectContaining({ approved: false }) })
    )
  })

  test('multiple previews shown simultaneously', async ({ page }) => {
    await page.routeWebSocket('**/ws**', ws => {
      ws.send(wsMsg('delegation_preview', { thread_id: 'tp-1', agent_id: 'Alice', task: 'Task A' }))
      ws.send(wsMsg('delegation_preview', { thread_id: 'tp-2', agent_id: 'Charlie', task: 'Task B' }))
    })

    await gotoChatSession(page)

    await expect(page.locator('[data-testid^="delegation-preview-tp-"]')).toHaveCount(2)
    await expect(page.locator('[data-testid="delegation-preview-agent"]').first()).toContainText('Alice')
    await expect(page.locator('[data-testid="delegation-preview-agent"]').last()).toContainText('Charlie')
  })

  test('duplicate delegation_preview for same thread_id is not shown twice', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('delegation_preview', { thread_id: 'tp-dup', agent_id: 'Dave', task: 'Task X' }))
    await expect(page.locator('[data-testid="delegation-preview-list"]')).toBeVisible()

    // Send a duplicate
    server.send(wsMsg('delegation_preview', { thread_id: 'tp-dup', agent_id: 'Dave', task: 'Task X' }))
    await page.waitForTimeout(200) // small wait for second message to process

    await expect(page.locator('[data-testid^="delegation-preview-tp-dup"]')).toHaveCount(1)
  })
})

// ── thread_started / thread panel ───────────────────────────────────────────

test.describe('Thread lifecycle WS events', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('thread_started opens the thread panel', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('thread_started', {
      thread_id:         'th-lifecycle-1',
      agent_id:          'Dave',
      task:              'Refactor auth',
      parent_message_id: 'msg-abc-123',
    }))

    // Active thread count > 0 → panel auto-opens; thread card shows task text
    await expect(page.getByText('Refactor auth').first()).toBeVisible({ timeout: 4_000 })
  })

  test('thread_started without parent_message_id does not crash', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('thread_started', {
      thread_id: 'th-no-parent',
      agent_id:  'Sam',
      task:      'Clean up logs',
    }))

    // The page should remain functional
    await expect(page.locator('.editor-content .ProseMirror')).toBeVisible()
    // And the thread should be registered (panel or thread count indicator visible)
    await expect(page.locator('text=Clean up logs').first()).toBeVisible({ timeout: 4_000 })
  })

  test('thread_done after thread_started updates status', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('thread_started', { thread_id: 'th-done-1', agent_id: 'Dave', task: 'Fix bug' }))
    await page.waitForTimeout(100)
    server.send(wsMsg('thread_done', { thread_id: 'th-done-1', summary: 'Fixed', status: 'completed' }))

    // Page should remain functional after done event
    await expect(page.locator('.editor-content .ProseMirror')).toBeVisible()
  })
})

// ── thread_inject responses ───────────────────────────────────────────────────

test.describe('Thread inject WS responses', () => {
  test.beforeEach(async ({ page }) => {
    await setupApiMocks(page)
  })

  test('thread_inject_ack does not crash the UI', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('thread_inject_ack', { thread_id: 'th-ack-1' }))

    // Page remains functional
    await expect(page.locator('.editor-content .ProseMirror')).toBeVisible()
  })

  test('thread_inject_error does not crash the UI', async ({ page }) => {
    const server = await setupInteractiveWS(page)
    await gotoChatSession(page)

    server.send(wsMsg('thread_inject_error', { thread_id: 'th-err-1', reason: 'buffer_full' }))

    // Page remains functional
    await expect(page.locator('.editor-content .ProseMirror')).toBeVisible()
  })
})
