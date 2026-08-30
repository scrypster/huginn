import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHistory } from 'vue-router'
import MemoryView from '../MemoryView.vue'

const statusMock = vi.fn()
const vaultsMock = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    muninn: {
      status: (...args: unknown[]) => statusMock(...args),
      vaults: (...args: unknown[]) => vaultsMock(...args),
    },
  },
  apiFetch: vi.fn(),
}))

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/connections', component: { template: '<div/>' } },
  ],
})

describe('MemoryView', () => {
  beforeEach(() => {
    statusMock.mockReset()
    vaultsMock.mockReset()
  })

  it('shows a purposeful empty state with an icon and a Connect action when no vault is connected', async () => {
    statusMock.mockResolvedValue({ connected: false })
    const wrapper = mount(MemoryView, { global: { plugins: [router] } })
    await flushPromises()

    const empty = wrapper.find('[data-testid="memory-empty-no-vault"]')
    expect(empty.exists()).toBe(true)
    expect(empty.find('svg').exists()).toBe(true)
    expect(empty.text()).toContain('No memory vault connected')
    const link = empty.find('a')
    expect(link.exists()).toBe(true)
    expect(link.text()).toContain('Connect MuninnDB')
  })

  it('does not show the no-vault empty state once connected', async () => {
    statusMock.mockResolvedValue({ connected: true })
    vaultsMock.mockResolvedValue({ vaults: ['default'] })
    const wrapper = mount(MemoryView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.find('[data-testid="memory-empty-no-vault"]').exists()).toBe(false)
  })

  it('renders using huginn design tokens, not undefined CSS custom properties', async () => {
    statusMock.mockResolvedValue({ connected: false })
    const wrapper = mount(MemoryView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.html()).not.toContain('var(--color-')
  })
})
