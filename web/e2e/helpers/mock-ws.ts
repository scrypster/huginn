import type { Page } from '@playwright/test'

// The app connects to: ws(s)://<host>/ws?token=<token>
// useHuginnWS.ts sets connected.value = true in ws.onopen — NOT via an onmessage
// handshake. The Playwright routeWebSocket handler body runs at connection time
// (equivalent to onopen), so no explicit send() is needed to make wsConnected true.
const WS_PATTERN = '**/ws**'

/**
 * Sets up a WebSocket mock that accepts the connection.
 * Because useHuginnWS.ts sets connected = true in ws.onopen (not via any
 * message handshake), simply accepting the connection is sufficient.
 * Any messages the app sends are swallowed unless the test intercepts them.
 */
export async function setupConnectedWS(page: Page) {
  await page.routeWebSocket(WS_PATTERN, _ws => {
    // Connection accepted; ws.onopen fires in the browser automatically.
    // useHuginnWS.ts sets connected.value = true inside ws.onopen — no
    // server-side handshake message is required.
  })
}

/**
 * Sets up a WebSocket mock that closes the connection before the browser's
 * onopen fires. Because useHuginnWS.ts only sets connected.value = true inside
 * ws.onopen, the closure prevents that assignment — wsConnected stays false.
 * useHuginnWS schedules a reconnect (setTimeout 2 s) after each onclose, but
 * every reconnect attempt is intercepted by the same route and closed again,
 * so the app remains perpetually disconnected for the duration of the test.
 *
 * Useful for testing the "disconnected" / red-dot / banner state.
 */
export async function setupDisconnectedWS(page: Page) {
  await page.routeWebSocket(WS_PATTERN, ws => {
    // Closing here is equivalent to rejecting before onopen — wsConnected never
    // becomes true. Repeated reconnect attempts are intercepted the same way.
    ws.close()
  })
}

/**
 * Alias for setupDisconnectedWS. Blocks all WebSocket connections by closing
 * them before the browser's onopen event, keeping wsConnected = false.
 * Prefer setupDisconnectedWS in tests that assert a "disconnected" state;
 * use blockWS when the intent is to prevent any connection from opening.
 */
export async function blockWS(page: Page) {
  await page.routeWebSocket(WS_PATTERN, ws => {
    ws.close()
  })
}

/**
 * Accepts the WebSocket and returns a handle so the test can send arbitrary
 * server-push messages (e.g. streaming tokens, tool events).
 *
 * Usage:
 *   const server = await setupInteractiveWS(page)
 *   server.send(JSON.stringify({ type: 'token', content: 'hello' }))
 */
export async function setupInteractiveWS(
  page: Page,
): Promise<{ send: (data: string) => void }> {
  let serverHandle: { send: (data: string) => void } = {
    send: () => { /* no-op until connected */ },
  }

  await page.routeWebSocket(WS_PATTERN, ws => {
    // Mutate (not reassign) so the already-returned reference is updated.
    serverHandle.send = (data: string) => ws.send(data)
  })

  return serverHandle
}
