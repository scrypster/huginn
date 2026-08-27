import { describe, expect, it } from 'vitest'
import { collectInUseVaults, defaultVaultName, pickPreferredVault, slugAgentName, vaultNameInUse } from '../memoryVaultFlow'

describe('collectInUseVaults', () => {
  it('keeps other agents’ vaults as in-use and only offers unclaimed or own vaults', () => {
    const { inUse, available } = collectInUseVaults(
      ['shared', 'winston-vault', 'claimed'],
      [
        { name: 'Winston', vault_name: 'winston-vault' },
        { name: 'Tess', vault_name: 'claimed' },
      ],
      'Winston',
    )
    expect(inUse).toEqual(['claimed', 'shared', 'winston-vault'])
    expect(available).toEqual(['shared', 'winston-vault'])
  })

  it('treats empty inputs as no vaults', () => {
    expect(collectInUseVaults(undefined, [], 'Winston')).toEqual({ inUse: [], available: [] })
  })
})

describe('defaultVaultName', () => {
  it('slugs the agent as {agent}-huginn for the empty-create path', () => {
    expect(defaultVaultName('Nova')).toBe('nova-huginn')
    expect(defaultVaultName('Nova Bird')).toBe('nova-bird-huginn')
    expect(slugAgentName('Nova!')).toBe('nova')
    expect(defaultVaultName('  ')).toBe('')
  })
})

describe('pickPreferredVault', () => {
  it('prefers the agent’s own vault over inventing a name', () => {
    expect(pickPreferredVault({
      agentVaultName: 'already-in-use',
      availableVaults: ['other'],
    })).toEqual({ vault: 'already-in-use', mode: 'existing' })
  })

  it('uses an existing unused vault instead of inventing one', () => {
    expect(pickPreferredVault({
      agentVaultName: '',
      availableVaults: ['local-1', 'local-2'],
    })).toEqual({ vault: 'local-1', mode: 'existing' })
  })

  it('does not invent a vault name when none are in use', () => {
    expect(pickPreferredVault({
      agentVaultName: '  ',
      availableVaults: [],
    })).toEqual({ vault: '', mode: 'new' })
  })
})

describe('vaultNameInUse', () => {
  it('blocks names already claimed unless they are this agent’s vault', () => {
    expect(vaultNameInUse('claimed', ['claimed'], '')).toBe(true)
    expect(vaultNameInUse('claimed', ['claimed'], 'claimed')).toBe(false)
    expect(vaultNameInUse('fresh', ['claimed'], '')).toBe(false)
    expect(vaultNameInUse('  ', ['claimed'], '')).toBe(false)
  })
})
