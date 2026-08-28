import { describe, it, expect } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useChatViewHeaderAndMembers } from '../useChatViewHeaderAndMembers'

function mountHarness() {
  let state: any
  const sessions = ref([{ id: 's1', title: 'Session One' }])
  const sessionId = ref('s1')
  const spaceId = ref('space-1')
  const activeSpace = ref({ kind: 'channel', leadAgent: 'Tom', memberAgents: ['Sam'] })
  const agentsList = ref([
    { name: 'Tom', color: '#58a6ff', description: 'Lead', vault_name: 'vault-tom' },
    { name: 'Sam', color: '#3fb950', description: 'Member', vault_name: 'vault-sam' },
  ])
  const selectedAgentName = ref('Tom')
  const selectedAgent = ref(agentsList.value[0])
  const threadPanelOpen = ref(false)
  const streaming = ref(false)
  const inFlightUserContent = ref('')
  const renameSession = (id: string, title: string) => {
    const s = sessions.value.find(s => s.id === id)
    if (s) s.title = title
  }

  const Harness = defineComponent({
    setup() {
      state = useChatViewHeaderAndMembers({
        sessions,
        sessionId,
        spaceId,
        formatSessionLabel: (s) => s.title || s.id,
        renameSession,
        activeSpace,
        agentsList,
        selectedAgentName,
        threadPanelOpen,
        selectedAgent,
        streaming,
        inFlightUserContent,
      })
      return () => null
    },
  })

  mount(Harness)
  return { state, sessions, threadPanelOpen, streaming, inFlightUserContent, activeSpace, agentsList }
}

describe('useChatViewHeaderAndMembers', () => {
  it('commits header edit through renameSession', async () => {
    const { state, sessions } = mountHarness()
    await state.startHeaderEdit()
    state.headerEditValue.value = 'Renamed Session'
    state.commitHeaderEdit()

    expect(sessions.value[0]?.title).toBe('Renamed Session')
  })

  it('toggles member panel and persists localStorage key', () => {
    const { state } = mountHarness()

    state.toggleMemberPanel()
    expect(state.memberPanelOpen.value).toBe(true)
    expect(localStorage.getItem('huginn:memberPanel:space-1')).toBe('true')

    state.toggleMemberPanel()
    expect(state.memberPanelOpen.value).toBe(false)
    expect(localStorage.getItem('huginn:memberPanel:space-1')).toBe('false')
  })

  it('collapses member panel while thread panel is open', async () => {
    const { state, threadPanelOpen } = mountHarness()
    state.memberPanelOpen.value = true

    threadPanelOpen.value = true
    await Promise.resolve()
    expect(state.memberPanelOpen.value).toBe(false)

    threadPanelOpen.value = false
    await Promise.resolve()
    expect(state.memberPanelOpen.value).toBe(true)
  })

  it('displayAgent names the mentioned member during an in-flight turn', async () => {
    const { state, streaming, inFlightUserContent } = mountHarness()
    expect(state.displayAgent.value?.name).toBe('Tom')

    streaming.value = true
    inFlightUserContent.value = '@Sam review this PR'
    await Promise.resolve()
    expect(state.displayAgent.value?.name).toBe('Sam')
  })

  it('header agent count includes roster names missing from agentsList', () => {
    const { state, activeSpace } = mountHarness()
    activeSpace.value = { kind: 'channel', leadAgent: 'Tom', memberAgents: ['Sam', 'driveprobe-1'] }
    expect(state.spaceAgents.value.map((a: { name: string }) => a.name)).toEqual(['Tom', 'Sam', 'driveprobe-1'])
    expect(state.spaceAgents.value).toHaveLength(3)
  })

  it('displayAgent stays on the lead during an unmentioned in-flight turn', async () => {
    const { state, streaming, inFlightUserContent } = mountHarness()

    streaming.value = true
    inFlightUserContent.value = 'what is the status?'
    await Promise.resolve()
    expect(state.displayAgent.value?.name).toBe('Tom')
  })
})
