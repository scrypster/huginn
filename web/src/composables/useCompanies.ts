import { ref, computed } from 'vue'
import { api } from './useApi'

export interface Company {
  id: string
  name: string
  icon: string
  color: string
  members: string[]
}

export interface FollowSpace {
  id: string
  companyId: string
  kind: 'dm' | 'channel' | string
  unseenCount: number
  forYou?: boolean
}

const SELECTED_KEY = 'huginn_selected_company_id'
const COLLAPSED_KEY = 'huginn_collapsed_company_ids'
const FOLLOW_UNREAD_KEY = 'huginn_follow_unread_space_ids'

const companies = ref<Company[]>([])
const selectedCompanyId = ref<string | null>(localStorage.getItem(SELECTED_KEY))
const loading = ref(false)
const creating = ref(false)

function readCollapsed(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '[]')
    return Array.isArray(raw) ? raw.filter((id): id is string => typeof id === 'string') : []
  } catch {
    return []
  }
}

function readFollowUnread(): Record<string, boolean> {
  try {
    const raw = JSON.parse(localStorage.getItem(FOLLOW_UNREAD_KEY) ?? '[]')
    if (!Array.isArray(raw)) return {}
    const out: Record<string, boolean> = {}
    for (const id of raw) {
      if (typeof id === 'string' && id) out[id] = true
    }
    return out
  } catch {
    return {}
  }
}

const collapsedCompanyIds = ref<string[]>(readCollapsed())

// Slack follow-model marks: space id → human has a follow/@me unread there.
// Channel unseenCount alone does NOT count (two agents talking is spectator).
// Persisted so a reload still lights a collapsed company-rail @me chip.
const followUnreadBySpace = ref<Record<string, boolean>>(readFollowUnread())

function persistCollapsed() {
  localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsedCompanyIds.value))
}

function persistFollowUnread() {
  const ids = Object.keys(followUnreadBySpace.value).filter(id => followUnreadBySpace.value[id])
  if (ids.length) localStorage.setItem(FOLLOW_UNREAD_KEY, JSON.stringify(ids))
  else localStorage.removeItem(FOLLOW_UNREAD_KEY)
}

function mapCompany(raw: Record<string, unknown>): Company {
  return {
    id: (raw.id as string) ?? '',
    name: (raw.name as string) ?? '',
    icon: (raw.icon as string) ?? '',
    color: (raw.color as string) ?? '',
    members: Array.isArray(raw.members) ? (raw.members as string[]) : [],
  }
}

function parseCompanies(raw: unknown): Company[] {
  if (Array.isArray(raw)) return raw.map(r => mapCompany(r as Record<string, unknown>)).filter(c => c.id)
  if (!raw || typeof raw !== 'object') return []
  const rec = raw as Record<string, unknown>
  const items = Array.isArray(rec.companies)
    ? rec.companies
    : Array.isArray(rec.Companies)
      ? rec.Companies
      : []
  return items.map(r => mapCompany(r as Record<string, unknown>)).filter(c => c.id)
}

/**
 * Company collapse badge: follow/@me only.
 * - DM unseen counts (the human is always a participant of their DM).
 * - Explicit follow marks (posted in that space/thread, or @mentioned).
 * Channel unseenCount alone does not badge — two agents talking in a
 * thread the human never joined must not light the company.
 */
export function companyHasFollowUnread(
  companyId: string,
  spaces: FollowSpace[],
  followBySpace: Record<string, boolean> = {},
): boolean {
  return spaces.some(s => {
    if ((s.companyId || '') !== companyId) return false
    if (s.forYou || followBySpace[s.id]) return true
    if (s.kind === 'dm' && s.unseenCount > 0) return true
    return false
  })
}

export function useCompanies() {
  // Desk when nothing is selected, the list is empty (API missing), or the
  // persisted id is no longer in the list. Empty list = desk only.
  const effectiveCompanyId = computed(() => {
    if (!companies.value.length) return null
    const id = selectedCompanyId.value
    if (id && companies.value.some(c => c.id === id)) return id
    return null
  })

  const selectedCompany = computed(() =>
    companies.value.find(c => c.id === effectiveCompanyId.value) ?? null,
  )

  const isDesk = computed(() => !effectiveCompanyId.value)

  async function fetchCompanies() {
    loading.value = true
    try {
      const raw = await api.companies.list()
      companies.value = parseCompanies(raw)
    } catch {
      // Fail soft: missing/404/network → desk only.
      companies.value = []
    } finally {
      loading.value = false
    }
  }

  function selectCompany(id: string | null) {
    selectedCompanyId.value = id
    if (id) localStorage.setItem(SELECTED_KEY, id)
    else localStorage.removeItem(SELECTED_KEY)
  }

  function upsertCompany(raw: unknown) {
    const c = mapCompany((raw ?? {}) as Record<string, unknown>)
    if (!c.id) return null
    const idx = companies.value.findIndex(x => x.id === c.id)
    if (idx >= 0) companies.value[idx] = c
    else companies.value = [...companies.value, c].sort((a, b) => a.name.localeCompare(b.name))
    return c
  }

  async function createCompany(opts: {
    name: string
    members?: string[]
    vault?: string
    icon?: string
    color?: string
  }): Promise<Company | null> {
    creating.value = true
    try {
      const raw = await api.companies.create({
        name: opts.name.trim(),
        members: opts.members ?? [],
        vault: opts.vault ?? '',
        icon: opts.icon ?? '',
        color: opts.color ?? '',
      })
      return upsertCompany(raw)
    } catch {
      return null
    } finally {
      creating.value = false
    }
  }

  async function seatMember(companyId: string, agent: string): Promise<Company | null> {
    try {
      const raw = await api.companies.seat(companyId, agent)
      return upsertCompany(raw)
    } catch {
      return null
    }
  }

  async function unseatMember(companyId: string, agent: string): Promise<Company | null> {
    try {
      const raw = await api.companies.unseat(companyId, agent)
      return upsertCompany(raw)
    } catch {
      return null
    }
  }

  async function deleteCompany(id: string): Promise<boolean> {
    if (!id) return false
    try {
      await api.companies.remove(id)
      companies.value = companies.value.filter(c => c.id !== id)
      if (selectedCompanyId.value === id) selectCompany(null)
      return true
    } catch {
      return false
    }
  }

  function isCompanyCollapsed(id: string): boolean {
    return collapsedCompanyIds.value.includes(id)
  }

  function toggleCompanyCollapsed(id: string) {
    const i = collapsedCompanyIds.value.indexOf(id)
    if (i >= 0) collapsedCompanyIds.value = collapsedCompanyIds.value.filter(x => x !== id)
    else collapsedCompanyIds.value = [...collapsedCompanyIds.value, id]
    persistCollapsed()
  }

  function setCompanyCollapsed(id: string, collapsed: boolean) {
    const is = collapsedCompanyIds.value.includes(id)
    if (collapsed && !is) collapsedCompanyIds.value = [...collapsedCompanyIds.value, id]
    if (!collapsed && is) collapsedCompanyIds.value = collapsedCompanyIds.value.filter(x => x !== id)
    persistCollapsed()
  }

  // API is source of truth. localStorage is only a cache for the next
  // paint before fetchSpaces() returns. replace = full list; merge =
  // company-filtered page (leave other companies' marks alone).
  function applyFollowUnreadFromSpaces(
    incoming: { id: string; forYou?: boolean }[],
    mode: 'replace' | 'merge' = 'replace',
  ) {
    const next: Record<string, boolean> = mode === 'replace' ? {} : { ...followUnreadBySpace.value }
    for (const s of incoming) {
      if (!s.id) continue
      if (s.forYou) next[s.id] = true
      else if (mode === 'merge') delete next[s.id]
    }
    followUnreadBySpace.value = next
    persistFollowUnread()
  }

  function noteFollowUnread(spaceId: string, on: boolean) {
    if (!spaceId) return
    if (on) {
      if (followUnreadBySpace.value[spaceId]) return
      followUnreadBySpace.value = { ...followUnreadBySpace.value, [spaceId]: true }
      persistFollowUnread()
    } else if (followUnreadBySpace.value[spaceId]) {
      const next = { ...followUnreadBySpace.value }
      delete next[spaceId]
      followUnreadBySpace.value = next
      persistFollowUnread()
    }
  }

  function companyFollowUnread(companyId: string, spaces: FollowSpace[]): boolean {
    return companyHasFollowUnread(companyId, spaces, followUnreadBySpace.value)
  }

  function agentSeatedIn(agent: string, companyId: string | null | undefined): boolean {
    if (!companyId) return true
    const c = companies.value.find(x => x.id === companyId)
    if (!c) return false
    const lower = agent.toLowerCase()
    return c.members.some(m => m.toLowerCase() === lower)
  }

  function clearCompanies() {
    companies.value = []
    selectedCompanyId.value = null
    localStorage.removeItem(SELECTED_KEY)
    collapsedCompanyIds.value = []
    localStorage.removeItem(COLLAPSED_KEY)
    followUnreadBySpace.value = {}
    localStorage.removeItem(FOLLOW_UNREAD_KEY)
    loading.value = false
    creating.value = false
  }

  return {
    companies,
    selectedCompanyId,
    selectedCompany,
    effectiveCompanyId,
    isDesk,
    loading,
    creating,
    collapsedCompanyIds,
    followUnreadBySpace,
    fetchCompanies,
    selectCompany,
    createCompany,
    seatMember,
    unseatMember,
    deleteCompany,
    isCompanyCollapsed,
    toggleCompanyCollapsed,
    setCompanyCollapsed,
    noteFollowUnread,
    applyFollowUnreadFromSpaces,
    companyFollowUnread,
    agentSeatedIn,
    clearCompanies,
  }
}
