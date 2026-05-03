import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setToken } from '../useApi'
import { apiFetch } from '../useFetch'

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  setToken('test-token')
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useFetch — apiFetch', () => {
  it('re-exports apiFetch from useApi', () => {
    expect(typeof apiFetch).toBe('function')
  })

  it('injects Authorization header automatically', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ ok: true }))
    await apiFetch('/api/v1/test')

    const [, opts] = spy.mock.calls[0]!
    const headers = opts?.headers as Record<string, string>
    expect(headers['Authorization']).toBe('Bearer test-token')
  })

  it('sends Content-Type: application/json by default', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({}))
    await apiFetch('/api/v1/test')

    const [, opts] = spy.mock.calls[0]!
    const headers = opts?.headers as Record<string, string>
    expect(headers['Content-Type']).toBe('application/json')
  })

  it('parses and returns JSON response body', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ name: 'huginn', version: 2 }))
    const result = await apiFetch<{ name: string; version: number }>('/api/v1/test')
    expect(result.name).toBe('huginn')
    expect(result.version).toBe(2)
  })

  it('throws on non-ok HTTP status', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response('not found', { status: 404 }),
    )
    await expect(apiFetch('/api/v1/missing')).rejects.toThrow()
  })

  it('forwards method and body options', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({ id: 'new' }))
    await apiFetch('/api/v1/items', { method: 'POST', body: JSON.stringify({ name: 'x' }) })

    const [url, opts] = spy.mock.calls[0]!
    expect(url).toBe('/api/v1/items')
    expect(opts?.method).toBe('POST')
    expect(JSON.parse(opts?.body as string)).toEqual({ name: 'x' })
  })

  it('allows caller-supplied headers to override defaults', async () => {
    const spy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(okJson({}))
    await apiFetch('/api/v1/test', {
      headers: { 'X-Custom': 'yes', 'Content-Type': 'text/plain' },
    })

    const [, opts] = spy.mock.calls[0]!
    const headers = opts?.headers as Record<string, string>
    expect(headers['X-Custom']).toBe('yes')
    expect(headers['Content-Type']).toBe('text/plain')
  })
})
