import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import CredentialModal from '../CredentialModal.vue'
import type { CredentialCatalogEntry } from '../../../composables/useCredentialCatalog'

// ── Catalog mock ──────────────────────────────────────────────────────────────
const mockGetCatalogEntry = vi.fn<[string], Promise<CredentialCatalogEntry | null>>()

vi.mock('../../../composables/useCredentialCatalog', () => ({
  getCredentialCatalogEntry: (id: string) => mockGetCatalogEntry(id),
  fetchCredentialCatalog: vi.fn().mockResolvedValue([]),
}))

// ── API mock ──────────────────────────────────────────────────────────────────
vi.mock('../../../composables/useApi', () => {
  return {
    api: {
      muninn: { test: vi.fn(), connect: vi.fn() },
      connections: {
        catalog: vi.fn().mockResolvedValue([]),
      },
      credentials: {
        testGeneric: vi.fn(),
        saveGeneric: vi.fn(),
      },
    },
  }
})

import { api } from '../../../composables/useApi'

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Minimal catalog entry for testing the generic form path. */
function makeCatalogEntry(overrides?: Partial<CredentialCatalogEntry>): CredentialCatalogEntry {
  return {
    id: 'datadog',
    name: 'Datadog',
    description: 'Metrics and monitoring',
    category: 'observability',
    icon: 'DD',
    icon_color: '#632ca6',
    type: 'credentials',
    default_label: 'Datadog',
    multi_account: false,
    fields: [
      {
        key: 'api_key',
        label: 'API Key',
        type: 'password',
        required: true,
        stored_in: 'creds',
        placeholder: 'xxxx',
      },
      {
        key: 'app_key',
        label: 'Application Key',
        type: 'password',
        required: true,
        stored_in: 'creds',
        placeholder: 'xxxx',
      },
    ],
    validation: { available: true },
    ...overrides,
  }
}

/** Catalog entry for muninn (type=database → resolveApi routes to api.muninn). */
function makeMuninnEntry(): CredentialCatalogEntry {
  return makeCatalogEntry({
    id: 'muninn',
    name: 'MuninnDB',
    description: 'Agent memory',
    category: 'databases',
    icon: 'M',
    icon_color: '#58a6ff',
    type: 'database',
    default_label: 'MuninnDB',
    fields: [
      { key: 'endpoint', label: 'Endpoint URL', type: 'url',      required: true, stored_in: 'metadata', placeholder: 'http://localhost:8475' },
      { key: 'username', label: 'Username',     type: 'text',     required: true, stored_in: 'metadata', placeholder: 'root' },
      { key: 'password', label: 'Password',     type: 'password', required: true, stored_in: 'creds',    placeholder: '••••••••' },
    ],
  })
}

const mountModal = (provider: string | null) =>
  mount(CredentialModal, {
    props: { provider },
    attachTo: document.body,
    global: { stubs: { Teleport: true } },
  })

describe('CredentialModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: provider not found in catalog.
    mockGetCatalogEntry.mockResolvedValue(null)
  })

  // ── Render ──────────────────────────────────────────────────────────────────

  it('renders nothing when provider is null', () => {
    const w = mountModal(null)
    expect(w.find('[data-testid="modal-panel"]').exists()).toBe(false)
  })

  it('renders GenericCredentialForm when provider is in the catalog', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeCatalogEntry())
    const w = mountModal('datadog')
    await flushPromises()
    expect(w.find('[data-testid="modal-title"]').text()).toContain('Datadog')
    // GenericCredentialForm fields use data-testid="field-{key}"
    expect(w.find('[data-testid="field-api_key"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-app_key"]').exists()).toBe(true)
  })

  it('renders MuninnDB form from catalog (type=database)', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeMuninnEntry())
    const w = mountModal('muninn')
    await flushPromises()
    expect(w.find('[data-testid="modal-panel"]').exists()).toBe(true)
    expect(w.find('[data-testid="modal-title"]').text()).toContain('MuninnDB')
    expect(w.find('[data-testid="field-endpoint"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-password"]').exists()).toBe(true)
  })

  it('shows catalog-error state when catalog fetch fails', async () => {
    mockGetCatalogEntry.mockRejectedValue(new Error('network error'))
    const w = mountModal('datadog')
    await flushPromises()
    expect(w.find('[data-testid="catalog-error"]').exists()).toBe(true)
  })

  // ── Close / keyboard ────────────────────────────────────────────────────────

  it('emits close when × button is clicked', async () => {
    mockGetCatalogEntry.mockResolvedValue(null)
    const w = mountModal('muninn')
    await flushPromises()
    await w.find('[data-testid="btn-close"]').trigger('click')
    expect(w.emitted('close')).toBeTruthy()
  })

  // ── Muninn (database type) test / connect ────────────────────────────────────
  // resolveApi('database') routes to api.muninn.test and api.muninn.connect.

  it('calls muninn.test and shows success result', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeMuninnEntry())
    vi.mocked(api.muninn.test).mockResolvedValueOnce({ ok: true })
    const w = mountModal('muninn')
    await flushPromises()
    await w.find('[data-testid="btn-test"]').trigger('click')
    await flushPromises()
    expect(api.muninn.test).toHaveBeenCalled()
    expect(w.find('[data-testid="test-result"]').text()).toContain('Connection successful')
  })

  it('calls muninn.connect, shows Connected! for 1.5s then emits connected', async () => {
    vi.useFakeTimers()
    mockGetCatalogEntry.mockResolvedValue(makeMuninnEntry())
    vi.mocked(api.muninn.connect).mockResolvedValueOnce({ ok: true })
    const w = mountModal('muninn')
    await flushPromises()
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(api.muninn.connect).toHaveBeenCalled()
    expect(w.find('[data-testid="save-msg"]').text()).toBe('Connected!')
    expect(w.emitted('connected')).toBeFalsy()
    vi.advanceTimersByTime(1500)
    expect(w.emitted('connected')).toBeTruthy()
    vi.useRealTimers()
  })

  it('shows save error message on muninn connect failure', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeMuninnEntry())
    vi.mocked(api.muninn.connect).mockRejectedValueOnce(new Error('bad credentials'))
    const w = mountModal('muninn')
    await flushPromises()
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="save-msg"]').text()).toContain('bad credentials')
  })

  // ── Generic (catalog) test / connect ────────────────────────────────────────

  it('calls credentials.testGeneric for catalog providers', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeCatalogEntry())
    vi.mocked(api.credentials.testGeneric).mockResolvedValueOnce({ ok: true })
    const w = mountModal('datadog')
    await flushPromises()
    await w.find('[data-testid="btn-test"]').trigger('click')
    await flushPromises()
    expect(api.credentials.testGeneric).toHaveBeenCalledWith('datadog', expect.any(Object))
    expect(w.find('[data-testid="test-result"]').text()).toContain('Connection successful')
  })

  it('calls credentials.saveGeneric for catalog providers and emits connected', async () => {
    vi.useFakeTimers()
    mockGetCatalogEntry.mockResolvedValue(makeCatalogEntry())
    vi.mocked(api.credentials.saveGeneric).mockResolvedValueOnce({
      id: 'uuid-1', provider: 'datadog', account_label: 'Datadog',
    })
    const w = mountModal('datadog')
    await flushPromises()
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(api.credentials.saveGeneric).toHaveBeenCalledWith('datadog', expect.any(Object))
    expect(w.find('[data-testid="save-msg"]').text()).toBe('Connected!')
    vi.advanceTimersByTime(1500)
    expect(w.emitted('connected')).toBeTruthy()
    vi.useRealTimers()
  })

  it('shows error when saveGeneric fails for catalog provider', async () => {
    mockGetCatalogEntry.mockResolvedValue(makeCatalogEntry())
    vi.mocked(api.credentials.saveGeneric).mockRejectedValueOnce(new Error('api_key invalid'))
    const w = mountModal('datadog')
    await flushPromises()
    await w.find('[data-testid="btn-connect"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="save-msg"]').text()).toContain('api_key invalid')
  })

  // ── Select + __custom__ ──────────────────────────────────────────────────────

  it('shows custom URL field only when select option is __custom__', async () => {
    const entry = makeCatalogEntry({
      id: 'datadog',
      fields: [
        {
          key: 'url',
          label: 'Site',
          type: 'select',
          required: false,
          stored_in: 'metadata',
          default: 'https://api.datadoghq.com',
          options: [
            { label: 'US1', value: 'https://api.datadoghq.com' },
            { label: 'Custom', value: '__custom__' },
          ],
        },
      ],
    })
    mockGetCatalogEntry.mockResolvedValue(entry)
    const w = mountModal('datadog')
    await flushPromises()
    expect(w.find('[data-testid="field-url-custom"]').exists()).toBe(false)
    await w.find('[data-testid="field-url"]').setValue('__custom__')
    expect(w.find('[data-testid="field-url-custom"]').exists()).toBe(true)
  })
})
