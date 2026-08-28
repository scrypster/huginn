/**
 * useFirstRun — tracks the space auto-opened for a fresh install so
 * ChatView knows when to show the welcome card, and whether the user has
 * dismissed it. Persisted to localStorage so a refresh doesn't lose it,
 * but the card only ever shows above an empty DM (no real messages yet).
 */
import { ref } from 'vue'

const WELCOME_SPACE_KEY = 'huginn:welcome_space_id'
const WELCOME_DISMISSED_KEY = 'huginn:welcome_dismissed'

function readStorage(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}
function writeStorage(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* quota exceeded / disabled */ }
}

const welcomeSpaceId = ref<string | null>(readStorage(WELCOME_SPACE_KEY))
const welcomeDismissed = ref<boolean>(readStorage(WELCOME_DISMISSED_KEY) === '1')

export function useFirstRun() {
  function markWelcomeSpace(id: string) {
    welcomeSpaceId.value = id
    writeStorage(WELCOME_SPACE_KEY, id)
  }

  function dismissWelcome() {
    welcomeDismissed.value = true
    writeStorage(WELCOME_DISMISSED_KEY, '1')
  }

  return { welcomeSpaceId, welcomeDismissed, markWelcomeSpace, dismissWelcome }
}
