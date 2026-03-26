import { ref, computed } from 'vue'
import { api } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

export interface Space {
  id: string
  name: string
  kind: 'dm' | 'channel'
  leadAgent: string
  memberAgents: string[]
  icon: string
  color: string
  unseenCount: number
  archivedAt?: string | null
}

function mapSpace(raw: Record<string, unknown>): Space {
  return {
    id: (raw.id as string) ?? '',
    name: (raw.name as string) ?? '',
    kind: raw.kind === 'channel' ? 'channel' : 'dm',
    leadAgent: (raw.lead_agent as string) ?? '',
    memberAgents: Array.isArray(raw.member_agents) ? (raw.member_agents as string[]) : [],
    icon: (raw.icon as string) ?? '',
    color: (raw.color as string) || '#58a6ff',
    unseenCount: (raw.unseen_count as number) ?? 0,
    archivedAt: (raw.archived_at as string) ?? null,
  }
}

const ACTIVE_SPACE_KEY = 'huginn_active_space_id'

// Module-level shared state
const spaces = ref<Space[]>([])
const activeSpaceId = ref<string | null>(localStorage.getItem(ACTIVE_SPACE_KEY))
const loading = ref(false)
const error = ref<string | null>(null)
const spaceSessionsMap = ref<Record<string, unknown[]>>({})

// wireSpaceWS registers WS listeners for real-time space lifecycle events.
// Call once from App.vue initApp() after creating the WS connection.
// Returns an unsubscribe function (for cleanup or test teardown).
//
// WS events handled:
//   space_member_added   — appends agent name to the space's memberAgents[]
//   space_member_removed — removes agent name from the space's memberAgents[]
//   space_created        — inserts the new space at the front of the list
//   space_updated        — merges updated space data into the list
//   space_archived       — removes the space from the list
//   space_activity       — sets the space's unseenCount from the backend's actual count
export function wireSpaceWS(ws: HuginnWS): () => void {
  const onMemberAdded = (msg: WSMessage): void => {
    const spaceId = msg.payload?.['space_id'] as string | undefined
    const agent = msg.payload?.['agent'] as string | undefined
    if (!spaceId || !agent) return
    const space = spaces.value.find(s => s.id === spaceId)
    if (space && !space.memberAgents.includes(agent)) {
      space.memberAgents = [...space.memberAgents, agent]
    }
  }

  const onMemberRemoved = (msg: WSMessage): void => {
    const spaceId = msg.payload?.['space_id'] as string | undefined
    const agent = msg.payload?.['agent'] as string | undefined
    if (!spaceId || !agent) return
    const space = spaces.value.find(s => s.id === spaceId)
    if (space) {
      space.memberAgents = space.memberAgents.filter(a => a !== agent)
    }
  }

  const onSpaceCreated = (msg: WSMessage): void => {
    const raw = msg.payload?.['space'] as Record<string, unknown> | undefined
    if (!raw) return
    const sp = mapSpace(raw)
    if (!spaces.value.some(s => s.id === sp.id)) {
      spaces.value.unshift(sp)
    }
  }

  const onSpaceUpdated = (msg: WSMessage): void => {
    const raw = msg.payload?.['space'] as Record<string, unknown> | undefined
    if (!raw) return
    const sp = mapSpace(raw)
    const idx = spaces.value.findIndex(s => s.id === sp.id)
    if (idx >= 0) {
      spaces.value[idx] = sp
    }
  }

  const onSpaceArchived = (msg: WSMessage): void => {
    const spaceId = msg.payload?.['space_id'] as string | undefined
    if (!spaceId) return
    spaces.value = spaces.value.filter(s => s.id !== spaceId)
    if (activeSpaceId.value === spaceId) {
      activeSpaceId.value = null
      localStorage.removeItem(ACTIVE_SPACE_KEY)
    }
  }

  const onSpaceActivity = (msg: WSMessage): void => {
    const spaceId = msg.payload?.['space_id'] as string | undefined
    const count = msg.payload?.['unseen_count'] as number | undefined
    if (!spaceId || count === undefined) return
    // For the active space, only allow count-to-zero updates through
    // (mark-read confirmation). Ignore increments — the user is viewing it.
    if (spaceId === activeSpaceId.value && count > 0) return
    const space = spaces.value.find(s => s.id === spaceId)
    if (space) {
      space.unseenCount = count
    }
  }

  ws.on('space_member_added', onMemberAdded)
  ws.on('space_member_removed', onMemberRemoved)
  ws.on('space_created', onSpaceCreated)
  ws.on('space_updated', onSpaceUpdated)
  ws.on('space_archived', onSpaceArchived)
  ws.on('space_activity', onSpaceActivity)

  return () => {
    ws.off('space_member_added', onMemberAdded)
    ws.off('space_member_removed', onMemberRemoved)
    ws.off('space_created', onSpaceCreated)
    ws.off('space_updated', onSpaceUpdated)
    ws.off('space_archived', onSpaceArchived)
    ws.off('space_activity', onSpaceActivity)
  }
}

export function useSpaces() {
  const channels = computed(() => spaces.value.filter(s => s.kind === 'channel'))
  const dms = computed(() => spaces.value.filter(s => s.kind === 'dm'))
  const activeSpace = computed(() => spaces.value.find(s => s.id === activeSpaceId.value) ?? null)

  async function fetchSpaces() {
    loading.value = true
    error.value = null
    try {
      const raw = await api.spaces.list()
      // API returns { Spaces: [...], NextCursor: "" } (paginated). Handle both
      // the legacy plain-array form and the current paginated-result form.
      const items: unknown[] = Array.isArray(raw)
        ? raw
        : Array.isArray((raw as Record<string, unknown>)?.Spaces)
          ? ((raw as Record<string, unknown>).Spaces as unknown[])
          : []
      spaces.value = items.map(r => mapSpace(r as Record<string, unknown>))
      // Clean up persisted activeSpaceId if the space no longer exists.
      if (activeSpaceId.value && !spaces.value.some(s => s.id === activeSpaceId.value)) {
        activeSpaceId.value = null
        localStorage.removeItem(ACTIVE_SPACE_KEY)
      }
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to load spaces'
    } finally {
      loading.value = false
    }
  }

  function setActiveSpace(id: string | null) {
    activeSpaceId.value = id
    if (id) {
      localStorage.setItem(ACTIVE_SPACE_KEY, id)
      // Optimistically clear the badge — user is now viewing this space.
      const sp = spaces.value.find(s => s.id === id)
      if (sp) sp.unseenCount = 0
    } else {
      localStorage.removeItem(ACTIVE_SPACE_KEY)
    }
  }

  async function openDM(agentName: string): Promise<Space | null> {
    try {
      const raw = await api.spaces.getDM(agentName)
      const sp = mapSpace(raw as Record<string, unknown>)
      const idx = spaces.value.findIndex(s => s.id === sp.id)
      if (idx >= 0) spaces.value[idx] = sp
      else spaces.value.unshift(sp)
      return sp
    } catch {
      return null
    }
  }

  async function createChannel(opts: { name: string; leadAgent: string; memberAgents: string[] }): Promise<Space | null> {
    error.value = null
    try {
      const raw = await api.spaces.createChannel({
        name: opts.name,
        lead_agent: opts.leadAgent,
        member_agents: opts.memberAgents,
      })
      const sp = mapSpace(raw as Record<string, unknown>)
      spaces.value.unshift(sp)
      return sp
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to create channel'
      return null
    }
  }

  async function deleteSpace(id: string): Promise<boolean> {
    error.value = null
    try {
      await api.spaces.deleteSpace(id)
      spaces.value = spaces.value.filter(s => s.id !== id)
      if (activeSpaceId.value === id) {
        activeSpaceId.value = null
        localStorage.removeItem(ACTIVE_SPACE_KEY)
      }
      return true
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to delete space'
      return false
    }
  }

  async function markRead(spaceId: string) {
    try {
      await api.spaces.markRead(spaceId)
      const sp = spaces.value.find(s => s.id === spaceId)
      if (sp) sp.unseenCount = 0
    } catch { /* ignore */ }
  }

  async function updateSpace(id: string, patch: { memberAgents?: string[]; leadAgent?: string; name?: string }): Promise<Space | null> {
    try {
      const apiPatch: Record<string, unknown> = {}
      if (patch.memberAgents !== undefined) apiPatch.member_agents = patch.memberAgents
      if (patch.leadAgent !== undefined) apiPatch.lead_agent = patch.leadAgent
      if (patch.name !== undefined) apiPatch.name = patch.name
      const raw = await api.spaces.updateSpace(id, apiPatch)
      const sp = mapSpace(raw as Record<string, unknown>)
      const idx = spaces.value.findIndex(s => s.id === id)
      if (idx >= 0) spaces.value[idx] = sp
      return sp
    } catch {
      return null
    }
  }

  async function fetchSpaceSessions(spaceId: string): Promise<unknown[]> {
    try {
      const result = await api.spaces.sessions(spaceId)
      const sessions = Array.isArray(result) ? result : []
      spaceSessionsMap.value[spaceId] = sessions
      return sessions
    } catch {
      return []
    }
  }

  function clearSpaces() {
    spaces.value = []
    activeSpaceId.value = null
    localStorage.removeItem(ACTIVE_SPACE_KEY)
    error.value = null
    spaceSessionsMap.value = {}
  }

  return {
    spaces,
    channels,
    dms,
    activeSpaceId,
    activeSpace,
    loading,
    error,
    fetchSpaces,
    setActiveSpace,
    openDM,
    createChannel,
    updateSpace,
    deleteSpace,
    markRead,
    fetchSpaceSessions,
    spaceSessionsMap,
    clearSpaces,
  }
}
