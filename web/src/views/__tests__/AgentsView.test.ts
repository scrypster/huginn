import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, mount, flushPromises, config } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

// ── Composable mocks (hoisted before component import) ────────────────
const mockUpdateAgent = vi.fn()
const mockRemoveAgent = vi.fn()

vi.mock('../../composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref([]),
    updateAgent: mockUpdateAgent,
    removeAgent: mockRemoveAgent,
  }),
}))

const mockSkillsLoad = vi.fn()
vi.mock('../../composables/useSkills', () => ({
  useInstalledSkills: () => ({
    skills: ref([]),
    loading: ref(false),
    load: mockSkillsLoad,
  }),
}))

const mockApiAgentsGet = vi.fn().mockResolvedValue({})
const mockApiAgentsUpdate = vi.fn().mockResolvedValue({})
const mockApiModelsList = vi.fn().mockResolvedValue([])
const mockApiModelsAvailable = vi.fn().mockResolvedValue({ models: [] })
const mockApiConnectionsList = vi.fn().mockResolvedValue([])
const mockApiSystemTools = vi.fn().mockResolvedValue([])
const mockApiMuninnStatus = vi.fn().mockResolvedValue({ connected: false })
const mockApiMuninnVaults = vi.fn().mockResolvedValue({ vaults: [] })
const mockApiMuninnCreateVault = vi.fn().mockResolvedValue({})

vi.mock('../../composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn().mockResolvedValue([]),
      get: (...args: unknown[]) => mockApiAgentsGet(...args),
      update: (...args: unknown[]) => mockApiAgentsUpdate(...args),
    },
    connections: { list: () => mockApiConnectionsList() },
    models: {
      list: vi.fn().mockResolvedValue([]),
      available: () => mockApiModelsAvailable(),
    },
    skills: { list: vi.fn().mockResolvedValue([]) },
    system: { tools: () => mockApiSystemTools() },
    muninn: {
      status: () => mockApiMuninnStatus(),
      vaults: () => mockApiMuninnVaults(),
      createVault: (...args: unknown[]) => mockApiMuninnCreateVault(...args),
    },
    runtime: { status: vi.fn().mockResolvedValue({ state: 'idle' }) },
  },
  getToken: vi.fn().mockReturnValue('test-token'),
}))

const mockRouterPush = vi.fn()
const mockRouterReplace = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {}, query: {} }),
  useRouter: () => ({ push: mockRouterPush, replace: mockRouterReplace }),
  RouterLink: { template: '<a><slot /></a>' },
}))

import AgentsView from '../AgentsView.vue'

function mountAgent(props: Record<string, unknown> = {}, opts: Record<string, unknown> = {}) {
  return shallowMount(AgentsView, {
    props,
    global: {
      stubs: {
        Teleport: true,
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
    ...opts,
  })
}

// ── Tests ─────────────────────────────────────────────────────────────
describe('AgentsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // stub fetch for deleteAgent
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('{}', { status: 200 })
    )
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // ── No agent selected state ──────────────────────────────────────
  it('mounts without throwing', () => {
    expect(() => mountAgent()).not.toThrow()
  })

  it('renders "Select an agent" when no agentName prop', async () => {
    const w = mountAgent()
    await flushPromises()
    expect(w.text()).toContain('Select an agent')
  })

  it('shows "New agent" button in empty state', async () => {
    const w = mountAgent()
    await flushPromises()
    const btn = w.find('[data-testid="new-agent-btn"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toContain('New agent')
  })

  it('clicking "New agent" navigates to /agents/new', async () => {
    const w = mountAgent()
    await flushPromises()
    await w.find('[data-testid="new-agent-btn"]').trigger('click')
    expect(mockRouterPush).toHaveBeenCalledWith('/agents/new')
  })

  // ── New agent form ───────────────────────────────────────────────
  it('shows editor form when agentName is "new"', async () => {
    const w = mountAgent({ agentName: 'new' })
    await flushPromises()
    // Should NOT show the empty state
    expect(w.text()).not.toContain('Select an agent')
  })

  it('form starts with default color #58a6ff for new agent', async () => {
    const w = mountAgent({ agentName: 'new' })
    await flushPromises()
    // Avatar element has the default color style
    const avatar = w.find('.w-20.h-20')
    expect(avatar.exists()).toBe(true)
    expect(avatar.attributes('style')).toContain('#58a6ff')
  })

  it('new agent starts dirty (save bar visible)', async () => {
    const w = mountAgent({ agentName: 'new' })
    await flushPromises()
    // The save bar should be visible (v-if="dirty")
    expect(w.text()).toContain('Unsaved changes')
  })

  // ── Loading existing agent ───────────────────────────────────────
  it('loads agent data when agentName prop is set', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Coder',
      model: 'claude-3',
      system_prompt: 'You are a coder.',
      color: '#3fb950',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Coder' })
    await flushPromises()
    expect(mockApiAgentsGet).toHaveBeenCalledWith('Coder')
    // Name should appear in the form input
    const nameInput = w.find('input[placeholder="Agent name"]')
    expect(nameInput.exists()).toBe(true)
    expect((nameInput.element as HTMLInputElement).value).toBe('Coder')
  })

  it('populates system prompt textarea', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Writer',
      model: 'gpt-4',
      system_prompt: 'Write amazing stories.',
      color: '#58a6ff',
      icon: 'W',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Writer' })
    await flushPromises()
    const textarea = w.find('textarea')
    expect(textarea.exists()).toBe(true)
    expect((textarea.element as HTMLTextAreaElement).value).toBe('Write amazing stories.')
  })

  it('shows char count for system prompt', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Writer',
      model: 'gpt-4',
      system_prompt: 'Hello',
      color: '#58a6ff',
      icon: 'W',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Writer' })
    await flushPromises()
    expect(w.text()).toContain('5 chars')
  })

  // ── Dirty state / save bar ───────────────────────────────────────
  it('typing in name input marks form dirty', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Coder',
      model: 'claude-3',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Coder' })
    await flushPromises()
    // Initially not dirty
    expect(w.text()).not.toContain('Unsaved changes')

    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('CoderRenamed')
    await nameInput.trigger('input')
    await nextTick()
    expect(w.text()).toContain('Unsaved changes')
  })

  // ── Save button ──────────────────────────────────────────────────
  it('save button calls api.agents.update and shows success', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'TestBot',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'T',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiAgentsUpdate.mockResolvedValueOnce({})
    const w = mountAgent({ agentName: 'TestBot' })
    await flushPromises()

    // Make dirty by editing name
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('TestBotEdited')
    await nameInput.trigger('input')
    await nextTick()

    // Click save
    const saveBtn = w.find('[data-testid="save-agent-btn-sticky"]')
    expect(saveBtn.exists()).toBe(true)
    await saveBtn.trigger('click')
    await flushPromises()

    expect(mockApiAgentsUpdate).toHaveBeenCalled()
    expect(w.text()).toContain('Saved successfully')
  })

  // ── Discard button ───────────────────────────────────────────────
  it('discard reverts form to original', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Coder',
      model: 'claude-3',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Coder' })
    await flushPromises()

    // Edit name
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('Renamed')
    await nameInput.trigger('input')
    await nextTick()
    expect(w.text()).toContain('Unsaved changes')

    // Click discard
    const discardBtn = w.findAll('button').find(b => b.text() === 'Discard')
    expect(discardBtn).toBeDefined()
    await discardBtn!.trigger('click')
    await nextTick()

    // Should revert
    expect((w.find('input[placeholder="Agent name"]').element as HTMLInputElement).value).toBe('Coder')
    expect(w.text()).not.toContain('Unsaved changes')
  })

  // ── Delete flow ──────────────────────────────────────────────────
  it('clicking delete shows confirmation modal', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ToDelete',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'D',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ToDelete' })
    await flushPromises()

    // Find and click "Delete agent" button
    const deleteBtn = w.findAll('button').find(b => b.text().includes('Delete agent'))
    expect(deleteBtn).toBeDefined()
    await deleteBtn!.trigger('click')
    await nextTick()

    // Confirmation text should appear
    expect(w.text()).toContain('Delete ToDelete?')
    expect(w.text()).toContain('This cannot be undone.')
  })

  it('confirming delete calls fetch DELETE and navigates away', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ToDelete',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'D',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ToDelete' })
    await flushPromises()

    // Open confirm
    const deleteBtn = w.findAll('button').find(b => b.text().includes('Delete agent'))
    await deleteBtn!.trigger('click')
    await nextTick()

    // Click the "Delete" confirmation button in the modal
    const confirmBtn = w.findAll('button').find(b => b.text().trim() === 'Delete')
    expect(confirmBtn).toBeDefined()
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/v1/agents/ToDelete',
      expect.objectContaining({ method: 'DELETE' })
    )
    expect(mockRemoveAgent).toHaveBeenCalledWith('ToDelete')
    expect(mockRouterPush).toHaveBeenCalledWith('/agents')
  })

  // ── Color palette ────────────────────────────────────────────────
  it('clicking a color swatch changes the avatar color', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ColorTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ColorTest' })
    await flushPromises()

    // Color palette buttons are in the left panel
    // The green color (#3fb950) is the second in the palette
    const colorButtons = w.findAll('button').filter(b => {
      const style = b.attributes('style')
      return style && style.includes('background') && style.includes('#3fb950')
    })
    if (colorButtons.length > 0) {
      await colorButtons[0].trigger('click')
      await nextTick()
      // Avatar should now use the new color
      const avatar = w.find('.w-20.h-20')
      expect(avatar.attributes('style')).toContain('#3fb950')
    }
  })

  // ── Icon letter ──────────────────────────────────────────────────
  it('editing icon letter updates the avatar', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'IconTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: '',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'IconTest' })
    await flushPromises()

    const iconInput = w.find('input[placeholder="A"]')
    expect(iconInput.exists()).toBe(true)
    await iconInput.setValue('Z')
    await iconInput.trigger('input')
    await nextTick()

    // The avatar should now display 'Z'
    const avatar = w.find('.w-20.h-20')
    expect(avatar.text()).toBe('Z')
  })

  // ── Memory type selection ────────────────────────────────────────
  it('renders memory type options (none, notes, muninndb)', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'MemTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: '',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'MemTest' })
    await flushPromises()

    expect(w.text()).toContain('No memory')
    expect(w.text()).toContain('Context notes')
    expect(w.text()).toContain('MuninnDB')
  })

  it('clicking "Context notes" memory option changes memory_type', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'MemTest2',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'M',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'MemTest2' })
    await flushPromises()

    // Find the "Context notes" button
    const notesBtn = w.findAll('button').find(b => b.text().includes('Context notes'))
    expect(notesBtn).toBeDefined()
    await notesBtn!.trigger('click')
    await nextTick()

    // Now the memory path should show
    expect(w.text()).toContain('.memory.md')
  })

  // ── Local access section ─────────────────────────────────────────
  it('shows local access section with summary', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LocalTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'LocalTest' })
    await flushPromises()

    const section = w.find('[data-testid="local-access-section"]')
    expect(section.exists()).toBe(true)
    expect(section.text()).toContain('Local Access')
    expect(section.text()).toContain('none')
  })

  it('allow all button toggles local_tools to ["*"]', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LocalAllTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'LocalAllTest' })
    await flushPromises()

    const btn = w.find('[data-testid="local-access-allow-all-btn"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await nextTick()

    // Summary should change to indicate allow all
    const section = w.find('[data-testid="local-access-section"]')
    expect(section.text()).toContain('all (including shell')
  })

  // ── Connections / toolbelt section ───────────────────────────────
  it('shows connections section with "none" when empty', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ConnTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ConnTest' })
    await flushPromises()

    const section = w.find('[data-testid="toolbelt-section"]')
    expect(section.exists()).toBe(true)
    expect(section.text()).toContain('Connections')
    expect(section.text()).toContain('none')
    expect(section.text()).toContain('No connections granted')
  })

  it('shows toolbelt entries when agent has connections', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ConnTest2',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [
        { connection_id: 'abc', provider: 'slack', approval_gate: false },
      ],
      skills: [],
      local_tools: [],
    })
    mockApiConnectionsList.mockResolvedValueOnce([
      { id: 'abc', provider: 'slack', account_label: 'My Slack' },
    ])
    const w = mountAgent({ agentName: 'ConnTest2' })
    await flushPromises()

    const entries = w.findAll('[data-testid="toolbelt-entry"]')
    expect(entries.length).toBeGreaterThanOrEqual(1)
  })

  it('connections allow all toggles to wildcard', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ConnAllTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ConnAllTest' })
    await flushPromises()

    const btn = w.find('[data-testid="connections-allow-all-btn"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await nextTick()

    const section = w.find('[data-testid="toolbelt-section"]')
    expect(section.text()).toContain('all connections')
  })

  // ── Skills section ───────────────────────────────────────────────
  it('shows skills section with "none" when empty', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'SkillTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'S',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'SkillTest' })
    await flushPromises()

    expect(w.text()).toContain('Skills')
    expect(w.text()).toContain('No skills assigned')
  })

  it('shows skills count when skills are assigned', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'SkillTest2',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'S',
      memory_type: 'none',
      toolbelt: [],
      skills: ['code-review', 'plan'],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'SkillTest2' })
    await flushPromises()

    expect(w.text()).toContain('2 assigned')
    expect(w.text()).toContain('code-review')
    expect(w.text()).toContain('plan')
  })

  // ── Model picker ─────────────────────────────────────────────────
  it('shows "No model selected" warning when model is empty', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'NoModel',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'N',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'NoModel' })
    await flushPromises()

    expect(w.text()).toContain('No model selected')
  })

  it('shows model name when model is set', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'WithModel',
      model: 'claude-sonnet-4-6',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'W',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'WithModel' })
    await flushPromises()

    expect(w.text()).toContain('claude-sonnet-4-6')
  })

  // ── Set as default ───────────────────────────────────────────────
  it('shows "Set as default" button for non-default agent', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'NonDefault',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'N',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiAgentsGetActive.mockResolvedValueOnce({ name: 'OtherAgent' })
    const w = mountAgent({ agentName: 'NonDefault' })
    await flushPromises()

    expect(w.text()).toContain('Set as default')
  })

  it('shows "Default agent" badge for active agent', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'MyDefault',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'D',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiAgentsGetActive.mockResolvedValueOnce({ name: 'MyDefault' })
    const w = mountAgent({ agentName: 'MyDefault' })
    await flushPromises()

    expect(w.text()).toContain('Default agent')
  })

  // ── System prompt editing ────────────────────────────────────────
  it('editing system prompt marks form dirty', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'PromptEdit',
      model: 'gpt-4',
      system_prompt: 'Original prompt',
      color: '#58a6ff',
      icon: 'P',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'PromptEdit' })
    await flushPromises()

    const textarea = w.find('textarea')
    await textarea.setValue('New prompt text')
    await textarea.trigger('input')
    await nextTick()

    expect(w.text()).toContain('Unsaved changes')
  })

  // ── Save error handling ──────────────────────────────────────────
  it('shows error message when save fails', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'FailSave',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'F',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiAgentsUpdate.mockRejectedValueOnce(new Error('Network failure'))
    const w = mountAgent({ agentName: 'FailSave' })
    await flushPromises()

    // Make dirty
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('FailSaveEdited')
    await nameInput.trigger('input')
    await nextTick()

    // Click save
    const saveBtn = w.find('[data-testid="save-agent-btn-sticky"]')
    await saveBtn.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('Network failure')
  })

  // ── onMounted loads ──────────────────────────────────────────────
  it('calls loadAvailableModels on mount', async () => {
    mountAgent({ agentName: 'LoadTest' })
    await flushPromises()
    expect(mockApiModelsAvailable).toHaveBeenCalled()
  })

  it('calls loadConnections on mount', async () => {
    mountAgent({ agentName: 'LoadTest2' })
    await flushPromises()
    expect(mockApiConnectionsList).toHaveBeenCalled()
  })

  it('calls loadMuninnInfo on mount', async () => {
    mountAgent({ agentName: 'LoadTest3' })
    await flushPromises()
    expect(mockApiMuninnStatus).toHaveBeenCalled()
  })

  // ── Model picker modal ───────────────────────────────────────────
  it('opens model picker modal when model button is clicked', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ModelPick',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'M',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiModelsAvailable.mockResolvedValueOnce({
      models: [{ name: 'llama3:latest', details: { parameter_size: '8B' } }],
    })
    const w = mountAgent({ agentName: 'ModelPick' })
    await flushPromises()

    // Click the "No model selected" button to open picker
    const modelBtn = w.findAll('button').find(b => b.text().includes('No model selected'))
    if (modelBtn) {
      await modelBtn.trigger('click')
      await nextTick()
      // Model picker modal content should appear (Teleport is stubbed inline)
      expect(w.text()).toContain('Select model')
    }
  })

  // ── Save rename flow ─────────────────────────────────────────────
  it('save with renamed agent navigates to new name', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'OldName',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'O',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiAgentsUpdate.mockResolvedValueOnce({})
    const w = mountAgent({ agentName: 'OldName' })
    await flushPromises()

    // Rename
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('NewName')
    await nameInput.trigger('input')
    await nextTick()

    // Save
    const saveBtn = w.find('[data-testid="save-agent-btn-sticky"]')
    await saveBtn.trigger('click')
    await flushPromises()

    expect(mockApiAgentsUpdate).toHaveBeenCalledWith('OldName', expect.objectContaining({ name: 'NewName' }))
    expect(mockRemoveAgent).toHaveBeenCalledWith('OldName')
    expect(mockRouterReplace).toHaveBeenCalledWith('/agents/NewName')
  })

  // ── MuninnDB memory type ─────────────────────────────────────────
  it('shows MuninnDB upgrade badge when not connected', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'MuninnTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: '',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiMuninnStatus.mockResolvedValueOnce({ connected: false })
    const w = mountAgent({ agentName: 'MuninnTest' })
    await flushPromises()

    expect(w.text()).toContain('Upgrade')
    expect(w.text()).toContain('Connect MuninnDB')
  })

  // ── Local tools display ──────────────────────────────────────────
  it('shows individual tool names when local_tools has specific tools', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LocalDetail',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: ['read_file', 'git_status'],
    })
    const w = mountAgent({ agentName: 'LocalDetail' })
    await flushPromises()

    const section = w.find('[data-testid="local-access-section"]')
    expect(section.text()).toContain('read_file')
    expect(section.text()).toContain('git_status')
  })

  // ── Manage local access button ───────────────────────────────────
  it('opens local access modal when manage button clicked', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LocalModal',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'LocalModal' })
    await flushPromises()

    const btn = w.find('[data-testid="manage-local-access-btn"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await nextTick()

    // Modal should show (Teleport is stubbed)
    expect(w.text()).toContain('Manage Local Access')
  })

  // ── Connections modal ────────────────────────────────────────────
  it('opens connections modal when manage button clicked', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ConnModal',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ConnModal' })
    await flushPromises()

    const btn = w.find('[data-testid="add-toolbelt-btn"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    await nextTick()

    // Modal should appear (Teleport stubbed)
    expect(w.text()).toContain('Manage Connections')
  })

  // ── Skills modal ─────────────────────────────────────────────────
  it('opens skills modal when manage button clicked', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'SkillModal',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'S',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'SkillModal' })
    await flushPromises()

    const btn = w.findAll('button').find(b => b.text().includes('Manage skills'))
    expect(btn).toBeDefined()
    await btn!.trigger('click')
    await nextTick()

    // Modal should appear (Teleport stubbed)
    expect(w.text()).toContain('Manage Skills')
  })

  // ── Connection label helpers ─────────────────────────────────────
  it('shows system tool labels in toolbelt entries', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'SysToolTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'S',
      memory_type: 'none',
      toolbelt: [
        { connection_id: 'system:github', provider: 'github_cli', approval_gate: false },
      ],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'SysToolTest' })
    await flushPromises()

    // system: prefix gets converted to "name (CLI)" format
    const badges = w.findAll('[data-testid="toolbelt-provider-badge"]')
    const githubBadge = badges.find(b => b.text().includes('github'))
    expect(githubBadge).toBeDefined()
  })

  // ── ESC key handler ──────────────────────────────────────────────
  it('registers keydown handler for ESC on mount', async () => {
    const addSpy = vi.spyOn(window, 'addEventListener')
    mountAgent({ agentName: 'KeyTest' })
    await flushPromises()
    expect(addSpy).toHaveBeenCalledWith('keydown', expect.any(Function))
  })

  // ── Memory MuninnDB with vault ───────────────────────────────────
  it('shows vault name and connected status when muninndb is configured', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'VaultTest',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'V',
      memory_type: 'muninndb',
      memory_enabled: true,
      vault_name: 'huginn-vault',
      memory_mode: 'conversational',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiMuninnStatus.mockResolvedValueOnce({ connected: true, username: 'test', endpoint: 'localhost' })
    mockApiMuninnVaults.mockResolvedValueOnce({ vaults: [{ name: 'huginn-vault', linked: true }] })
    const w = mountAgent({ agentName: 'VaultTest' })
    await flushPromises()

    expect(w.text()).toContain('huginn-vault')
    expect(w.text()).toContain('connected')
  })

  // ── New agent save ───────────────────────────────────────────────
  it('saving new agent uses form name as URL key', async () => {
    mockApiAgentsUpdate.mockResolvedValueOnce({})
    const w = mountAgent({ agentName: 'new' })
    await flushPromises()

    // Fill in form
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('BrandNew')
    await nameInput.trigger('input')
    await nextTick()

    const saveBtn = w.find('[data-testid="save-agent-btn-sticky"]')
    await saveBtn.trigger('click')
    await flushPromises()

    expect(mockApiAgentsUpdate).toHaveBeenCalledWith('BrandNew', expect.objectContaining({ name: 'BrandNew' }))
  })

  // ── Agent load error handling ────────────────────────────────────
  it('handles loadAgent failure gracefully', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mockApiAgentsGet.mockRejectedValueOnce(new Error('Agent not found'))
    const w = mountAgent({ agentName: 'Missing' })
    await flushPromises()

    expect(consoleSpy).toHaveBeenCalledWith('Failed to load agent', expect.any(Error))
    consoleSpy.mockRestore()
  })

  // ── Connections allow all toggle off ─────────────────────────────
  it('toggling connections allow all off clears toolbelt', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ConnToggle',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'C',
      memory_type: 'none',
      toolbelt: [{ connection_id: '*', provider: '*', profile: '', approval_gate: false }],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ConnToggle' })
    await flushPromises()

    const section = w.find('[data-testid="toolbelt-section"]')
    expect(section.text()).toContain('all connections')

    // Toggle off
    const btn = w.find('[data-testid="connections-allow-all-btn"]')
    await btn.trigger('click')
    await nextTick()

    expect(section.text()).toContain('none')
  })

  // ── Local allow all toggle off ───────────────────────────────────
  it('toggling local allow all off clears local_tools', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LocalToggle',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: ['*'],
    })
    const w = mountAgent({ agentName: 'LocalToggle' })
    await flushPromises()

    const section = w.find('[data-testid="local-access-section"]')
    expect(section.text()).toContain('all (including shell')

    // Toggle off
    const btn = w.find('[data-testid="local-access-allow-all-btn"]')
    await btn.trigger('click')
    await nextTick()

    expect(section.text()).toContain('none')
  })

  // ── deriveMemoryType backwards compat ─────────────────────────────
  it('derives memory_type from context_notes_enabled for legacy agents', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LegacyNotes',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      // No memory_type field — legacy format
      context_notes_enabled: true,
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'LegacyNotes' })
    await flushPromises()

    // Should show the notes memory path
    expect(w.text()).toContain('.memory.md')
  })

  it('derives memory_type from memory_enabled for legacy muninndb agents', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'LegacyMuninn',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'L',
      // No memory_type field — legacy format
      memory_enabled: true,
      vault_name: 'test-vault',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiMuninnStatus.mockResolvedValueOnce({ connected: true, username: 'u', endpoint: 'e' })
    mockApiMuninnVaults.mockResolvedValueOnce({ vaults: [{ name: 'test-vault', linked: true }] })
    const w = mountAgent({ agentName: 'LegacyMuninn' })
    await flushPromises()

    expect(w.text()).toContain('test-vault')
    expect(w.text()).toContain('connected')
  })

  // ── detectProvider model groups ──────────────────────────────────
  it('shows anthropic model in model picker', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ProviderTest',
      model: 'claude-sonnet-4-6',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'P',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiModelsAvailable.mockResolvedValueOnce({
      models: [
        { name: 'llama3:latest', details: { parameter_size: '8B' } },
      ],
      builtin_models: [
        { name: 'claude-sonnet-4-6', details: { parameter_size: '' } },
      ],
    })
    const w = mountAgent({ agentName: 'ProviderTest' })
    await flushPromises()

    // Model name should display in the header
    expect(w.text()).toContain('claude-sonnet-4-6')
  })

  // ── loadAvailableModels error ────────────────────────────────────
  it('handles models unavailable gracefully', async () => {
    mockApiModelsAvailable.mockRejectedValueOnce(new Error('Ollama down'))
    mountAgent({ agentName: 'ModelFail' })
    await flushPromises()
    // Should not throw — error handled internally
  })

  // ── loadConnections error ────────────────────────────────────────
  it('handles connections load error gracefully', async () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mockApiConnectionsList.mockRejectedValueOnce(new Error('Network'))
    mountAgent({ agentName: 'ConnFail' })
    await flushPromises()
    expect(consoleSpy).toHaveBeenCalled()
    consoleSpy.mockRestore()
  })

  // ── ensureVault on save ──────────────────────────────────────────
  it('save with muninndb creates vault if not linked', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'VaultCreate',
      model: 'gpt-4',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'V',
      memory_type: 'muninndb',
      memory_enabled: true,
      vault_name: 'new-vault',
      memory_mode: 'conversational',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    mockApiMuninnStatus.mockResolvedValueOnce({ connected: true })
    mockApiMuninnVaults.mockResolvedValueOnce({ vaults: [{ name: 'other-vault', linked: true }] })
    mockApiMuninnCreateVault.mockResolvedValueOnce({})
    mockApiAgentsUpdate.mockResolvedValueOnce({})

    const w = mountAgent({ agentName: 'VaultCreate' })
    await flushPromises()

    // Make dirty
    const nameInput = w.find('input[placeholder="Agent name"]')
    await nameInput.setValue('VaultCreate')
    await nameInput.trigger('input')
    await nextTick()

    const saveBtn = w.find('[data-testid="save-agent-btn-sticky"]')
    await saveBtn.trigger('click')
    await flushPromises()

    expect(mockApiMuninnCreateVault).toHaveBeenCalledWith(
      expect.objectContaining({ vault_name: 'new-vault' })
    )
  })

  // ── connectionIcon helpers ───────────────────────────────────────
  it('renders toolbelt entries with provider-specific icons', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'IconTest2',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'I',
      memory_type: 'none',
      toolbelt: [
        { connection_id: 'system:aws', provider: 'aws', profile: 'prod', approval_gate: false },
      ],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'IconTest2' })
    await flushPromises()

    // AWS system tool should show
    expect(w.text()).toContain('aws')
    expect(w.text()).toContain('prod')
  })

  // ── Switching agent resets delete modal ───────────────────────────
  it('switching agent clears delete confirmation', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Agent1',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'A',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'Agent1' })
    await flushPromises()

    // Switch to different agent
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'Agent2',
      model: '',
      system_prompt: '',
      color: '#3fb950',
      icon: 'B',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    await w.setProps({ agentName: 'Agent2' })
    await flushPromises()

    // Agent 2 should now be loaded
    const nameInput = w.find('input[placeholder="Agent name"]')
    expect((nameInput.element as HTMLInputElement).value).toBe('Agent2')
  })

  // ── modelsError display ──────────────────────────────────────────
  it('shows models error with empty model list', async () => {
    mockApiModelsAvailable.mockResolvedValueOnce({ error: 'Ollama not reachable', models: [] })
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'ModelErr',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'M',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    const w = mountAgent({ agentName: 'ModelErr' })
    await flushPromises()

    // Component should still render fine
    expect(w.text()).toContain('No model selected')
  })

  // ── Delete error handling ────────────────────────────────────────
  it('shows error when delete fails', async () => {
    mockApiAgentsGet.mockResolvedValueOnce({
      name: 'FailDelete',
      model: '',
      system_prompt: '',
      color: '#58a6ff',
      icon: 'F',
      memory_type: 'none',
      toolbelt: [],
      skills: [],
      local_tools: [],
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation((_url, init) => {
      if ((init as RequestInit)?.method === 'DELETE') {
        return Promise.resolve(new Response(
          JSON.stringify({ error: 'Agent in use' }),
          { status: 409, headers: { 'Content-Type': 'application/json' } },
        ))
      }
      return Promise.resolve(new Response('{}', { status: 200 }))
    })

    // Use mount (not shallowMount) so Teleport content is accessible
    const w = mount(AgentsView, {
      props: { agentName: 'FailDelete' },
      global: {
        stubs: { Teleport: true, Transition: false, RouterLink: { template: '<a><slot /></a>' } },
      },
    })
    await flushPromises()

    // Open confirm
    const deleteBtn = w.findAll('button').find(b => b.text().includes('Delete agent'))
    await deleteBtn!.trigger('click')
    await nextTick()

    // Confirm delete — Teleport is stubbed so content renders inline
    const confirmBtn = w.findAll('button').find(b => b.text().trim() === 'Delete')
    expect(confirmBtn).toBeDefined()
    await confirmBtn!.trigger('click')
    await flushPromises()

    expect(w.text()).toContain('Agent in use')
  })
})
