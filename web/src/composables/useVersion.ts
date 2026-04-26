import { ref, computed } from 'vue'
import { api } from './useApi'

/**
 * useVersion — fetches the running server's build version exactly once per
 * app lifetime and exposes it for unobtrusive UI confirmation ("are we on
 * the new version yet?"). Used in three places:
 *
 *   • H-logo tooltip (App.vue)
 *   • Profile / Cloud popover footer (App.vue)
 *   • Settings → About row (SettingsView.vue)
 *
 * The version comes from /api/v1/health, which the Go backend populates
 * from main.version (build-time -ldflags) via Server.SetVersion. An empty
 * server value falls back to "dev" on the backend, so the only way this
 * composable shows a blank label is if the network call hasn't finished
 * yet — handled below by versionLabel which substitutes "…" until then.
 */

// Module-level singleton: the version is global to the app, fetching it
// per-component would waste round-trips and cause label flicker.
const version = ref<string>('')

// inflight is the dedup guard: when a component mounts and immediately
// triggers loadVersion alongside, say, App.vue's onMounted, we want both
// callers to await the same promise instead of issuing two GETs. Reset to
// null only on failure, so a successful load is permanently cached.
let inflight: Promise<void> | null = null

export function useVersion() {
  async function loadVersion(): Promise<void> {
    // Already cached — nothing to do.
    if (version.value) return
    // A request is already in flight — wait on the same promise so all
    // callers resolve together with a single network round-trip.
    if (inflight) return inflight

    inflight = (async () => {
      try {
        const h = await api.health()
        // Defensive: server contract guarantees a string, but we guard
        // against a future schema change silently storing `undefined`.
        if (typeof h.version === 'string' && h.version.length > 0) {
          version.value = h.version
        }
      } catch {
        // Swallow: a missing version label is preferable to a noisy console
        // error in production. Resetting `inflight` below lets the caller
        // retry on the next tick (e.g. when the network recovers).
      } finally {
        inflight = null
      }
    })()

    return inflight
  }

  // versionLabel always renders *something* so the tooltip / popover row
  // never displays as empty whitespace. Once loadVersion resolves, the ref
  // updates and Vue's reactivity propagates the real value automatically.
  const versionLabel = computed(() => version.value || '…')

  return { version, versionLabel, loadVersion }
}
