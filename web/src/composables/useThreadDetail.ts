import { ref } from 'vue'
import { getToken } from './useApi'
import type { HuginnWS, WSMessage } from './useHuginnWS'

export interface ThreadMessage {
  id: string
  role: 'user' | 'assistant' | 'tool_call' | 'tool_result'
  content: string
  agent: string
  seq: number
  created_at: string
  tool_name?: string  // set for tool_call and tool_result rows
  toolName?: string
  type?: string
  streaming?: boolean // true while tokens are still arriving
}

export interface ThreadArtifact {
  id: string
  kind: 'code_patch' | 'document' | 'timeline' | 'structured_data' | 'file_bundle'
  title: string
  content: string
  metadata_json?: string
  agent_name: string
  status: 'draft' | 'accepted' | 'rejected' | 'superseded' | 'failed'
  rejection_reason?: string
  triggering_message_id?: string
}

interface MessageThreadAPIResponse {
  messages: ThreadMessage[]
  thread_id?: string
  session_id?: string
  delegation_chain?: string[]
}

const isOpen = ref(false)
const threadMessageId = ref<string | null>(null)
const messages = ref<ThreadMessage[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const artifact = ref<ThreadArtifact | null>(null)
const delegationChain = ref<string[]>([])

// Debounce timer for WS-triggered refetches.
let refetchTimer: ReturnType<typeof setTimeout> | null = null

async function fetchThreadMessages(messageId: string): Promise<MessageThreadAPIResponse> {
  const res = await fetch(`/api/v1/messages/${encodeURIComponent(messageId)}/thread`, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getToken()}`,
    },
  })
  if (res.status === 401) {
    // Token may be stale — not retrying here to keep it simple
    throw new Error('Unauthorized: please refresh the page')
  }
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`Failed to load thread: ${res.status} ${body}`)
  }
  const data = await res.json()
  // Legacy bare-array shape (old server / some tests):
  if (Array.isArray(data)) {
    return { messages: data as ThreadMessage[], delegation_chain: [] }
  }
  return {
    messages: Array.isArray(data.messages) ? (data.messages as ThreadMessage[]) : [],
    thread_id: data.thread_id as string | undefined,
    session_id: data.session_id as string | undefined,
    delegation_chain: Array.isArray(data.delegation_chain)
      ? (data.delegation_chain as string[])
      : [],
  }
}

async function loadArtifactForThread(agentName: string, messageId: string): Promise<void> {
  if (!agentName || !messageId) return
  try {
    const since = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString()
    const res = await fetch(
      `/api/v1/agents/${encodeURIComponent(agentName)}/artifacts?since=${encodeURIComponent(since)}&limit=20`,
      {
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${getToken()}`,
        },
      }
    )
    if (!res.ok) return
    const data = await res.json()
    const list: ThreadArtifact[] = Array.isArray(data)
      ? data
      : Array.isArray(data.artifacts)
      ? data.artifacts
      : []
    const found = list.find(a => a.triggering_message_id === messageId)
    artifact.value = found ?? null
  } catch {
    artifact.value = null
  }
}

async function updateArtifactStatus(
  artifactId: string,
  status: 'accepted' | 'rejected',
  reason?: string
): Promise<void> {
  const body: Record<string, string> = { status }
  if (reason) body.reason = reason
  try {
    await fetch(`/api/v1/artifacts/${encodeURIComponent(artifactId)}/status`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${getToken()}`,
      },
      body: JSON.stringify(body),
    })
    if (artifact.value && artifact.value.id === artifactId) {
      artifact.value = { ...artifact.value, status, ...(reason ? { rejection_reason: reason } : {}) }
    }
  } catch { /* ignore */ }
}

// scheduleRefetch debounces refetch calls triggered by WS events to avoid
// flooding the API when multiple thread_done events arrive close together.
// On success it updates messages in-place; on failure the existing messages are kept.
function scheduleRefetch(): void {
  if (refetchTimer !== null) {
    clearTimeout(refetchTimer)
  }
  refetchTimer = setTimeout(async () => {
    refetchTimer = null
    const id = threadMessageId.value
    if (!id || !isOpen.value) return
    try {
      const result = await fetchThreadMessages(id)
      messages.value = result.messages
      if (result.delegation_chain && result.delegation_chain.length > 0) {
        delegationChain.value = result.delegation_chain
      }
    } catch {
      // Keep existing messages on failure — non-fatal.
    }
  }, 300)
}

// streamingMsgId tracks the synthetic streaming message currently being built
// from thread_token events. Reset on thread_done or panel close.
let streamingMsgId: string | null = null

// wireThreadDetailWS registers WS listeners for thread lifecycle events.
// Call once from App.vue initApp() after creating the WS connection.
// Returns an unsubscribe function (for cleanup or test teardown).
//
// WS events handled:
//   thread_started  — appends to delegationChain if panel is open for that thread
//   thread_token    — streams tokens into a live streaming bubble
//   thread_done     — finalizes streaming bubble, triggers a debounced message refetch
//   thread_status   — triggers refetch when terminal status reached
//   thread_result   — structured completion notification (no-op for panel, shown in main chat)
export function wireThreadDetailWS(ws: HuginnWS): () => void {
  const onStarted = (msg: WSMessage): void => {
    const tid = msg.payload?.['thread_id'] as string | undefined
    const agentId = msg.payload?.['agent_id'] as string | undefined
    if (!tid || !threadMessageId.value) return
    // Append agent to delegationChain for the currently-open message thread.
    // The chain is accumulated from successive thread_started events.
    if (agentId && !delegationChain.value.includes(agentId)) {
      delegationChain.value = [...delegationChain.value, agentId]
    }
  }

  const onToken = (msg: WSMessage): void => {
    // Only stream into the panel if it's open.
    if (!isOpen.value || !threadMessageId.value) return
    const token = msg.payload?.['token'] as string | undefined
    const agentId = (msg.payload?.['agent_id'] as string | undefined) ?? ''
    if (!token) return

    if (!streamingMsgId) {
      // Create a new synthetic streaming message.
      streamingMsgId = `streaming-${Date.now()}`
      messages.value = [
        ...messages.value,
        {
          id: streamingMsgId,
          role: 'assistant' as const,
          content: token,
          agent: agentId,
          seq: Date.now(),
          created_at: new Date().toISOString(),
          streaming: true,
        },
      ]
    } else {
      // Append token to the existing streaming message.
      messages.value = messages.value.map(m =>
        m.id === streamingMsgId ? { ...m, content: m.content + token } : m
      )
    }
  }

  const onDone = (msg: WSMessage): void => {
    const tid = msg.payload?.['thread_id'] as string | undefined
    if (!tid || !threadMessageId.value) return
    // Finalize the streaming bubble (mark not streaming), then refetch to get the
    // persisted version from the server with correct ID and timestamp.
    if (streamingMsgId) {
      messages.value = messages.value.map(m =>
        m.id === streamingMsgId ? { ...m, streaming: false } : m
      )
      streamingMsgId = null
    }
    scheduleRefetch()
  }

  const onStatus = (msg: WSMessage): void => {
    const tid = msg.payload?.['thread_id'] as string | undefined
    const status = msg.payload?.['status'] as string | undefined
    if (!tid || !threadMessageId.value) return
    if (status && ['done', 'error', 'cancelled'].includes(status)) {
      scheduleRefetch()
    }
  }

  // Live tool call display: add a temporary tool_call bubble when Sam uses a tool.
  // These are replaced by persisted rows on the next refetch after thread_done.
  const onToolCall = (msg: WSMessage): void => {
    if (!isOpen.value || !threadMessageId.value) return
    const toolName = msg.payload?.['tool'] as string | undefined
    const args = msg.payload?.['args'] as Record<string, unknown> | undefined
    if (!toolName) return
    const liveId = `live-tool-${toolName}-${Date.now()}`
    messages.value = [
      ...messages.value,
      {
        id: liveId,
        role: 'tool_call' as const,
        content: JSON.stringify({ name: toolName, args: args ?? {} }),
        agent: '',
        seq: Date.now(),
        created_at: new Date().toISOString(),
        tool_name: toolName,
      },
    ]
  }

  ws.on('thread_started', onStarted)
  ws.on('thread_token', onToken)
  ws.on('thread_done', onDone)
  ws.on('thread_status', onStatus)
  ws.on('thread_tool_call', onToolCall)

  return () => {
    ws.off('thread_started', onStarted)
    ws.off('thread_token', onToken)
    ws.off('thread_done', onDone)
    ws.off('thread_status', onStatus)
    ws.off('thread_tool_call', onToolCall)
  }
}

export function useThreadDetail() {
  async function open(messageId: string, agentName = ''): Promise<void> {
    threadMessageId.value = messageId
    isOpen.value = true
    loading.value = true
    error.value = null
    messages.value = []
    artifact.value = null
    delegationChain.value = []

    try {
      const result = await fetchThreadMessages(messageId)
      messages.value = result.messages
      delegationChain.value = result.delegation_chain ?? []
      // Try to load artifact for thread
      const firstAgent = agentName || result.messages.find(m => m.role === 'assistant')?.agent || ''
      if (firstAgent) {
        await loadArtifactForThread(firstAgent, messageId)
      }
    } catch (e) {
      error.value = (e as Error).message ?? 'Failed to load thread'
    } finally {
      loading.value = false
    }
  }

  function close(): void {
    if (refetchTimer !== null) {
      clearTimeout(refetchTimer)
      refetchTimer = null
    }
    isOpen.value = false
    threadMessageId.value = null
    messages.value = []
    error.value = null
    artifact.value = null
    delegationChain.value = []
    streamingMsgId = null
  }

  async function handleAcceptArtifact(id: string): Promise<void> {
    await updateArtifactStatus(id, 'accepted')
  }

  async function handleRejectArtifact(id: string, reason?: string): Promise<void> {
    await updateArtifactStatus(id, 'rejected', reason)
  }

  return {
    isOpen,
    threadMessageId,
    messages,
    loading,
    error,
    artifact,
    delegationChain,
    open,
    close,
    handleAcceptArtifact,
    handleRejectArtifact,
  }
}
