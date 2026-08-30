import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('../../../composables/useApi', () => ({
  api: {
    hooks: {
      list: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      reload: vi.fn(),
      test: vi.fn(),
      audit: vi.fn(),
    },
  },
}))

import SettingsHooksTab from '../SettingsHooksTab.vue'
import { api } from '../../../composables/useApi'
import type { HookEntry } from '../../../composables/useApi'

const sampleHook: HookEntry = {
  id: 'block-force-push',
  event: 'PreToolUse',
  match: { tools: ['bash'] },
  action: { type: 'command', command: 'exit 1', timeout_secs: 10 },
  enabled: true,
  scope: 'workspace',
}

async function mountTab() {
  const w = mount(SettingsHooksTab)
  await flushPromises()
  return w
}

describe('SettingsHooksTab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [] })
    vi.mocked(api.hooks.audit).mockResolvedValue({ entries: [] })
  })

  it('shows an empty state with no hooks configured', async () => {
    const w = await mountTab()
    expect(w.text()).toContain('No hooks configured')
    expect(api.hooks.list).toHaveBeenCalled()
  })

  it('renders configured hooks from the list endpoint', async () => {
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [sampleHook] })
    const w = await mountTab()
    const rows = w.findAll('[data-testid="hook-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('block-force-push')
    expect(rows[0].text()).toContain('PreToolUse')
    expect(rows[0].text()).toContain('workspace')
  })

  it('creates a hook via the add form', async () => {
    vi.mocked(api.hooks.create).mockResolvedValue(sampleHook)
    const w = await mountTab()

    await w.get('input[placeholder="block-force-push"]').setValue('deny-rm')
    await w.get('input[placeholder="bash, write_*"]').setValue('bash')
    await w.get('textarea').setValue('exit 1')
    await w.get('[data-testid="hooks-save"]').trigger('click')
    await flushPromises()

    expect(api.hooks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        id: 'deny-rm',
        match: { tools: ['bash'] },
        action: expect.objectContaining({ command: 'exit 1' }),
      })
    )
  })

  it('rejects an empty command before calling the API', async () => {
    const w = await mountTab()
    await w.get('input[placeholder="block-force-push"]').setValue('needs-a-command')
    await w.get('input[placeholder="bash, write_*"]').setValue('bash')
    await w.get('[data-testid="hooks-save"]').trigger('click')
    await flushPromises()
    expect(api.hooks.create).not.toHaveBeenCalled()
    expect(w.text()).toContain('Command is required')
  })

  it('toggles a hook enabled/disabled', async () => {
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [sampleHook] })
    vi.mocked(api.hooks.update).mockResolvedValue({ ...sampleHook, enabled: false })
    const w = await mountTab()

    await w.get('[data-testid="hook-toggle"]').trigger('change')
    await flushPromises()

    expect(api.hooks.update).toHaveBeenCalledWith(
      'block-force-push',
      expect.objectContaining({ enabled: false })
    )
  })

  it('deletes a hook after confirmation', async () => {
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [sampleHook] })
    vi.mocked(api.hooks.delete).mockResolvedValue({ deleted: true })
    vi.stubGlobal('confirm', vi.fn(() => true))
    const w = await mountTab()

    await w.get('[data-testid="hook-delete"]').trigger('click')
    await flushPromises()

    expect(api.hooks.delete).toHaveBeenCalledWith('block-force-push')
  })

  it('does not delete when the confirmation is declined', async () => {
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [sampleHook] })
    vi.stubGlobal('confirm', vi.fn(() => false))
    const w = await mountTab()

    await w.get('[data-testid="hook-delete"]').trigger('click')
    await flushPromises()

    expect(api.hooks.delete).not.toHaveBeenCalled()
  })

  it('test-runs a hook and renders the result', async () => {
    vi.mocked(api.hooks.list).mockResolvedValue({ hooks: [sampleHook] })
    vi.mocked(api.hooks.test).mockResolvedValue({ allowed: false, exit_code: 1, output: 'blocked by policy' })
    const w = await mountTab()

    const buttons = w.findAll('button').filter(b => b.text() === 'Test')
    await buttons[0].trigger('click')
    await flushPromises()

    expect(api.hooks.test).toHaveBeenCalledWith(expect.objectContaining({ id: 'block-force-push' }))
    expect(w.text()).toContain('blocked')
    expect(w.text()).toContain('blocked by policy')
  })

  it('reloads hooks from disk on demand', async () => {
    vi.mocked(api.hooks.reload).mockResolvedValue({ reloaded: true })
    const w = await mountTab()

    await w.get('[data-testid="hooks-reload"]').trigger('click')
    await flushPromises()

    expect(api.hooks.reload).toHaveBeenCalled()
    expect(api.hooks.list).toHaveBeenCalledTimes(2) // initial mount + after reload
  })

  it('copies the schema example to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = await mountTab()

    await w.get('[data-testid="hooks-copy-schema"]').trigger('click')
    await flushPromises()

    expect(writeText).toHaveBeenCalled()
    const written = writeText.mock.calls[0][0] as string
    expect(written).toContain('"hooks"')
    expect(written).toContain('PreToolUse')
  })

  it('surfaces a load error instead of silently showing an empty list', async () => {
    vi.mocked(api.hooks.list).mockRejectedValue(new Error('malformed hooks.json'))
    const w = await mountTab()
    expect(w.text()).toContain('malformed hooks.json')
  })
})
