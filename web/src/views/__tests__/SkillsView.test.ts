import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockInstalledSkills = ref<any[]>([])
const mockInstalledLoading = ref(false)
const mockInstalledError = ref<string | null>(null)
const mockInstalledLoad = vi.fn()
const mockInstalledToggleEnabled = vi.fn().mockResolvedValue(undefined)
const mockInstalledUninstall = vi.fn().mockResolvedValue(undefined)

const mockRegistryIndex = ref<any[]>([])
const mockRegistryCollections = ref<any[]>([])
const mockRegistryLoading = ref(false)
const mockRegistryError = ref<string | null>(null)
const mockRegistryLoad = vi.fn()
const mockRegistryInstall = vi.fn().mockResolvedValue(undefined)
const mockRegistryIsInstalling = vi.fn().mockReturnValue(false)

vi.mock('../../composables/useSkills', () => ({
  useInstalledSkills: () => ({
    skills: mockInstalledSkills,
    loading: mockInstalledLoading,
    error: mockInstalledError,
    load: mockInstalledLoad,
    toggleEnabled: mockInstalledToggleEnabled,
    uninstall: mockInstalledUninstall,
  }),
  useRegistrySkills: () => ({
    index: mockRegistryIndex,
    collections: mockRegistryCollections,
    loading: mockRegistryLoading,
    error: mockRegistryError,
    load: mockRegistryLoad,
    install: mockRegistryInstall,
    isInstalling: mockRegistryIsInstalling,
    installCollection: vi.fn().mockResolvedValue(undefined),
  }),
  createSkill: vi.fn().mockResolvedValue('new-skill'),
}))

const mockApiAgentsList = vi.fn().mockResolvedValue([])

vi.mock('../../composables/useApi', () => ({
  api: {
    agents: {
      list: (...args: unknown[]) => mockApiAgentsList(...args),
      get: vi.fn().mockResolvedValue({ name: 'agent1', skills: [] }),
      update: vi.fn().mockResolvedValue({}),
    },
    skills: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {}, query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

import SkillsView from '../SkillsView.vue'

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView(props: Record<string, unknown> = {}) {
  return shallowMount(SkillsView, {
    props,
    global: {
      stubs: {
        Teleport: true,
        Transition: true,
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
}

const sampleSkill = {
  name: 'code-review',
  author: 'huginn',
  source: 'registry',
  enabled: true,
  tool_count: 2,
  version: '1.0.0',
}

const disabledSkill = {
  name: 'legacy-tool',
  author: 'community',
  source: 'local',
  enabled: false,
  tool_count: 0,
  version: '0.5.0',
}

beforeEach(() => {
  mockInstalledSkills.value = [sampleSkill, disabledSkill]
  mockInstalledLoading.value = false
  mockInstalledError.value = null
  mockRegistryIndex.value = []
  mockRegistryCollections.value = []
  mockRegistryLoading.value = false
  mockRegistryError.value = null
  mockInstalledLoad.mockReset()
  mockInstalledUninstall.mockReset().mockResolvedValue(undefined)
  mockInstalledToggleEnabled.mockReset().mockResolvedValue(undefined)
  mockApiAgentsList.mockReset().mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SkillsView (installed tab)', () => {
  it('renders the Installed Skills heading', async () => {
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('Installed Skills')
  })

  it('renders skill names from the installed list', async () => {
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('code-review')
    expect(w.text()).toContain('legacy-tool')
  })

  it('shows empty state when no skills are installed', async () => {
    mockInstalledSkills.value = []
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('No skills installed yet')
  })

  it('shows loading spinner while skills are loading', async () => {
    mockInstalledSkills.value = []
    mockInstalledLoading.value = true
    const w = mountView()
    await nextTick()
    // Loading state is rendered — the spinner div is present; text won't show skill list
    expect(w.text()).not.toContain('No skills installed yet')
  })

  it('shows error message when installed.error is set', async () => {
    mockInstalledSkills.value = []
    mockInstalledError.value = 'Failed to load skills'
    const w = mountView()
    await nextTick()
    expect(w.text()).toContain('Failed to load skills')
  })

  it('filters installed skills by search query', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.installedQuery = 'code'
    await nextTick()
    expect(vm.filteredInstalled.some((s: any) => s.name === 'code-review')).toBe(true)
    expect(vm.filteredInstalled.some((s: any) => s.name === 'legacy-tool')).toBe(false)
  })

  it('shows "No skills match" when filter produces no results', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.installedQuery = 'zzz_no_match_zzz'
    await nextTick()
    expect(w.text()).toContain('No skills match')
  })

  it('calls installed.load and loadAgents on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockInstalledLoad).toHaveBeenCalled()
    expect(mockApiAgentsList).toHaveBeenCalled()
  })

  it('confirmUninstall sets pendingUninstall to the skill name', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.confirmUninstall('code-review')
    await nextTick()
    expect(vm.pendingUninstall).toBe('code-review')
  })

  it('doUninstall calls installed.uninstall with the pending skill name', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingUninstall = 'code-review'
    await vm.doUninstall()
    await flushPromises()
    expect(mockInstalledUninstall).toHaveBeenCalledWith('code-review')
  })

  it('doUninstall clears pendingUninstall after deletion', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingUninstall = 'code-review'
    await vm.doUninstall()
    await flushPromises()
    expect(vm.pendingUninstall).toBeNull()
  })

  it('doUninstall is a no-op when pendingUninstall is null', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.pendingUninstall = null
    await vm.doUninstall()
    expect(mockInstalledUninstall).not.toHaveBeenCalled()
  })

  it('toggleSkill calls installed.toggleEnabled with the correct args', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    await vm.toggleSkill('code-review', false)
    await flushPromises()
    expect(mockInstalledToggleEnabled).toHaveBeenCalledWith('code-review', false)
  })

  it('toggleSkill sets actionError on failure', async () => {
    mockInstalledToggleEnabled.mockRejectedValueOnce(new Error('Toggle failed'))
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    await vm.toggleSkill('code-review', false)
    await flushPromises()
    expect(vm.actionError).toContain('Toggle failed')
  })

  it('openUsageModal sets showUsageModal and usageModalSkill', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.openUsageModal(sampleSkill)
    await nextTick()
    expect(vm.showUsageModal).toBe(true)
    expect(vm.usageModalSkill).toEqual(sampleSkill)
  })

  it('closeUsageModal clears showUsageModal and usageModalSkill', async () => {
    const w = mountView()
    await nextTick()
    const vm = w.vm as any
    vm.openUsageModal(sampleSkill)
    await nextTick()
    vm.closeUsageModal()
    await nextTick()
    expect(vm.showUsageModal).toBe(false)
    expect(vm.usageModalSkill).toBeNull()
  })
})

describe('SkillsView (browse tab)', () => {
  it('renders the Skills Marketplace heading in browse tab', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    expect(w.text()).toContain('Skills Marketplace')
  })

  it('calls registry.load on mount when tab is browse', async () => {
    mountView({ tab: 'browse' })
    await flushPromises()
    expect(mockRegistryLoad).toHaveBeenCalled()
  })

  it('shows loading indicator while registry loads', async () => {
    mockRegistryLoading.value = true
    const w = mountView({ tab: 'browse' })
    await nextTick()
    // Loading div is rendered; the skills grid should not appear
    expect(w.text()).not.toContain('Individual Skills')
  })

  it('shows registry error message when registry.error is set', async () => {
    mockRegistryError.value = 'Registry unavailable'
    const w = mountView({ tab: 'browse' })
    await nextTick()
    expect(w.text()).toContain('Registry unavailable')
  })

  it('requestInstall sets pendingInstall to the selected registry skill', async () => {
    const registrySkill = {
      id: 'rs-1', name: 'code-review', display_name: 'Code Review', description: 'Reviews code',
      author: 'huginn', category: 'testing', tags: [], source_url: '', collection: '',
    }
    mockRegistryIndex.value = [registrySkill]
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.requestInstall(registrySkill)
    await nextTick()
    expect(vm.pendingInstall).toEqual(registrySkill)
  })
})

describe('SkillsView (create tab)', () => {
  it('renders Create Skill heading', async () => {
    const w = mountView({ tab: 'create' })
    await nextTick()
    expect(w.text()).toContain('Create Skill')
  })

  it('renders the textarea for skill content', async () => {
    const w = mountView({ tab: 'create' })
    await nextTick()
    expect(w.find('textarea').exists()).toBe(true)
  })
})

// ── Additional browse tab tests ────────────────────────────────────────────

const sampleRegistrySkill = {
  id: 'rs-code-review',
  name: 'code-review',
  display_name: 'Code Review',
  description: 'Automated code review assistant',
  author: 'huginn',
  category: 'testing',
  tags: ['git', 'review'],
  source_url: 'https://example.com',
  collection: '',
  version: '1.0.0',
}

const sampleRegistrySkill2 = {
  id: 'rs-deploy',
  name: 'deploy',
  display_name: 'Deploy',
  description: 'Deployment helper for cloud infra',
  author: 'community',
  category: 'devops',
  tags: ['cloud'],
  source_url: '',
  collection: '',
  version: '0.9.0',
}

describe('SkillsView (browse tab — search filtering)', () => {
  beforeEach(() => {
    mockRegistryIndex.value = [sampleRegistrySkill, sampleRegistrySkill2]
  })

  it('filteredRegistry shows matching skills when browseQuery matches name', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.browseQuery = 'code'
    await nextTick()
    expect(vm.filteredRegistry.some((s: any) => s.name === 'code-review')).toBe(true)
    expect(vm.filteredRegistry.some((s: any) => s.name === 'deploy')).toBe(false)
  })

  it('filteredRegistry shows matching skills when browseQuery matches description', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.browseQuery = 'cloud infra'
    await nextTick()
    expect(vm.filteredRegistry.some((s: any) => s.name === 'deploy')).toBe(true)
    expect(vm.filteredRegistry.some((s: any) => s.name === 'code-review')).toBe(false)
  })

  it('shows empty-state text when browseQuery matches no skills', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.browseQuery = 'zzz_no_match_zzz'
    await nextTick()
    expect(vm.filteredRegistry).toHaveLength(0)
    expect(w.text()).toContain('No results')
  })

  it('registryCategories computed includes categories from index', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    expect(vm.registryCategories).toContain('testing')
    expect(vm.registryCategories).toContain('devops')
  })

  it('setting categoryFilter limits filteredRegistry to that category', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.categoryFilter = 'devops'
    await nextTick()
    expect(vm.filteredRegistry.every((s: any) => s.category === 'devops')).toBe(true)
    expect(vm.filteredRegistry.some((s: any) => s.name === 'deploy')).toBe(true)
    expect(vm.filteredRegistry.some((s: any) => s.name === 'code-review')).toBe(false)
  })

  it('combined browseQuery + categoryFilter narrows results correctly', async () => {
    const extra = {
      id: 'rs-extra',
      name: 'cloud-deploy',
      display_name: 'Cloud Deploy',
      description: 'Another cloud tool',
      author: 'community',
      category: 'devops',
      tags: [],
      source_url: '',
      collection: '',
    }
    mockRegistryIndex.value = [sampleRegistrySkill, sampleRegistrySkill2, extra]
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.categoryFilter = 'devops'
    vm.browseQuery = 'deploy'
    await nextTick()
    // Only 'deploy' name matches "deploy" + devops category — cloud-deploy also matches
    expect(vm.filteredRegistry.every((s: any) => s.category === 'devops')).toBe(true)
    expect(vm.filteredRegistry.some((s: any) => s.name === 'code-review')).toBe(false)
  })
})

describe('SkillsView (browse tab — install flow)', () => {
  beforeEach(() => {
    mockRegistryIndex.value = [sampleRegistrySkill, sampleRegistrySkill2]
    mockRegistryInstall.mockReset().mockResolvedValue(undefined)
    mockInstalledLoad.mockReset()
  })

  it('clicking requestInstall sets pendingInstall to that skill', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.requestInstall(sampleRegistrySkill)
    await nextTick()
    expect(vm.pendingInstall).toEqual(sampleRegistrySkill)
  })

  it('confirmInstall calls registry.install with pendingInstall name', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.pendingInstall = sampleRegistrySkill
    await vm.confirmInstall()
    await flushPromises()
    expect(mockRegistryInstall).toHaveBeenCalledWith('code-review')
  })

  it('confirmInstall calls installed.load after install to refresh list', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.pendingInstall = sampleRegistrySkill
    await vm.confirmInstall()
    await flushPromises()
    expect(mockInstalledLoad).toHaveBeenCalled()
  })

  it('confirmInstall clears pendingInstall after success', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.pendingInstall = sampleRegistrySkill
    await vm.confirmInstall()
    await flushPromises()
    expect(vm.pendingInstall).toBeNull()
  })

  it('shows error state when registry.error is set', async () => {
    mockRegistryError.value = 'Registry unavailable'
    const w = mountView({ tab: 'browse' })
    await nextTick()
    expect(w.text()).toContain('Registry unavailable')
  })

  it('shows loading spinner when registry.loading is true', async () => {
    mockRegistryLoading.value = true
    const w = mountView({ tab: 'browse' })
    await nextTick()
    // When loading, the skills grid is hidden — no "Individual Skills" text
    expect(w.text()).not.toContain('Individual Skills')
  })
})

describe('SkillsView (browse tab — skill selection)', () => {
  beforeEach(() => {
    mockRegistryIndex.value = [sampleRegistrySkill, sampleRegistrySkill2]
  })

  it('selectSkill sets selectedSkillItem to the clicked skill', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.selectSkill(sampleRegistrySkill)
    await nextTick()
    expect(vm.selectedSkillItem).toEqual(sampleRegistrySkill)
  })

  it('selectedSkillItem panel shows selected skill name', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.selectSkill(sampleRegistrySkill)
    await nextTick()
    expect(w.text()).toContain('Code Review')
  })

  it('selectedSkillItem panel shows selected skill description', async () => {
    const w = mountView({ tab: 'browse' })
    await nextTick()
    const vm = w.vm as any
    vm.selectSkill(sampleRegistrySkill)
    await nextTick()
    expect(w.text()).toContain('Automated code review assistant')
  })
})

// Grab the hoisted mock for createSkill — the vi.mock above sets it as vi.fn()
// We import it after the mock is registered so we get the mocked version.
import { createSkill as mockCreateSkill } from '../../composables/useSkills'

describe('SkillsView (create tab — extended)', () => {
  beforeEach(() => {
    vi.mocked(mockCreateSkill).mockReset().mockResolvedValue('new-skill')
  })

  it('saveSkill calls createSkill with the textarea content', async () => {
    vi.mocked(mockCreateSkill).mockResolvedValueOnce('my-skill')
    const w = mountView({ tab: 'create' })
    await nextTick()
    const vm = w.vm as any
    vm.createContent = '# custom skill content'
    await vm.saveSkill()
    await flushPromises()
    expect(mockCreateSkill).toHaveBeenCalledWith('# custom skill content')
  })

  it('shows success message after successful save', async () => {
    vi.mocked(mockCreateSkill).mockResolvedValueOnce('my-new-skill')
    const w = mountView({ tab: 'create' })
    await nextTick()
    const vm = w.vm as any
    vm.createContent = '# valid content'
    await vm.saveSkill()
    await flushPromises()
    expect(vm.createSuccess).toBe('my-new-skill')
  })

  it('shows error message after failed save', async () => {
    vi.mocked(mockCreateSkill).mockRejectedValueOnce(new Error('bad skill yaml'))
    const w = mountView({ tab: 'create' })
    await nextTick()
    const vm = w.vm as any
    vm.createContent = '# invalid'
    await vm.saveSkill()
    await flushPromises()
    expect(vm.createError).toContain('bad skill yaml')
  })

  it('saving is true while saveSkill is in-flight and false after', async () => {
    let resolveFn!: (v: string) => void
    const pending = new Promise<string>(r => { resolveFn = r })
    vi.mocked(mockCreateSkill).mockReturnValueOnce(pending)
    const w = mountView({ tab: 'create' })
    await nextTick()
    const vm = w.vm as any
    vm.createContent = '# skill'
    const savePromise = vm.saveSkill()
    await nextTick()
    expect(vm.saving).toBe(true)
    resolveFn('ok-skill')
    await savePromise
    await flushPromises()
    expect(vm.saving).toBe(false)
  })
})

// Import the api mock reference for agent removal tests
import { api as mockApi } from '../../composables/useApi'

describe('SkillsView (agent skill removal)', () => {
  beforeEach(() => {
    vi.mocked(mockApi.agents.get).mockReset().mockResolvedValue({ name: 'agent1', skills: ['code-review'] })
    vi.mocked(mockApi.agents.update).mockReset().mockResolvedValue({})
  })

  it('removeSkillFromAgent calls api.agents.get and api.agents.update', async () => {
    mockApiAgentsList.mockResolvedValue([{ name: 'agent1', color: '#fff', icon: '', skills: ['code-review'] }])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.openUsageModal(sampleSkill)
    await nextTick()
    await vm.removeSkillFromAgent('agent1')
    await flushPromises()
    expect(mockApi.agents.get).toHaveBeenCalledWith('agent1')
    expect(mockApi.agents.update).toHaveBeenCalled()
  })

  it('removeSkillFromAgent sets removeError on API failure', async () => {
    mockApiAgentsList.mockResolvedValue([{ name: 'agent1', color: '#fff', icon: '', skills: ['code-review'] }])
    vi.mocked(mockApi.agents.get).mockRejectedValueOnce(new Error('network error'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.openUsageModal(sampleSkill)
    await nextTick()
    await vm.removeSkillFromAgent('agent1')
    await flushPromises()
    expect(vm.removeError).toContain('network error')
  })
})

describe('SkillsView (computed: agentsBySkill)', () => {
  it('maps skill names to agents that have them assigned', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'agent-alpha', color: '#fff', icon: '', skills: ['code-review', 'deploy'] },
      { name: 'agent-beta', color: '#aaa', icon: '', skills: ['code-review'] },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const map = vm.agentsBySkill
    expect(map['code-review']).toHaveLength(2)
    expect(map['deploy']).toHaveLength(1)
    expect(map['deploy'][0].name).toBe('agent-alpha')
  })

  it('returns empty map when no agents have skills assigned', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'agent-x', color: '#fff', icon: '', skills: [] },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(Object.keys(vm.agentsBySkill)).toHaveLength(0)
  })
})
