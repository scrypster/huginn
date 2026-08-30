import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import CompanySeatPicker from '../CompanySeatPicker.vue'

const companies = [
  { id: 'huginn', name: 'Huginn', icon: 'H', color: '#58a6ff', members: ['Winston', 'Steve', 'Reggie'] },
  { id: 'lab', name: 'Lab', icon: 'L', color: '#3fb950', members: ['Winston', 'Sam'] },
  { id: 'acme', name: 'Acme', icon: 'A', color: '#d29922', members: [] },
]

const agents = [
  { name: 'Winston', color: '#58a6ff', icon: 'W', model: 'sonnet', is_default: true },
  { name: 'Steve', color: '#3fb950', icon: 'S', model: 'sonnet' },
  { name: 'Reggie', color: '#d29922', icon: 'R', model: 'haiku' },
  { name: 'Sam', color: '#f78166', icon: 'S', model: 'sonnet' },
]

function manyCompanies(n = 30) {
  return Array.from({ length: n }, (_, i) => {
    const num = String(i + 1).padStart(2, '0')
    return {
      id: `c${num}`,
      name: `Company ${num}`,
      icon: num.slice(-1),
      color: '#58a6ff',
      members: i < 2 ? ['Winston'] : [],
    }
  })
}

describe('CompanySeatPicker', () => {
  it('opens a searchable picker, not dead Already-in rows', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companies, mode: 'companies' },
    })
    expect(wrapper.find('[data-testid="company-seat-search"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-seat-join-acme"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-seat-join-huginn"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="company-seat-join-lab"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="company-seat-seated-huginn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-seat-seated-lab"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="company-seat-status"]').text()).toBe('In Huginn, Lab')
    expect(wrapper.text()).not.toMatch(/Already in/)
    expect(wrapper.html()).not.toMatch(/workspace|org|project/i)
    expect(wrapper.html()).not.toContain('text-huginn-muted/30')
  })

  it('filters by keyboard and stays usable at 30 companies', async () => {
    const list = manyCompanies(30)
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companies: list, mode: 'companies' },
    })
    expect(wrapper.findAll('[data-testid^="company-seat-join-"]')).toHaveLength(28)
    await wrapper.get('[data-testid="company-seat-search"]').setValue('12')
    await nextTick()
    expect(wrapper.find('[data-testid="company-seat-join-c12"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid^="company-seat-join-"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="company-seat-join-c03"]').exists()).toBe(false)
  })

  it('confirms before seating and does not emit on cancel', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companies, mode: 'companies' },
    })
    await wrapper.get('[data-testid="company-seat-join-acme"]').trigger('click')
    await nextTick()
    expect(wrapper.get('[data-testid="company-seat-confirm"]').text()).toContain('Add Winston to Acme?')
    await wrapper.get('[data-testid="company-seat-confirm-cancel"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="company-seat-confirm"]').exists()).toBe(false)
    expect(wrapper.emitted('seat')).toBeFalsy()

    await wrapper.get('[data-testid="company-seat-join-acme"]').trigger('click')
    await wrapper.get('[data-testid="company-seat-confirm-add"]').trigger('click')
    expect(wrapper.emitted('seat')).toEqual([['Winston', 'acme']])
  })

  it('confirms before unseating a quiet seated company', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companies, mode: 'companies' },
    })
    await wrapper.get('[data-testid="company-seat-unseat-lab"]').trigger('click')
    await nextTick()
    expect(wrapper.get('[data-testid="company-seat-confirm"]').text()).toContain('Remove Winston from Lab?')
    await wrapper.get('[data-testid="company-seat-confirm-cancel"]').trigger('click')
    await nextTick()
    expect(wrapper.emitted('unseat')).toBeFalsy()

    await wrapper.get('[data-testid="company-seat-unseat-lab"]').trigger('click')
    await wrapper.get('[data-testid="company-seat-confirm-remove"]').trigger('click')
    expect(wrapper.emitted('unseat')).toEqual([['Winston', 'lab']])
  })

  it('drag-drop confirm mode asks before write', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companyId: 'acme', companies, mode: 'confirm' },
    })
    await nextTick()
    expect(wrapper.get('[data-testid="company-seat-confirm"]').text()).toContain('Add Winston to Acme?')
    expect(wrapper.find('[data-testid="company-seat-search"]').exists()).toBe(false)
    await wrapper.get('[data-testid="company-seat-confirm-add"]').trigger('click')
    expect(wrapper.emitted('seat')).toEqual([['Winston', 'acme']])
  })

  it('people mode from company-rail plus confirms before seat', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: {
        companyId: 'lab',
        companies,
        agents,
        mode: 'people',
      },
    })
    expect(wrapper.text()).toContain('Add people to Lab')
    expect(wrapper.find('[data-testid="company-roster-picker"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="company-roster-seat-Steve"]').exists()).toBe(true)
    await wrapper.get('[data-testid="company-roster-seat-Steve"]').trigger('click')
    await nextTick()
    expect(wrapper.get('[data-testid="company-seat-confirm"]').text()).toContain('Add Steve to Lab?')
    await wrapper.get('[data-testid="company-seat-confirm-add"]').trigger('click')
    expect(wrapper.emitted('seat')).toEqual([['Steve', 'lab']])
  })

  it('Enter on a filtered joinable row opens confirm', async () => {
    const wrapper = mount(CompanySeatPicker, {
      props: { agent: 'Winston', companies, mode: 'companies' },
    })
    await wrapper.get('[data-testid="company-seat-search"]').setValue('Acme')
    await nextTick()
    await wrapper.get('[data-testid="company-seat-picker"]').trigger('keydown', { key: 'Enter' })
    await nextTick()
    expect(wrapper.get('[data-testid="company-seat-confirm"]').text()).toContain('Add Winston to Acme?')
  })
})
