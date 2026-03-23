import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

// ── Mocks (hoisted before component import) ───────────────────────────────────

const mockConfig = ref<Record<string, unknown> | null>(null)
const mockConfigLoading = ref(false)
const mockExternallyChanged = ref(false)
const mockLoadConfig = vi.fn()
const mockSaveConfig = vi.fn()

vi.mock('../../composables/useConfig', () => ({
  useConfig: () => ({
    config: mockConfig,
    loading: mockConfigLoading,
    externallyChanged: mockExternallyChanged,
    loadConfig: mockLoadConfig,
    saveConfig: mockSaveConfig,
  }),
}))

const mockApiRuntimeStatus = vi.fn().mockResolvedValue({ state: 'running', session_id: 'abc', machine_id: 'machine-1' })

vi.mock('../../composables/useApi', () => ({
  api: {
    runtime: {
      status: (...args: unknown[]) => mockApiRuntimeStatus(...args),
    },
    config: {
      get: vi.fn().mockResolvedValue({}),
      update: vi.fn().mockResolvedValue({ saved: true, requires_restart: false }),
    },
  },
}))

import SettingsView from '../SettingsView.vue'

// ── Sample config ─────────────────────────────────────────────────────────────

const sampleConfig = {
  workspace_path: '/home/user/projects',
  max_turns: 50,
  bash_timeout_secs: 120,
  context_limit_kb: 200,
  diff_review_mode: 'auto',
  compact_mode: 'auto',
  git_stage_on_write: false,
  notepads_enabled: true,
  vision_enabled: false,
  semantic_search: false,
  tools_enabled: true,
  brave_api_key: '',
  allowed_tools: [],
  disallowed_tools: [],
  web_ui: { enabled: true, port: 8080, bind: '127.0.0.1', auto_open: true },
  integrations: {
    google: { client_id: '', client_secret: '' },
    github: { client_id: '', client_secret: '' },
    slack: { client_id: '', client_secret: '' },
    jira: { client_id: '', client_secret: '' },
    bitbucket: { client_id: '', client_secret: '' },
  },
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function mountView() {
  return shallowMount(SettingsView, {
    global: {
      stubs: {
        Teleport: true,
        Transition: true,
      },
    },
  })
}

beforeEach(() => {
  mockConfig.value = null
  mockConfigLoading.value = false
  mockExternallyChanged.value = false
  mockLoadConfig.mockReset().mockResolvedValue(sampleConfig)
  mockSaveConfig.mockReset().mockResolvedValue({ saved: true, requires_restart: false })
  mockApiRuntimeStatus.mockReset().mockResolvedValue({ state: 'running', session_id: 'abc', machine_id: 'machine-1' })
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SettingsView', () => {
  it('renders the Settings sidebar label', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Settings')
  })

  it('calls loadConfig on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockLoadConfig).toHaveBeenCalled()
  })

  it('calls api.runtime.status on mount', async () => {
    mountView()
    await flushPromises()
    expect(mockApiRuntimeStatus).toHaveBeenCalled()
  })

  it('renders General tab navigation button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('General')
  })

  it('renders Tools tab navigation button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Tools')
  })

  it('renders Web UI tab navigation button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Web UI')
  })

  it('renders Integrations tab navigation button', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('Integrations')
  })

  it('defaults to general tab on mount', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.activeTab).toBe('general')
  })

  it('switching tabs updates activeTab state', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.activeTab = 'tools'
    await nextTick()
    expect(vm.activeTab).toBe('tools')
  })

  it('populateForm sets form values from config data', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    expect(vm.form.workspace_path).toBe('/home/user/projects')
    expect(vm.form.max_turns).toBe(50)
    expect(vm.form.bash_timeout_secs).toBe(120)
  })

  it('populateForm sets dirty to false', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.dirty = true
    vm.populateForm(sampleConfig)
    await nextTick()
    expect(vm.dirty).toBe(false)
  })

  it('discard resets form to original values and clears dirty flag', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    // Mutate a field then discard
    vm.form.max_turns = 999
    vm.dirty = true
    vm.discard()
    await nextTick()
    expect(vm.form.max_turns).toBe(50)
    expect(vm.dirty).toBe(false)
  })

  it('save calls saveConfig with the current form values', async () => {
    mockConfig.value = sampleConfig as unknown as Record<string, unknown>
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    await vm.save()
    await flushPromises()
    expect(mockSaveConfig).toHaveBeenCalled()
  })

  it('save clears dirty flag on success', async () => {
    mockConfig.value = sampleConfig as unknown as Record<string, unknown>
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    vm.dirty = true
    await vm.save()
    await flushPromises()
    expect(vm.dirty).toBe(false)
  })

  it('save sets saveError when saveConfig throws', async () => {
    mockConfig.value = sampleConfig as unknown as Record<string, unknown>
    mockSaveConfig.mockRejectedValueOnce(new Error('Save failed'))
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    await vm.save()
    await flushPromises()
    expect(vm.saveError).toBe(true)
    expect(vm.saveMsg).toContain('Save failed')
  })

  it('save throws when config is not loaded', async () => {
    mockConfig.value = null
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    // populateForm sets originalForm so discard works; but config is null
    vm.populateForm(sampleConfig)
    await nextTick()
    // Manually null config to simulate not-loaded
    mockConfig.value = null
    await vm.save()
    await flushPromises()
    expect(vm.saveError).toBe(true)
  })

  it('syncToolsFromText parses allowed tools text into array', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.allowedToolsText = 'read_file\nwrite_file\nbash'
    vm.syncToolsFromText()
    await nextTick()
    expect(vm.form.allowed_tools).toEqual(['read_file', 'write_file', 'bash'])
  })

  it('syncToolsFromText parses disallowed tools text into array', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.disallowedToolsText = 'bash\nweb_search'
    vm.syncToolsFromText()
    await nextTick()
    expect(vm.form.disallowed_tools).toEqual(['bash', 'web_search'])
  })

  it('shows Unsaved changes indicator when dirty is true', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.dirty = true
    await nextTick()
    expect(w.text()).toContain('Unsaved changes')
  })

  it('does not show Unsaved changes indicator when dirty is false', async () => {
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.dirty = false
    await nextTick()
    expect(w.text()).not.toContain('Unsaved changes')
  })

  it('shows externally changed banner when externallyChanged is true', async () => {
    const w = mountView()
    await flushPromises()
    mockExternallyChanged.value = true
    await nextTick()
    expect(w.text()).toContain('Config updated externally')
  })

  it('save sets saveMsg to "Saved" on success without restart required', async () => {
    mockConfig.value = sampleConfig as unknown as Record<string, unknown>
    mockSaveConfig.mockResolvedValueOnce({ saved: true, requires_restart: false })
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    await vm.save()
    await flushPromises()
    expect(vm.saveMsg).toBe('Settings saved')
  })

  it('save sets saveMsg to include restart message when requires_restart is true', async () => {
    mockConfig.value = sampleConfig as unknown as Record<string, unknown>
    mockSaveConfig.mockResolvedValueOnce({ saved: true, requires_restart: true })
    const w = mountView()
    await flushPromises()
    const vm = w.vm as any
    vm.populateForm(sampleConfig)
    await nextTick()
    await vm.save()
    await flushPromises()
    expect(vm.saveMsg).toContain('restart')
  })
})
