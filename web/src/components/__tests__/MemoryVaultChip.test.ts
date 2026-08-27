import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

const mockConnectLocal = vi.fn()
const mockVaults = vi.fn()
const mockCreateVault = vi.fn()
const mockAgentsList = vi.fn()
const mockAgentsGet = vi.fn()
const mockAgentsUpdate = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    muninn: {
      connectLocal: (...args: unknown[]) => mockConnectLocal(...args),
      vaults: (...args: unknown[]) => mockVaults(...args),
      createVault: (...args: unknown[]) => mockCreateVault(...args),
    },
    agents: {
      list: (...args: unknown[]) => mockAgentsList(...args),
      get: (...args: unknown[]) => mockAgentsGet(...args),
      update: (...args: unknown[]) => mockAgentsUpdate(...args),
    },
  },
}))

import MemoryVaultChip from '../MemoryVaultChip.vue'

const chip = {
  kind: 'connect' as const,
  text: "Winston isn't using a Muninn vault yet. Connect or create one.",
  agentName: 'Winston',
}

function mountChip(extra: Record<string, unknown> = {}) {
  return mount(MemoryVaultChip, {
    props: {
      chip,
      agentName: 'Winston',
      agentVaultName: '',
      knownAgents: [{ name: 'Winston', vault_name: '' }],
      ...extra,
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockConnectLocal.mockResolvedValue({
    ok: true,
    connected: true,
    installed: true,
    running: true,
    vaults: [],
  })
  mockVaults.mockResolvedValue({ vaults: [] })
  mockAgentsList.mockResolvedValue([{ name: 'Winston', vault_name: '' }])
  mockAgentsGet.mockResolvedValue({ name: 'Winston', model: 'gpt-4', vault_name: '' })
  mockAgentsUpdate.mockResolvedValue({})
  mockCreateVault.mockResolvedValue({ vault_name: 'named-by-user' })
})

describe('MemoryVaultChip', () => {
  it('renders the quiet chip text and is clickable', () => {
    const w = mountChip()
    const root = w.find('[data-testid="memory-vault-chip"]')
    expect(root.exists()).toBe(true)
    expect(root.text()).toContain('Connect or create one')
    expect(root.text()).not.toContain('Memory vault unavailable')
    expect(w.find('[data-testid="memory-vault-chip-action"]').exists()).toBe(true)
  })

  it('first-click connects local Muninn and prefills {agent}-huginn when no vault is in use', async () => {
    const w = mountChip()
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(mockConnectLocal).toHaveBeenCalled()
    const modal = w.find('[data-testid="memory-vault-modal"]')
    expect(modal.exists()).toBe(true)
    const input = w.find('[data-testid="memory-vault-new-name"]')
    expect(input.exists()).toBe(true)
    expect((input.element as HTMLInputElement).value).toBe('winston-huginn')
    const confirm = w.find('[data-testid="memory-vault-confirm"]')
    expect(confirm.text()).toBe('Create and connect')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
    expect(w.find('[data-testid="memory-vault-modes"]').exists()).toBe(true)
    expect(w.find('[data-testid="memory-vault-mode-immersive"]').exists()).toBe(true)
    expect(w.find('[data-testid="memory-vault-mode-conversational"]').exists()).toBe(true)
    expect(w.find('[data-testid="memory-vault-mode-passive"]').exists()).toBe(true)
    expect(w.html()).not.toContain('mdb_')
  })

  it('prefills nova-huginn for Nova on the empty-create path', async () => {
    const w = mountChip({
      agentName: 'Nova',
      knownAgents: [{ name: 'Nova', vault_name: '' }],
      chip: {
        kind: 'connect' as const,
        text: "Nova isn't using a Muninn vault yet. Connect or create one.",
        agentName: 'Nova',
      },
    })
    mockAgentsList.mockResolvedValue([{ name: 'Nova', vault_name: '' }])
    mockAgentsGet.mockResolvedValue({ name: 'Nova', model: 'gpt-4', vault_name: '' })
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()
    expect((w.find('[data-testid="memory-vault-new-name"]').element as HTMLInputElement).value).toBe('nova-huginn')
  })

  it('prefers an existing unused vault instead of inventing a name', async () => {
    mockConnectLocal.mockResolvedValue({
      ok: true,
      connected: true,
      running: true,
      vaults: ['local-main'],
    })
    mockVaults.mockResolvedValue({ vaults: ['local-main'] })
    const w = mountChip()
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(w.find('[data-testid="memory-vault-existing"]').exists()).toBe(true)
    expect(w.find('[data-testid="memory-vault-new-name"]').exists()).toBe(false)
    const select = w.find('[data-testid="memory-vault-existing"]')
    expect((select.element as HTMLSelectElement).value).toBe('local-main')
    expect(w.find('[data-testid="memory-vault-confirm"]').text()).toBe('Connect')
  })

  it('auto-connects the agent’s own vault without inventing another name', async () => {
    const w = mountChip({ agentVaultName: 'winston-already' })
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(mockConnectLocal).toHaveBeenCalled()
    expect(mockCreateVault).not.toHaveBeenCalled()
    expect(mockAgentsUpdate).toHaveBeenCalled()
    const payload = mockAgentsUpdate.mock.calls[0][1] as { vault_name?: string }
    expect(payload.vault_name).toBe('winston-already')
    expect(w.emitted('connected')).toBeTruthy()
    expect(w.find('[data-testid="memory-vault-modal"]').exists()).toBe(false)
  })

  it('create confirm attaches the prefilled name, memory mode, and never renders a token', async () => {
    mockCreateVault.mockResolvedValue({ vault_name: 'winston-huginn', token: 'mdb_should_never_render' })
    const w = mountChip()
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    await w.find('[data-testid="memory-vault-mode-immersive"]').trigger('click')
    await w.find('[data-testid="memory-vault-confirm"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(mockCreateVault).toHaveBeenCalledWith({ vault_name: 'winston-huginn', agent_label: 'huginn-Winston' })
    expect(mockAgentsUpdate).toHaveBeenCalled()
    const payload = mockAgentsUpdate.mock.calls[0][1] as { vault_name?: string; memory_type?: string; memory_mode?: string }
    expect(payload.vault_name).toBe('winston-huginn')
    expect(payload.memory_type).toBe('muninndb')
    expect(payload.memory_mode).toBe('immersive')
    expect(w.html()).not.toContain('mdb_should_never_render')
    expect(w.emitted('connected')).toBeTruthy()
  })

  it('blocks creating a vault name already in use', async () => {
    mockVaults.mockResolvedValue({ vaults: [] })
    mockAgentsList.mockResolvedValue([
      { name: 'Winston', vault_name: '' },
      { name: 'Tess', vault_name: 'claimed' },
    ])
    const w = mountChip({
      knownAgents: [
        { name: 'Winston', vault_name: '' },
        { name: 'Tess', vault_name: 'claimed' },
      ],
    })
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    await w.find('[data-testid="memory-vault-new-name"]').setValue('claimed')
    await nextTick()
    expect(w.find('[data-testid="memory-vault-in-use"]').exists()).toBe(true)
    expect((w.find('[data-testid="memory-vault-confirm"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(mockCreateVault).not.toHaveBeenCalled()
  })

  it('shows a quiet offline line and no Connect when local Muninn is missing', async () => {
    mockConnectLocal.mockRejectedValue(new Error('local Muninn is not available'))
    const w = mountChip()
    await w.find('[data-testid="memory-vault-chip-action"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(w.find('[data-testid="memory-vault-offline"]').exists()).toBe(true)
    expect(w.find('[data-testid="memory-vault-offline"]').text()).toBe("Muninn isn't running")
    expect(w.find('[data-testid="memory-vault-confirm"]').exists()).toBe(false)
    expect(w.find('[data-testid="memory-vault-new-name"]').exists()).toBe(false)
    expect(w.html()).not.toContain("Couldn't reach local Muninn")
  })

  it('dismiss emits without opening the modal', async () => {
    const w = mountChip()
    await w.find('[data-testid="memory-vault-chip-dismiss"]').trigger('click')
    expect(w.emitted('dismiss')).toBeTruthy()
    expect(w.find('[data-testid="memory-vault-modal"]').exists()).toBe(false)
    expect(mockConnectLocal).not.toHaveBeenCalled()
  })
})
