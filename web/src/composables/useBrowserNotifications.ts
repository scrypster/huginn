import { ref } from 'vue'

// Module-level singletons — shared across all callers (same pattern as useNotifications.ts).
// Declared outside the function so every useBrowserNotifications() call returns the same refs.
const _isSupported = typeof Notification !== 'undefined'
const enabled = ref(localStorage.getItem('huginn:notifications:enabled') === 'true')
const permission = ref<NotificationPermission>(_isSupported ? Notification.permission : 'denied')

export function useBrowserNotifications() {
  async function requestPermission() {
    if (!_isSupported) return
    const result = await Notification.requestPermission()
    permission.value = result
    if (result === 'granted') enabled.value = true
    localStorage.setItem('huginn:notifications:enabled', String(result === 'granted'))
  }

  function notify(title: string, body: string, tag: string, onClick?: () => void) {
    if (!_isSupported || !enabled.value || permission.value !== 'granted') return
    if (!document.hidden) return // tab is active — in-app UI is sufficient
    const n = new Notification(title, { body, tag, icon: '/favicon.ico' })
    if (onClick) n.onclick = () => { window.focus(); onClick() }
  }

  function toggle(value: boolean) {
    if (value && permission.value !== 'granted') {
      requestPermission()
    } else {
      enabled.value = value
      localStorage.setItem('huginn:notifications:enabled', String(value))
    }
  }

  return { isSupported: _isSupported, enabled, permission, requestPermission, notify, toggle }
}
