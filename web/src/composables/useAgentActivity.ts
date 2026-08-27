import { reactive } from 'vue'
import { useThreads } from './useThreads'
import { useCompanies } from './useCompanies'
import { useSessions } from './useSessions'
import type { HuginnWS, WSMessage } from './useHuginnWS'

// Live activity for left-nav pulse. Reuses thread run-state, in-thread
// space_reply_typing, and session streaming. Idle = no pulse. Error is
// terminal / human-quiet — not a spinner.

const typingAgents = reactive<Record<string, number>>({})
const streamingAgents = reactive<Record<string, number>>({})



function setFlag(map: Record<string, number>, name: string, on: boolean) {
  const key = name.toLowerCase()
  if (!key) return
  if (on) map[key] = 1
  else delete map[key]
}

function agentFromSession(sessionId: string | undefined, sessions: { id: string; agent?: string; agent_id: string }[]): string {
  if (!sessionId) return ''
  const s = sessions.find(x => x.id === sessionId)
  return (s?.agent || s?.agent_id || '').trim()
}

export function useAgentActivity() {
  const { isAgentActive } = useThreads()
  const { agentSeatedIn, effectiveCompanyId } = useCompanies()
  const { sessions } = useSessions()

  function isAgentLive(agentName: string): boolean {
    const key = agentName.toLowerCase()
    if (!key) return false
    if (isAgentActive(agentName)) return true
    if (typingAgents[key]) return true
    if (streamingAgents[key]) return true
    return false
  }

  /**
   * Pulse the left-nav row when the agent is thinking/working anywhere.
   * companyId scopes specialists: Lab must not pulse Huginn-only Steve
   * unless he is seated in Lab. Omit / null = current rail filter
   * (Desk = anyone live; a selected company = seated members only).
   */
  function isAgentPulsing(agentName: string, companyId?: string | null): boolean {
    if (!isAgentLive(agentName)) return false
    const scope = companyId === undefined ? effectiveCompanyId.value : companyId
    if (!scope) return true
    return agentSeatedIn(agentName, scope)
  }

  function noteTyping(agent: string, on: boolean) {
    setFlag(typingAgents, agent, on)
  }

  function noteStreaming(agent: string, on: boolean) {
    setFlag(streamingAgents, agent, on)
  }

  function wireActivityWS(ws: HuginnWS): () => void {
    const onTyping = (msg: WSMessage) => {
      const agent = typeof msg.payload?.['agent'] === 'string' ? msg.payload['agent'] as string : ''
      if (agent) setFlag(typingAgents, agent, true)
    }
    const onTypingDone = (msg: WSMessage) => {
      const agent = typeof msg.payload?.['agent'] === 'string' ? msg.payload['agent'] as string : ''
      if (agent) setFlag(typingAgents, agent, false)
    }
    const onToken = (msg: WSMessage) => {
      const fromPayload = typeof msg.payload?.['agent'] === 'string' ? msg.payload['agent'] as string : ''
      const agent = fromPayload || agentFromSession(msg.session_id, sessions.value)
      if (agent) setFlag(streamingAgents, agent, true)
    }
    const onIdle = (msg: WSMessage) => {
      const fromPayload = typeof msg.payload?.['agent'] === 'string' ? msg.payload['agent'] as string : ''
      const agent = fromPayload || agentFromSession(msg.session_id, sessions.value)
      if (agent) setFlag(streamingAgents, agent, false)
    }
    ws.on('space_reply_typing', onTyping)
    ws.on('space_reply_typing_done', onTypingDone)
    ws.on('token', onToken)
    ws.on('done', onIdle)
    ws.on('error', onIdle)
    return () => {
      ws.off('space_reply_typing', onTyping)
      ws.off('space_reply_typing_done', onTypingDone)
      ws.off('token', onToken)
      ws.off('done', onIdle)
      ws.off('error', onIdle)
    }
  }

  function clearActivity() {
    for (const k of Object.keys(typingAgents)) delete typingAgents[k]
    for (const k of Object.keys(streamingAgents)) delete streamingAgents[k]
  }

  return {
    typingAgents,
    streamingAgents,
    isAgentLive,
    isAgentPulsing,
    noteTyping,
    noteStreaming,
    wireActivityWS,
    clearActivity,
  }
}
