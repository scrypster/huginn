import { ref, computed } from 'vue'
import { api } from './useApi'

const version = ref<string>('')
const stale = ref<boolean>(false)
let inflight: Promise<void> | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null

export function useVersion() {
  async function loadVersion(): Promise<void> {
    if (version.value && !stale.value) return
    if (inflight) return inflight

    inflight = (async () => {
      try {
        const h = await api.health()
        if (typeof h.version === 'string' && h.version.length > 0) {
          version.value = h.version
        }
        stale.value = h.stale ?? false
      } catch {
        // swallow — missing version label is preferable to a noisy error
      } finally {
        inflight = null
      }
    })()

    return inflight
  }

  function startPolling(): void {
    if (pollTimer !== null) return
    pollTimer = setInterval(async () => {
      try {
        const h = await api.health()
        stale.value = h.stale ?? false
        if (typeof h.version === 'string' && h.version.length > 0) {
          version.value = h.version
        }
      } catch {
        // ignore — server may be restarting
      }
    }, 60_000)
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  const versionLabel = computed(() => version.value || '…')

  return { version, versionLabel, stale, loadVersion, startPolling, stopPolling }
}
