import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CategoryNav from '../CategoryNav.vue'
import type { CatalogConnection } from '../../../composables/useConnectionsCatalog'

const EMPTY_CONNECTIONS: CatalogConnection[] = []

function mountNav(connections = EMPTY_CONNECTIONS, category = 'all') {
  return mount(CategoryNav, {
    props: { category, connections },
    attachTo: document.body,
  })
}

describe('CategoryNav', () => {
  it('renders All and My Connections as special items', () => {
    const w = mountNav()
    expect(w.text()).toContain('All')
    expect(w.text()).toContain('My Connections')
  })

  it('renders all expected category labels', () => {
    const w = mountNav()
    const text = w.text()
    expect(text).toContain('Communication')
    expect(text).toContain('Dev Tools')
    expect(text).toContain('Cloud')
    expect(text).toContain('Productivity')
    expect(text).toContain('Databases')
    expect(text).toContain('Observability')
    expect(text).toContain('System')
  })

  it('includes observability in categoryOrder', () => {
    // Verify observability connections are counted and the label renders.
    // Use a fake observability entry to confirm the category filter works.
    const connections: CatalogConnection[] = [
      {
        id: 'datadog', name: 'Datadog', description: 'Metrics',
        category: 'observability', icon: 'DD', iconColor: '#632ca6',
        type: 'credentials', multiAccount: false,
        state: { connected: true, identity: 'prod' },
      },
    ]
    const w = mount(CategoryNav, {
      props: { category: 'observability', connections },
    })
    expect(w.text()).toContain('Observability')
    // Connected count badge should appear (1 connected observability entry)
    expect(w.text()).toContain('1')
  })

  it('emits update:category when a category button is clicked', async () => {
    const w = mountNav()
    const buttons = w.findAll('button')
    // Find the "Cloud" button and click it
    const cloud = buttons.find(b => b.text().includes('Cloud'))
    expect(cloud).toBeDefined()
    await cloud!.trigger('click')
    expect(w.emitted('update:category')?.[0]).toEqual(['cloud'])
  })

  it('shows connected count badge on My Connections', () => {
    const connections: CatalogConnection[] = [
      {
        id: 'slack', name: 'Slack', description: '',
        category: 'communication', icon: 'S', iconColor: '#4A154B',
        type: 'oauth', multiAccount: true,
        state: { connected: true },
      },
    ]
    const w = mount(CategoryNav, {
      props: { category: 'all', connections },
    })
    // connectedCount = 1, should appear next to My Connections
    expect(w.text()).toContain('1')
  })

  it('shows skeleton rows when loading=true', () => {
    const w = mount(CategoryNav, {
      props: { category: 'all', connections: EMPTY_CONNECTIONS, loading: true },
    })
    // Skeleton rows replace category buttons — real labels should not appear
    expect(w.text()).not.toContain('Communication')
    expect(w.find('.animate-pulse').exists()).toBe(true)
  })

  it('shows real category buttons when loading=false', () => {
    const w = mount(CategoryNav, {
      props: { category: 'all', connections: EMPTY_CONNECTIONS, loading: false },
    })
    expect(w.text()).toContain('Communication')
    expect(w.find('.animate-pulse').exists()).toBe(false)
  })
})
