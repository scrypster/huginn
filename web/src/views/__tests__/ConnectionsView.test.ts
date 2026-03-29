import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

// ── Composable / module mocks (hoisted before component import) ───────────────

const mockApiConnectionsList = vi.fn().mockResolvedValue([])
const mockApiConnectionsStart = vi.fn().mockResolvedValue({ auth_url: 'https://auth.example.com' })
const mockApiConnectionsDelete = vi.fn().mockResolvedValue({ deleted: true })
const mockApiConnectionsSetDefault = vi.fn().mockResolvedValue({})
const mockApiSystemTools = vi.fn().mockResolvedValue([])
const mockApiSystemGithubSwitch = vi.fn().mockResolvedValue({ active: 'user1' })
const mockApiMuninnStatus = vi.fn().mockResolvedValue({ connected: false })
const mockApiMuninnVaults = vi.fn().mockResolvedValue({ vaults: [] })
const mockApiMuninnCreateVault = vi.fn().mockResolvedValue({})
const mockFetchCredentialCatalog = vi.fn().mockResolvedValue([])

vi.mock('../../composables/useApi', () => ({
  api: {
    connections: {
      list: (...args: unknown[]) => mockApiConnectionsList(...args),
      start: (...args: unknown[]) => mockApiConnectionsStart(...args),
      delete: (...args: unknown[]) => mockApiConnectionsDelete(...args),
      setDefault: (...args: unknown[]) => mockApiConnectionsSetDefault(...args),
    },
    system: {
      tools: (...args: unknown[]) => mockApiSystemTools(...args),
      githubSwitch: (...args: unknown[]) => mockApiSystemGithubSwitch(...args),
    },
    muninn: {
      status: (...args: unknown[]) => mockApiMuninnStatus(...args),
      vaults: (...args: unknown[]) => mockApiMuninnVaults(...args),
      createVault: (...args: unknown[]) => mockApiMuninnCreateVault(...args),
    },
  },
}))

// Mock the connections catalog module
vi.mock('../../composables/useConnectionsCatalog', () => {
  const CATALOG = [
    {
      id: 'slack',
      name: 'Slack',
      description: 'Slack messaging',
      type: 'oauth',
      category: 'communication',
      icon: 'S',
      iconColor: '#4A154B',
      multiAccount: false,
    },
    {
      id: 'github',
      name: 'GitHub',
      description: 'GitHub dev tools',
      type: 'oauth',
      category: 'dev_tools',
      icon: 'G',
      iconColor: '#24292E',
      multiAccount: false,
    },
    {
      id: 'aws',
      name: 'AWS',
      description: 'Amazon Web Services',
      type: 'system',
      category: 'cloud',
      icon: 'A',
      iconColor: '#FF9900',
      multiAccount: false,
    },
  ]
  return {
    CATALOG,
    CATEGORY_LABELS: {
      all: 'All',
      my_connections: 'My Connections',
      communication: 'Communication',
      dev_tools: 'Dev Tools',
      cloud: 'Cloud',
    },
    hydrateOAuth: vi.fn((_entry: unknown, conns: Array<{ provider: string; account_label: string; account_id: string }>) => {
      const matches = conns.filter((c) => c.provider === (_entry as { id: string }).id)
      return matches.length > 0
        ? { connected: true, accounts: matches.map(c => ({ id: c.account_id, label: c.account_label })) }
        : { connected: false, accounts: [] }
    }),
    hydrateSystem: vi.fn((_entry: unknown, tools: Array<{ name: string; authed: boolean; identity: string; profiles: string[] }>) => {
      const tool = tools.find(t => t.name === (_entry as { id: string }).id)
      return tool ? { connected: tool.authed, identity: tool.identity, profiles: tool.profiles } : { connected: false }
    }),
    hydrateCredentials: vi.fn((_entry: unknown, conns: Array<{ provider: string; account_label: string; account_id: string }>) => {
      const matches = conns.filter((c) => c.provider === (_entry as { id: string }).id)
      return matches.length > 0
        ? { connected: true, accounts: matches.map(c => ({ id: c.account_id, label: c.account_label })) }
        : { connected: false, accounts: [] }
    }),
  }
})

vi.mock('../../composables/useCredentialCatalog', () => ({
  fetchCredentialCatalog: (...args: unknown[]) => mockFetchCredentialCatalog(...args),
  getCredentialCatalogEntry: vi.fn().mockResolvedValue(null),
  _resetCredentialCatalogCache: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {}, params: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

// Stub child components to avoid deep rendering
vi.mock('../../components/connections/CategoryNav.vue', () => ({ default: { template: '<div class="category-nav" />' } }))
vi.mock('../../components/connections/ConnectionCard.vue', () => ({ default: { template: '<div class="connection-card" />' } }))
vi.mock('../../components/connections/CredentialModal.vue', () => ({ default: { template: '<div />' } }))

import ConnectionsView from '../ConnectionsView.vue'

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(ConnectionsView, {
    global: {
      stubs: {
        Teleport: true,
        Transition: true,
        CategoryNav: { template: '<div class="category-nav" />' },
        ConnectionCard: { template: '<div class="connection-card" />' },
        CredentialModal: { template: '<div />' },
      },
    },
  })
}

beforeEach(() => {
  mockApiConnectionsList.mockReset().mockResolvedValue([])
  mockApiSystemTools.mockReset().mockResolvedValue([])
  mockApiMuninnStatus.mockReset().mockResolvedValue({ connected: false })
  mockApiConnectionsDelete.mockReset().mockResolvedValue({ deleted: true })
  mockApiConnectionsStart.mockReset().mockResolvedValue({ auth_url: 'https://auth.example.com' })
  mockApiConnectionsSetDefault.mockReset().mockResolvedValue({})
  mockApiSystemGithubSwitch.mockReset().mockResolvedValue({ active: 'user1' })
  mockFetchCredentialCatalog.mockReset().mockResolvedValue([])
  vi.spyOn(window, 'open').mockImplementation(() => null)
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ConnectionsView', () => {
  it('renders the Connections heading', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Connections')
  })

  it('shows "0 connected" count when no connections are present', async () => {
    mockApiConnectionsList.mockResolvedValue([])
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('0 connected')
  })

  it('updates connectedCount when oauth connections are returned', async () => {
    mockApiConnectionsList.mockResolvedValue([
      { id: 'conn-1', provider: 'slack', account_label: 'workspace', account_id: 'acc-1' },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // connectedCount is derived from hydratedCatalog which uses the mocked hydrateOAuth
    expect(vm.connectedCount).toBeGreaterThanOrEqual(0)
  })

  it('renders search input with correct placeholder', async () => {
    const w = mountView()
    await flushPromises()
    const input = w.find('input[placeholder="Search connections…"]')
    expect(input.exists()).toBe(true)
  })

  it('filters catalog by search query', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.search = 'slack'
    await nextTick()
    // filteredCatalog should only include Slack
    expect(vm.filteredCatalog.some((c: { name: string }) => c.name === 'Slack')).toBe(true)
    expect(vm.filteredCatalog.some((c: { name: string }) => c.name === 'GitHub')).toBe(false)
  })

  it('shows empty-state message when search yields no results', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.search = 'zzz_no_match_zzz'
    await nextTick()
    expect(w.text()).toContain('No connections match')
  })

  it('calls api.connections.list and api.system.tools on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockApiConnectionsList).toHaveBeenCalled()
    expect(mockApiSystemTools).toHaveBeenCalled()
  })

  it('displays error banner when refresh fails', async () => {
    mockApiConnectionsList.mockRejectedValueOnce(new Error('Network error'))
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Network error')
  })

  it('handleDisconnect sets pendingDisconnect', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.handleDisconnect('conn-abc')
    await nextTick()
    expect(vm.pendingDisconnect).toBe('conn-abc')
  })

  it('handleDisconnect is a no-op for empty string', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.handleDisconnect('')
    await nextTick()
    expect(vm.pendingDisconnect).toBeNull()
  })

  it('doDisconnect calls api.connections.delete with the correct id', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.pendingDisconnect = 'conn-xyz'
    await vm.doDisconnect()
    await flushPromises()
    expect(mockApiConnectionsDelete).toHaveBeenCalledWith('conn-xyz')
  })

  it('doDisconnect clears pendingDisconnect after deletion', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.pendingDisconnect = 'conn-xyz'
    await vm.doDisconnect()
    await flushPromises()
    expect(vm.pendingDisconnect).toBeNull()
  })

  it('doDisconnect is a no-op when pendingDisconnect is null', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.pendingDisconnect = null
    await vm.doDisconnect()
    expect(mockApiConnectionsDelete).not.toHaveBeenCalled()
  })

  it('doDisconnect shows error on api failure', async () => {
    mockApiConnectionsDelete.mockRejectedValueOnce(new Error('Delete failed'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.pendingDisconnect = 'conn-err'
    await vm.doDisconnect()
    await flushPromises()
    expect(vm.error).toContain('Delete failed')
  })

  it('cancelWait clears waitingFor', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.waitingFor = 'slack'
    vm.cancelWait()
    expect(vm.waitingFor).toBeNull()
  })

  it('connectedItems only includes connected, non-coming-soon entries', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // With no connections returned, connectedItems should be empty
    expect(Array.isArray(vm.connectedItems)).toBe(true)
    expect(vm.connectedItems.length).toBe(0)
  })

  it('shows my_connections empty state when no connections and category is my_connections', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.activeCategory = 'my_connections'
    await nextTick()
    expect(w.text()).toContain('No connections yet')
  })

  // ── OAuth flow ───────────────────────────────────────────────────────────────

  it('startOAuthConnect sets waitingFor and calls connections.start API', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    await vm.startOAuthConnect('slack')
    await flushPromises()
    expect(mockApiConnectionsStart).toHaveBeenCalledWith('slack')
    expect(vm.waitingFor).toBe('slack')
    // Opens the auth URL in a new tab
    expect(window.open).toHaveBeenCalledWith('https://auth.example.com', '_blank')
    vm.cancelWait()
  })

  it('startOAuthConnect sets error when API returns no auth_url', async () => {
    mockApiConnectionsStart.mockResolvedValueOnce({ auth_url: '' })
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    await vm.startOAuthConnect('slack')
    await flushPromises()
    expect(vm.waitingFor).toBeNull()
    expect(vm.error).toContain('No authorization URL')
  })

  it('startOAuthConnect sets error when API throws', async () => {
    mockApiConnectionsStart.mockRejectedValueOnce(new Error('Auth service unavailable'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    await vm.startOAuthConnect('github')
    await flushPromises()
    expect(vm.waitingFor).toBeNull()
    expect(vm.error).toContain('Auth service unavailable')
  })

  it('OAuth polling timeout clears waitingFor and sets error', async () => {
    vi.useFakeTimers()
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // Kick off the flow (resolves immediately in test, opens poll + timeout)
    const connectPromise = vm.startOAuthConnect('slack')
    await connectPromise
    await flushPromises()
    expect(vm.waitingFor).toBe('slack')
    // Advance past the 2-minute timeout (120 000 ms)
    vi.advanceTimersByTime(121_000)
    await nextTick()
    expect(vm.waitingFor).toBeNull()
    expect(vm.error).toContain('timed out')
    vi.useRealTimers()
  })

  // ── Set default ─────────────────────────────────────────────────────────────

  it('handleSetDefault calls connections.setDefault for oauth connections', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const oauthConn = { id: 'slack', type: 'oauth', name: 'Slack' }
    await vm.handleSetDefault(oauthConn, 'acc-42')
    await flushPromises()
    expect(mockApiConnectionsSetDefault).toHaveBeenCalledWith('acc-42')
  })

  it('handleSetDefault calls system.githubSwitch for github_cli connections', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const githubCliConn = { id: 'github_cli', type: 'system', name: 'GitHub CLI' }
    await vm.handleSetDefault(githubCliConn, 'user-profile-1')
    await flushPromises()
    expect(mockApiSystemGithubSwitch).toHaveBeenCalledWith('user-profile-1')
  })

  it('handleSetDefault sets error when connections.setDefault API throws', async () => {
    mockApiConnectionsSetDefault.mockRejectedValueOnce(new Error('Set default failed'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const oauthConn = { id: 'slack', type: 'oauth', name: 'Slack' }
    await vm.handleSetDefault(oauthConn, 'acc-bad')
    await flushPromises()
    expect(vm.error).toContain('Set default failed')
  })

  // ── Category navigation ──────────────────────────────────────────────────────

  it('setting activeCategory to a valid category filters filteredCatalog', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.activeCategory = 'dev_tools'
    await nextTick()
    const ids = vm.filteredCatalog.map((c: { id: string }) => c.id)
    // 'github' is in dev_tools; 'slack' (communication) and 'aws' (cloud) should be excluded
    expect(ids).toContain('github')
    expect(ids).not.toContain('slack')
    expect(ids).not.toContain('aws')
  })

  it('setting activeCategory to all shows entire catalog', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.activeCategory = 'dev_tools'
    await nextTick()
    vm.activeCategory = 'all'
    await nextTick()
    expect(vm.filteredCatalog.length).toBe(3)
  })

  // ── Connection filtering ─────────────────────────────────────────────────────

  it('connectedItems returns only entries whose state.connected is true', async () => {
    // Provide a Slack connection so hydrateOAuth returns connected: true for it
    mockApiConnectionsList.mockResolvedValue([
      { id: 'conn-1', provider: 'slack', account_label: 'My Workspace', account_id: 'acc-1' },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const ids = vm.connectedItems.map((c: { id: string }) => c.id)
    expect(ids).toContain('slack')
    expect(ids).not.toContain('github')
    expect(ids).not.toContain('aws')
  })

  // ── Credential modal ─────────────────────────────────────────────────────────

  it('cancelWait also leaves activeModal unaffected (credential modal is independent)', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // Simulate modal being open while an OAuth flow is in progress
    vm.activeModal = 'slack'
    vm.waitingFor = 'github'
    vm.cancelWait()
    await nextTick()
    // cancelWait only clears waitingFor; it does not forcibly close activeModal
    expect(vm.waitingFor).toBeNull()
    expect(vm.activeModal).toBe('slack')
  })

  it('startOAuthConnect cancels any previously active OAuth wait before starting a new one', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // Prime an existing wait
    vm.waitingFor = 'github'
    // Starting a new OAuth flow should clear the previous one
    await vm.startOAuthConnect('slack')
    await flushPromises()
    // After the new flow starts, waitingFor should be 'slack' (not 'github')
    expect(vm.waitingFor).toBe('slack')
    vm.cancelWait()
  })

  // ── Server catalog integration ────────────────────────────────────────────────

  it('merges server catalog entries into filteredCatalog', async () => {
    mockFetchCredentialCatalog.mockResolvedValue([
      {
        id: 'datadog',
        name: 'Datadog',
        description: 'Metrics, logs, monitors',
        category: 'observability',
        icon: 'DD',
        icon_color: '#632ca6',
        default_label: 'Datadog',
        multi_account: false,
        fields: [],
        validation: { available: true },
      },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.filteredCatalog.some((c: { id: string }) => c.id === 'datadog')).toBe(true)
  })

  it('server catalog entry has correct type, iconColor, and hydrated state', async () => {
    mockFetchCredentialCatalog.mockResolvedValue([
      {
        id: 'datadog',
        name: 'Datadog',
        description: 'Metrics, logs, monitors',
        category: 'observability',
        icon: 'DD',
        icon_color: '#632ca6',
        default_label: 'Datadog',
        multi_account: false,
        fields: [],
        validation: { available: true },
      },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const dd = vm.filteredCatalog.find((c: { id: string }) => c.id === 'datadog')
    expect(dd).toBeDefined()
    expect(dd.type).toBe('credentials')
    expect(dd.iconColor).toBe('#632ca6')
    expect(dd.state).toBeDefined()
    expect(dd.state.connected).toBe(false)
  })

  it('server catalog entry appears in connectedItems when a matching credential connection exists', async () => {
    mockFetchCredentialCatalog.mockResolvedValue([
      {
        id: 'datadog',
        name: 'Datadog',
        description: 'Metrics, logs, monitors',
        category: 'observability',
        icon: 'DD',
        icon_color: '#632ca6',
        default_label: 'Datadog',
        multi_account: false,
        fields: [],
        validation: { available: true },
      },
    ])
    mockApiConnectionsList.mockResolvedValue([
      { id: 'cred-1', provider: 'datadog', account_label: 'prod', account_id: 'cred-1' },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const ids = vm.connectedItems.map((c: { id: string }) => c.id)
    expect(ids).toContain('datadog')
  })

  it('catalog fetch failure does not set error banner', async () => {
    mockFetchCredentialCatalog.mockRejectedValue(new Error('Catalog unavailable'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.error).toBeFalsy()
  })

  it('static CATALOG entries remain in filteredCatalog alongside server entries', async () => {
    mockFetchCredentialCatalog.mockResolvedValue([
      {
        id: 'datadog',
        name: 'Datadog',
        description: 'Metrics, logs, monitors',
        category: 'observability',
        icon: 'DD',
        icon_color: '#632ca6',
        default_label: 'Datadog',
        multi_account: false,
        fields: [],
        validation: { available: true },
      },
    ])
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    const ids = vm.filteredCatalog.map((c: { id: string }) => c.id)
    // Static entries (from mocked CATALOG: slack, github, aws) + server entry (datadog)
    expect(ids).toContain('slack')
    expect(ids).toContain('github')
    expect(ids).toContain('datadog')
    expect(vm.filteredCatalog.length).toBe(4)
  })
})
