import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SlackBotForm from '../SlackBotForm.vue'
import JiraServiceForm from '../JiraServiceForm.vue'
import LinearForm from '../LinearForm.vue'
import GitLabForm from '../GitLabForm.vue'
import DiscordForm from '../DiscordForm.vue'
import VercelForm from '../VercelForm.vue'
import StripeForm from '../StripeForm.vue'

const props = { testing: false, saving: false }

describe('SlackBotForm', () => {
  it('renders bot token field', () => {
    const w = mount(SlackBotForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('exposes getPayload with token and label', () => {
    const w = mount(SlackBotForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
  })
})

describe('JiraServiceForm', () => {
  it('renders instance url, email, token fields', () => {
    const w = mount(JiraServiceForm, { props })
    expect(w.find('[data-testid="field-instance-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-email"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload includes instance_url, email, token', () => {
    const w = mount(JiraServiceForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('instance_url')
    expect(payload).toHaveProperty('email')
    expect(payload).toHaveProperty('token')
  })
})

describe('LinearForm', () => {
  it('renders api key field', () => {
    const w = mount(LinearForm, { props })
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
})

describe('GitLabForm', () => {
  it('renders base url and token fields', () => {
    const w = mount(GitLabForm, { props })
    expect(w.find('[data-testid="field-base-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('defaults base url to https://gitlab.com', () => {
    const w = mount(GitLabForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload.base_url).toBe('https://gitlab.com')
  })
})

describe('DiscordForm', () => {
  it('renders bot token field', () => {
    const w = mount(DiscordForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
})

describe('VercelForm', () => {
  it('renders api token field', () => {
    const w = mount(VercelForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
})

describe('StripeForm', () => {
  it('renders secret key field', () => {
    const w = mount(StripeForm, { props })
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
})
