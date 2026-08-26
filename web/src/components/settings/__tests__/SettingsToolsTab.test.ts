import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsToolsTab from '../SettingsToolsTab.vue'
import { DENY_WINS_COPY, TOOLS_ENABLED_SERVE_HINT } from '../../../utils/honesty'

function mountTab(overrides: {
  tools_enabled?: boolean
  allowed?: string
  disallowed?: string
} = {}) {
  return mount(SettingsToolsTab, {
    props: {
      form: { tools_enabled: overrides.tools_enabled ?? false, brave_api_key: '' },
      allowedToolsText: overrides.allowed ?? 'read_file\nwrite_file\nbash',
      disallowedToolsText: overrides.disallowed ?? 'bash\nweb_search',
      showBraveKey: false,
    },
  })
}

describe('SettingsToolsTab honesty', () => {
  it('does not present tools_enabled as a master off switch for serve', () => {
    const w = mountTab({ tools_enabled: false })
    const note = w.get('[data-testid="tools-enabled-serve-note"]')
    expect(note.text()).toBe(TOOLS_ENABLED_SERVE_HINT)
    expect(w.text()).toContain('TUI / CLI')
    expect(w.text().toLowerCase()).not.toContain('allow huginn to use tools (file read/write, bash, etc.)')
  })

  it('shows an allow+deny conflict warning and documents that deny wins', () => {
    const w = mountTab()
    const warn = w.get('[data-testid="tool-list-conflict"]')
    expect(warn.text()).toContain('bash')
    expect(warn.text()).toContain(DENY_WINS_COPY)
  })

  it('hides the conflict warning when lists do not overlap', () => {
    const w = mountTab({ allowed: 'read_file', disallowed: 'bash' })
    expect(w.find('[data-testid="tool-list-conflict"]').exists()).toBe(false)
  })
})
