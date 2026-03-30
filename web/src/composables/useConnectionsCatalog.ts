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
  if (entry.type !== 'credentials' && entry.type !== 'database') return null
  const conns = connections.filter(c => c.provider === entry.id)
  if (conns.length === 0) return { connected: false }
  const first = conns[0]!
  return {
    connected: true,
    identity: first.account_label,
    accounts: conns.map(c => ({ id: c.id, label: c.account_label })),
  }
}
