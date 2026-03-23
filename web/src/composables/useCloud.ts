import { ref } from 'vue'
import { api } from './useApi'

export interface CloudStatus {
  registered: boolean
  connected: boolean
  machine_id?: string
  cloud_url?: string
}

// Module-level shared state (singleton across all component instances)
const status = ref<CloudStatus>({ registered: false, connected: false })
const loading = ref(false)
const connecting = ref(false)
const disconnecting = ref(false)
const error = ref<string | null>(null)
// Generation counter: incremented on connect() and disconnect() to interrupt stale poll loops.
let connectGen = 0

export function useCloud() {
  async function fetchStatus() {
    loading.value = true
    error.value = null
    try {
      status.value = await api.cloud.status()
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function _fetchStatusSilent() {
    try {
      status.value = await api.cloud.status()
    } catch {
      // silently ignore during polling
    }
  }

  async function connect() {
    connectGen++
    const gen = connectGen
    connecting.value = true
    error.value = null
    try {
      await api.cloud.connect()
      // Poll until fully connected (WebSocket up) or 5 min timeout.
      // The loop exits early if disconnect() is called (connectGen incremented).
      const start = Date.now()
      while (Date.now() - start < 300_000) {
        await new Promise(r => setTimeout(r, 2000))
        if (gen !== connectGen) break
        await _fetchStatusSilent()
        if (gen !== connectGen || status.value.connected) break
      }
    } catch (e) {
      if (gen === connectGen) error.value = (e as Error).message
    } finally {
      if (gen === connectGen) connecting.value = false
    }
  }

  async function disconnect() {
    connectGen++ // invalidate any in-progress connect poll loop
    disconnecting.value = true
    error.value = null
    try {
      await api.cloud.disconnect()
      await fetchStatus()
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      disconnecting.value = false
    }
  }

  return { status, loading, connecting, disconnecting, error, fetchStatus, connect, disconnect }
}
