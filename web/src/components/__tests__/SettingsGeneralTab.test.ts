import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SettingsGeneralTab from '../settings/SettingsGeneralTab.vue'

// ── Mock useApi ────────────────────────────────────────────────────────────────

const mockLogLevelGet = vi.fn()
const mockLogLevelSet = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    logLevel: {
      get: () => mockLogLevelGet(),
      set: (level: string) => mockLogLevelSet(level),
    },
  },
}))

// ── Helpers ────────────────────────────────────────────────────────────────────

function makeForm(): Record<string, unknown> {
  return {
    workspace_path: '',
    max_turns: 50,
    bash_timeout_secs: 120,
    context_limit_kb: 200,
    diff_review_mode: 'auto',
    compact_mode: 'auto',
    git_stage_on_write: false,
    notepads_enabled: false,
    vision_enabled: false,
    semantic_search: false,
  }
}

// ── Tests ──────────────────────────────────────────────────────────────────────

describe('SettingsGeneralTab — Log Level control', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads current log level on mount and shows Warn (default)', async () => {
    mockLogLevelGet.mockResolvedValue({ level: 'WARN' })

    const wrapper = mount(SettingsGeneralTab, {
      props: { form: makeForm() },
    })
    await flushPromises()

    const select = wrapper.find('select[data-testid="log-level-select"]')
    if (select.exists()) {
      expect((select.element as HTMLSelectElement).value).toBe('warn')
    } else {
      // Fallback: look for the select by its options content
      const allSelects = wrapper.findAll('select')
      const logSelect = allSelects.find(s =>
        s.text().includes('Warn (default)'),
      )
      expect(logSelect).toBeDefined()
      expect((logSelect!.element as HTMLSelectElement).value).toBe('warn')
    }
  })

  it('calls api.logLevel.set when the dropdown changes', async () => {
    mockLogLevelGet.mockResolvedValue({ level: 'WARN' })
    mockLogLevelSet.mockResolvedValue({ level: 'DEBUG' })

    const wrapper = mount(SettingsGeneralTab, {
      props: { form: makeForm() },
    })
    await flushPromises()

    const allSelects = wrapper.findAll('select')
    const logSelect = allSelects.find(s =>
      s.text().includes('Warn (default)'),
    )
    expect(logSelect).toBeDefined()

    // Simulate user changing to debug
    await logSelect!.setValue('debug')

    expect(mockLogLevelSet).toHaveBeenCalledWith('debug')
  })

  it('falls back to warn when GET fails', async () => {
    mockLogLevelGet.mockRejectedValue(new Error('network error'))

    const wrapper = mount(SettingsGeneralTab, {
      props: { form: makeForm() },
    })
    await flushPromises()

    const allSelects = wrapper.findAll('select')
    const logSelect = allSelects.find(s =>
      s.text().includes('Warn (default)'),
    )
    expect(logSelect).toBeDefined()
    expect((logSelect!.element as HTMLSelectElement).value).toBe('warn')
  })
})
