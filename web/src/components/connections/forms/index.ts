import type { Component } from 'vue'
import MuninnForm from './MuninnForm.vue'
import DatadogForm from './DatadogForm.vue'
import SplunkForm from './SplunkForm.vue'
import SlackBotForm from './SlackBotForm.vue'
import JiraServiceForm from './JiraServiceForm.vue'
import LinearForm from './LinearForm.vue'
import GitLabForm from './GitLabForm.vue'
import DiscordForm from './DiscordForm.vue'
import VercelForm from './VercelForm.vue'
import StripeForm from './StripeForm.vue'
import PagerDutyForm from './PagerDutyForm.vue'
import NewRelicForm from './NewRelicForm.vue'
import ElasticForm from './ElasticForm.vue'
import GrafanaForm from './GrafanaForm.vue'
import CrowdStrikeForm from './CrowdStrikeForm.vue'
import TerraformForm from './TerraformForm.vue'
import ServiceNowForm from './ServiceNowForm.vue'
import NotionForm from './NotionForm.vue'
import AirtableForm from './AirtableForm.vue'
import HubSpotForm from './HubSpotForm.vue'
import ZendeskForm from './ZendeskForm.vue'
import AsanaForm from './AsanaForm.vue'
import MondayForm from './MondayForm.vue'

export type CredentialProvider =
  | 'muninn' | 'datadog' | 'splunk'
  | 'slack_bot' | 'jira_sa' | 'linear' | 'gitlab'
  | 'discord' | 'vercel' | 'stripe'
  | 'pagerduty' | 'newrelic' | 'elastic' | 'grafana'
  | 'crowdstrike' | 'terraform' | 'servicenow'
  | 'notion' | 'airtable' | 'hubspot' | 'zendesk' | 'asana' | 'monday'

export interface ProviderMeta {
  name: string
  icon: string
  iconColor: string
}

export const PROVIDER_META: Record<CredentialProvider, ProviderMeta> = {
  muninn:      { name: 'MuninnDB',              icon: 'M',   iconColor: '#58a6ff' },
  datadog:     { name: 'Datadog',               icon: 'DD',  iconColor: '#632ca6' },
  splunk:      { name: 'Splunk',                icon: 'SP',  iconColor: '#000000' },
  slack_bot:   { name: 'Slack (Bot)',           icon: 'SB',  iconColor: '#4a154b' },
  jira_sa:     { name: 'Jira (Service Account)', icon: 'JS', iconColor: '#0052cc' },
  linear:      { name: 'Linear',               icon: 'LN',  iconColor: '#5e6ad2' },
  gitlab:      { name: 'GitLab',               icon: 'GL',  iconColor: '#fc6d26' },
  discord:     { name: 'Discord',              icon: 'DC',  iconColor: '#5865f2' },
  vercel:      { name: 'Vercel',               icon: 'V',   iconColor: '#000000' },
  stripe:      { name: 'Stripe',               icon: 'S',   iconColor: '#635bff' },
  pagerduty:   { name: 'PagerDuty',            icon: 'PD',  iconColor: '#06ac38' },
  newrelic:    { name: 'New Relic',            icon: 'NR',  iconColor: '#1ce783' },
  elastic:     { name: 'Elastic',              icon: 'ES',  iconColor: '#005571' },
  grafana:     { name: 'Grafana',              icon: 'GF',  iconColor: '#f46800' },
  crowdstrike: { name: 'CrowdStrike',          icon: 'CS',  iconColor: '#e4002b' },
  terraform:   { name: 'Terraform Cloud',      icon: 'TF',  iconColor: '#7b42bc' },
  servicenow:  { name: 'ServiceNow',           icon: 'SN',  iconColor: '#81b5a1' },
  notion:      { name: 'Notion',               icon: 'N',   iconColor: '#191919' },
  airtable:    { name: 'Airtable',             icon: 'AT',  iconColor: '#18bfff' },
  hubspot:     { name: 'HubSpot',              icon: 'HS',  iconColor: '#ff7a59' },
  zendesk:     { name: 'Zendesk',              icon: 'ZD',  iconColor: '#03363d' },
  asana:       { name: 'Asana',                icon: 'AS',  iconColor: '#f06a6a' },
  monday:      { name: 'Monday.com',           icon: 'MN',  iconColor: '#f65d50' },
}

export const FORM_COMPONENTS: Record<CredentialProvider, Component> = {
  muninn:      MuninnForm,
  datadog:     DatadogForm,
  splunk:      SplunkForm,
  slack_bot:   SlackBotForm,
  jira_sa:     JiraServiceForm,
  linear:      LinearForm,
  gitlab:      GitLabForm,
  discord:     DiscordForm,
  vercel:      VercelForm,
  stripe:      StripeForm,
  pagerduty:   PagerDutyForm,
  newrelic:    NewRelicForm,
  elastic:     ElasticForm,
  grafana:     GrafanaForm,
  crowdstrike: CrowdStrikeForm,
  terraform:   TerraformForm,
  servicenow:  ServiceNowForm,
  notion:      NotionForm,
  airtable:    AirtableForm,
  hubspot:     HubSpotForm,
  zendesk:     ZendeskForm,
  asana:       AsanaForm,
  monday:      MondayForm,
}
