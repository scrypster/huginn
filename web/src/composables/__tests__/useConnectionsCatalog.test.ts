import { describe, it, expect } from 'vitest'
import {
  hydrateOAuth,
  hydrateSystem,
  hydrateCredentials,
  type CatalogEntry,
} from '../useConnectionsCatalog'

// Minimal catalog entries for each type — defined inline (no static CATALOG export)
const oauthEntry: CatalogEntry = {
  id: 'github', name: 'GitHub', description: 'GitHub dev tools',
  category: 'dev_tools', icon: 'G', iconColor: '#24292E',
  type: 'oauth', multiAccount: false,
}
const multiOAuthEntry: CatalogEntry = {
  id: 'google', name: 'Google', description: 'Google services',
  category: 'productivity', icon: 'Go', iconColor: '#4285F4',
  type: 'oauth', multiAccount: true,
}
const systemEntry: CatalogEntry = {
  id: 'github_cli', name: 'GitHub CLI', description: 'GitHub CLI tool',
  category: 'dev_tools', icon: 'GH', iconColor: '#24292E',
  type: 'system', multiAccount: false,
}
const awsEntry: CatalogEntry = {
  id: 'aws', name: 'AWS', description: 'Amazon Web Services',
  category: 'cloud', icon: 'A', iconColor: '#FF9900',
  type: 'system', multiAccount: false,
}
const credEntry: CatalogEntry = {
  id: 'datadog', name: 'Datadog', description: 'Metrics, logs',
  category: 'observability', icon: 'DD', iconColor: '#632ca6',
  type: 'credentials', multiAccount: false,
}
const comingSoonEntry: CatalogEntry = {
  id: 'teams', name: 'Teams', description: 'Microsoft Teams',
  category: 'communication', icon: 'T', iconColor: '#6264A7',
  type: 'coming_soon', multiAccount: false,
}

// ─────────────────────────────────────────────────────────────────────────────
describe('hydrateOAuth', () => {
  it('returns null for non-oauth types', () => {
    expect(hydrateOAuth(credEntry, [])).toBeNull()
    expect(hydrateOAuth(systemEntry, [])).toBeNull()
    expect(hydrateOAuth(comingSoonEntry, [])).toBeNull()
  })

  it('returns { connected: false } when no connections exist for provider', () => {
    const state = hydrateOAuth(oauthEntry, [])
    expect(state).toEqual({ connected: false })
  })

  it('returns { connected: false } when connections exist for other providers', () => {
    const conns = [{ id: 'c1', provider: 'slack', account_label: 'alice@example.com' }]
    const state = hydrateOAuth(oauthEntry, conns)  // github not slack
    expect(state).toEqual({ connected: false })
  })

  it('returns connected state with accounts array for a single connection', () => {
    const conns = [{ id: 'c1', provider: 'github', account_label: 'alice@example.com' }]
    const state = hydrateOAuth(oauthEntry, conns)
    expect(state).toEqual({
      connected: true,
      accounts: [{ id: 'c1', label: 'alice@example.com' }],
      identity: 'alice@example.com',
    })
  })

  it('returns multiple accounts for multi-account oauth entry', () => {
    const conns = [
      { id: 'c1', provider: 'google', account_label: 'alice@gmail.com' },
      { id: 'c2', provider: 'google', account_label: 'bob@gmail.com' },
    ]
    const state = hydrateOAuth(multiOAuthEntry, conns)
    expect(state?.connected).toBe(true)
    expect(state?.accounts).toHaveLength(2)
    expect(state?.accounts![0]).toEqual({ id: 'c1', label: 'alice@gmail.com' })
    expect(state?.accounts![1]).toEqual({ id: 'c2', label: 'bob@gmail.com' })
    expect(state?.identity).toBe('alice@gmail.com')
  })
})

// ─────────────────────────────────────────────────────────────────────────────
describe('hydrateSystem', () => {
  it('returns null for non-system types', () => {
    expect(hydrateSystem(oauthEntry, [])).toBeNull()
    expect(hydrateSystem(credEntry, [])).toBeNull()
    expect(hydrateSystem(comingSoonEntry, [])).toBeNull()
  })

  it('returns { connected: false } when tool is not found', () => {
    const state = hydrateSystem(awsEntry, [])
    expect(state).toEqual({ connected: false })
  })

  it('returns connected state for an authed tool', () => {
    const tools = [
      { name: 'aws', installed: true, authed: true, identity: 'arn:aws:iam::123:user/mjb', profiles: ['default', 'prod'] },
    ]
    const state = hydrateSystem(awsEntry, tools)
    expect(state?.connected).toBe(true)
    expect(state?.identity).toBe('arn:aws:iam::123:user/mjb')
    expect(state?.profiles).toEqual(['default', 'prod'])
  })

  it('returns { connected: false } for an installed but not authed tool', () => {
    const tools = [{ name: 'aws', installed: true, authed: false, identity: '' }]
    const state = hydrateSystem(awsEntry, tools)
    expect(state?.connected).toBe(false)
    expect(state?.identity).toBeUndefined()
  })

  it('maps github_cli entry to "github" tool name', () => {
    const tools = [{ name: 'github', installed: true, authed: true, identity: 'octocat' }]
    const state = hydrateSystem(systemEntry, tools)
    expect(state?.connected).toBe(true)
    expect(state?.identity).toBe('octocat')
  })

  it('returns accounts from profiles for github_cli when profiles exist', () => {
    const tools = [
      { name: 'github', installed: true, authed: true, identity: 'octocat', profiles: ['work', 'personal'] },
    ]
    const state = hydrateSystem(systemEntry, tools)
    expect(state?.connected).toBe(true)
    expect(state?.accounts).toEqual([
      { id: 'work', label: 'work' },
      { id: 'personal', label: 'personal' },
    ])
    expect(state?.identity).toBe('octocat')
  })

  it('omits undefined identity from result', () => {
    const tools = [{ name: 'aws', installed: true, authed: true, identity: '' }]
    const state = hydrateSystem(awsEntry, tools)
    expect(state?.identity).toBeUndefined()
  })
})

// ─────────────────────────────────────────────────────────────────────────────
describe('hydrateCredentials', () => {
  it('returns null for non-credentials/database types', () => {
    expect(hydrateCredentials(oauthEntry, [])).toBeNull()
    expect(hydrateCredentials(systemEntry, [])).toBeNull()
    expect(hydrateCredentials(comingSoonEntry, [])).toBeNull()
  })

  it('returns connected state for database type providers', () => {
    const dbEntry: CatalogEntry = {
      id: 'muninn', name: 'MuninnDB', description: 'Agent memory',
      category: 'databases', icon: 'M', iconColor: '#58a6ff',
      type: 'database', multiAccount: false,
    }
    const conns = [{ id: 'c1', provider: 'muninn', account_label: 'local' }]
    const state = hydrateCredentials(dbEntry, conns)
    expect(state?.connected).toBe(true)
    expect(state?.identity).toBe('local')
  })

  it('returns { connected: false } when no matching connection', () => {
    const state = hydrateCredentials(credEntry, [])
    expect(state).toEqual({ connected: false })
  })

  it('returns { connected: false } when connections exist for other providers', () => {
    const conns = [{ id: 'c1', provider: 'splunk', account_label: 'prod' }]
    const state = hydrateCredentials(credEntry, conns)  // credEntry is datadog
    expect(state).toEqual({ connected: false })
  })

  it('returns connected state with identity and accounts', () => {
    const conns = [{ id: 'c1', provider: 'datadog', account_label: 'prod-dd' }]
    const state = hydrateCredentials(credEntry, conns)
    expect(state).toEqual({
      connected: true,
      identity: 'prod-dd',
      accounts: [{ id: 'c1', label: 'prod-dd' }],
    })
  })
})
