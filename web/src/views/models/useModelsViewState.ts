import { ref, computed, onMounted, watch, type Ref } from 'vue'
import type { Router } from 'vue-router'
import { api, type BuiltinStatus, type BuiltinCatalogEntry, type BuiltinInstalledModel, type ProviderModel } from '../../composables/useApi'
import { useConfig } from '../../composables/useConfig'

interface OllamaModel {
  name: string
  size?: number
  details?: { parameter_size?: string; quantization_level?: string }
}

export function useModelsViewState(providerFromRoute: Ref<string | undefined>, router: Router) {
  const { config, loading, externallyChanged, loadConfig, saveConfig } = useConfig()

  const providers = [
    { value: 'ollama', label: 'Ollama (local)' },
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'openai', label: 'OpenAI' },
    { value: 'openrouter', label: 'OpenRouter' },
    { value: 'google', label: 'Google AI Studio' },
    { value: 'vertex', label: 'Google Vertex AI' },
    { value: 'deepseek', label: 'DeepSeek' },
    { value: 'zai', label: 'Z.ai (GLM)' },
    { value: 'custom', label: 'Custom (OpenAI-compatible)' },
    { value: 'builtin', label: 'Built-in (llama.cpp)' },
  ]

  const currentProvider = ref(providerFromRoute.value || 'ollama')
  const form = ref({
    backend_endpoint: '',
    backend_api_key: '',
    // Vertex AI fields (only meaningful when currentProvider === 'vertex')
    backend_project: '',
    backend_location: '',
    backend_credentials_path: '',
  })
  const perProviderForm = ref<Record<string, { endpoint: string; apiKey: string }>>({})
  const agentsList = ref<Array<{ name: string; model?: string }>>([])
  const dirty = ref(false)
  const saving = ref(false)
  const saveMsg = ref('')
  const saveError = ref(false)
  const showApiKey = ref(false)
  const isApiKeyRedacted = computed(() => form.value.backend_api_key === '[REDACTED]')
  const availableModels = ref<OllamaModel[]>([])
  const modelsLoading = ref(false)
  const ollamaStatus = ref<'unknown' | 'connected' | 'error'>('unknown')
  const pullModelName = ref('')
  const pulling = ref(false)
  const pullMsg = ref('')
  const pullError = ref(false)
  const deletingModels = ref<Set<string>>(new Set())
  const deleteError = ref<string | null>(null)
  const deleteConfirm = ref<{ name: string; type: 'ollama' | 'builtin' } | null>(null)
  const showPullModal = ref(false)
  const showEndpointEditor = ref(false)
  const showRuntimeEditor = ref(false)
  const modelSearch = ref('')
  const builtinSearch = ref('')

  const providerModels = ref<ProviderModel[]>([])
  const providerModelsLoading = ref(false)
  const providerModelsError = ref('')
  const providerSearch = ref('')
  const showApiKeyEditor = ref(false)

  const filteredModels = computed(() => {
    if (!modelSearch.value) return availableModels.value
    const q = modelSearch.value.toLowerCase()
    return availableModels.value.filter(m => m.name.toLowerCase().includes(q))
  })

  const builtinStatus = ref<BuiltinStatus | null>(null)
  const builtinNotConfigured = ref(false)
  const builtinCatalog = ref<BuiltinCatalogEntry[]>([])
  const builtinInstalled = ref<BuiltinInstalledModel[]>([])
  const builtinLoading = ref(false)
  const builtinDownloading = ref(false)
  const builtinDownloadProgress = ref<{ downloaded: number; total: number } | null>(null)
  const builtinDownloadError = ref('')
  const builtinPulling = ref<Record<string, boolean>>({})
  const builtinPullProgress = ref<Record<string, { downloaded: number; total: number; speed: number }>>({})
  const builtinPullError = ref<Record<string, string>>({})
  const builtinActivating = ref(false)
  const builtinActivateMsg = ref('')
  const builtinActivateError = ref(false)
  const deletingBuiltin = ref<Set<string>>(new Set())

  const filteredCatalog = computed(() => {
    if (!builtinSearch.value) return builtinCatalog.value
    const q = builtinSearch.value.toLowerCase()
    return builtinCatalog.value.filter(m => m.name.toLowerCase().includes(q))
  })

  const isApiKeyConfigured = computed(() =>
    !!form.value.backend_api_key && form.value.backend_api_key !== '',
  )

  const isVertexCredentialsConfigured = computed(() =>
    !!form.value.backend_credentials_path && form.value.backend_credentials_path !== '',
  )

  const filteredProviderModels = computed(() => {
    if (!providerSearch.value) return providerModels.value
    const q = providerSearch.value.toLowerCase()
    return providerModels.value.filter(m =>
      m.id.toLowerCase().includes(q) ||
      (m.name?.toLowerCase().includes(q) ?? false) ||
      (m.description?.toLowerCase().includes(q) ?? false) ||
      (m.provider?.toLowerCase().includes(q) ?? false) ||
      (m.tags?.some(t => t.toLowerCase().includes(q)) ?? false),
    )
  })

  const providerDisplayName = computed(() => {
    switch (currentProvider.value) {
      case 'anthropic': return 'Anthropic'
      case 'openai': return 'OpenAI'
      case 'openrouter': return 'OpenRouter'
      case 'google': return 'Google AI Studio'
      case 'vertex': return 'Google Vertex AI'
      case 'deepseek': return 'DeepSeek'
      case 'zai': return 'Z.ai (GLM)'
      case 'custom': return 'Custom (OpenAI-compatible)'
      default: return currentProvider.value
    }
  })

  const endpointDisplay = computed(() => {
    const url = form.value.backend_endpoint || 'localhost:11434'
    return url.replace(/^https?:\/\//, '')
  })

  const providerEndpointPlaceholder = computed(() => {
    switch (currentProvider.value) {
      case 'ollama': return 'http://localhost:11434'
      case 'anthropic': return 'https://api.anthropic.com'
      case 'openai': return 'https://api.openai.com/v1'
      case 'openrouter': return 'https://openrouter.ai/api/v1'
      case 'google': return 'https://generativelanguage.googleapis.com'
      case 'vertex': return 'https://{LOCATION}-aiplatform.googleapis.com'
      case 'deepseek': return 'https://api.deepseek.com'
      case 'zai': return 'https://api.z.ai/api/paas/v4'
      case 'custom': return 'https://your-provider.example.com/v1'
      default: return 'https://...'
    }
  })

  function formatBuiltinProgress(downloaded: number, total: number): string {
    const toMB = (b: number) => (b / 1e6).toFixed(1)
    if (total > 0) return `${toMB(downloaded)} / ${toMB(total)} MB`
    return `${toMB(downloaded)} MB`
  }

  async function loadBuiltinData(refresh = false) {
    builtinLoading.value = true
    builtinNotConfigured.value = false
    try {
      const [status, catalog, installed] = await Promise.all([
        api.builtin.status().catch((e: unknown) => {
          if (e instanceof Error && e.message.includes(': 503')) builtinNotConfigured.value = true
          return null
        }),
        api.builtin.catalog(refresh).catch(() => [] as BuiltinCatalogEntry[]),
        api.builtin.installedModels().catch(() => [] as BuiltinInstalledModel[]),
      ])
      builtinStatus.value = status
      builtinCatalog.value = catalog
      builtinInstalled.value = installed
    } finally {
      builtinLoading.value = false
    }
  }

  function startDownloadRuntime() {
    if (builtinDownloading.value) return
    builtinDownloading.value = true
    builtinDownloadProgress.value = null
    builtinDownloadError.value = ''
    api.builtin.downloadRuntimeStream(
      (e) => { builtinDownloadProgress.value = e },
      () => {
        builtinDownloading.value = false
        loadBuiltinData()
      },
      (msg) => {
        builtinDownloading.value = false
        builtinDownloadError.value = msg
      },
    )
  }

  function startPullModel(name: string) {
    if (builtinPulling.value[name]) return
    builtinPulling.value = { ...builtinPulling.value, [name]: true }
    builtinPullProgress.value = { ...builtinPullProgress.value, [name]: { downloaded: 0, total: 0, speed: 0 } }
    builtinPullError.value = { ...builtinPullError.value, [name]: '' }
    api.builtin.pullModelStream(
      name,
      (e) => { builtinPullProgress.value = { ...builtinPullProgress.value, [name]: e } },
      () => {
        builtinPulling.value = { ...builtinPulling.value, [name]: false }
        loadBuiltinData()
      },
      (msg) => {
        builtinPulling.value = { ...builtinPulling.value, [name]: false }
        builtinPullError.value = { ...builtinPullError.value, [name]: msg }
      },
    )
  }

  function deleteBuiltinModel(name: string) {
    if (deletingBuiltin.value.has(name)) return
    deleteConfirm.value = { name, type: 'builtin' }
  }

  async function activateBuiltin(model: string) {
    if (builtinActivating.value) return
    builtinActivating.value = true
    builtinActivateMsg.value = ''
    builtinActivateError.value = false
    try {
      await api.builtin.activate(model)
      builtinActivateMsg.value = `Activated ${model}. Restart Huginn to apply.`
      await loadBuiltinData()
    } catch (e: unknown) {
      builtinActivateMsg.value = e instanceof Error ? e.message : 'Activation failed'
      builtinActivateError.value = true
    } finally {
      builtinActivating.value = false
      setTimeout(() => { builtinActivateMsg.value = '' }, 5000)
    }
  }

  async function loadProviderModels(_force = false) {
    if (currentProvider.value === 'ollama' || currentProvider.value === 'builtin') return
    providerModelsLoading.value = true
    providerModelsError.value = ''
    try {
      providerModels.value = await api.providers.models(currentProvider.value)
    } catch (e: unknown) {
      providerModelsError.value = e instanceof Error ? e.message : 'Failed to load models'
      providerModels.value = []
    } finally {
      providerModelsLoading.value = false
    }
  }

  function formatContextLength(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(0)}M ctx`
    if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K ctx`
    return `${n} ctx`
  }

  function formatPrice(usdPerMillion: number): string {
    if (usdPerMillion === 0) return '0'
    if (usdPerMillion < 0.01) return usdPerMillion.toFixed(4)
    if (usdPerMillion < 1) return usdPerMillion.toFixed(3)
    return usdPerMillion.toFixed(2)
  }

  function pricingColorClass(usdPerMillion: number): string {
    if (usdPerMillion === 0) return 'text-huginn-green'
    if (usdPerMillion < 1) return 'text-huginn-green'
    if (usdPerMillion < 5) return 'text-huginn-yellow'
    return 'text-huginn-red'
  }

  function providerBadgeStyle(provider: string): Record<string, string> {
    const map: Record<string, { bg: string; color: string; border: string }> = {
      openai: { bg: 'rgba(63,185,80,0.08)', color: 'rgba(63,185,80,0.85)', border: 'rgba(63,185,80,0.25)' },
      anthropic: { bg: 'rgba(210,153,34,0.08)', color: 'rgba(210,153,34,0.85)', border: 'rgba(210,153,34,0.25)' },
      google: { bg: 'rgba(63,185,80,0.08)', color: 'rgba(63,185,80,0.75)', border: 'rgba(63,185,80,0.2)' },
      mistral: { bg: 'rgba(88,166,255,0.08)', color: 'rgba(88,166,255,0.75)', border: 'rgba(88,166,255,0.2)' },
      deepseek: { bg: 'rgba(130,80,255,0.08)', color: 'rgba(130,80,255,0.85)', border: 'rgba(130,80,255,0.25)' },
      meta: { bg: 'rgba(88,166,255,0.1)', color: 'rgba(88,166,255,0.85)', border: 'rgba(88,166,255,0.3)' },
      qwen: { bg: 'rgba(88,166,255,0.08)', color: 'rgba(88,166,255,0.7)', border: 'rgba(88,166,255,0.2)' },
      cohere: { bg: 'rgba(210,153,34,0.08)', color: 'rgba(210,153,34,0.75)', border: 'rgba(210,153,34,0.2)' },
      perplexity: { bg: 'rgba(63,185,80,0.08)', color: 'rgba(63,185,80,0.7)', border: 'rgba(63,185,80,0.2)' },
      x: { bg: 'rgba(200,200,200,0.08)', color: 'rgba(200,200,200,0.7)', border: 'rgba(200,200,200,0.2)' },
    }
    const style = map[provider.toLowerCase()] ?? { bg: 'rgba(125,125,125,0.08)', color: 'rgba(125,125,125,0.7)', border: 'rgba(125,125,125,0.2)' }
    return { background: style.bg, color: style.color, border: `1px solid ${style.border}` }
  }

  function selectProvider(value: string) {
    perProviderForm.value[currentProvider.value] = {
      endpoint: form.value.backend_endpoint,
      apiKey: form.value.backend_api_key,
    }
    currentProvider.value = value
    router.replace(`/models/${value}`)
    const saved = perProviderForm.value[value]
    const cfg = config.value?.backend
    form.value.backend_endpoint = saved?.endpoint ?? (cfg?.provider === value ? cfg?.endpoint || '' : '')
    form.value.backend_api_key = saved?.apiKey ?? (cfg?.provider === value ? cfg?.api_key || '' : '')
    dirty.value = false
    showApiKey.value = false
    showApiKeyEditor.value = false
    providerSearch.value = ''
    providerModels.value = []
    providerModelsError.value = ''
    if (value === 'builtin') {
      loadBuiltinData()
    } else if (value !== 'ollama') {
      loadProviderModels()
    }
  }

  async function discardChanges() {
    const cfg = await loadConfig()
    form.value.backend_endpoint = cfg.backend?.endpoint || ''
    form.value.backend_api_key = cfg.backend?.api_key || ''
    form.value.backend_project = cfg.backend?.project || ''
    form.value.backend_location = cfg.backend?.location || ''
    form.value.backend_credentials_path = cfg.backend?.credentials_path || ''
    dirty.value = false
  }

  function formatSize(bytes: number): string {
    const gb = bytes / 1e9
    return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(bytes / 1e6).toFixed(0)} MB`
  }

  async function loadAvailableModels() {
    modelsLoading.value = true
    try {
      const data = await (api as unknown as { models: { available(): Promise<{ models?: OllamaModel[]; error?: string }> } }).models.available()
      if (data.error) {
        ollamaStatus.value = 'error'
        availableModels.value = []
      } else {
        ollamaStatus.value = 'connected'
        availableModels.value = data.models ?? []
      }
    } catch {
      ollamaStatus.value = 'error'
      availableModels.value = []
    } finally {
      modelsLoading.value = false
    }
  }

  async function pullModel(name: string) {
    if (!name || pulling.value) return
    pulling.value = true
    pullMsg.value = ''
    pullError.value = false
    try {
      await (api as unknown as { models: { pull(n: string): Promise<{ status: string }> } }).models.pull(name)
      pullMsg.value = `Pulled ${name} successfully`
      pullModelName.value = ''
      await loadAvailableModels()
    } catch (e: unknown) {
      pullMsg.value = e instanceof Error ? e.message : 'Pull failed'
      pullError.value = true
    } finally {
      pulling.value = false
      setTimeout(() => { pullMsg.value = '' }, 5000)
    }
  }

  function closePullModal() {
    if (pulling.value) return
    showPullModal.value = false
    pullModelName.value = ''
    pullMsg.value = ''
    pullError.value = false
  }

  async function deleteOllamaModel(name: string) {
    if (deletingModels.value.has(name)) return
    deleteConfirm.value = { name, type: 'ollama' }
  }

  async function confirmDeleteModel() {
    if (!deleteConfirm.value) return
    const { name, type } = deleteConfirm.value
    deleteConfirm.value = null
    deleteError.value = null
    if (type === 'ollama') {
      deletingModels.value = new Set([...deletingModels.value, name])
      try {
        await (api as unknown as { models: { delete(n: string): Promise<{ deleted: boolean }> } }).models.delete(name)
        await loadAvailableModels()
      } catch (e) {
        deleteError.value = e instanceof Error ? e.message : 'Delete failed'
      } finally {
        const next = new Set(deletingModels.value)
        next.delete(name)
        deletingModels.value = next
      }
    } else {
      deletingBuiltin.value = new Set([...deletingBuiltin.value, name])
      try {
        await api.builtin.delete(name)
        await loadBuiltinData()
      } catch {
      } finally {
        const next = new Set(deletingBuiltin.value)
        next.delete(name)
        deletingBuiltin.value = next
      }
    }
  }

  async function loadAgentsList() {
    try {
      const data = await api.agents.list() as Array<{ name: string; model?: string }>
      agentsList.value = data
    } catch {
      agentsList.value = []
    }
  }

  function agentsUsingModel(modelName: string): string[] {
    return agentsList.value.filter(a => a.model === modelName).map(a => a.name)
  }

  async function save() {
    saving.value = true
    saveMsg.value = ''
    saveError.value = false
    try {
      if (!config.value) throw new Error('Config not loaded')
      const updated = {
        ...config.value,
        backend: {
          ...config.value.backend,
          provider: currentProvider.value,
          endpoint: form.value.backend_endpoint,
          api_key: form.value.backend_api_key,
          project: form.value.backend_project,
          location: form.value.backend_location,
          credentials_path: form.value.backend_credentials_path,
        },
      }
      await saveConfig(updated)
      dirty.value = false
      saveMsg.value = 'Saved'
      setTimeout(() => { saveMsg.value = '' }, 3000)
      if (currentProvider.value !== 'ollama' && currentProvider.value !== 'builtin') {
        loadProviderModels()
      }
    } catch (e: unknown) {
      saveMsg.value = e instanceof Error ? e.message : 'Save failed'
      saveError.value = true
    } finally {
      saving.value = false
    }
  }

  watch(providerFromRoute, (val) => {
    if (val && val !== currentProvider.value) currentProvider.value = val
  })

  onMounted(async () => {
    const cfg = await loadConfig()
    const savedProvider = cfg.backend?.provider || 'ollama'
    if (!providerFromRoute.value) {
      currentProvider.value = savedProvider
      router.replace(`/models/${currentProvider.value}`)
    }
    form.value.backend_endpoint = cfg.backend?.endpoint || ''
    form.value.backend_api_key = cfg.backend?.api_key || ''
    form.value.backend_project = cfg.backend?.project || ''
    form.value.backend_location = cfg.backend?.location || ''
    form.value.backend_credentials_path = cfg.backend?.credentials_path || ''
    await loadAvailableModels()
    await loadAgentsList()
    if (currentProvider.value === 'builtin') {
      await loadBuiltinData()
    } else if (currentProvider.value !== 'ollama') {
      loadProviderModels()
    }
  })

  return {
    config,
    loading,
    externallyChanged,
    providers,
    currentProvider,
    form,
    perProviderForm,
    agentsList,
    dirty,
    saving,
    saveMsg,
    saveError,
    showApiKey,
    isApiKeyRedacted,
    availableModels,
    modelsLoading,
    ollamaStatus,
    pullModelName,
    pulling,
    pullMsg,
    pullError,
    deletingModels,
    deleteError,
    deleteConfirm,
    showPullModal,
    showEndpointEditor,
    showRuntimeEditor,
    modelSearch,
    builtinSearch,
    providerModels,
    providerModelsLoading,
    providerModelsError,
    providerSearch,
    showApiKeyEditor,
    filteredModels,
    filteredCatalog,
    isApiKeyConfigured,
    isVertexCredentialsConfigured,
    filteredProviderModels,
    providerDisplayName,
    endpointDisplay,
    builtinStatus,
    builtinNotConfigured,
    builtinCatalog,
    builtinInstalled,
    builtinLoading,
    builtinDownloading,
    builtinDownloadProgress,
    builtinDownloadError,
    builtinPulling,
    builtinPullProgress,
    builtinPullError,
    builtinActivating,
    builtinActivateMsg,
    builtinActivateError,
    deletingBuiltin,
    providerEndpointPlaceholder,
    formatBuiltinProgress,
    loadBuiltinData,
    startDownloadRuntime,
    startPullModel,
    deleteBuiltinModel,
    activateBuiltin,
    loadProviderModels,
    formatContextLength,
    formatPrice,
    pricingColorClass,
    providerBadgeStyle,
    selectProvider,
    discardChanges,
    formatSize,
    loadAvailableModels,
    pullModel,
    closePullModal,
    deleteOllamaModel,
    confirmDeleteModel,
    loadAgentsList,
    agentsUsingModel,
    save,
  }
}
