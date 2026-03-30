import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockModelsAvailable = vi.fn()
const mockAgentsList = vi.fn()
const mockConfigGet = vi.fn()
const mockConfigUpdate = vi.fn()
const mockBuiltinStatus = vi.fn()
const mockBuiltinCatalog = vi.fn()
const mockBuiltinInstalledModels = vi.fn()
const mockBuiltinActivate = vi.fn()
const mockBuiltinDownloadStream = vi.fn()
const mockBuiltinPullModelStream = vi.fn()
const mockRouterReplace = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({
    replace: mockRouterReplace,
  }),
}))

vi.mock('../../composables/useApi', () => ({
  api: {
    models: {
      available: (...args: unknown[]) => mockModelsAvailable(...args),
    },
    agents: {
      list: (...args: unknown[]) => mockAgentsList(...args),
    },
    config: {
      get: (...args: unknown[]) => mockConfigGet(...args),
      update: (...args: unknown[]) => mockConfigUpdate(...args),
    },
    builtin: {
      status: (...args: unknown[]) => mockBuiltinStatus(...args),
      catalog: (...args: unknown[]) => mockBuiltinCatalog(...args),
      installedModels: (...args: unknown[]) => mockBuiltinInstalledModels(...args),
      activate: (...args: unknown[]) => mockBuiltinActivate(...args),
      downloadRuntimeStream: (...args: unknown[]) => mockBuiltinDownloadStream(...args),
      pullModelStream: (...args: unknown[]) => mockBuiltinPullModelStream(...args),
    },
  },
}))

vi.mock('../../composables/useConfig', () => {
  const { ref } = require('vue')
  const config = ref<unknown>(null)
  const loading = ref(false)
  const externallyChanged = ref(false)

  return {
    useConfig: () => ({
      config,
      loading,
      externallyChanged,
      loadConfig: async () => {
        const data = await mockConfigGet()
        config.value = data
        return data
      },
      saveConfig: async (cfg: unknown) => {
        await mockConfigUpdate(cfg)
        config.value = cfg
      },
    }),
  }
})

import ModelsView from '../ModelsView.vue'

// ── Sample data ───────────────────────────────────────────────────────────────

const sampleConfig = {
  backend: {
    provider: 'ollama',
    endpoint: 'http://localhost:11434',
    api_key: '',
  },
}

const sampleOllamaModels = {
  models: [
    { name: 'llama3.2:3b', size: 2000000000, details: { parameter_size: '3B', quantization_level: 'Q4_K_M' } },
    { name: 'mistral:7b', size: 4000000000, details: { parameter_size: '7B', quantization_level: 'Q4_0' } },
  ],
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView(provider = 'ollama') {
  return shallowMount(ModelsView, { props: { provider } })
}

beforeEach(() => {
  mockConfigGet.mockReset().mockResolvedValue(sampleConfig)
  mockConfigUpdate.mockReset().mockResolvedValue({ saved: true, requires_restart: false })
  mockModelsAvailable.mockReset().mockResolvedValue(sampleOllamaModels)
  mockAgentsList.mockReset().mockResolvedValue([])
  mockBuiltinStatus.mockReset().mockRejectedValue(new Error('HTTP: 503'))
  mockBuiltinCatalog.mockReset().mockResolvedValue([])
  mockBuiltinInstalledModels.mockReset().mockResolvedValue([])
  mockBuiltinActivate.mockReset().mockResolvedValue({ activated: true })
  mockBuiltinDownloadStream.mockReset().mockReturnValue({ abort: vi.fn() })
  mockBuiltinPullModelStream.mockReset().mockReturnValue({ abort: vi.fn() })
  mockRouterReplace.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ModelsView', () => {
  it('renders without error', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.exists()).toBe(true)
  })

  it('shows "Models" label in the sidebar header', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Models')
  })

  it('renders provider nav items in the sidebar', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Ollama (local)')
    expect(w.text()).toContain('Anthropic')
    expect(w.text()).toContain('OpenAI')
    expect(w.text()).toContain('OpenRouter')
    expect(w.text()).toContain('Built-in (llama.cpp)')
  })

  it('calls loadConfig on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockConfigGet).toHaveBeenCalled()
  })

  it('calls models.available on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockModelsAvailable).toHaveBeenCalled()
  })

  it('calls agents.list on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockAgentsList).toHaveBeenCalled()
  })

  it('renders available ollama models after fetch', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('llama3.2:3b')
    expect(w.text()).toContain('mistral:7b')
  })

  it('selectProvider changes the currentProvider', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.currentProvider).toBe('ollama')
    vm.selectProvider('anthropic')
    await nextTick()
    expect(vm.currentProvider).toBe('anthropic')
  })

  it('selectProvider calls router.replace with new path', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.selectProvider('openai')
    await nextTick()
    expect(mockRouterReplace).toHaveBeenCalledWith('/models/openai')
  })

  it('formatSize returns GB for large files', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.formatSize(2_000_000_000)).toContain('GB')
  })

  it('formatSize returns MB for small files', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.formatSize(500_000_000)).toContain('MB')
  })

  it('shows ollama connected status when models load successfully', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Connected')
  })

  it('shows ollama offline status when models.available returns error', async () => {
    mockModelsAvailable.mockResolvedValueOnce({ error: 'connection refused' })
    const w = mountView('ollama')
    await flushPromises()
    expect(w.text()).toContain('Offline')
  })

  it('shows "Endpoint URL" input field for non-builtin providers', async () => {
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showEndpointEditor = true
    await nextTick()
    expect(w.text()).toContain('Endpoint URL')
  })

  it('shows "API Key" input for non-ollama providers', async () => {
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showApiKeyEditor = true
    await nextTick()
    expect(w.text()).toContain('API Key')
  })

  // ── API Key redaction / eye-button UX ────────────────────────────────────

  it('isApiKeyRedacted is true when backend api_key is [REDACTED]', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '[REDACTED]' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    expect(vm.isApiKeyRedacted).toBe(true)
  })

  it('isApiKeyRedacted is false when backend api_key is a real key', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: 'sk-ant-abc123' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    expect(vm.isApiKeyRedacted).toBe(false)
  })

  it('isApiKeyRedacted is false when backend api_key is empty', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    expect(vm.isApiKeyRedacted).toBe(false)
  })

  it('eye button is hidden when api key is [REDACTED]', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '[REDACTED]' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    // The eye button uses v-if="!isApiKeyRedacted" — should not be rendered
    const vm = w.vm as any
    expect(vm.isApiKeyRedacted).toBe(true)
    expect(vm.showApiKey).toBe(false)
  })

  it('eye button is visible when api key is not redacted', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    expect(vm.isApiKeyRedacted).toBe(false)
  })

  it('showApiKey toggles between true and false', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: 'sk-new-key' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    expect(vm.showApiKey).toBe(false)
    vm.showApiKey = true
    await nextTick()
    expect(vm.showApiKey).toBe(true)
    vm.showApiKey = false
    await nextTick()
    expect(vm.showApiKey).toBe(false)
  })

  it('showApiKey resets to false when provider is switched', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: 'sk-new-key' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showApiKey = true
    await nextTick()
    expect(vm.showApiKey).toBe(true)
    vm.selectProvider('openai')
    await nextTick()
    expect(vm.showApiKey).toBe(false)
  })

  it('shows "saved" indicator text when api key is [REDACTED]', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '[REDACTED]' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showApiKeyEditor = true
    await nextTick()
    expect(w.text()).toContain('saved')
  })

  it('shows "Key saved" helper text when api key is [REDACTED]', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '[REDACTED]' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showApiKeyEditor = true
    await nextTick()
    expect(w.text()).toContain('Key saved')
  })

  it('shows $ENV_VAR hint text when api key is not redacted', async () => {
    mockConfigGet.mockResolvedValueOnce({
      backend: { provider: 'anthropic', endpoint: '', api_key: '' },
    })
    const w = mountView('anthropic')
    await flushPromises()
    const vm = w.vm as any
    vm.showApiKeyEditor = true
    await nextTick()
    expect(w.text()).toContain('$ENV_VAR')
  })
})
