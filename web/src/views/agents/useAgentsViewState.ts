import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount, type Ref } from 'vue'
import type { Router } from 'vue-router'
import { api, apiFetch, getToken } from '../../composables/useApi'
import type { Connection, ToolbeltEntry, SystemToolStatus } from '../../composables/useApi'
import { useInstalledSkills } from '../../composables/useSkills'
import { useAgents, type AgentSummary } from '../../composables/useAgents'
import { useAgentCapabilityMatrix } from './useAgentCapabilityMatrix'

interface OllamaModel {
  name: string
  source?: string
  size_bytes?: number
  details?: { parameter_size?: string; quantization_level?: string }
}

type MemoryType = 'none' | 'context' | 'muninndb'
type MemoryMode = 'passive' | 'conversational' | 'immersive'

interface AgentForm {
  name: string
  model: string
  system_prompt: string
  color: string
  icon: string
  memory_type: MemoryType
  memory_enabled: boolean
  context_notes_enabled: boolean
  vault_name: string
  memory_mode: MemoryMode
  vault_description: string
  toolbelt: ToolbeltEntry[]
  skills: string[]
  local_tools: string[]
  heartbeat_enabled: boolean
  heartbeat_cron: string
}

interface VaultItem {
  name: string
  linked: boolean
}

type VaultHealthStatus = 'ok' | 'degraded' | 'unavailable' | 'unknown'
interface VaultHealth {
  status: VaultHealthStatus
  tools_count: number
  warning: string
  latency_ms: number
}

type MemoryModalState = {
  open: boolean
  vaultChoice: 'existing' | 'new'
  selectedVault: string
  newVaultName: string
  newVaultDesc: string
  mode: MemoryMode
}

interface ModelGroup {
  provider: string
  icon: string
  color: string
  models: (OllamaModel & { _family?: string })[]
}

const LOCAL_TOOL_CATALOG = [
  {
    category: 'File System',
    icon: '📁',
    tools: [
      { name: 'read_file', label: 'Read files', description: 'Read file contents' },
      { name: 'list_dir', label: 'List directory', description: 'List files in a directory' },
      { name: 'search_files', label: 'Search files', description: 'Search file contents by pattern' },
      { name: 'grep', label: 'Grep', description: 'Search for text across files' },
      { name: 'write_file', label: 'Write files', description: 'Create or overwrite files ⚠' },
      { name: 'edit_file', label: 'Edit files', description: 'Edit specific lines in a file ⚠' },
    ],
  },
  {
    category: 'Git',
    icon: '🌿',
    tools: [
      { name: 'git_status', label: 'Git status', description: 'Show working tree status' },
      { name: 'git_log', label: 'Git log', description: 'Show commit history' },
      { name: 'git_diff', label: 'Git diff', description: 'Show file diffs' },
      { name: 'git_blame', label: 'Git blame', description: 'Show who last modified each line' },
      { name: 'git_commit', label: 'Git commit', description: 'Commit staged changes ⚠' },
      { name: 'git_branch', label: 'Git branch', description: 'Create or switch branches ⚠' },
      { name: 'git_stash', label: 'Git stash', description: 'Stash or restore working changes ⚠' },
    ],
  },
  {
    category: 'Code Intelligence',
    icon: '🔍',
    tools: [
      { name: 'find_definition', label: 'Find definition', description: 'Jump to symbol definition (LSP)' },
      { name: 'list_symbols', label: 'List symbols', description: 'List all symbols in a file (LSP)' },
    ],
  },
  {
    category: 'Web',
    icon: '🌐',
    tools: [
      { name: 'fetch_url', label: 'Fetch URL', description: 'Fetch content from a URL' },
      { name: 'web_search', label: 'Web search', description: 'Search the web (requires Brave API key)' },
    ],
  },
] as const

const SHELL_TOOLS = [
  { name: 'bash', label: 'Bash', description: 'Run arbitrary shell commands. Requires approval on every use.' },
  { name: 'run_tests', label: 'Run tests', description: 'Run the project test suite. Requires approval on every use.' },
] as const

const SHELL_TOOL_NAMES = new Set(['bash', 'run_tests'])

export function useAgentsViewState(agentName: Ref<string | undefined>, router: Router) {
  const { agents, loading, updateAgent, removeAgent: removeFromList, fetchAgents } = useAgents()
  const capabilityMatrix = useAgentCapabilityMatrix()

  async function openDM(agent: AgentSummary) {
    try {
      const space = await apiFetch<{ id: string }>(`/api/v1/spaces/dm/${encodeURIComponent(agent.name)}`)
      router.push(`/space/${space.id}`)
    } catch {
      router.push(`/agents/${agent.name}`)
    }
  }

  const isStaleRefreshing = ref(false)
  let staleDebounceTimer: ReturnType<typeof setTimeout> | null = null

  function scheduleStaleRefresh() {
    if (staleDebounceTimer !== null) return
    staleDebounceTimer = setTimeout(async () => {
      staleDebounceTimer = null
      if (!agentName.value || agentName.value === 'new') {
        fetchAgents().catch(() => {})
        return
      }
      isStaleRefreshing.value = true
      try {
        await Promise.all([
          loadAgent(agentName.value),
          fetchAgents(),
        ])
      } catch {
      } finally {
        isStaleRefreshing.value = false
      }
    }, 500)
  }

  function onVisibilityChange() {
    if (document.visibilityState === 'visible') scheduleStaleRefresh()
  }

  function onWindowFocus() {
    scheduleStaleRefresh()
  }

  const form = ref<AgentForm>({
    name: '',
    model: '',
    system_prompt: '',
    color: '#58a6ff',
    icon: '',
    memory_type: 'none',
    memory_enabled: false,
    context_notes_enabled: false,
    vault_name: '',
    memory_mode: 'conversational',
    vault_description: '',
    toolbelt: [],
    skills: [],
    local_tools: [],
    heartbeat_enabled: false,
    heartbeat_cron: '',
  })
  const original = ref('')
  const dirty = ref(false)
  const saving = ref(false)
  const saveMsg = ref('')
  const saveError = ref(false)
  const loadError = ref(false)
  const loadErrorMsg = ref('')
  const showDeleteConfirm = ref(false)
  const availableModels = ref<OllamaModel[]>([])
  const modelsLoading = ref(false)
  const modelsError = ref('')

  const showModelPicker = ref(false)
  const modelSearch = ref('')
  const modelSearchInput = ref<HTMLInputElement | null>(null)

  const muninnConnected = ref(false)
  const muninnEndpoint = ref('')
  const existingVaults = ref<VaultItem[]>([])
  const linkedVaultNames = computed(() => existingVaults.value.filter(v => v.linked).map(v => v.name))
  const allVaultNames = computed(() => existingVaults.value.map(v => v.name))
  const vaultCheckTimeout = ref<ReturnType<typeof setTimeout> | null>(null)

  const vaultHealth = ref<VaultHealth>({ status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 })
  let vaultHealthInterval: ReturnType<typeof setInterval> | null = null

  const memoryModal = ref<MemoryModalState>({
    open: false,
    vaultChoice: 'existing',
    selectedVault: '',
    newVaultName: '',
    newVaultDesc: '',
    mode: 'conversational',
  })

  const previousMemoryType = ref<MemoryType>('none')

  function selectMuninnDB() {
    previousMemoryType.value = form.value.memory_type
    form.value.memory_type = 'muninndb'
    form.value.memory_enabled = true
    markDirty()
    if (!form.value.vault_name) {
      openMemoryModal()
    }
  }

  function cancelMemoryModal() {
    memoryModal.value.open = false
    if (!form.value.vault_name && form.value.memory_type === 'muninndb') {
      form.value.memory_type = previousMemoryType.value
      form.value.memory_enabled = previousMemoryType.value === 'muninndb'
      form.value.context_notes_enabled = previousMemoryType.value === 'context'
    }
  }

  function openMemoryModal() {
    const isNew = form.value.vault_name && !allVaultNames.value.includes(form.value.vault_name)
    memoryModal.value = {
      open: true,
      vaultChoice: isNew || !form.value.vault_name ? 'new' : 'existing',
      selectedVault: allVaultNames.value.includes(form.value.vault_name) ? form.value.vault_name : (allVaultNames.value[0] || ''),
      newVaultName: isNew ? form.value.vault_name : '',
      newVaultDesc: form.value.vault_description,
      mode: form.value.memory_mode || 'conversational',
    }
    if (memoryModal.value.vaultChoice === 'new' && !memoryModal.value.newVaultName && form.value.name) {
      const slug = form.value.name.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/-{2,}/g, '-').replace(/^-|-$/g, '')
      if (slug) memoryModal.value.newVaultName = 'huginn-' + slug
    }
  }

  function saveMemoryModal() {
    if (memoryModal.value.vaultChoice === 'existing') {
      form.value.vault_name = memoryModal.value.selectedVault
      form.value.vault_description = ''
    } else {
      form.value.vault_name = memoryModal.value.newVaultName
      form.value.vault_description = memoryModal.value.newVaultDesc
    }
    form.value.memory_mode = memoryModal.value.mode
    memoryModal.value.open = false
    markDirty()
    loadMuninnInfo()
  }

  const availableConnections = ref<Connection[]>([])
  const systemTools = ref<SystemToolStatus[]>([])

  const showConnectionsModal = ref(false)
  const modalToolbelt = ref<ToolbeltEntry[]>([])

  const showSkillsModal = ref(false)
  const modalSkills = ref<string[]>([])

  const colorPalette = ['#58a6ff', '#3fb950', '#d29922', '#f85149', '#bc8cff', '#79c0ff']

  function detectProvider(name: string, source?: string): { provider: string; icon: string; color: string; family: string } {
    if (source === 'built-in') return { provider: 'Built-in', icon: 'H', color: '#e3b341', family: 'llama.cpp' }
    const n = name.toLowerCase()
    if (n.startsWith('claude')) return { provider: 'Anthropic', icon: 'A', color: '#cc785c', family: '' }
    if (n.startsWith('gpt') || n.startsWith('o1') || n.startsWith('o3') || n.startsWith('o4')) return { provider: 'OpenAI', icon: 'O', color: '#10a37f', family: '' }
    if (n.startsWith('gemini')) return { provider: 'Google', icon: 'G', color: '#4285f4', family: '' }
    if (n.startsWith('nomic') || n.startsWith('mxbai') || n.includes('embed')) return { provider: 'Embeddings', icon: 'E', color: '#64748b', family: '' }
    if (n.startsWith('llama')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Meta' }
    if (n.startsWith('qwen')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Qwen' }
    if (n.startsWith('deepseek')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'DeepSeek' }
    if (n.startsWith('phi')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Microsoft' }
    if (n.startsWith('mistral') || n.startsWith('mixtral')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Mistral' }
    if (n.startsWith('gemma')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Google' }
    if (n.startsWith('codellama')) return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: 'Meta' }
    return { provider: 'Ollama', icon: '◎', color: '#4a9eff', family: '' }
  }

  const filteredModelGroups = computed((): ModelGroup[] => {
    const search = modelSearch.value.toLowerCase().trim()
    const groups: Record<string, ModelGroup> = {}
    const allModels = [...availableModels.value]
    if (form.value.model && !allModels.some(m => m.name === form.value.model)) {
      allModels.unshift({ name: form.value.model })
    }
    for (const m of allModels) {
      if (search && !m.name.toLowerCase().includes(search)) continue
      const { provider, icon, color, family } = detectProvider(m.name, m.source)
      if (!groups[provider]) groups[provider] = { provider, icon, color, models: [] }
      groups[provider].models.push({ ...m, _family: family })
    }
    const order: Record<string, number> = { Anthropic: 0, OpenAI: 1, Google: 2, OpenRouter: 3, Ollama: 4, 'Built-in': 5, Embeddings: 6 }
    return Object.values(groups).sort((a, b) => {
      const oa = order[a.provider] ?? 3
      const ob = order[b.provider] ?? 3
      return oa !== ob ? oa - ob : a.provider.localeCompare(b.provider)
    })
  })

  function selectModel(name: string) {
    form.value.model = name
    markDirty()
    showModelPicker.value = false
    modelSearch.value = ''
  }

  watch(showModelPicker, (v) => {
    if (v) nextTick(() => modelSearchInput.value?.focus())
  })

  function markDirty() { dirty.value = true }

  const memoryModes: { value: MemoryMode; label: string; description: string; behaviors: string[] }[] = [
    {
      value: 'passive',
      label: 'Passive',
      description: 'Uses memory only when you explicitly ask. Minimal footprint — good for focused single-task agents.',
      behaviors: [
        'Recalls only when you say "recall" or "what do you remember"',
        'Stores only when you say "remember this"',
        'Extracts entities from what you ask it to store',
        'No automatic memory activity between requests',
      ],
    },
    {
      value: 'conversational',
      label: 'Conversational',
      description: 'Proactively recalls at session start, writes new learnings, links related memories, and signals helpful/unhelpful recalls. The balanced default.',
      behaviors: [
        'Recalls context at the start of every conversation',
        'Re-recalls when the topic shifts significantly',
        'Stores facts, decisions, preferences, and project context',
        'Uses batch writes when multiple topics are covered',
        'Extracts entities and builds knowledge graph relationships',
        'Links related memories with typed relationships (supports, depends_on, contradicts…)',
        'Records decisions with rationale and alternatives via muninn_decide',
        'Evolves stale memories instead of creating duplicates',
        'Signals helpful/unhelpful recalls to improve recall quality over time',
      ],
    },
    {
      value: 'immersive',
      label: 'Immersive',
      description: 'Full knowledge-graph stewardship. Orients at every session start, recalls before every action, maintains lifecycle, and continuously improves recall quality.',
      behaviors: [
        'Calls "where did we leave off?" at every session start',
        'Recalls before every significant decision or action',
        'Uses deep, causal, and adversarial recall modes for complex topics',
        'Stores every fact, decision, observation, and preference atomically',
        'Always extracts entities and entity relationships at write time',
        'Links memories proactively; surfaces contradictions to you',
        'Evolves changed facts with a reason — no duplicates',
        'Consolidates fragmented memories on the same topic',
        'Records decisions with rationale, alternatives, and supporting memory IDs',
        'Tracks goal and task lifecycle (active → completed → blocked…)',
        'Stores hierarchical knowledge as memory trees (plans, specs, breakdowns)',
        'Continuous feedback loop on every recalled memory improves scoring over time',
      ],
    },
  ]

  async function loadMuninnInfo() {
    try {
      const statusData = await api.muninn.status()
      muninnConnected.value = statusData.connected
      muninnEndpoint.value = statusData.connected ? `${statusData.username} @ ${statusData.endpoint}` : ''
      const vaultData = await api.muninn.vaults()
      existingVaults.value = (vaultData.vaults || []) as unknown as VaultItem[]
    } catch {
    }
  }

  async function pollVaultHealth() {
    const name = agentName.value
    if (!name || name === 'new') {
      vaultHealth.value = { status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 }
      return
    }
    try {
      const resp = await fetch(`/api/v1/agents/${encodeURIComponent(name)}/vault-status`, {
        headers: { Authorization: `Bearer ${getToken()}` },
      })
      if (resp.ok) {
        const data = await resp.json() as VaultHealth
        vaultHealth.value = data
      }
    } catch {
    }
  }

  function startVaultHealthPolling() {
    stopVaultHealthPolling()
    pollVaultHealth()
    vaultHealthInterval = setInterval(pollVaultHealth, 60_000)
  }

  function stopVaultHealthPolling() {
    if (vaultHealthInterval !== null) {
      clearInterval(vaultHealthInterval)
      vaultHealthInterval = null
    }
    vaultHealth.value = { status: 'unknown', tools_count: 0, warning: '', latency_ms: 0 }
  }

  function connectionLabel(id: string): string {
    if (id.startsWith('system:')) {
      return id.slice('system:'.length) + ' (CLI)'
    }
    const c = availableConnections.value.find(c => c.id === id)
    return c ? (c.account_label || c.provider) : id
  }

  function connectionIcon(connId: string): { bg: string; fg: string; label: string } {
    if (connId.startsWith('system:')) {
      const name = connId.slice('system:'.length)
      if (name === 'github') return { bg: 'rgba(240,246,252,0.09)', fg: 'rgba(240,246,252,0.75)', label: 'GH' }
      if (name === 'aws') return { bg: 'rgba(255,153,0,0.18)', fg: '#FF9900', label: 'AW' }
      if (name === 'gcloud') return { bg: 'rgba(66,133,244,0.18)', fg: '#4285F4', label: 'GC' }
      const n = name.slice(0, 2).toUpperCase()
      return { bg: 'rgba(100,116,139,0.18)', fg: 'rgba(148,163,184,0.85)', label: n }
    }
    const c = availableConnections.value.find(conn => conn.id === connId)
    const provider = (c?.provider || connId).toLowerCase()
    if (provider.includes('google')) return { bg: 'rgba(66,133,244,0.18)', fg: '#4285F4', label: 'G' }
    if (provider.includes('github')) return { bg: 'rgba(240,246,252,0.09)', fg: 'rgba(240,246,252,0.75)', label: 'GH' }
    if (provider.includes('slack')) return { bg: 'rgba(74,21,75,0.35)', fg: '#C4B5FD', label: 'SL' }
    if (provider.includes('linear')) return { bg: 'rgba(94,106,210,0.22)', fg: '#818CF8', label: 'LN' }
    if (provider.includes('notion')) return { bg: 'rgba(255,255,255,0.09)', fg: 'rgba(255,255,255,0.75)', label: 'N' }
    const chars = ((c?.account_label || c?.provider || connId).slice(0, 2)).toUpperCase()
    return { bg: 'rgba(88,166,255,0.15)', fg: '#58a6ff', label: chars }
  }

  function systemProviderName(toolName: string): string {
    const providerMap: Record<string, string> = { github: 'github_cli', aws: 'aws', gcloud: 'gcloud' }
    return providerMap[toolName] || toolName
  }

  const allAssignableToolbeltEntries = computed<ToolbeltEntry[]>(() => {
    const entries: ToolbeltEntry[] = []

    for (const conn of availableConnections.value) {
      if (!capabilityMatrix.isAssignableConnection(conn)) continue
      entries.push({
        connection_id: conn.id,
        provider: conn.provider,
        approval_gate: false,
      })
    }
    for (const tool of systemTools.value) {
      const provider = systemProviderName(tool.name)
      if (tool.profiles && tool.profiles.length > 1) {
        for (const profile of tool.profiles) {
          entries.push({
            connection_id: 'system:' + tool.name,
            provider,
            profile,
            approval_gate: false,
          })
        }
      } else {
        entries.push({
          connection_id: 'system:' + tool.name,
          provider,
          profile: tool.profiles?.[0] || undefined,
          approval_gate: false,
        })
      }
    }
    return entries
  })

  function normalizedToolbelt(entries: ToolbeltEntry[]): string {
    return JSON.stringify(
      [...entries]
        .map(entry => ({
          connection_id: entry.connection_id,
          provider: entry.provider,
          profile: entry.profile || '',
          approval_gate: !!entry.approval_gate,
        }))
        .sort((a, b) => {
          const aKey = `${a.connection_id}::${a.profile}`
          const bKey = `${b.connection_id}::${b.profile}`
          return aKey.localeCompare(bKey)
        }),
    )
  }

  const connectionValidationIssues = computed(() =>
    form.value.toolbelt
      .map(entry => ({
        entry,
        reason: capabilityMatrix.entryReason(entry),
      }))
      .filter(issue => !!issue.reason),
  )

  const modalConnectionValidationIssues = computed(() =>
    modalToolbelt.value
      .map(entry => ({
        entry,
        reason: capabilityMatrix.entryReason(entry),
      }))
      .filter(issue => !!issue.reason),
  )

  function modalEntryIssueReason(entry: ToolbeltEntry): string | null {
    const issue = modalConnectionValidationIssues.value.find(
      i => i.entry.connection_id === entry.connection_id && (i.entry.profile || '') === (entry.profile || ''),
    )
    return issue?.reason || null
  }

  async function loadConnections() {
    loadError.value = false
    loadErrorMsg.value = ''
    try {
      const [conns, tools] = await Promise.all([
        api.connections.list() as Promise<Connection[]>,
        api.system.tools(),
      ])
      availableConnections.value = conns
      systemTools.value = tools.filter(t => t.authed)
      await capabilityMatrix.refreshMatrix()
    } catch (e) {
      console.error('loadConnections failed:', e)
      loadError.value = true
      loadErrorMsg.value = 'Failed to load connections. Please refresh.'
    }
  }

  const availableSkills = ref<{ name: string }[]>([])

  async function loadAvailableSkills() {
    try {
      const { skills, load } = useInstalledSkills()
      await load()
      availableSkills.value = skills.value.map(s => ({ name: s.name }))
    } catch {
    }
  }

  function openConnectionsModal() {
    modalToolbelt.value = JSON.parse(JSON.stringify(form.value.toolbelt))
    capabilityMatrix.resetValidation()
    showConnectionsModal.value = true
  }

  async function saveConnectionsModal() {
    const valid = await capabilityMatrix.validateToolbelt(modalToolbelt.value)
    if (!valid || capabilityMatrix.hasIssues(modalToolbelt.value)) {
      saveMsg.value = capabilityMatrix.firstReason(modalToolbelt.value) || 'Invalid connection assignment.'
      saveError.value = true
      return
    }
    form.value.toolbelt = JSON.parse(JSON.stringify(modalToolbelt.value))
    showConnectionsModal.value = false
    if (agentName.value && agentName.value !== 'new') {
      await save()
    } else {
      markDirty()
    }
  }

  const modalAddableConnections = computed(() =>
    availableConnections.value.filter(c =>
      capabilityMatrix.isAssignableConnection(c) &&
      !modalToolbelt.value.some(e => e.connection_id === c.id),
    ),
  )

  const modalAddableSystemToolsForModal = computed(() =>
    systemTools.value.filter(t => {
      const connId = 'system:' + t.name
      if (t.profiles && t.profiles.length > 1) {
        return !t.profiles.every(p => modalToolbelt.value.some(e => e.connection_id === connId && e.profile === p))
      }
      return !modalToolbelt.value.some(e => e.connection_id === connId)
    }),
  )

  function modalIsProfileAdded(tool: SystemToolStatus, profile: string): boolean {
    return modalToolbelt.value.some(e => e.connection_id === 'system:' + tool.name && e.profile === profile)
  }

  function modalAddConnection(conn: Connection) {
    modalToolbelt.value.push({ connection_id: conn.id, provider: conn.provider, approval_gate: false })
  }

  function modalAddSystemTool(tool: SystemToolStatus, profile: string) {
    const provider = systemProviderName(tool.name)
    modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: profile || undefined, approval_gate: false })
  }

  function modalRemoveEntry(idx: number) {
    modalToolbelt.value.splice(idx, 1)
  }

  function modalRemoveAll() {
    modalToolbelt.value = []
  }

  function modalAddAll() {
    modalAddableConnections.value.forEach(conn => {
      modalToolbelt.value.push({ connection_id: conn.id, provider: conn.provider, approval_gate: false })
    })
    modalAddableSystemToolsForModal.value.forEach(tool => {
      const provider = systemProviderName(tool.name)
      if (tool.profiles && tool.profiles.length > 1) {
        tool.profiles.forEach(p => {
          if (!modalIsProfileAdded(tool, p)) {
            modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: p, approval_gate: false })
          }
        })
      } else {
        const profile = tool.profiles?.[0] || ''
        modalToolbelt.value.push({ connection_id: 'system:' + tool.name, provider, profile: profile || undefined, approval_gate: false })
      }
    })
  }

  function modalToggleApprovalGate(idx: number) {
    const entry = modalToolbelt.value[idx]
    if (entry) entry.approval_gate = !entry.approval_gate
  }

  function openSkillsModal() {
    modalSkills.value = [...form.value.skills]
    showSkillsModal.value = true
  }

  async function saveSkillsModal() {
    form.value.skills = [...modalSkills.value]
    showSkillsModal.value = false
    if (agentName.value && agentName.value !== 'new') {
      await save()
    } else {
      markDirty()
    }
  }

  const modalAddableSkills = computed(() =>
    availableSkills.value.filter(s => !modalSkills.value.includes(s.name)),
  )

  function modalAddSkill(name: string) {
    if (!modalSkills.value.includes(name)) modalSkills.value.push(name)
  }

  function modalRemoveSkill(idx: number) {
    modalSkills.value.splice(idx, 1)
  }

  function addAllSkills() {
    const toAdd = modalAddableSkills.value.map(s => s.name)
    modalSkills.value = [...modalSkills.value, ...toAdd]
  }

  function clearAllSkills() {
    modalSkills.value = []
  }

  const isLocalAllowAll = computed(() =>
    form.value.local_tools.length === 1 && form.value.local_tools[0] === '*',
  )

  const localAccessSummary = computed(() => {
    if (!form.value.local_tools.length) return 'none'
    if (isLocalAllowAll.value) return 'all (including shell ⚡)'
    return form.value.local_tools.join(' · ')
  })

  function toggleLocalAllowAll() {
    if (isLocalAllowAll.value) {
      form.value.local_tools = []
    } else {
      form.value.local_tools = ['*']
    }
    markDirty()
  }

  const showLocalAccessModal = ref(false)
  const modalLocalTools = ref<string[]>([])
  const hoveredGrantedIdx = ref(-1)
  const hoveredAvailableName = ref('')
  const hoveredAvailableConn = ref('')
  const hoveredAssignedIdx = ref(-1)

  function openLocalAccessModal() {
    modalLocalTools.value = [...form.value.local_tools.filter(n => n !== '*')]
    showLocalAccessModal.value = true
  }

  async function saveLocalAccessModal() {
    form.value.local_tools = [...modalLocalTools.value]
    showLocalAccessModal.value = false
    if (agentName.value && agentName.value !== 'new') {
      await save()
    } else {
      markDirty()
    }
  }

  function localModalGrant(name: string) {
    if (!modalLocalTools.value.includes(name)) {
      modalLocalTools.value.push(name)
    }
  }

  function localModalGrantAll() {
    const all = [
      ...LOCAL_TOOL_CATALOG.flatMap(cat => cat.tools.map(t => t.name)),
      ...SHELL_TOOLS.map(t => t.name),
    ]
    for (const name of all) {
      if (!modalLocalTools.value.includes(name)) {
        modalLocalTools.value.push(name)
      }
    }
  }

  function isShellTool(name: string) { return SHELL_TOOL_NAMES.has(name) }

  function toolLabel(name: string): string {
    for (const cat of LOCAL_TOOL_CATALOG) {
      const found = cat.tools.find((t) => t.name === name)
      if (found) return found.label
    }
    const shell = SHELL_TOOLS.find((t) => t.name === name)
    return shell?.label ?? name
  }

  function toolDescription(name: string): string {
    for (const cat of LOCAL_TOOL_CATALOG) {
      const found = cat.tools.find((t) => t.name === name)
      if (found) return found.description
    }
    const shell = SHELL_TOOLS.find((t) => t.name === name)
    return shell?.description ?? ''
  }

  const isConnectionsAllowAll = computed(() =>
    normalizedToolbelt(form.value.toolbelt) === normalizedToolbelt(allAssignableToolbeltEntries.value) &&
    allAssignableToolbeltEntries.value.length > 0,
  )

  const connectionsSummary = computed(() => {
    if (!form.value.toolbelt.length) return 'none'
    if (isConnectionsAllowAll.value) return 'all connections'
    return form.value.toolbelt.length + ' attached'
  })

  function toggleConnectionsAllowAll() {
    if (isConnectionsAllowAll.value) {
      form.value.toolbelt = []
    } else {
      form.value.toolbelt = JSON.parse(JSON.stringify(allAssignableToolbeltEntries.value))
    }
    markDirty()
  }

  async function ensureVault() {
    if (form.value.memory_type !== 'muninndb' || !form.value.vault_name) return
    if (linkedVaultNames.value.includes(form.value.vault_name)) return
    await api.muninn.createVault({ vault_name: form.value.vault_name, agent_label: `huginn-${form.value.name}` })
  }

  function deriveMemoryType(data: any): MemoryType {
    if (data.memory_type) {
      if (data.memory_type === 'notes') return 'context'
      return data.memory_type as MemoryType
    }
    if (data.context_notes_enabled) return 'context'
    return data.memory_enabled ? 'muninndb' : 'none'
  }

  async function loadAgent(name: string) {
    try {
      const data = await api.agents.get(name) as unknown as AgentForm
      const memType = deriveMemoryType(data)
      form.value = {
        name: data.name || name,
        model: data.model || '',
        system_prompt: data.system_prompt || '',
        color: (data as AgentForm & { color?: string }).color || '#58a6ff',
        icon: (data as AgentForm & { icon?: string }).icon || '',
        memory_type: memType,
        memory_enabled: memType === 'muninndb',
        context_notes_enabled: !!(data as any).context_notes_enabled,
        vault_name: (data as any).vault_name || '',
        memory_mode: (data.memory_mode as MemoryMode) || 'conversational',
        vault_description: (data as any).vault_description || '',
        toolbelt: (data as any).toolbelt || [],
        skills: (data as any).skills || [],
        local_tools: (data as any).local_tools ?? [],
        heartbeat_enabled: (data as any).heartbeat_enabled ?? false,
        heartbeat_cron: (data as any).heartbeat_cron ?? '',
      }
      original.value = JSON.stringify(form.value)
      dirty.value = false
      saveMsg.value = ''
    } catch (e) {
      console.error('Failed to load agent', e)
    }
  }

  async function loadAvailableModels() {
    modelsLoading.value = true
    modelsError.value = ''
    try {
      const data = await api.models.available() as { models?: OllamaModel[]; builtin_models?: OllamaModel[]; provider_models?: OllamaModel[]; error?: string }
      if (data.error && !data.models?.length && !data.builtin_models?.length) {
        modelsError.value = 'Ollama not reachable'
      }
      const ollamaModels = (data.models ?? []) as OllamaModel[]
      const builtinModels = ((data.builtin_models ?? []) as OllamaModel[]).map(m => ({ ...m, source: 'built-in' }))
      const providerModels = (data.provider_models ?? []) as OllamaModel[]
      availableModels.value = [...providerModels, ...ollamaModels, ...builtinModels]
    } catch {
      modelsError.value = 'Ollama not reachable'
      availableModels.value = []
    } finally {
      modelsLoading.value = false
    }
  }

  function validateAgentForm(): string | null {
    if (!form.value.name?.trim()) return 'Agent name is required'
    if (/[/\\\0:]/.test(form.value.name) || /[\x00-\x1f]/.test(form.value.name)) {
      return 'Agent name contains invalid characters'
    }
    if (!form.value.model?.trim()) return 'A model is required — select one from the model picker'
    return null
  }

  async function save() {
    const validationError = validateAgentForm()
    if (validationError) {
      saveMsg.value = validationError
      saveError.value = true
      return
    }
    const toolbeltValid = await capabilityMatrix.validateToolbelt(form.value.toolbelt)
    if (!toolbeltValid || capabilityMatrix.hasIssues(form.value.toolbelt)) {
      saveMsg.value = capabilityMatrix.firstReason(form.value.toolbelt) || 'Invalid connection assignment.'
      saveError.value = true
      return
    }
    saving.value = true
    saveMsg.value = ''
    saveError.value = false
    try {
      await ensureVault()
      const originalName = (agentName.value && agentName.value !== 'new') ? agentName.value : form.value.name
      await api.agents.update(originalName, form.value)
      updateAgent(form.value.name, { ...form.value })
      if (agentName.value && form.value.name !== agentName.value) {
        removeFromList(agentName.value)
        router.replace(`/agents/${form.value.name}`)
      }
      original.value = JSON.stringify(form.value)
      dirty.value = false
      saveMsg.value = 'Saved successfully'
      setTimeout(() => { saveMsg.value = '' }, 3000)
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      if (msg.includes('409') || msg.toLowerCase().includes('conflict')) {
        saveMsg.value = 'Agent was modified by another client — please reload'
      } else {
        saveMsg.value = msg
      }
      saveError.value = true
    } finally {
      saving.value = false
    }
  }

  function discard() {
    form.value = JSON.parse(original.value)
    dirty.value = false
  }

  function confirmDelete() { showDeleteConfirm.value = true }

  async function deleteAgent() {
    try {
      const resp = await fetch(`/api/v1/agents/${form.value.name}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${getToken()}` },
      })
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({ error: 'Delete failed' }))
        saveMsg.value = body.error || `Delete failed (${resp.status})`
        saveError.value = true
        showDeleteConfirm.value = false
        return
      }
      removeFromList(form.value.name)
      router.push('/agents')
    } catch (e: unknown) {
      saveMsg.value = e instanceof Error ? e.message : 'Network error'
      saveError.value = true
      showDeleteConfirm.value = false
    }
  }

  function createNew() {
    router.push('/agents/new')
  }

  watch(agentName, (name) => {
    showDeleteConfirm.value = false
    if (name && name !== 'new') {
      loadAgent(name)
      startVaultHealthPolling()
    } else {
      stopVaultHealthPolling()
      if (name === 'new') {
        form.value = {
          name: '',
          model: '',
          system_prompt: '',
          color: '#58a6ff',
          icon: '',
          memory_type: 'none',
          memory_enabled: false,
          context_notes_enabled: false,
          vault_name: '',
          memory_mode: 'conversational',
          vault_description: '',
          toolbelt: [],
          skills: [],
          local_tools: [],
          heartbeat_enabled: false,
          heartbeat_cron: '',
        }
        original.value = ''
        dirty.value = true
      }
    }
  }, { immediate: true })

  onMounted(() => {
    loadAvailableModels()
    loadMuninnInfo()
    loadConnections()
    loadAvailableSkills()
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('focus', onWindowFocus)
  })

  onBeforeUnmount(() => {
    if (vaultCheckTimeout.value) clearTimeout(vaultCheckTimeout.value)
    stopVaultHealthPolling()
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('focus', onWindowFocus)
    if (staleDebounceTimer !== null) {
      clearTimeout(staleDebounceTimer)
      staleDebounceTimer = null
    }
  })

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    if (showConnectionsModal.value) { showConnectionsModal.value = false; return }
    if (showSkillsModal.value) { showSkillsModal.value = false; return }
    if (memoryModal.value.open) { cancelMemoryModal(); return }
    if (showModelPicker.value) { showModelPicker.value = false; modelSearch.value = '' }
  }
  onMounted(() => window.addEventListener('keydown', onKeydown))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

  return {
    agents,
    loading,
    openDM,
    isStaleRefreshing,
    form,
    original,
    dirty,
    saving,
    saveMsg,
    saveError,
    loadError,
    loadErrorMsg,
    showDeleteConfirm,
    availableModels,
    modelsLoading,
    modelsError,
    showModelPicker,
    modelSearch,
    modelSearchInput,
    muninnConnected,
    muninnEndpoint,
    existingVaults,
    linkedVaultNames,
    allVaultNames,
    vaultCheckTimeout,
    vaultHealth,
    memoryModal,
    previousMemoryType,
    availableConnections,
    systemTools,
    showConnectionsModal,
    modalToolbelt,
    showSkillsModal,
    modalSkills,
    colorPalette,
    filteredModelGroups,
    memoryModes,
    availableSkills,
    modalAddableConnections,
    modalAddableSystemToolsForModal,
    modalAddableSkills,
    connectionValidationIssues,
    modalConnectionValidationIssues,
    modalEntryIssueReason,
    isLocalAllowAll,
    localAccessSummary,
    showLocalAccessModal,
    modalLocalTools,
    hoveredGrantedIdx,
    hoveredAvailableName,
    hoveredAvailableConn,
    hoveredAssignedIdx,
    isConnectionsAllowAll,
    connectionsSummary,
    LOCAL_TOOL_CATALOG,
    SHELL_TOOLS,
    selectMuninnDB,
    cancelMemoryModal,
    openMemoryModal,
    saveMemoryModal,
    detectProvider,
    selectModel,
    markDirty,
    loadMuninnInfo,
    pollVaultHealth,
    startVaultHealthPolling,
    stopVaultHealthPolling,
    connectionLabel,
    connectionIcon,
    loadConnections,
    loadAvailableSkills,
    openConnectionsModal,
    saveConnectionsModal,
    modalIsProfileAdded,
    modalAddConnection,
    modalAddSystemTool,
    modalRemoveEntry,
    modalRemoveAll,
    modalAddAll,
    modalToggleApprovalGate,
    openSkillsModal,
    saveSkillsModal,
    modalAddSkill,
    modalRemoveSkill,
    addAllSkills,
    clearAllSkills,
    toggleLocalAllowAll,
    openLocalAccessModal,
    saveLocalAccessModal,
    localModalGrant,
    localModalGrantAll,
    isShellTool,
    toolLabel,
    toolDescription,
    toggleConnectionsAllowAll,
    capabilityMatrix,
    ensureVault,
    deriveMemoryType,
    loadAgent,
    loadAvailableModels,
    validateAgentForm,
    save,
    discard,
    confirmDelete,
    deleteAgent,
    createNew,
    onVisibilityChange,
    onWindowFocus,
    onKeydown,
  }
}
