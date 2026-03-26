/**
 * useConfig tests
 *
 * Mocks globalThis.fetch directly since useApi uses fetch internally
 * and jsdom doesn't support relative URLs without a base.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

const baseConfig = {
  reasoner_model: 'gpt-4',
  ollama_base_url: '',
  backend: { type: 'openai', endpoint: '', provider: 'openai', api_key: '' },
  theme: 'dark',
  context_limit_kb: 128,
  git_stage_on_write: false,
  workspace_path: '/tmp',
  max_turns: 10,
  tools_enabled: true,
  allowed_tools: [],
  disallowed_tools: [],
  bash_timeout_secs: 30,
  diff_review_mode: 'auto',
  notepads_enabled: true,
  compact_mode: 'auto',
  compact_trigger: 80,
  vision_enabled: false,
  brave_api_key: '',
  web_ui: { enabled: true, port: 3000, auto_open: false, bind: 'localhost' },
  integrations: {
    google: { client_id: '', client_secret: '' },
    github: { client_id: '', client_secret: '' },
    slack: { client_id: '', client_secret: '' },
    jira: { client_id: '', client_secret: '' },
    bitbucket: { client_id: '', client_secret: '' },
  },
  cloud: { url: '' },
  version: 1,
}

async function freshUseConfig() {
  vi.resetModules()
  const apiMod = await import('../useApi')
  apiMod.setToken('test-token')
  const mod = await import('../useConfig')
  return mod.useConfig
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('useConfig — loadConfig', () => {
  it('populates config.value with API response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(baseConfig))

    const useConfig = await freshUseConfig()
    const { config, loadConfig } = useConfig()
    await loadConfig()

    expect(config.value).toEqual(baseConfig)
    expect(config.value?.theme).toBe('dark')
  })

  it('returns the loaded config from loadConfig()', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(baseConfig))

    const useConfig = await freshUseConfig()
    const { loadConfig } = useConfig()
    const result = await loadConfig()

    expect(result.theme).toBe('dark')
    expect(result.version).toBe(1)
  })

  it('throws when api.config.get fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('server error'))

    const useConfig = await freshUseConfig()
    const { loadConfig } = useConfig()
    await expect(loadConfig()).rejects.toThrow('server error')
  })
})

describe('useConfig — saveConfig', () => {
  it('calls api.config.update (PUT /api/v1/config) with the provided config', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ saved: true, requires_restart: false })
    )

    const useConfig = await freshUseConfig()
    const { saveConfig } = useConfig()
    await saveConfig(baseConfig as never)

    const [url, opts] = fetchSpy.mock.calls[0]
    expect(url).toBe('/api/v1/config')
    expect(opts?.method).toBe('PUT')
    expect(JSON.parse(opts?.body as string)).toEqual(baseConfig)
  })

  it('updates config.value to the saved config', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ saved: true, requires_restart: false })
    )

    const useConfig = await freshUseConfig()
    const { config, saveConfig } = useConfig()
    const updated = { ...baseConfig, theme: 'light' }
    await saveConfig(updated as never)

    expect(config.value?.theme).toBe('light')
  })

  it('clears externallyChanged after save', async () => {
    // load returns base, then save clears externallyChanged
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(baseConfig))                               // loadConfig GET
      .mockResolvedValueOnce(ok({ saved: true, requires_restart: false })) // saveConfig PUT

    const useConfig = await freshUseConfig()
    const { config, externallyChanged, loadConfig, saveConfig } = useConfig()
    await loadConfig()

    await saveConfig({ ...baseConfig, theme: 'system' } as never)
    expect(externallyChanged.value).toBe(false)
    expect(config.value?.theme).toBe('system')
  })

  it('returns requires_restart from saveConfig', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      ok({ saved: true, requires_restart: true })
    )

    const useConfig = await freshUseConfig()
    const { saveConfig } = useConfig()
    const result = await saveConfig(baseConfig as never)

    expect(result.requires_restart).toBe(true)
  })
})

describe('useConfig — poll timer management', () => {
  it('starts a 30 s poll interval on first mount', async () => {
    vi.useFakeTimers()
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval')

    const useConfig = await freshUseConfig()
    useConfig() // first mount — starts poll timer

    expect(setIntervalSpy).toHaveBeenCalledWith(expect.any(Function), 30_000)
  })

  it('poll detects no change when config is identical', async () => {
    vi.useFakeTimers()

    // loadConfig + poll both return the same base config
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(ok(baseConfig))

    const useConfig = await freshUseConfig()
    const { externallyChanged, loadConfig } = useConfig()
    await loadConfig()

    // Advance exactly one poll interval — triggers pollForChanges once
    await vi.advanceTimersByTimeAsync(30_000)
    // Let the promise from pollForChanges resolve
    await Promise.resolve()
    await Promise.resolve()

    expect(externallyChanged.value).toBe(false)
  })

  it('sets externallyChanged=true when poll detects a difference', async () => {
    vi.useFakeTimers()

    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(ok(baseConfig))                         // loadConfig
      .mockResolvedValue(ok({ ...baseConfig, theme: 'system' }))    // poll returns different

    const useConfig = await freshUseConfig()
    const { config, externallyChanged, loadConfig } = useConfig()
    await loadConfig()

    // Advance exactly one poll interval — triggers pollForChanges once
    await vi.advanceTimersByTimeAsync(30_000)
    // Let the promise chain from pollForChanges resolve
    await Promise.resolve()
    await Promise.resolve()

    expect(externallyChanged.value).toBe(true)
    expect(config.value?.theme).toBe('system')
  })
})
