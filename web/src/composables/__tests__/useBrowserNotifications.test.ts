import { describe, it, expect, vi, beforeEach } from 'vitest'

// We need to reset modules between tests to get fresh singleton state.
async function fresh() {
  vi.resetModules()
  const mod = await import('../useBrowserNotifications')
  return mod.useBrowserNotifications
}

describe('useBrowserNotifications', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('isSupported is false when Notification API is absent', async () => {
    const origNotif = (globalThis as any).Notification
    delete (globalThis as any).Notification
    const useBrowserNotifications = await fresh()
    const { isSupported } = useBrowserNotifications()
    expect(isSupported).toBe(false)
    ;(globalThis as any).Notification = origNotif
  })

  it('notify does nothing when document.hidden is false', async () => {
    Object.defineProperty(document, 'hidden', { value: false, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'granted' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'granted'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify does nothing when enabled is false', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'granted' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = false
    permission.value = 'granted'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify does nothing when permission is not granted', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const MockNotification = vi.fn()
    ;(globalThis as any).Notification = Object.assign(MockNotification, { permission: 'default' })
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'default'
    notify('title', 'body', 'tag')
    expect(MockNotification).not.toHaveBeenCalled()
  })

  it('notify fires Notification when enabled, granted, and tab hidden', async () => {
    Object.defineProperty(document, 'hidden', { value: true, writable: true, configurable: true })
    const instances: any[] = []
    function MockNotification(title: string, opts: any) { instances.push({ title, opts }) }
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { notify, enabled, permission } = useBrowserNotifications()
    enabled.value = true
    permission.value = 'granted'
    notify('Agent', 'Hello', 'session-done-123')
    expect(instances).toHaveLength(1)
    expect(instances[0]!.title).toBe('Agent')
    expect(instances[0]!.opts.tag).toBe('session-done-123')
  })

  it('enabled and permission are module-level singletons', async () => {
    const MockNotification = vi.fn()
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const a = useBrowserNotifications()
    const b = useBrowserNotifications()
    a.enabled.value = true
    expect(b.enabled.value).toBe(true) // same ref
  })

  it('enabled persists to localStorage', async () => {
    const MockNotification = vi.fn()
    MockNotification.permission = 'granted'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle } = useBrowserNotifications()
    toggle(false)
    expect(localStorage.getItem('huginn:notifications:enabled')).toBe('false')
  })

  it('toggle calls requestPermission when permission is default', async () => {
    const requestPermission = vi.fn().mockResolvedValue('granted')
    function MockNotification() {}
    MockNotification.permission = 'default'
    MockNotification.requestPermission = requestPermission
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle, permission } = useBrowserNotifications()
    permission.value = 'default'
    toggle(true)
    expect(requestPermission).toHaveBeenCalledOnce()
  })

  it('toggle sets enabled=false without calling requestPermission', async () => {
    const requestPermission = vi.fn()
    function MockNotification() {}
    MockNotification.permission = 'granted'
    MockNotification.requestPermission = requestPermission
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { toggle, enabled, permission } = useBrowserNotifications()
    permission.value = 'granted'
    enabled.value = true
    toggle(false)
    expect(enabled.value).toBe(false)
    expect(requestPermission).not.toHaveBeenCalled()
  })

  it('permission initialized from Notification.permission on construction', async () => {
    function MockNotification() {}
    MockNotification.permission = 'denied'
    ;(globalThis as any).Notification = MockNotification
    const useBrowserNotifications = await fresh()
    const { permission } = useBrowserNotifications()
    expect(permission.value).toBe('denied')
  })
})
