// web/src/composables/useConnectionsCatalog.ts

export type ConnectionCategory =
  | 'all'
  | 'my_connections'
  | 'communication'
  | 'dev_tools'
  | 'cloud'
  | 'productivity'
  | 'databases'
  | 'observability'
  | 'system'

export const CATEGORY_LABELS: Record<ConnectionCategory, string> = {
  all:            'All',
  my_connections: 'My Connections',
  communication:  'Communication',
  dev_tools:      'Dev Tools',
  cloud:          'Cloud',
  productivity:   'Productivity',
  databases:      'Databases',
  observability:  'Observability',
  system:         'System',
}

export interface CatalogEntry {
  id: string                       // join key: matches provider name, tool name, or 'muninn'
  name: string
  description: string
  category: ConnectionCategory
  icon: string                     // 1-3 char abbreviation
  iconColor: string                // hex background color for icon badge
  type: 'oauth' | 'system' | 'database' | 'credentials' | 'coming_soon'
  multiAccount?: boolean           // can connect multiple accounts
}

export interface AccountEntry {
  id: string      // connection UUID — used for DELETE /api/v1/connections/:id
  label: string   // email or display name shown in the UI
}

export interface ConnectionState {
  connected: boolean
  accounts?: AccountEntry[]        // OAuth: all accounts for this provider
  identity?: string                // system tools / single-account fallback
  profiles?: string[]              // AWS named profiles, gcloud configurations
  error?: string
}

export interface CatalogConnection extends CatalogEntry {
  state: ConnectionState | null    // null = coming_soon or not yet loaded
}

export const CATALOG: CatalogEntry[] = [
  // ── Communication ────────────────────────────────────────────────────────
  {
    id:          'slack',
    name:        'Slack',
    description: 'Send messages, read channels, search conversations',
    category:    'communication',
    icon:        '#',
    iconColor:   '#4a154b',
    type:        'oauth',
    multiAccount: true,
  },
  {
    id:          'slack_bot',
    name:        'Slack (Bot)',
    description: 'Send messages, read channels, search conversations via bot token',
    category:    'communication',
    icon:        'SB',
    iconColor:   '#4a154b',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'discord',
    name:        'Discord',
    description: 'Read channels, send messages, manage servers',
    category:    'communication',
    icon:        'DC',
    iconColor:   '#5865f2',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'teams',
    name:        'Microsoft Teams',
    description: 'Messages, meetings, channels',
    category:    'communication',
    icon:        'MT',
    iconColor:   '#6264a7',
    type:        'coming_soon',
  },

  // ── Dev Tools ─────────────────────────────────────────────────────────────
  {
    id:          'github',
    name:        'GitHub',
    description: 'Repositories, pull requests, issues, workflows',
    category:    'dev_tools',
    icon:        'GH',
    iconColor:   '#333333',
    type:        'oauth',
    multiAccount: false,
  },
  {
    id:          'bitbucket',
    name:        'Bitbucket',
    description: 'Repositories, pull requests, pipelines',
    category:    'dev_tools',
    icon:        'BB',
    iconColor:   '#0052cc',
    type:        'oauth',
    multiAccount: false,
  },
  {
    id:          'jira',
    name:        'Jira',
    description: 'Issues, projects, sprints, boards',
    category:    'dev_tools',
    icon:        'J',
    iconColor:   '#0052cc',
    type:        'oauth',
    multiAccount: false,
  },
  {
    id:          'jira_sa',
    name:        'Jira (Service Account)',
    description: 'Issues, projects, sprints via service account API token',
    category:    'dev_tools',
    icon:        'JS',
    iconColor:   '#0052cc',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'linear',
    name:        'Linear',
    description: 'Issues, projects, cycles, roadmaps',
    category:    'dev_tools',
    icon:        'LN',
    iconColor:   '#5e6ad2',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'gitlab',
    name:        'GitLab',
    description: 'Repositories, merge requests, CI/CD pipelines',
    category:    'dev_tools',
    icon:        'GL',
    iconColor:   '#fc6d26',
    type:        'credentials',
    multiAccount: false,
  },

  // ── Cloud ─────────────────────────────────────────────────────────────────
  {
    id:          'aws',
    name:        'AWS',
    description: 'S3, Lambda, EC2, and all AWS services',
    category:    'cloud',
    icon:        'AWS',
    iconColor:   '#ff9900',
    type:        'system',
  },
  {
    id:          'gcloud',
    name:        'Google Cloud',
    description: 'GCS, BigQuery, Cloud Run, and other GCP services',
    category:    'cloud',
    icon:        'GC',
    iconColor:   '#4285f4',
    type:        'system',
  },
  {
    id:          'azure',
    name:        'Azure',
    description: 'Storage, Functions, Kubernetes, and Azure services',
    category:    'cloud',
    icon:        'AZ',
    iconColor:   '#0078d4',
    type:        'coming_soon',
  },
  {
    id:          'vercel',
    name:        'Vercel',
    description: 'Deployments, domains, environment variables',
    category:    'cloud',
    icon:        'V',
    iconColor:   '#000000',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'terraform',
    name:        'Terraform Cloud',
    description: 'Workspaces, runs, variables, plans',
    category:    'cloud',
    icon:        'TF',
    iconColor:   '#7b42bc',
    type:        'credentials',
    multiAccount: false,
  },

  // ── Productivity ──────────────────────────────────────────────────────────
  {
    id:          'google',
    name:        'Google',
    description: 'Gmail, Google Drive, Google Calendar',
    category:    'productivity',
    icon:        'G',
    iconColor:   '#4285f4',
    type:        'oauth',
    multiAccount: true,
  },
  {
    id:          'notion',
    name:        'Notion',
    description: 'Pages, databases, search',
    category:    'productivity',
    icon:        'N',
    iconColor:   '#191919',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'airtable',
    name:        'Airtable',
    description: 'Bases, tables, records',
    category:    'productivity',
    icon:        'AT',
    iconColor:   '#18bfff',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'hubspot',
    name:        'HubSpot',
    description: 'Contacts, companies, deals',
    category:    'productivity',
    icon:        'HS',
    iconColor:   '#ff7a59',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'asana',
    name:        'Asana',
    description: 'Projects, tasks, workspaces',
    category:    'productivity',
    icon:        'AS',
    iconColor:   '#f06a6a',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'monday',
    name:        'Monday.com',
    description: 'Boards, items, workflows',
    category:    'productivity',
    icon:        'MN',
    iconColor:   '#f65d50',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'stripe',
    name:        'Stripe',
    description: 'Payments, subscriptions, customers',
    category:    'productivity',
    icon:        'S',
    iconColor:   '#635bff',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'servicenow',
    name:        'ServiceNow',
    description: 'Incidents, changes, service catalog, CMDB',
    category:    'productivity',
    icon:        'SN',
    iconColor:   '#81b5a1',
    type:        'credentials',
    multiAccount: false,
  },

  // ── Databases ─────────────────────────────────────────────────────────────
  {
    id:          'muninn',
    name:        'MuninnDB',
    description: 'Agent memory and long-term knowledge storage',
    category:    'databases',
    icon:        'M',
    iconColor:   '#58a6ff',
    type:        'database',
  },

  // ── Observability ────────────────────────────────────────────────────────
  {
    id:          'datadog',
    name:        'Datadog',
    description: 'Metrics, logs, monitors, dashboards, events, hosts, SLOs',
    category:    'observability',
    icon:        'DD',
    iconColor:   '#632ca6',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'splunk',
    name:        'Splunk',
    description: 'Search logs, indexes, saved searches, alerts, dashboards',
    category:    'observability',
    icon:        'SP',
    iconColor:   '#000000',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'pagerduty',
    name:        'PagerDuty',
    description: 'Incidents, services, on-call schedules, alerts',
    category:    'observability',
    icon:        'PD',
    iconColor:   '#06ac38',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'newrelic',
    name:        'New Relic',
    description: 'NRQL queries, alerts, dashboards, services, entities',
    category:    'observability',
    icon:        'NR',
    iconColor:   '#1ce783',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'elastic',
    name:        'Elastic',
    description: 'Search indices, cluster health, documents',
    category:    'observability',
    icon:        'ES',
    iconColor:   '#005571',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'grafana',
    name:        'Grafana',
    description: 'Dashboards, alerts, data sources, search',
    category:    'observability',
    icon:        'GF',
    iconColor:   '#f46800',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'crowdstrike',
    name:        'CrowdStrike',
    description: 'Detections, incidents, host management, threat intelligence',
    category:    'observability',
    icon:        'CS',
    iconColor:   '#e4002b',
    type:        'credentials',
    multiAccount: false,
  },

  // ── System ────────────────────────────────────────────────────────────────
  {
    id:          'zendesk',
    name:        'Zendesk',
    description: 'Tickets, users, support',
    category:    'system',
    icon:        'ZD',
    iconColor:   '#03363d',
    type:        'credentials',
    multiAccount: false,
  },
  {
    id:          'github_cli',
    name:        'GitHub CLI (gh)',
    description: 'Repositories, pull requests, issues via gh CLI',
    category:    'system',
    icon:        'GH',
    iconColor:   '#333333',
    type:        'system',
  },
]

// Hydrate catalog entries with live state from the API
export function hydrateOAuth(
  entry: CatalogEntry,
  connections: Array<{ id: string; provider: string; account_label: string }>,
): ConnectionState | null {
  if (entry.type !== 'oauth') return null
  const conns = connections.filter(c => c.provider === entry.id)
  if (conns.length === 0) return { connected: false }
  const first = conns[0]!
  return {
    connected: true,
    accounts: conns.map(c => ({ id: c.id, label: c.account_label })),
    // keep identity as the first account label for legacy consumers
    identity: first.account_label,
  }
}

export function hydrateSystem(
  entry: CatalogEntry,
  tools: Array<{ name: string; installed: boolean; authed: boolean; identity: string; profiles?: string[] }>,
): ConnectionState | null {
  if (entry.type !== 'system') return null
  // Map catalog IDs to tool names
  const toolName = entry.id === 'github_cli' ? 'github' : entry.id
  const tool = tools.find(t => t.name === toolName)
  if (!tool) return { connected: false }

  // GitHub CLI: surface profiles as switchable account rows
  if (entry.id === 'github_cli' && tool.profiles?.length) {
    return {
      connected: tool.authed,
      identity:  tool.identity || undefined,
      accounts:  tool.profiles.map(p => ({ id: p, label: p })),
    }
  }

  return {
    connected: tool.authed,
    identity:  tool.identity || undefined,
    profiles:  tool.profiles,
  }
}

export function hydrateCredentials(
  entry: CatalogEntry,
  connections: Array<{ id: string; provider: string; account_label: string }>,
): ConnectionState | null {
  if (entry.type !== 'credentials') return null
  const conns = connections.filter(c => c.provider === entry.id)
  if (conns.length === 0) return { connected: false }
  const first = conns[0]!
  return {
    connected: true,
    identity: first.account_label,
    accounts: conns.map(c => ({ id: c.id, label: c.account_label })),
  }
}
