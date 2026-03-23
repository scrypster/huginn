import { ref, computed } from 'vue'
import type { HuginnWS, WSMessage } from './useHuginnWS'

export interface SwarmAgent {
  id: string
  name: string
  status: 'waiting' | 'running' | 'done' | 'error' | 'cancelled'
  /** Accumulated output, capped at MAX_OUTPUT_BYTES to bound memory. */
  output: string
  success?: boolean
  error?: string
}

export interface SwarmState {
  sessionId: string
  agents: SwarmAgent[]
  complete: boolean
  cancelled: boolean
  droppedEvents: number
}

/** Maximum bytes of output retained per agent (keeps the tail). */
const MAX_OUTPUT_BYTES = 64 * 1024

// Module-level reactive state — one active swarm at a time per browser tab.
const swarmState = ref<SwarmState | null>(null)

/**
 * wireSwarmWS registers WebSocket listeners for swarm lifecycle events.
 * Call once from App.vue after the WS connection is established.
 * Returns an unsubscribe function for cleanup/test teardown.
 *
 * Events handled:
 *   swarm_start        — initialises SwarmState with agent list
 *   swarm_agent_status — updates an agent's status (and success/error fields)
 *   swarm_agent_token  — appends output to an agent (bounded to MAX_OUTPUT_BYTES)
 *   swarm_complete     — marks the swarm as finished
 *   swarm_drop_warning — updates droppedEvents reactively during execution
 */
export function wireSwarmWS(ws: HuginnWS, getSessionId: () => string): () => void {
  const onSwarmStart = (msg: WSMessage): void => {
    const rawAgents = (msg.payload?.['agents'] ?? []) as Array<{ id: string; name: string }>
    swarmState.value = {
      sessionId: getSessionId(),
      agents: rawAgents.map(a => ({
        id: a.id,
        name: a.name,
        status: 'waiting',
        output: '',
      })),
      complete: false,
      cancelled: false,
      droppedEvents: 0,
    }
  }

  const onAgentStatus = (msg: WSMessage): void => {
    if (!swarmState.value) return
    // Guard: ignore events belonging to a different session.
    if (msg.session_id && msg.session_id !== swarmState.value.sessionId) return

    const agentId = msg.payload?.['agent_id'] as string | undefined
    const status = msg.payload?.['status'] as SwarmAgent['status'] | undefined
    if (!agentId || !status) return

    const agent = swarmState.value.agents.find(a => a.id === agentId)
    if (!agent) return

    agent.status = status
    if (msg.payload && 'success' in msg.payload) {
      agent.success = msg.payload['success'] as boolean
    }
    if (msg.payload?.['error']) {
      agent.error = msg.payload['error'] as string
    }
  }

  const onAgentToken = (msg: WSMessage): void => {
    if (!swarmState.value) return
    if (msg.session_id && msg.session_id !== swarmState.value.sessionId) return

    const agentId = msg.payload?.['agent_id'] as string | undefined
    const content = (msg.payload?.['content'] as string | undefined) ?? ''
    if (!agentId || !content) return

    const agent = swarmState.value.agents.find(a => a.id === agentId)
    if (!agent) return

    agent.output += content
    // Bound output size: keep the most recent MAX_OUTPUT_BYTES bytes.
    if (agent.output.length > MAX_OUTPUT_BYTES) {
      agent.output = agent.output.slice(agent.output.length - MAX_OUTPUT_BYTES)
    }
  }

  const onSwarmComplete = (msg: WSMessage): void => {
    if (!swarmState.value) return
    if (msg.session_id && msg.session_id !== swarmState.value.sessionId) return

    swarmState.value.complete = true
    swarmState.value.cancelled = (msg.payload?.['cancelled'] as boolean) ?? false
    swarmState.value.droppedEvents = (msg.payload?.['dropped_events'] as number) ?? 0
  }

  const onDropWarning = (msg: WSMessage): void => {
    if (!swarmState.value) return
    if (msg.session_id && msg.session_id !== swarmState.value.sessionId) return

    const dropped = msg.payload?.['dropped'] as number | undefined
    if (dropped != null) {
      swarmState.value.droppedEvents = dropped
    }
  }

  ws.on('swarm_start', onSwarmStart)
  ws.on('swarm_agent_status', onAgentStatus)
  ws.on('swarm_agent_token', onAgentToken)
  ws.on('swarm_complete', onSwarmComplete)
  ws.on('swarm_drop_warning', onDropWarning)

  return () => {
    ws.off('swarm_start', onSwarmStart)
    ws.off('swarm_agent_status', onAgentStatus)
    ws.off('swarm_agent_token', onAgentToken)
    ws.off('swarm_complete', onSwarmComplete)
    ws.off('swarm_drop_warning', onDropWarning)
  }
}

/** useSwarmStatus provides reactive access to the current swarm execution state. */
export function useSwarmStatus() {
  const isSwarmActive = computed(
    () => swarmState.value !== null && !swarmState.value.complete
  )

  /** Clear swarm state (e.g., when navigating away or starting a new swarm). */
  function clearSwarm() {
    swarmState.value = null
  }

  return {
    swarmState,
    isSwarmActive,
    clearSwarm,
  }
}
