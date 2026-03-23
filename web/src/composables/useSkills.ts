import { ref, onUnmounted } from 'vue'
import { getToken } from './useApi'
import { useOptimisticUpdate } from './useOptimisticUpdate'

// Interfaces
export interface InstalledSkill {
  name: string
  author: string
  source: string  // "registry", "local", "github:user/repo"
  enabled: boolean
  tool_count: number
  version?: string
}

export interface RegistrySkill {
  id: string
  name: string
  display_name: string
  description: string
  author: string
  category: string
  tags: string[]
  source_url: string
  collection: string
  version?: string
}

export interface RegistryCollection {
  id: string
  name: string
  display_name: string
  author: string
  description: string
  skills: string[]
}

/**
 * skillsFetch: simple fetch wrapper that delegates auth to useApi's shared token.
 */
async function skillsFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${getToken()}`,
      ...(opts.headers as Record<string, string> || {}),
    },
  })
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`${path}: ${res.status} ${body}`)
  }
  return res.json()
}

// useInstalledSkills composable
export function useInstalledSkills() {
  const skills = ref<InstalledSkill[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const { update: optimisticUpdate, remove: optimisticRemove } =
    useOptimisticUpdate(skills, s => s.name)

  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      skills.value = await skillsFetch<InstalledSkill[]>('/api/v1/skills')
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
    } finally {
      loading.value = false
    }
  }

  async function toggleEnabled(name: string, enabled: boolean): Promise<void> {
    error.value = null
    const endpoint = enabled
      ? `/api/v1/skills/${encodeURIComponent(name)}/enable`
      : `/api/v1/skills/${encodeURIComponent(name)}/disable`
    try {
      await optimisticUpdate(name, { enabled }, () =>
        skillsFetch(endpoint, { method: 'PUT' }),
      )
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      throw e
    }
  }

  async function uninstall(name: string): Promise<void> {
    error.value = null
    try {
      await optimisticRemove(name, () =>
        skillsFetch(`/api/v1/skills/${encodeURIComponent(name)}`, { method: 'DELETE' }),
      )
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      throw e
    }
  }

  async function execute(name: string, input: string, signal?: AbortSignal): Promise<string> {
    if (input.length > 32_000) {
      throw new Error('Input too large (max 32 000 characters)')
    }
    const res = await skillsFetch<{ output: string; skill: string }>(
      `/api/v1/skills/${encodeURIComponent(name)}/execute`,
      { method: 'POST', body: JSON.stringify({ input }), signal },
    )
    return res.output
  }

  function wireWS(ws: WebSocket): void {
    const handler = (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data as string)
        if (msg.type === 'skill_changed') {
          load()
        }
      } catch { /* ignore malformed messages */ }
    }
    ws.addEventListener('message', handler)
    onUnmounted(() => ws.removeEventListener('message', handler))
  }

  return {
    skills,
    loading,
    error,
    load,
    toggleEnabled,
    uninstall,
    execute,
    wireWS,
  }
}

// useRegistrySkills composable
export function useRegistrySkills() {
  const index = ref<RegistrySkill[]>([])
  const collections = ref<RegistryCollection[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  const installing = ref<Set<string>>(new Set())

  async function load(refresh?: boolean): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const url = refresh ? '/api/v1/skills/registry/index?refresh=1' : '/api/v1/skills/registry/index'
      const raw = await skillsFetch<any>(url)
      index.value = Array.isArray(raw) ? raw : (raw?.skills ?? [])
      collections.value = raw?.collections ?? []
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
    } finally {
      loading.value = false
    }
  }

  async function installCollection(skillNames: string[], installFn: (name: string) => Promise<void>): Promise<void> {
    for (const name of skillNames) {
      await installFn(name)
    }
  }

  async function search(q: string): Promise<RegistrySkill[]> {
    error.value = null
    try {
      const params = new URLSearchParams({ q })
      return await skillsFetch<RegistrySkill[]>(`/api/v1/skills/registry/search?${params.toString()}`)
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      throw e
    }
  }

  async function install(name: string): Promise<void> {
    error.value = null
    installing.value.add(name)
    // Trigger reactivity by creating new Set
    installing.value = new Set(installing.value)
    try {
      await skillsFetch('/api/v1/skills/install', {
        method: 'POST',
        body: JSON.stringify({ target: name }),
      })
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Unknown error'
      throw e
    } finally {
      installing.value.delete(name)
      // Trigger reactivity by creating new Set
      installing.value = new Set(installing.value)
    }
  }

  function isInstalling(name: string): boolean {
    return installing.value.has(name)
  }

  return {
    index,
    collections,
    loading,
    error,
    installing,
    load,
    search,
    install,
    installCollection,
    isInstalling,
  }
}

// createSkill function
export async function createSkill(content: string): Promise<string> {
  const response = await skillsFetch<{ name: string }>('/api/v1/skills', {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
  return response.name
}
