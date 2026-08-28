import { describe, it, expect, beforeEach } from 'vitest'
import { useFirstRun } from '../useFirstRun'

describe('useFirstRun', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('starts with no welcome space and not dismissed', () => {
    const { welcomeSpaceId, welcomeDismissed } = useFirstRun()
    expect(welcomeSpaceId.value).toBeNull()
    expect(welcomeDismissed.value).toBe(false)
  })

  it('markWelcomeSpace records the auto-opened space id and persists it', () => {
    const { welcomeSpaceId, markWelcomeSpace } = useFirstRun()
    markWelcomeSpace('space-123')
    expect(welcomeSpaceId.value).toBe('space-123')
    expect(localStorage.getItem('huginn:welcome_space_id')).toBe('space-123')
  })

  it('dismissWelcome flips the dismissed flag and persists it', () => {
    const { welcomeDismissed, dismissWelcome } = useFirstRun()
    dismissWelcome()
    expect(welcomeDismissed.value).toBe(true)
    expect(localStorage.getItem('huginn:welcome_dismissed')).toBe('1')
  })

  it('a second call to useFirstRun shares the same underlying state', () => {
    const a = useFirstRun()
    a.markWelcomeSpace('space-456')
    const b = useFirstRun()
    expect(b.welcomeSpaceId.value).toBe('space-456')
  })
})
