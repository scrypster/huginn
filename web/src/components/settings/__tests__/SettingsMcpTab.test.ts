import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsMcpTab from '../SettingsMcpTab.vue'
import type { MCPServerStatus } from '../../../composables/useApi'

function mountTab(overrides: {
  browserEnabled?: boolean
  browserStatus?: MCPServerStatus
} = {}) {
  return mount(SettingsMcpTab, {
    props: {
      mcpServers: [],
      newMcp: { name: '', transport: 'stdio', command: '', argsText: '', url: '', envText: '' },
      mcpAddError: '',
      browserEnabled: overrides.browserEnabled ?? false,
      browserStatus: overrides.browserStatus,
    },
  })
}

describe('SettingsMcpTab Browser toggle', () => {
  it('emits toggleBrowser(true) when the switch is clicked off -> on', async () => {
    const w = mountTab({ browserEnabled: false })
    const buttons = w.findAll('button')
    // The toggle switch is the first button rendered inside the Browser section.
    await buttons[0].trigger('click')
    expect(w.emitted('toggleBrowser')).toBeTruthy()
    expect(w.emitted('toggleBrowser')?.[0]).toEqual([true])
  })

  it('does not show status text when disabled', () => {
    const w = mountTab({ browserEnabled: false })
    expect(w.text()).not.toContain('Checking status')
    expect(w.text()).not.toContain('Running')
  })

  it('shows an actionable install hint when the binary is missing', () => {
    const w = mountTab({
      browserEnabled: true,
      browserStatus: {
        name: 'playwright',
        connected: false,
        tool_count: 0,
        binary_found: false,
        install_hint: 'Not installed. Run: npm install @playwright/mcp@latest',
      },
    })
    expect(w.text()).toContain('Not installed. Run: npm install @playwright/mcp@latest')
  })

  it('shows running status with tool count when connected', () => {
    const w = mountTab({
      browserEnabled: true,
      browserStatus: {
        name: 'playwright',
        connected: true,
        circuit_state: 'closed',
        tool_count: 12,
        binary_found: true,
      },
    })
    expect(w.text()).toContain('Running')
    expect(w.text()).toContain('12 tools available')
  })

  it('shows a checking-status placeholder while browserStatus is not yet loaded', () => {
    const w = mountTab({ browserEnabled: true, browserStatus: undefined })
    expect(w.text()).toContain('Checking status')
  })
})
