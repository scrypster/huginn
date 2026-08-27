export type MuninnPresence = {
  connected?: boolean
  installed?: boolean
  running?: boolean
  detected?: boolean
}

export type MemoryChipKind = 'connect' | 'legacy'

export type MemoryChip = {
  kind: MemoryChipKind
  text: string
  agentName: string
}

export function isVaultMemoryWarning(content: string): boolean {
  const c = (content || '').toLowerCase()
  return (
    c.includes('memory vault unavailable') ||
    c.includes('memory features are disabled') ||
    c.includes('muninn config unavailable') ||
    c.includes('vault unavailable')
  )
}

/** Installed, running, detected, or already connected — otherwise hide the chip. */
export function isMuninnAvailable(muninn: MuninnPresence): boolean {
  return !!(muninn.installed || muninn.running || muninn.detected || muninn.connected)
}

/** Settings-only one-liner. Chat stays quiet when Muninn is missing. */
export function muninnSettingsHint(muninn: MuninnPresence): string | null {
  if (isMuninnAvailable(muninn)) return null
  return "Muninn isn't running"
}

export function resolveMemoryChip(opts: {
  agentName?: string
  vaultName?: string
  memoryType?: string
  contextNotesEnabled?: boolean
  muninn: MuninnPresence
  dismissed?: boolean
  inChat?: boolean
}): MemoryChip | null {
  if (opts.dismissed) return null
  if (opts.inChat === false) return null

  const name = (opts.agentName || '').trim() || 'This agent'
  const hasVault = !!(opts.vaultName && opts.vaultName.trim())
  const connected = !!opts.muninn.connected
  const legacy = !!(
    opts.contextNotesEnabled ||
    opts.memoryType === 'context' ||
    opts.memoryType === 'notes'
  )

  // No chip / no modal connect path when Muninn is not on the machine.
  if (!isMuninnAvailable(opts.muninn)) return null

  if (hasVault && connected) return null

  if (legacy && !hasVault) {
    return {
      kind: 'legacy',
      text: 'This agent is using markdown files for memory (legacy). Connect a Muninn vault instead.',
      agentName: name,
    }
  }

  if (!hasVault || !connected) {
    return {
      kind: 'connect',
      text: `${name} isn't using a Muninn vault yet. Connect or create one.`,
      agentName: name,
    }
  }

  return null
}
