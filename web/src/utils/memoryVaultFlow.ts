export type MemoryVaultPick = {
  vault: string
  mode: 'existing' | 'new'
}

export function collectInUseVaults(
  fromApi: string[] | undefined,
  fromAgents: Array<{ name?: string; vault_name?: string }>,
  currentAgent?: string,
): { inUse: string[]; available: string[] } {
  const claimed = new Map<string, string>()
  for (const a of fromAgents || []) {
    const vault = (a.vault_name || '').trim()
    const name = (a.name || '').trim()
    if (vault && name) claimed.set(vault, name)
  }
  const apiVaults = [...new Set((fromApi || []).map(v => v.trim()).filter(Boolean))]
  const inUse = [...new Set([...claimed.keys(), ...apiVaults])].sort()
  const me = (currentAgent || '').trim()
  const available = apiVaults
    .filter(v => {
      const owner = claimed.get(v)
      return !owner || owner === me
    })
    .sort()
  return { inUse, available }
}

export function slugAgentName(agentName: string): string {
  return (agentName || '')
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
    .replace(/-{2,}/g, '-')
    .replace(/^-|-$/g, '')
}

/** Empty-create default only. Picker still wins when unused vaults exist. */
export function defaultVaultName(agentName: string): string {
  const slug = slugAgentName(agentName)
  return slug ? `${slug}-huginn` : ''
}

export function pickPreferredVault(opts: {
  agentVaultName?: string
  availableVaults: string[]
}): MemoryVaultPick {
  const agent = (opts.agentVaultName || '').trim()
  if (agent) return { vault: agent, mode: 'existing' }
  if (opts.availableVaults.length > 0) {
    const first = opts.availableVaults[0]
    if (first) return { vault: first, mode: 'existing' }
  }
  return { vault: '', mode: 'new' }
}

export function vaultNameInUse(name: string, inUse: string[], currentAgentVault?: string): boolean {
  const n = name.trim()
  if (!n) return false
  if (currentAgentVault && n === currentAgentVault.trim()) return false
  return inUse.includes(n)
}
