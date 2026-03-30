import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import GenericCredentialForm from '../GenericCredentialForm.vue'
import type { FieldDefinition } from '../../../composables/useCredentialCatalog'

// ── Helpers ───────────────────────────────────────────────────────────────────

function field(overrides: Partial<FieldDefinition> & { key: string; label: string }): FieldDefinition {
  return {
    type: 'text',
    required: true,
    stored_in: 'creds',
    ...overrides,
  }
}

const mountForm = (
  fields: FieldDefinition[],
  extra: Record<string, unknown> = {},
) =>
  mount(GenericCredentialForm, {
    props: { fields, testing: false, saving: false, ...extra },
    attachTo: document.body,
  })

// ── Loading / error states ────────────────────────────────────────────────────

describe('GenericCredentialForm — states', () => {
  it('shows loading state when loading=true', () => {
    const w = mountForm([], { loading: true })
    expect(w.find('[data-testid="catalog-loading"]').exists()).toBe(true)
    expect(w.find('[data-testid="catalog-error"]').exists()).toBe(false)
  })

  it('shows error state when error=true', () => {
    const w = mountForm([], { error: true })
    expect(w.find('[data-testid="catalog-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="catalog-loading"]').exists()).toBe(false)
  })

  it('error takes priority over loading', () => {
    const w = mountForm([], { loading: true, error: true })
    expect(w.find('[data-testid="catalog-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="catalog-loading"]').exists()).toBe(false)
  })
})

// ── Field rendering ───────────────────────────────────────────────────────────

describe('GenericCredentialForm — field rendering', () => {
  it('renders a text field', () => {
    const w = mountForm([field({ key: 'username', label: 'Username', type: 'text' })])
    expect(w.find('[data-testid="field-username"]').attributes('type')).toBe('text')
  })

  it('renders a password field with type=password', () => {
    const w = mountForm([field({ key: 'api_key', label: 'API Key', type: 'password' })])
    expect(w.find('[data-testid="field-api_key"]').attributes('type')).toBe('password')
  })

  it('renders a url field with type=url', () => {
    const w = mountForm([field({ key: 'endpoint', label: 'Endpoint', type: 'url' })])
    expect(w.find('[data-testid="field-endpoint"]').attributes('type')).toBe('url')
  })

  it('renders an email field with type=email', () => {
    const w = mountForm([field({ key: 'email', label: 'Email', type: 'email' })])
    expect(w.find('[data-testid="field-email"]').attributes('type')).toBe('email')
  })

  it('renders a select field', () => {
    const w = mountForm([field({
      key: 'region',
      label: 'Region',
      type: 'select',
      options: [
        { label: 'US', value: 'us' },
        { label: 'EU', value: 'eu' },
      ],
    })])
    expect(w.find('[data-testid="field-region"]').element.tagName).toBe('SELECT')
  })

  it('renders a subdomain field', () => {
    const w = mountForm([field({
      key: 'subdomain',
      label: 'Subdomain',
      type: 'subdomain',
      help_text: 'Your subdomain at *.zendesk.com',
    })])
    expect(w.find('[data-testid="field-subdomain"]').exists()).toBe(true)
  })

  it('renders help_text beneath the field', () => {
    const w = mountForm([field({
      key: 'token',
      label: 'Token',
      type: 'text',
      help_text: 'Find this in your account settings',
    })])
    expect(w.find('[data-testid="help-token"]').text()).toContain('Find this in your account settings')
  })

  it('marks optional fields with "(optional)"', () => {
    const w = mountForm([field({ key: 'note', label: 'Note', type: 'text', required: false })])
    expect(w.find('[data-testid="field-wrapper-note"]').text()).toContain('(optional)')
  })

  it('always renders the label field', () => {
    const w = mountForm([])
    expect(w.find('[data-testid="field-label"]').exists()).toBe(true)
  })

  it('pre-populates fields that have a catalog default', () => {
    const w = mountForm([field({
      key: 'site',
      label: 'Site',
      type: 'text',
      default: 'https://api.example.com',
    })])
    const el = w.find('[data-testid="field-site"]').element as HTMLInputElement
    expect(el.value).toBe('https://api.example.com')
  })
})

// ── Disabled states ───────────────────────────────────────────────────────────

describe('GenericCredentialForm — disabled states', () => {
  it('disables all inputs when testing=true', () => {
    const w = mountForm(
      [field({ key: 'token', label: 'Token', type: 'password' })],
      { testing: true },
    )
    const input = w.find('[data-testid="field-token"]').element as HTMLInputElement
    expect(input.disabled).toBe(true)
  })

  it('disables all inputs when saving=true', () => {
    const w = mountForm(
      [field({ key: 'token', label: 'Token', type: 'password' })],
      { saving: true },
    )
    const input = w.find('[data-testid="field-token"]').element as HTMLInputElement
    expect(input.disabled).toBe(true)
  })
})

// ── getPayload ────────────────────────────────────────────────────────────────

describe('GenericCredentialForm — getPayload()', () => {
  it('returns typed field values and empty label by default', async () => {
    const w = mountForm([
      field({ key: 'api_key', label: 'API Key', type: 'password' }),
      field({ key: 'app_key', label: 'App Key', type: 'password' }),
    ])
    await w.find('[data-testid="field-api_key"]').setValue('secret-api')
    await w.find('[data-testid="field-app_key"]').setValue('secret-app')

    const payload = (w.vm as any).getPayload()
    expect(payload).toEqual({ api_key: 'secret-api', app_key: 'secret-app', label: '' })
  })

  it('includes the label field in the payload', async () => {
    const w = mountForm([field({ key: 'token', label: 'Token', type: 'text' })])
    await w.find('[data-testid="field-label"]').setValue('production')

    const payload = (w.vm as any).getPayload()
    expect(payload.label).toBe('production')
  })

  it('returns catalog default value when user has not changed the field', () => {
    const w = mountForm([field({
      key: 'endpoint',
      label: 'Endpoint',
      type: 'url',
      default: 'http://localhost:8475',
    })])
    const payload = (w.vm as any).getPayload()
    expect(payload.endpoint).toBe('http://localhost:8475')
  })

  it('substitutes custom URL when select value is __custom__', async () => {
    const w = mountForm([field({
      key: 'site',
      label: 'Site',
      type: 'select',
      options: [
        { label: 'US1', value: 'https://api.datadoghq.com' },
        { label: 'Custom', value: '__custom__' },
      ],
    })])
    await w.find('[data-testid="field-site"]').setValue('__custom__')
    await flushPromises()
    await w.find('[data-testid="field-site-custom"]').setValue('https://my.custom.host')

    const payload = (w.vm as any).getPayload()
    expect(payload.site).toBe('https://my.custom.host')
  })

  it('uses the selected option value (not __custom__) when a normal option is chosen', async () => {
    const w = mountForm([field({
      key: 'site',
      label: 'Site',
      type: 'select',
      options: [
        { label: 'US1', value: 'https://api.datadoghq.com' },
        { label: 'EU1', value: 'https://api.datadoghq.eu' },
      ],
      default: 'https://api.datadoghq.com',
    })])
    await w.find('[data-testid="field-site"]').setValue('https://api.datadoghq.eu')

    const payload = (w.vm as any).getPayload()
    expect(payload.site).toBe('https://api.datadoghq.eu')
  })

  it('returns empty string for custom URL when __custom__ selected but nothing typed', async () => {
    const w = mountForm([field({
      key: 'site',
      label: 'Site',
      type: 'select',
      options: [{ label: 'Custom', value: '__custom__' }],
    })])
    await w.find('[data-testid="field-site"]').setValue('__custom__')
    await flushPromises()

    const payload = (w.vm as any).getPayload()
    expect(payload.site).toBe('')
  })

  it('handles multiple fields of mixed types correctly', async () => {
    const w = mountForm([
      field({ key: 'endpoint', label: 'Endpoint', type: 'url' }),
      field({ key: 'username', label: 'Username', type: 'text' }),
      field({ key: 'password', label: 'Password', type: 'password' }),
    ])
    await w.find('[data-testid="field-endpoint"]').setValue('http://localhost:8475')
    await w.find('[data-testid="field-username"]').setValue('root')
    await w.find('[data-testid="field-password"]').setValue('s3cr3t')

    const payload = (w.vm as any).getPayload()
    expect(payload).toMatchObject({
      endpoint: 'http://localhost:8475',
      username: 'root',
      password: 's3cr3t',
    })
  })
})
