import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import CompanySwitcher from '../CompanySwitcher.vue'
import { useSpaces } from '../../composables/useSpaces'
import { useCompanies } from '../../composables/useCompanies'
import { setToken } from '../../composables/useApi'

function okJson(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

const companiesPayload = [
  { id: 'acme', name: 'Acme', icon: '', color: '#58a6ff', members: [] },
  { id: 'globex', name: 'Globex', icon: '', color: '#3fb950', members: [] },
]

const spacesPayload = [
  { id: 'ch-desk', name: 'general', kind: 'channel', lead_agent: 'winston', member_agents: [], icon: '', color: '#58a6ff', unseen_count: 0, company_id: '' },
  { id: 'ch-acme', name: 'acme-eng', kind: 'channel', lead_agent: 'atlas', member_agents: [], icon: '', color: '#58a6ff', unseen_count: 0, company_id: 'acme' },
  { id: 'ch-globex', name: 'globex-ops', kind: 'channel', lead_agent: 'hermes', member_agents: [], icon: '', color: '#58a6ff', unseen_count: 0, company_id: 'globex' },
  { id: 'dm-desk', name: 'winston', kind: 'dm', lead_agent: 'winston', member_agents: [], icon: '', color: '#58a6ff', unseen_count: 0, company_id: '' },
  { id: 'dm-acme', name: 'acme-bot', kind: 'dm', lead_agent: 'acme-bot', member_agents: [], icon: '', color: '#58a6ff', unseen_count: 0, company_id: 'acme' },
]

const RailHarness = defineComponent({
  components: { CompanySwitcher },
  setup() {
    const { channels, dms } = useSpaces()
    return { channels, dms }
  },
  template: `
    <div>
      <CompanySwitcher />
      <div data-testid="rail">
        <div v-for="s in channels" :key="s.id" :data-testid="'rail-channel-' + s.id">{{ s.name }}</div>
        <div v-for="s in dms" :key="s.id" :data-testid="'rail-dm-' + s.id">{{ s.name }}</div>
      </div>
    </div>
  `,
})

function railIds(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper.findAll('[data-testid^="rail-"]').map(n => n.attributes('data-testid')!)
}

beforeEach(() => {
  setToken('test-token')
  localStorage.clear()
  useSpaces().clearSpaces()
  useCompanies().clearCompanies()
  vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/v1/companies')) return Promise.resolve(okJson(companiesPayload))
    if (url.includes('/api/v1/spaces')) return Promise.resolve(okJson({ Spaces: spacesPayload, NextCursor: '' }))
    return Promise.resolve(okJson({}))
  })
})

afterEach(() => {
  setToken('')
  vi.restoreAllMocks()
  localStorage.clear()
  useSpaces().clearSpaces()
  useCompanies().clearCompanies()
})

describe('CompanySwitcher', () => {
  it('labels the control Company and defaults to Desk', async () => {
    const wrapper = mount(RailHarness)
    await flushPromises()
    const text = wrapper.find('[data-testid="company-switcher"]').text()
    expect(text).toContain('Company')
    expect(text).toContain('Desk')
    expect(text).not.toMatch(/workspace|org|team/i)
    expect(wrapper.html()).not.toMatch(/Huginn Cloud|machine_id|Connect to Huginn/i)
  })

  it('desk shows every channel and DM; picking a company filters the rail', async () => {
    const { fetchSpaces } = useSpaces()
    await fetchSpaces()

    const wrapper = mount(RailHarness)
    await flushPromises()

    expect(railIds(wrapper).sort()).toEqual([
      'rail-channel-ch-acme',
      'rail-channel-ch-desk',
      'rail-channel-ch-globex',
      'rail-dm-dm-acme',
      'rail-dm-dm-desk',
    ])

    await wrapper.get('[data-testid="company-switcher-trigger"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="company-switcher-menu"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="company-switcher-desk"]').text()).toContain('Desk')
    expect(wrapper.get('[data-testid="company-switcher-option-acme"]').text()).toContain('Acme')

    await wrapper.get('[data-testid="company-switcher-option-acme"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(railIds(wrapper).sort()).toEqual([
      'rail-channel-ch-acme',
      'rail-dm-dm-acme',
    ])
    expect(wrapper.get('[data-testid="company-switcher-trigger"]').text()).toContain('Acme')
    expect(localStorage.getItem('huginn_selected_company_id')).toBe('acme')

    await wrapper.get('[data-testid="company-switcher-trigger"]').trigger('click')
    await nextTick()
    await wrapper.get('[data-testid="company-switcher-desk"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(railIds(wrapper).sort()).toEqual([
      'rail-channel-ch-acme',
      'rail-channel-ch-desk',
      'rail-channel-ch-globex',
      'rail-dm-dm-acme',
      'rail-dm-dm-desk',
    ])
    expect(wrapper.get('[data-testid="company-switcher-trigger"]').text()).toContain('Desk')
  })

  it('empty companies list stays on desk and still shows the control', async () => {
    vi.mocked(globalThis.fetch).mockImplementation((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/v1/companies')) {
        return Promise.resolve(new Response('missing', { status: 404 }))
      }
      if (url.includes('/api/v1/spaces')) return Promise.resolve(okJson({ Spaces: spacesPayload, NextCursor: '' }))
      return Promise.resolve(okJson({}))
    })
    const { fetchSpaces } = useSpaces()
    await fetchSpaces()

    const wrapper = mount(RailHarness)
    await flushPromises()

    expect(wrapper.get('[data-testid="company-switcher-trigger"]').text()).toContain('Desk')
    expect(railIds(wrapper).length).toBe(5)
  })
})
