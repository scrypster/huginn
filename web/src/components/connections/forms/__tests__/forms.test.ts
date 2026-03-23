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

import PagerDutyForm from '../PagerDutyForm.vue'
import NewRelicForm from '../NewRelicForm.vue'
import ElasticForm from '../ElasticForm.vue'
import GrafanaForm from '../GrafanaForm.vue'
import CrowdStrikeForm from '../CrowdStrikeForm.vue'
import TerraformForm from '../TerraformForm.vue'
import ServiceNowForm from '../ServiceNowForm.vue'

describe('PagerDutyForm', () => {
  it('renders api key field', () => {
    const w = mount(PagerDutyForm, { props })
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
  it('getPayload returns api_token (not api_key) and label', () => {
    const w = mount(PagerDutyForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('api_token')
    expect(payload).not.toHaveProperty('api_key')
    expect(payload).toHaveProperty('label')
    expect(payload.api_token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('NewRelicForm', () => {
  it('renders api key and account id fields', () => {
    const w = mount(NewRelicForm, { props })
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-account-id"]').exists()).toBe(true)
  })
  it('getPayload returns api_key, account_id, and label', () => {
    const w = mount(NewRelicForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('api_key')
    expect(payload).toHaveProperty('account_id')
    expect(payload).toHaveProperty('label')
    expect(payload.api_key).toBe('')
    expect(payload.account_id).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('ElasticForm', () => {
  it('renders base url and api key fields', () => {
    const w = mount(ElasticForm, { props })
    expect(w.find('[data-testid="field-base-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
  it('getPayload returns url (not base_url), api_key, and label', () => {
    const w = mount(ElasticForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('url')
    expect(payload).not.toHaveProperty('base_url')
    expect(payload).toHaveProperty('api_key')
    expect(payload).toHaveProperty('label')
    expect(payload.url).toBe('')
    expect(payload.api_key).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('GrafanaForm', () => {
  it('renders base url and api key fields', () => {
    const w = mount(GrafanaForm, { props })
    expect(w.find('[data-testid="field-base-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
  it('getPayload returns url (not base_url), token (not api_key), and label', () => {
    const w = mount(GrafanaForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('url')
    expect(payload).not.toHaveProperty('base_url')
    expect(payload).toHaveProperty('token')
    expect(payload).not.toHaveProperty('api_key')
    expect(payload).toHaveProperty('label')
    expect(payload.url).toBe('')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('CrowdStrikeForm', () => {
  it('renders client id and client secret fields', () => {
    const w = mount(CrowdStrikeForm, { props })
    expect(w.find('[data-testid="field-client-id"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-client-secret"]').exists()).toBe(true)
  })
  it('defaults base url to https://api.crowdstrike.com', () => {
    const w = mount(CrowdStrikeForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload.base_url).toBe('https://api.crowdstrike.com')
  })
  it('getPayload returns base_url, client_id, client_secret, and label', () => {
    const w = mount(CrowdStrikeForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('base_url')
    expect(payload).toHaveProperty('client_id')
    expect(payload).toHaveProperty('client_secret')
    expect(payload).toHaveProperty('label')
    expect(payload.client_id).toBe('')
    expect(payload.client_secret).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('TerraformForm', () => {
  it('renders api token field', () => {
    const w = mount(TerraformForm, { props })
    expect(w.find('[data-testid="field-api-token"]').exists()).toBe(true)
  })
  it('getPayload returns token (not api_token) and label', () => {
    const w = mount(TerraformForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).not.toHaveProperty('api_token')
    expect(payload).toHaveProperty('label')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('ServiceNowForm', () => {
  it('renders instance url, username, password fields', () => {
    const w = mount(ServiceNowForm, { props })
    expect(w.find('[data-testid="field-instance-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-username"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-password"]').exists()).toBe(true)
  })
  it('getPayload returns instance_url, username, password, and label', () => {
    const w = mount(ServiceNowForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('instance_url')
    expect(payload).toHaveProperty('username')
    expect(payload).toHaveProperty('password')
    expect(payload).toHaveProperty('label')
    expect(payload.instance_url).toBe('')
    expect(payload.username).toBe('')
    expect(payload.password).toBe('')
    expect(payload.label).toBe('')
  })
})

import AirtableForm from '../AirtableForm.vue'
import AsanaForm from '../AsanaForm.vue'
import HubSpotForm from '../HubSpotForm.vue'
import MondayForm from '../MondayForm.vue'
import NotionForm from '../NotionForm.vue'
import ZendeskForm from '../ZendeskForm.vue'

describe('AirtableForm', () => {
  it('renders api key field', () => {
    const w = mount(AirtableForm, { props })
    expect(w.find('[data-testid="field-api-key"]').exists()).toBe(true)
  })
  it('getPayload returns api_key and label', () => {
    const w = mount(AirtableForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('api_key')
    expect(payload).toHaveProperty('label')
    expect(payload.api_key).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('AsanaForm', () => {
  it('renders token field', () => {
    const w = mount(AsanaForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload returns token and label', () => {
    const w = mount(AsanaForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('HubSpotForm', () => {
  it('renders token field', () => {
    const w = mount(HubSpotForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload returns token and label', () => {
    const w = mount(HubSpotForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('MondayForm', () => {
  it('renders token field', () => {
    const w = mount(MondayForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload returns token and label', () => {
    const w = mount(MondayForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('NotionForm', () => {
  it('renders token field', () => {
    const w = mount(NotionForm, { props })
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload returns token and label', () => {
    const w = mount(NotionForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})

describe('ZendeskForm', () => {
  it('renders subdomain, email, and token fields', () => {
    const w = mount(ZendeskForm, { props })
    expect(w.find('[data-testid="field-subdomain"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-email"]').exists()).toBe(true)
    expect(w.find('[data-testid="field-token"]').exists()).toBe(true)
  })
  it('getPayload returns subdomain, email, token, and label', () => {
    const w = mount(ZendeskForm, { props })
    const payload = (w.vm as any).getPayload()
    expect(payload).toHaveProperty('subdomain')
    expect(payload).toHaveProperty('email')
    expect(payload).toHaveProperty('token')
    expect(payload).toHaveProperty('label')
    expect(payload.subdomain).toBe('')
    expect(payload.email).toBe('')
    expect(payload.token).toBe('')
    expect(payload.label).toBe('')
  })
})
