import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import CompanyCreateModal from '../CompanyCreateModal.vue'
import { useCompanies } from '../../composables/useCompanies'
import { setToken } from '../../composables/useApi'

const agents = [
  { name: 'Winston', color: '#58a6ff', icon: 'W', model: 'sonnet', is_default: true },
  { name: 'Steve', color: '#3fb950', icon: 'S', model: 'sonnet' },
  { name: 'Reggie', color: '#d29922', icon: 'R', model: 'haiku' },
  { name: 'Sam', color: '#f78166', icon: 'S', model: 'sonnet' },
]

vi.mock('../../composables/useAgents', () => {
  const { ref } = require('vue')
  return {
    useAgents: () => ({
      agents: ref([
        { name: 'Winston', color: '#58a6ff', icon: 'W', model: 'sonnet', is_default: true },
        { name: 'Steve', color: '#3fb950', icon: 'S', model: 'sonnet' },
        { name: 'Reggie', color: '#d29922', icon: 'R', model: 'haiku' },
        { name: 'Sam', color: '#f78166', icon: 'S', model: 'sonnet' },
      ]),
      fetchAgents: vi.fn().mockResolvedValue(undefined),
    }),
  }
})

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  setToken('test-token')
  localStorage.clear()
  useCompanies().clearCompanies()
  vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url.includes('/api/v1/companies') && init?.method === 'POST' && !url.includes('/members')) {
      return Promise.resolve(okJson({
        id: 'lab-1', name: 'Lab', icon: 'L', color: '#58a6ff', members: ['Winston'],
      }, 201))
    }
    if (url.includes('/api/v1/companies')) return Promise.resolve(okJson([]))
    return Promise.resolve(okJson({}))
  })
})

afterEach(() => {
  setToken('')
  vi.restoreAllMocks()
  localStorage.clear()
  useCompanies().clearCompanies()
})

describe('CompanyCreateModal', () => {
  it('create-company flow renders name step, not a vault-first form dump', async () => {
    const wrapper = mount(CompanyCreateModal)
    await flushPromises()
    const text = wrapper.get('[data-testid="company-create-modal"]').text()
    expect(text).toContain('New company')
    expect(text).toContain('Company')
    expect(text).not.toMatch(/workspace|org|project/i)
    expect(wrapper.find('[data-testid="company-name-input"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-vault-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="company-create-continue"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="company-name-input"]').setValue('Lab')
    await nextTick()
    expect(wrapper.get('[data-testid="company-create-continue"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="company-create-continue"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="company-roster-picker"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-create-submit"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Seat people')
  })
})
