import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CompanyRosterPicker from '../CompanyRosterPicker.vue'

const agents = [
  { name: 'Winston', color: '#58a6ff', icon: 'W', model: 'sonnet', is_default: true },
  { name: 'Steve', color: '#3fb950', icon: 'S', model: 'sonnet' },
  { name: 'Reggie', color: '#d29922', icon: 'R', model: 'haiku' },
  { name: 'Sam', color: '#f78166', icon: 'S', model: 'sonnet' },
]

const companies = [
  { id: 'huginn', name: 'Huginn', icon: 'H', color: '#58a6ff', members: ['Winston', 'Steve', 'Reggie'] },
  { id: 'lab', name: 'Lab', icon: 'L', color: '#3fb950', members: ['Winston', 'Sam'] },
]

describe('CompanyRosterPicker', () => {
  it('Lab roster picker does not list Huginn-only Reggie', () => {
    const wrapper = mount(CompanyRosterPicker, {
      props: {
        agents,
        seated: ['Winston', 'Sam'],
        companies,
        companyId: 'lab',
        mode: 'roster',
      },
    })
    expect(wrapper.find('[data-testid="company-roster-agent-Winston"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-roster-agent-Sam"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-roster-agent-Reggie"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="company-roster-agent-Steve"]').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/workspace|org|project/i)
  })

  it('seat mode lists known agents and marks who is already seated', () => {
    const wrapper = mount(CompanyRosterPicker, {
      props: {
        agents,
        seated: ['Winston', 'Sam'],
        companies,
        companyId: 'lab',
        mode: 'seat',
      },
    })
    expect(wrapper.find('[data-testid="company-roster-agent-Reggie"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-roster-seat-Reggie"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-roster-unseat-Winston"]').exists()).toBe(true)
  })

  it('confirmExternally emits unseat immediately instead of the inline Unseat/No row', async () => {
    const wrapper = mount(CompanyRosterPicker, {
      props: {
        agents,
        seated: ['Winston', 'Sam'],
        companies,
        companyId: 'lab',
        mode: 'seat',
        confirmExternally: true,
      },
    })
    await wrapper.get('[data-testid="company-roster-unseat-Winston"]').trigger('click')
    expect(wrapper.emitted('unseat')).toEqual([['Winston']])
    expect(wrapper.find('[data-testid="company-roster-unseat-confirm-Winston"]').exists()).toBe(false)
  })
})
