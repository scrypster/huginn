import { ref, computed, nextTick, watch, type Ref } from 'vue'
import { agentDisplayDescription } from '../../utils/agentDescription'
import { resolveDisplayAgent, type DisplayAgentLike, type DisplaySpaceLike } from './respondingAgent'

type SessionLike = { id: string; title?: string }
type SpaceLike = DisplaySpaceLike
type AgentLike = DisplayAgentLike & { system_prompt?: string }

interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

type Params = {
  sessions: Ref<SessionLike[]>
  sessionId: Ref<string | undefined>
  spaceId: Ref<string | undefined>
  formatSessionLabel: (s: SessionLike) => string
  renameSession: (id: string, title: string) => void
  activeSpace: Ref<SpaceLike>
  agentsList: Ref<AgentLike[]>
  selectedAgentName: Ref<string>
  threadPanelOpen: Ref<boolean>
  selectedAgent: Ref<AgentLike | null>
  streaming?: Ref<boolean>
  inFlightUserContent?: Ref<string>
}

export function useChatViewHeaderAndMembers(params: Params) {
  const headerEditing = ref(false)
  const headerEditValue = ref('')
  const headerInputEl = ref<HTMLInputElement | null>(null)

  async function startHeaderEdit() {
    const s = params.sessions.value.find(s => s.id === params.sessionId.value)
    headerEditValue.value = s?.title ?? ''
    headerEditing.value = true
    await nextTick()
    headerInputEl.value?.focus()
    headerInputEl.value?.select()
  }

  function commitHeaderEdit() {
    if (!headerEditing.value) return
    headerEditing.value = false
    if (params.sessionId.value) params.renameSession(params.sessionId.value, headerEditValue.value.trim())
  }

  function cancelHeaderEdit() {
    headerEditing.value = false
  }

  const sessionLabel = computed(() => {
    const s = params.sessions.value.find(s => s.id === params.sessionId.value)
    return s ? params.formatSessionLabel(s) : (params.sessionId.value?.slice(0, 8) ?? '')
  })

  const spaceAgents = computed(() => {
    const space = params.activeSpace.value
    if (!space) return []
    const names = [space.leadAgent, ...space.memberAgents.filter(m => m !== space.leadAgent)]
    return names.map(n => params.agentsList.value.find(a => a.name === n)).filter((a): a is AgentLike => !!a)
  })

  const spaceAgentPreviews = computed(() => spaceAgents.value.slice(0, 3))

  const spaceMemberCards = computed<SpaceMemberCard[]>(() => {
    const space = params.activeSpace.value
    if (!space) return []
    const leadName = space.leadAgent
    const names = [leadName, ...space.memberAgents]
    return names.map(n => {
      const agent = params.agentsList.value.find(a => a.name === n)
      return {
        name: n,
        description: agentDisplayDescription(agent ? { ...agent, name: n } : { name: n }),
        vaultName: agent?.vault_name ?? '',
        isLead: n === leadName,
        color: agent?.color ?? '#58a6ff',
      }
    })
  })

  const displayAgent = computed(() =>
    resolveDisplayAgent({
      space: params.activeSpace.value,
      agents: params.agentsList.value,
      selectedAgent: params.selectedAgent.value,
      streaming: params.streaming?.value ?? false,
      inFlightUserContent: params.inFlightUserContent?.value ?? '',
    }),
  )

  const memberPanelOpen = ref(false)
  const memberPanelStoredState = ref(false)

  watch(() => params.spaceId.value, (id) => {
    if (id) {
      memberPanelOpen.value = localStorage.getItem(`huginn:memberPanel:${id}`) === 'true'
    }
  }, { immediate: true })

  function toggleMemberPanel() {
    if (!params.spaceId.value) return
    memberPanelOpen.value = !memberPanelOpen.value
    localStorage.setItem(`huginn:memberPanel:${params.spaceId.value}`, String(memberPanelOpen.value))
  }

  watch(params.threadPanelOpen, (open) => {
    if (open) {
      memberPanelStoredState.value = memberPanelOpen.value
      memberPanelOpen.value = false
    } else {
      memberPanelOpen.value = memberPanelStoredState.value
    }
  })

  return {
    headerEditing,
    headerEditValue,
    headerInputEl,
    startHeaderEdit,
    commitHeaderEdit,
    cancelHeaderEdit,
    sessionLabel,
    spaceAgents,
    spaceAgentPreviews,
    spaceMemberCards,
    displayAgent,
    memberPanelOpen,
    memberPanelStoredState,
    toggleMemberPanel,
  }
}
