/**
 * useVersion tests
 *
 * Strategy: mock globalThis.fetch directly (the underlying primitive that
 * useApi uses). vi.resetModules() between tests so the singleton cache
 * starts fresh.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'

function ok(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

async function freshVersion() {
  vi.resetModules()
  const apiMod = await import('../useApi')
  apiMod.setToken('test-token')
  const mod = await import('../useVersion')
  return mod.useVersion()
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useVersion', () => {
  it('loadVersion fetches /api/v1/health and stores body.version', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ status: 'ok', version: 'v1.2.3-test', satellite_connected: false }),
    )

    const { version, loadVersion } = await freshVersion()
    expect(version.value).toBe('') // pre-fetch placeholder

    await loadVersion()

    expect(version.value).toBe('v1.2.3-test')
  })

  it('loadVersion is idempotent: a second call does not refetch', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ status: 'ok', version: 'v1.2.3' }),
    )

    const { version, loadVersion } = await freshVersion()
    await loadVersion()
    await loadVersion()
    await loadVersion()

    expect(version.value).toBe('v1.2.3')
    expect(spy).toHaveBeenCalledTimes(1)
  })

  it('concurrent loadVersion calls share a single in-flight request', async () => {
    let resolve!: (r: Response) => void
    const pending = new Promise<Response>((r) => { resolve = r })
    const spy = vi.spyOn(globalThis, 'fetch').mockReturnValue(pending)

    const { loadVersion } = await freshVersion()
    const p1 = loadVersion()
    const p2 = loadVersion()
    const p3 = loadVersion()

    // All three calls in flight, but only one network request issued.
    expect(spy).toHaveBeenCalledTimes(1)

    resolve(ok({ status: 'ok', version: 'v9.9.9' }))
    await Promise.all([p1, p2, p3])
  })

  it('on fetch failure leaves version empty and allows retry', async () => {
    const spy = vi.spyOn(globalThis, 'fetch')
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(ok({ status: 'ok', version: 'v2.0.0' }))

    const { version, loadVersion } = await freshVersion()

    await loadVersion()
    expect(version.value).toBe('') // failed call must not poison the cache

    await loadVersion()
    expect(version.value).toBe('v2.0.0')
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it('versionLabel exposes a non-empty fallback even before load resolves', async () => {
    const { versionLabel } = await freshVersion()
    // No fetch yet — label still has to render *something* in the UI so the
    // tooltip doesn't show as empty / "undefined". The composable picks a
    // neutral placeholder until the real value lands.
    expect(versionLabel.value.length).toBeGreaterThan(0)
  })
})
