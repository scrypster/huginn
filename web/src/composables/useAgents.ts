import { ref, onUnmounted } from 'vue'
import { api } from './useApi'

export interface AgentSummary {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  [key: string]: unknown
}

// Module-level singleton — shared across all components
const agents = ref<AgentSummary[]>([])
const loading = ref(false)

export function useAgents() {
  async function fetchAgents() {
    loading.value = true
    try {
      const data = await api.agents.list()
      agents.value = data as unknown as AgentSummary[]
    } catch { /* ignore */ } finally {
      loading.value = false
    }
  }

  function updateAgent(name: string, patch: Partial<AgentSummary>) {
    const idx = agents.value.findIndex(a => a.name === name)
    if (idx >= 0) {
      agents.value[idx] = { ...agents.value[idx], ...patch } as AgentSummary
    } else {
      agents.value.push(patch as AgentSummary)
    }
  }

  function removeAgent(name: string) {
    agents.value = agents.value.filter(a => a.name !== name)
  }

  function getAgentByName(name: string): AgentSummary | undefined {
    return agents.value.find(a => a.name.toLowerCase() === name.toLowerCase())
  }

  function wireWS(ws: WebSocket): void {
    const handler = (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data as string)
        if (msg.type === 'agent_changed') {
          const { name: agentName, action } = msg.payload ?? {}
          if (action === 'deleted') {
            removeAgent(agentName as string)
          } else if (action === 'created' || action === 'updated') {
            // Re-fetch the full list to get the latest data including all fields.
            fetchAgents()
          }
        }
      } catch { /* ignore malformed messages */ }
    }
    ws.addEventListener('message', handler)
    onUnmounted(() => ws.removeEventListener('message', handler))
  }

  return { agents, loading, fetchAgents, updateAgent, removeAgent, getAgentByName, wireWS }
}
