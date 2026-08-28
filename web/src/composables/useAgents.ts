import { ref, getCurrentInstance, onUnmounted } from 'vue'
import { api } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

export interface AgentSummary {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  description?: string          // agent's one-line description
  heartbeat_enabled?: boolean   // whether a heartbeat cron is active
  vault_name?: string           // MuninnDB vault name if memory is configured
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

  function wireWS(ws: HuginnWS): void {
    if (!ws) return
    const onChanged = (msg: WSMessage) => {
      const { name: agentName, action, ...rest } = (msg.payload ?? {}) as Record<string, unknown> & { name?: string; action?: string }
      if (action === 'deleted') {
        removeAgent(agentName as string)
      } else if (action === 'created' || action === 'updated') {
        // Optimistically reflect the change immediately (no page reload needed),
        // then re-fetch the full list in the background to pick up all fields.
        if (agentName) updateAgent(agentName, { ...rest, name: agentName } as Partial<AgentSummary>)
        fetchAgents()
      }
    }
    ws.on('agent_changed', onChanged)
    if (getCurrentInstance() != null) {
      onUnmounted(() => ws.off('agent_changed', onChanged))
    }
  }

  return { agents, loading, fetchAgents, updateAgent, removeAgent, getAgentByName, wireWS }
}
