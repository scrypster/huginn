import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setToken } from '../useApi'

function okJson(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

async function freshCompanies() {
  vi.resetModules()
  const apiMod = await import('../useApi')
  apiMod.setToken('test-token')
  const mod = await import('../useCompanies')
  return mod.useCompanies()
}

beforeEach(() => {
  localStorage.clear()
  setToken('test-token')
})

afterEach(() => {
  setToken('')
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('useCompanies', () => {
  it('populates companies from a plain array', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson([{ id: 'acme', name: 'Acme', icon: '', color: '#58a6ff', members: [] }]),
    )
    const { fetchCompanies, companies, isDesk } = await freshCompanies()
    await fetchCompanies()
    expect(companies.value).toHaveLength(1)
    expect(companies.value[0].name).toBe('Acme')
    expect(isDesk.value).toBe(true)
  })

  it('fails soft to an empty list (desk only) when the API is missing', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response('not found', { status: 404 }),
    )
    const { fetchCompanies, companies, effectiveCompanyId } = await freshCompanies()
    await fetchCompanies()
    expect(companies.value).toEqual([])
    expect(effectiveCompanyId.value).toBeNull()
  })

  it('fails soft on network error', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('network'))
    const { fetchCompanies, companies, isDesk } = await freshCompanies()
    await fetchCompanies()
    expect(companies.value).toEqual([])
    expect(isDesk.value).toBe(true)
  })

  it('persists selected company id in localStorage', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      okJson([{ id: 'acme', name: 'Acme' }]),
    )
    const { fetchCompanies, selectCompany, selectedCompanyId, isDesk, effectiveCompanyId } = await freshCompanies()
    await fetchCompanies()
    selectCompany('acme')
    expect(selectedCompanyId.value).toBe('acme')
    expect(localStorage.getItem('huginn_selected_company_id')).toBe('acme')
    expect(isDesk.value).toBe(false)
    expect(effectiveCompanyId.value).toBe('acme')

    selectCompany(null)
    expect(localStorage.getItem('huginn_selected_company_id')).toBeNull()
    expect(isDesk.value).toBe(true)
  })

  it('treats an unknown persisted id as desk once the list loads', async () => {
    localStorage.setItem('huginn_selected_company_id', 'ghost')
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson([{ id: 'acme', name: 'Acme' }]),
    )
    const { fetchCompanies, selectedCompanyId, effectiveCompanyId, isDesk } = await freshCompanies()
    expect(selectedCompanyId.value).toBe('ghost')
    await fetchCompanies()
    expect(effectiveCompanyId.value).toBeNull()
    expect(isDesk.value).toBe(true)
  })

  it('deleteCompany removes leftover and desks if selected', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    fetchMock.mockResolvedValueOnce(
      okJson([{ id: 'leftover', name: 'WringerH-wr87035' }, { id: 'huginn', name: 'Huginn' }]),
    )
    const { fetchCompanies, companies, selectCompany, deleteCompany, isDesk, effectiveCompanyId } = await freshCompanies()
    await fetchCompanies()
    selectCompany('leftover')
    fetchMock.mockResolvedValueOnce(okJson({ ok: true }))
    const ok = await deleteCompany('leftover')
    expect(ok).toBe(true)
    expect(companies.value.map(c => c.id)).toEqual(['huginn'])
    expect(isDesk.value).toBe(true)
    expect(effectiveCompanyId.value).toBeNull()
  })

  it('does not expose a vault field on the mapped company', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      okJson([{ id: 'acme', name: 'Acme', vault: 'should-not-map' }]),
    )
    const { fetchCompanies, companies } = await freshCompanies()
    await fetchCompanies()
    expect(companies.value[0]).not.toHaveProperty('vault')
  })
})

describe('company collapse + follow unread', () => {
  it('remembers collapsed company ids in localStorage', async () => {
    const { toggleCompanyCollapsed, isCompanyCollapsed, collapsedCompanyIds } = await freshCompanies()
    expect(isCompanyCollapsed('lab')).toBe(false)
    toggleCompanyCollapsed('lab')
    expect(isCompanyCollapsed('lab')).toBe(true)
    expect(JSON.parse(localStorage.getItem('huginn_collapsed_company_ids') || '[]')).toContain('lab')
    toggleCompanyCollapsed('lab')
    expect(isCompanyCollapsed('lab')).toBe(false)
    expect(collapsedCompanyIds.value).not.toContain('lab')
  })

  it('collapsed company shows badge iff follow/@me unread', async () => {
    const { companyHasFollowUnread } = await import('../useCompanies')
    const spaces = [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel' as const, unseenCount: 4 },
      { id: 'dm-sam', companyId: 'lab', kind: 'dm' as const, unseenCount: 0 },
      { id: 'dm-win', companyId: 'huginn', kind: 'dm' as const, unseenCount: 2 },
    ]
    // Spectator channel unseen (two agents talking) must NOT badge Lab.
    expect(companyHasFollowUnread('lab', spaces, {})).toBe(false)
    // DM unseen: human is a participant.
    expect(companyHasFollowUnread('lab', [
      { id: 'dm-sam', companyId: 'lab', kind: 'dm', unseenCount: 1 },
    ], {})).toBe(true)
    // @me / follow mark on a channel the human joined.
    expect(companyHasFollowUnread('lab', spaces, { 'ch-lab': true })).toBe(true)
    // Other company's DM does not badge Lab.
    expect(companyHasFollowUnread('lab', spaces, {})).toBe(false)
  })

  it('persists follow/@me marks so a reload still badges the company rail', async () => {
    const { noteFollowUnread, companyFollowUnread, followUnreadBySpace } = await freshCompanies()
    const spaces = [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel' as const, unseenCount: 0 },
    ]
    expect(companyFollowUnread('lab', spaces)).toBe(false)
    noteFollowUnread('ch-lab', true)
    expect(followUnreadBySpace.value['ch-lab']).toBe(true)
    expect(JSON.parse(localStorage.getItem('huginn_follow_unread_space_ids') || '[]')).toEqual(['ch-lab'])
    expect(companyFollowUnread('lab', spaces)).toBe(true)
    // Spectator channel unseen still does not badge without a follow mark.
    expect(companyFollowUnread('lab', [
      { id: 'ch-other', companyId: 'lab', kind: 'channel', unseenCount: 9 },
    ])).toBe(false)
  })

  it('rehydrates follow/@me marks from localStorage after a reload', async () => {
    localStorage.setItem('huginn_follow_unread_space_ids', JSON.stringify(['ch-lab']))
    const { followUnreadBySpace, companyFollowUnread } = await freshCompanies()
    expect(followUnreadBySpace.value['ch-lab']).toBe(true)
    expect(companyFollowUnread('lab', [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel', unseenCount: 0 },
    ])).toBe(true)
  })

  it('clears a persisted follow mark when the space is noted read', async () => {
    const { noteFollowUnread, followUnreadBySpace } = await freshCompanies()
    noteFollowUnread('ch-lab', true)
    noteFollowUnread('ch-other', true)
    noteFollowUnread('ch-lab', false)
    expect(followUnreadBySpace.value['ch-lab']).toBeUndefined()
    expect(JSON.parse(localStorage.getItem('huginn_follow_unread_space_ids') || '[]')).toEqual(['ch-other'])
  })
})

  it('badges from API forYou even without a local follow map', async () => {
    const { companyHasFollowUnread } = await import('../useCompanies')
    expect(companyHasFollowUnread('lab', [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel', unseenCount: 0, forYou: true },
    ], {})).toBe(true)
    expect(companyHasFollowUnread('lab', [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel', unseenCount: 9, forYou: false },
    ], {})).toBe(false)
  })

  it('rehydrates follow/@me from the API and treats localStorage as a cache', async () => {
    localStorage.setItem('huginn_follow_unread_space_ids', JSON.stringify(['stale-space']))
    const { applyFollowUnreadFromSpaces, followUnreadBySpace, companyFollowUnread } = await freshCompanies()
    expect(followUnreadBySpace.value['stale-space']).toBe(true)
    applyFollowUnreadFromSpaces([
      { id: 'ch-lab', forYou: true },
      { id: 'ch-quiet', forYou: false },
    ], 'replace')
    expect(followUnreadBySpace.value['ch-lab']).toBe(true)
    expect(followUnreadBySpace.value['stale-space']).toBeUndefined()
    expect(followUnreadBySpace.value['ch-quiet']).toBeUndefined()
    expect(JSON.parse(localStorage.getItem('huginn_follow_unread_space_ids') || '[]')).toEqual(['ch-lab'])
    expect(companyFollowUnread('lab', [
      { id: 'ch-lab', companyId: 'lab', kind: 'channel', unseenCount: 0, forYou: true },
    ])).toBe(true)
  })

  it('merge mode updates only the incoming page', async () => {
    const { applyFollowUnreadFromSpaces, followUnreadBySpace } = await freshCompanies()
    applyFollowUnreadFromSpaces([{ id: 'desk-ch', forYou: true }], 'replace')
    applyFollowUnreadFromSpaces([
      { id: 'lab-ch', forYou: true },
      { id: 'lab-done', forYou: false },
    ], 'merge')
    expect(followUnreadBySpace.value['desk-ch']).toBe(true)
    expect(followUnreadBySpace.value['lab-ch']).toBe(true)
    expect(followUnreadBySpace.value['lab-done']).toBeUndefined()
  })
