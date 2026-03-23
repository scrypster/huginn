import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import CredentialModal from '../CredentialModal.vue'

// Mock the api module
vi.mock('../../../composables/useApi', () => {
  return {
    api: {
      muninn: { test: vi.fn(), connect: vi.fn() },
      credentials: {
        datadogTest: vi.fn(), datadogSave: vi.fn(),
        splunkTest: vi.fn(), splunkSave: vi.fn(),
        slackBotTest: vi.fn(), slackBotSave: vi.fn(),
        jiraSATest: vi.fn(), jiraSASave: vi.fn(),
        linearTest: vi.fn(), linearSave: vi.fn(),
        gitlabTest: vi.fn(), gitlabSave: vi.fn(),
        discordTest: vi.fn(), discordSave: vi.fn(),
        vercelTest: vi.fn(), vercelSave: vi.fn(),
        stripeTest: vi.fn(), stripeSave: vi.fn(),
        pagerdutyTest: vi.fn(), pagerdutySave: vi.fn(),
        newrelicTest: vi.fn(), newrelicSave: vi.fn(),
        elasticTest: vi.fn(), elasticSave: vi.fn(),
        grafanaTest: vi.fn(), grafanaSave: vi.fn(),
        crowdstrikeTest: vi.fn(), crowdstrikeSave: vi.fn(),
        terraformTest: vi.fn(), terraformSave: vi.fn(),
        servicenowTest: vi.fn(), servicenowSave: vi.fn(),
      },
    },
  }
})

import { api } from '../../../composables/useApi'
import type { CredentialProvider } from '../forms/index'

// Teleport renders inline in tests
const mountModal = (provider: CredentialProvider | null) =>
  mount(CredentialModal, {
    props: { provider },
    attachTo: document.body,
    global: { stubs: { Teleport: true } },
  })

describe('CredentialModal', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders nothing when provider is null', () => {
    const w = mountModal(null)
    expect(w.find('[data-testid="modal-panel"]').exists()).toBe(false)
  })

  it('renders MuninnDB form when provider is "muninn"', () => {
    const w = mountModal('muninn')
    expect(w.find('[data-testid="modal-panel"]').exists()).toBe(true)
    expect(w.find('[data-testid="modal-title"]').text()).toContain('MuninnDB')
    expect(w.find('[data-testid="field-endpoint"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-password"]').exists()).toBe(true)
  })

  it('renders Datadog form when provider is "datadog"', () => {
    const w = mountModal('datadog')
    expect(w.find('[data-testid="modal-title"]').text()).toContain('Datadog')
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-app-key"]').exists()).toBe(true)
  })

  it('renders Splunk form when provider is "splunk"', () => {
    const w = mountModal('splunk')
    expect(w.find('[data-testid="modal-title"]').text()).toContain('Splunk')
    expect(w.find('[data-testid="field-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })

  it('emits close when × button is clicked', async () => {
    const w = mountModal('datadog')
    await w.find('[data-testid="btn-close"]').trigger('click')
    expect(w.emitted('close')).toBeTruthy()
  })

  it('calls muninn.test and shows success result', async () => {
    vi.mocked(api.muninn.test).mockResolvedValueOnce({ ok: true })
    const w = mountModal('muninn')
    await w.find('[data-testid="btn-test"]').trigger('click')
    await flushPromises()
    expect(api.muninn.test).toHaveBeenCalled()
    expect(w.find('[data-testid="test-result"]').text()).toContain('Connection successful')
  })

  it('calls muninn.connect, shows Connected! for 1.5s then emits connected', async () => {
    vi.useFakeTimers()
    vi.mocked(api.muninn.connect).mockResolvedValueOnce(undefined)
    const w = mountModal('muninn')
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(api.muninn.connect).toHaveBeenCalled()
    // Shows success message immediately
    expect(w.find('[data-testid="save-msg"]').text()).toBe('Connected!')
    // connected NOT emitted yet (waiting 1500ms)
    expect(w.emitted('connected')).toBeFalsy()
    // Advance timers past the delay
    vi.advanceTimersByTime(1500)
    expect(w.emitted('connected')).toBeTruthy()
    vi.useRealTimers()
  })

  it('shows save error message on connect failure', async () => {
    vi.mocked(api.muninn.connect).mockRejectedValueOnce(new Error('bad credentials'))
    const w = mountModal('muninn')
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="save-msg"]').text()).toContain('bad credentials')
  })

  it('shows custom URL field only when Datadog site is "custom"', async () => {
    const w = mountModal('datadog')
    expect(w.find('[data-testid="field-custom-url"]').exists()).toBe(false)
    await w.find('[data-testid="field-site"]').setValue('custom')
    expect(w.find('[data-testid="field-custom-url"]').exists()).toBe(true)
  })
})
