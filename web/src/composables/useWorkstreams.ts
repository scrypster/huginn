import { ref } from 'vue'
import { getToken } from './useApi'

export interface Workstream {
  id: string
  name: string
  description: string
  created_at: string
}

async function workstreamFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getToken()}`,
      ...(opts.headers as Record<string, string> || {}),
    },
  })
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`API ${path}: ${res.status} ${body}`)
  }
  return res.json()
}

const workstreams = ref<Workstream[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

export function useWorkstreams() {
  async function list(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const data = await workstreamFetch<Workstream[] | { workstreams: Workstream[] }>('/api/v1/workstreams')
      if (Array.isArray(data)) {
        workstreams.value = data
      } else if (Array.isArray((data as { workstreams: Workstream[] }).workstreams)) {
        workstreams.value = (data as { workstreams: Workstream[] }).workstreams
      } else {
        workstreams.value = []
      }
    } catch (e) {
      error.value = (e as Error).message ?? 'Failed to load workstreams'
    } finally {
      loading.value = false
    }
  }

  async function create(name: string, description = ''): Promise<Workstream | null> {
    error.value = null
    try {
      const data = await workstreamFetch<Workstream>('/api/v1/workstreams', {
        method: 'POST',
        body: JSON.stringify({ name, description }),
      })
      workstreams.value.push(data)
      return data
    } catch (e) {
      error.value = (e as Error).message ?? 'Failed to create workstream'
      return null
    }
  }

  async function remove(id: string): Promise<boolean> {
    error.value = null
    try {
      await workstreamFetch(`/api/v1/workstreams/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      })
      workstreams.value = workstreams.value.filter(w => w.id !== id)
      return true
    } catch (e) {
      error.value = (e as Error).message ?? 'Failed to delete workstream'
      return false
    }
  }

  async function tagSession(workstreamId: string, sessionId: string): Promise<boolean> {
    error.value = null
    try {
      await workstreamFetch(`/api/v1/workstreams/${encodeURIComponent(workstreamId)}/sessions`, {
        method: 'POST',
        body: JSON.stringify({ session_id: sessionId }),
      })
      return true
    } catch (e) {
      error.value = (e as Error).message ?? 'Failed to tag session'
      return false
    }
  }

  /**
   * Parse a /project create "name" slash command.
   * Returns the project name, or null if the input doesn't match.
   */
  function parseProjectCreateCommand(input: string): string | null {
    const trimmed = input.trim()
    // Match: /project create "name" or /project create name
    const quoted = trimmed.match(/^\/project\s+create\s+"([^"]+)"/)
    if (quoted) return quoted[1]!.trim()
    const unquoted = trimmed.match(/^\/project\s+create\s+(.+)/)
    if (unquoted) return unquoted[1]!.trim()
    return null
  }

  return {
    workstreams,
    loading,
    error,
    list,
    create,
    remove,
    tagSession,
    parseProjectCreateCommand,
  }
}
