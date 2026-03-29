import type { Component } from 'vue'
import MuninnForm from './MuninnForm.vue'
import SlackBotForm from './SlackBotForm.vue'
import JiraServiceForm from './JiraServiceForm.vue'
import LinearForm from './LinearForm.vue'
import GitLabForm from './GitLabForm.vue'
import DiscordForm from './DiscordForm.vue'
import VercelForm from './VercelForm.vue'
import StripeForm from './StripeForm.vue'

// Bespoke providers: OAuth / non-API-key auth that cannot be handled by the
// generic catalog form.  All other providers use GenericCredentialForm driven
// by the server-side credential catalog.
export type CredentialProvider =
  | 'muninn'
  | 'slack_bot' | 'jira_sa' | 'linear' | 'gitlab'
  | 'discord' | 'vercel' | 'stripe'
  // Catalog providers (15) — CredentialModal routes these to GenericCredentialForm.
  | 'datadog' | 'splunk' | 'pagerduty' | 'newrelic' | 'elastic' | 'grafana'
  | 'crowdstrike' | 'terraform' | 'servicenow'
  | 'notion' | 'airtable' | 'hubspot' | 'zendesk' | 'asana' | 'monday'

export interface ProviderMeta {
  name: string
  icon: string
  iconColor: string
}

// Metadata for bespoke providers only.  Catalog providers get their metadata
// from the server-side catalog entry (CredentialModal prefers catalogEntry).
export const PROVIDER_META: Partial<Record<CredentialProvider, ProviderMeta>> = {
  muninn:    { name: 'MuninnDB',               icon: 'M',  iconColor: '#58a6ff' },
  slack_bot: { name: 'Slack (Bot)',            icon: 'SB', iconColor: '#4a154b' },
  jira_sa:   { name: 'Jira (Service Account)', icon: 'JS', iconColor: '#0052cc' },
  linear:    { name: 'Linear',                icon: 'LN', iconColor: '#5e6ad2' },
  gitlab:    { name: 'GitLab',                icon: 'GL', iconColor: '#fc6d26' },
  discord:   { name: 'Discord',               icon: 'DC', iconColor: '#5865f2' },
  vercel:    { name: 'Vercel',                icon: 'V',  iconColor: '#000000' },
  stripe:    { name: 'Stripe',                icon: 'S',  iconColor: '#635bff' },
}

// Only bespoke providers have dedicated form components.
export const FORM_COMPONENTS: Partial<Record<CredentialProvider, Component>> = {
  muninn:    MuninnForm,
  slack_bot: SlackBotForm,
  jira_sa:   JiraServiceForm,
  linear:    LinearForm,
  gitlab:    GitLabForm,
  discord:   DiscordForm,
  vercel:    VercelForm,
  stripe:    StripeForm,
}
