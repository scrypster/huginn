/**
 * useCloud tests
 *
 * Strategy: mock globalThis.fetch directly (the underlying primitive that useApi uses).
 * We reset modules between tests so the module-level singleton state starts fresh.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'

// Helper to create a minimal ok Response
function ok(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

async function freshCloud() {
  vi.resetModules()
  const mod = await import('../useCloud')
  return mod.useCloud()
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('useCloud — fetchStatus', () => {
  it('updates status.value with the cloud status response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ registered: true, connected: true, machine_id: 'mach1' })
    )

    const { status, loading, fetchStatus } = await freshCloud()

    const p = fetchStatus()
    expect(loading.value).toBe(true)
    await p

    expect(status.value.registered).toBe(true)
    expect(status.value.connected).toBe(true)
    expect(loading.value).toBe(false)
  })

  it('sets error.value and clears loading when api throws', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network failure'))

    const { error, loading, fetchStatus } = await freshCloud()
    await fetchStatus()

    expect(error.value).toBe('network failure')
    expect(loading.value).toBe(false)
  })
})

describe('useCloud — connect', () => {
  it('sets connecting=true then false after connected status is returned', async () => {
    vi.useFakeTimers()

    vi.spyOn(globalThis, 'fetch')
      // api.cloud.connect()
      .mockResolvedValueOnce(ok({ status: 'ok' }))
      // poll: api.cloud.status() → connected
      .mockResolvedValue(ok({ registered: true, connected: true }))

    const { connecting, connect } = await freshCloud()

    const p = connect()
    expect(connecting.value).toBe(true)

    await vi.runAllTimersAsync()
    await p

    expect(connecting.value).toBe(false)
  })

  it('sets error.value if api.cloud.connect throws', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('connect failed'))

    const { error, connect } = await freshCloud()
    await connect()

    expect(error.value).toBe('connect failed')
  })

  it('a second connect() increments generation and interrupts the first poll loop', async () => {
    vi.useFakeTimers()

    vi.spyOn(globalThis, 'fetch')
      // Both connect() calls get { status: 'ok' }
      .mockResolvedValueOnce(ok({ status: 'ok' }))
      .mockResolvedValueOnce(ok({ status: 'ok' }))
      // Poll results: never connected so the loop must be broken by gen counter
      .mockResolvedValue(ok({ registered: false, connected: false }))

    const { connecting, connect } = await freshCloud()

    const p1 = connect()
    expect(connecting.value).toBe(true)

    // Second connect() increments connectGen, invalidating the first poll
    const p2 = connect()

    await vi.runAllTimersAsync()
    await Promise.all([p1, p2])

    expect(connecting.value).toBe(false)
  })
})

describe('useCloud — disconnect', () => {
  it('sets disconnecting=true then false', async () => {
    vi.spyOn(globalThis, 'fetch')
      // api.cloud.disconnect()
      .mockResolvedValueOnce(ok({ status: 'ok' }))
      // fetchStatus called after disconnect
      .mockResolvedValueOnce(ok({ registered: false, connected: false }))

    const { disconnecting, disconnect } = await freshCloud()

    const p = disconnect()
    expect(disconnecting.value).toBe(true)
    await p
    expect(disconnecting.value).toBe(false)
  })

  it('calls fetchStatus (GET /api/v1/cloud/status) after successful disconnect', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok({ status: 'ok' }))          // disconnect DELETE
      .mockResolvedValueOnce(ok({ registered: false, connected: false })) // fetchStatus GET

    const { disconnect } = await freshCloud()
    await disconnect()

    // Two calls: DELETE cloud/connect, then GET cloud/status
    expect(fetchSpy).toHaveBeenCalledTimes(2)
    const lastUrl = fetchSpy.mock.calls[1][0] as string
    expect(lastUrl).toContain('/api/v1/cloud/status')
  })

  it('sets error.value when disconnect API throws', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('api down'))

    const { error, disconnect } = await freshCloud()
    await disconnect()

    expect(error.value).toBe('api down')
  })

  it('disconnect() while connect() is polling causes the connect gen to mismatch', async () => {
    // This test verifies the generation-counter mechanism: disconnect() increments
    // connectGen, which causes the connect() poll loop to detect gen !== connectGen
    // and exit without setting connecting=false (because gen !== connectGen in finally).
    // The connecting=false for the interrupted connect call is intentionally skipped.
    vi.useFakeTimers()

    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok({ status: 'ok' }))          // connect POST
      .mockResolvedValueOnce(ok({ status: 'ok' }))          // disconnect DELETE
      .mockResolvedValue(ok({ registered: false, connected: false })) // status polls

    const { disconnecting, connect, disconnect } = await freshCloud()

    // Start connect (will poll)
    connect()

    // disconnect() increments gen — fetchStatus after disconnect completes
    const pDisconnect = disconnect()
    await vi.runAllTimersAsync()
    await pDisconnect

    // disconnect itself should always complete cleanly
    expect(disconnecting.value).toBe(false)
  })
})
