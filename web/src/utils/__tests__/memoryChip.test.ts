import { describe, expect, it } from 'vitest'
import { isVaultMemoryWarning, muninnSettingsHint, resolveMemoryChip } from '../memoryChip'

describe('isVaultMemoryWarning', () => {
  it('matches the old transcript dump without treating other warnings as vault alarms', () => {
    expect(isVaultMemoryWarning('⚠️ Memory vault unavailable: muninn config unavailable. Memory features are disabled for this session.')).toBe(true)
    expect(isVaultMemoryWarning('Vault unavailable')).toBe(true)
    expect(isVaultMemoryWarning('Permission required')).toBe(false)
  })
})

describe('resolveMemoryChip', () => {
  it('shows a connect chip when muninn is installed and the agent has no vault', () => {
    const chip = resolveMemoryChip({
      agentName: 'Winston',
      vaultName: '',
      muninn: { installed: true, running: true, connected: false },
      inChat: true,
    })
    expect(chip?.kind).toBe('connect')
    expect(chip?.text).toBe("Winston isn't using a Muninn vault yet. Connect or create one.")
  })

  it('uses legacy copy for markdown / context notes without a vault', () => {
    const chip = resolveMemoryChip({
      agentName: 'Winston',
      memoryType: 'context',
      contextNotesEnabled: true,
      muninn: { installed: true, running: true },
      inChat: true,
    })
    expect(chip?.kind).toBe('legacy')
    expect(chip?.text).toContain('markdown files for memory (legacy)')
  })

  it('hides the chip when muninn is not installed and not running', () => {
    const chip = resolveMemoryChip({
      agentName: 'Winston',
      muninn: { installed: false, running: false, connected: false, detected: false },
      inChat: true,
    })
    expect(chip).toBeNull()
  })

  it('hides when status has not loaded yet', () => {
    expect(resolveMemoryChip({
      agentName: 'Winston',
      muninn: {},
      inChat: true,
    })).toBeNull()
  })

  it('hides when the agent already has a connected vault', () => {
    const chip = resolveMemoryChip({
      agentName: 'Winston',
      vaultName: 'huginn:agent:mj:winston',
      muninn: { installed: true, running: true, connected: true },
      inChat: true,
    })
    expect(chip).toBeNull()
  })

  it('stays hidden after dismiss and out of chat', () => {
    expect(resolveMemoryChip({
      agentName: 'Winston',
      muninn: { installed: true, running: true },
      dismissed: true,
      inChat: true,
    })).toBeNull()
    expect(resolveMemoryChip({
      agentName: 'Winston',
      muninn: { installed: true, running: true },
      inChat: false,
    })).toBeNull()
  })
})

describe('muninnSettingsHint', () => {
  it('is quiet in chat and only speaks in settings when muninn is missing', () => {
    expect(muninnSettingsHint({ installed: false, running: false, detected: false, connected: false }))
      .toBe("Muninn isn't running")
    expect(muninnSettingsHint({ installed: true, running: false })).toBeNull()
    expect(muninnSettingsHint({ connected: true })).toBeNull()
  })
})

  it('hides the connect nag when Muninn is already connected', () => {
    const chip = resolveMemoryChip({
      agentName: 'Winston',
      vaultName: '',
      muninn: { installed: true, running: true, connected: true, detected: true },
      inChat: true,
    })
    expect(chip).toBeNull()
  })
