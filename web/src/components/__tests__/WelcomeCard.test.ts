import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createWebHistory } from 'vue-router'
import WelcomeCard from '../WelcomeCard.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/', component: { template: '<div/>' } }, { path: '/settings', component: { template: '<div/>' } }],
})

async function mountCard(props: Record<string, unknown> = {}) {
  const wrapper = mount(WelcomeCard, {
    props: { agentName: 'Fable', ...props },
    global: { plugins: [router] },
  })
  await router.isReady()
  return wrapper
}

describe('WelcomeCard', () => {
  it('renders the agent name and the three example prompts', async () => {
    const wrapper = await mountCard()
    expect(wrapper.text()).toContain('Fable')
    const examples = wrapper.findAll('[data-testid="welcome-card-example"]')
    expect(examples).toHaveLength(3)
    expect(wrapper.text()).toContain('hire a teammate')
    expect(wrapper.text()).toContain('ask me the time')
    expect(wrapper.text()).toContain('give me a coding task')
  })

  it('emits dismiss when the close button is clicked', async () => {
    const wrapper = await mountCard()
    await wrapper.find('[data-testid="welcome-card-dismiss"]').trigger('click')
    expect(wrapper.emitted('dismiss')).toBeTruthy()
  })

  it('emits use-example with the prompt text when an example is clicked', async () => {
    const wrapper = await mountCard()
    const examples = wrapper.findAll('[data-testid="welcome-card-example"]')
    await examples[0]!.trigger('click')
    expect(wrapper.emitted('use-example')?.[0]).toEqual(['hire a teammate'])
  })

  it('shows a Settings pointer when no model is configured', async () => {
    const wrapper = await mountCard({ modelConfigured: false })
    expect(wrapper.find('[data-testid="welcome-card-model-hint"]').exists()).toBe(true)
  })

  it('hides the Settings pointer when a model is configured', async () => {
    const wrapper = await mountCard({ modelConfigured: true })
    expect(wrapper.find('[data-testid="welcome-card-model-hint"]').exists()).toBe(false)
  })
})
