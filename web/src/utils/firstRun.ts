/**
 * firstRun — detects a fresh install (no spaces, no sessions) so the app
 * can auto-open a DM with the default agent and show a welcome card,
 * instead of dropping the user on a bare "pick a channel" empty state.
 */

/** True when there are no spaces and no sessions — i.e. nothing has ever been created. */
export function isFreshInstall(spaceCount: number, sessionCount: number): boolean {
  return spaceCount === 0 && sessionCount === 0
}

export interface AgentLike {
  name: string
  is_default?: boolean
}

/** Picks the default agent (is_default), falling back to the first agent. */
export function pickDefaultAgent<T extends AgentLike>(agents: T[]): T | null {
  if (!agents.length) return null
  return agents.find(a => a.is_default) ?? agents[0]!
}

export interface BackendLike {
  type?: string
  api_key?: string
  endpoint?: string
}

export interface ConfigLike {
  reasoner_model?: string
  ollama_base_url?: string
  backend?: BackendLike
}

/**
 * Heuristic: is a model/backend configured well enough to actually run a
 * chat? Ollama only needs a base URL; hosted providers need an API key.
 */
export function isModelConfigured(cfg: ConfigLike | null | undefined): boolean {
  if (!cfg) return false
  const backend = cfg.backend
  if (!backend || !backend.type) return false
  if (backend.type === 'ollama') return !!(backend.endpoint || cfg.ollama_base_url)
  return !!backend.api_key
}
