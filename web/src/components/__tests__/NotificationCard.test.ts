import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import NotificationCard from '../NotificationCard.vue'
import type { Notification } from '../../composables/useNotifications'

function makeNotification(overrides: Partial<Notification> = {}): Notification {
  return {
    id: 'notif-1',
    routine_id: 'routine-1',
    run_id: 'run-1',
    summary: 'Disk usage at 95%',
    detail: 'Server disk is almost full. Free up space immediately.',
    severity: 'urgent',
    status: 'pending',
    created_at: '2024-01-15T14:30:00Z',
    updated_at: '2024-01-15T14:30:00Z',
    ...overrides,
  }
}

describe('NotificationCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the notification summary', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ summary: 'Disk usage at 95%' }) },
    })
    expect(wrapper.html()).toContain('Disk usage at 95%')
  })

  it('renders the formatted timestamp', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ created_at: '2024-01-15T14:30:00Z' }) },
    })
    // Should have some time string rendered
    const timeSpan = wrapper.find('span.text-\\[10px\\].text-huginn-muted.flex-shrink-0')
    expect(timeSpan.exists()).toBe(true)
    // Time should not be empty for a valid timestamp
    expect(timeSpan.text()).not.toBe('')
  })

  it('renders the severity label', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'urgent' }) },
    })
    expect(wrapper.html()).toContain('urgent')
  })

  it('applies red border class for urgent severity', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'urgent' }) },
    })
    const card = wrapper.find('div.border')
    expect(card.classes()).toContain('border-huginn-red/40')
  })

  it('applies yellow border class for warning severity', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'warning' }) },
    })
    const card = wrapper.find('div.border')
    expect(card.classes()).toContain('border-yellow-500/30')
  })

  it('renders a blue dot for info severity', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'info' }) },
    })
    const dot = wrapper.find('.bg-huginn-blue')
    expect(dot.exists()).toBe(true)
  })

  it('renders a red dot for urgent severity', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'urgent' }) },
    })
    const dot = wrapper.find('.bg-huginn-red')
    expect(dot.exists()).toBe(true)
  })

  it('renders a yellow dot for warning severity', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ severity: 'warning' }) },
    })
    const dot = wrapper.find('.bg-yellow-400')
    expect(dot.exists()).toBe(true)
  })

  it('shows "View Detail" button initially', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification() },
    })
    const btn = wrapper.findAll('button').find(b => b.text() === 'View Detail')
    expect(btn).toBeTruthy()
  })

  it('toggles detail section when "View Detail" is clicked', async () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ detail: 'Full detail text here.' }) },
    })
    // Detail should not be visible initially
    expect(wrapper.html()).not.toContain('Full detail text here.')
    const viewBtn = wrapper.findAll('button').find(b => b.text() === 'View Detail')
    await viewBtn!.trigger('click')
    // After click, detail should be visible
    expect(wrapper.html()).toContain('Full detail text here.')
    // Button text should toggle to 'Collapse'
    expect(wrapper.html()).toContain('Collapse')
  })

  it('collapses detail section when "Collapse" is clicked', async () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ detail: 'Full detail text.' }) },
    })
    const viewBtn = wrapper.findAll('button').find(b => b.text() === 'View Detail')
    await viewBtn!.trigger('click')
    // Now expanded - find Collapse button
    const collapseBtn = wrapper.findAll('button').find(b => b.text() === 'Collapse')
    await collapseBtn!.trigger('click')
    expect(wrapper.html()).not.toContain('Full detail text.')
    expect(wrapper.html()).toContain('View Detail')
  })

  it('emits action event with id and "dismiss" when Dismiss is clicked', async () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ id: 'notif-42' }) },
    })
    const dismissBtn = wrapper.findAll('button').find(b => b.text() === 'Dismiss')
    expect(dismissBtn).toBeTruthy()
    await dismissBtn!.trigger('click')
    expect(wrapper.emitted('action')).toBeTruthy()
    expect(wrapper.emitted('action')![0]).toEqual(['notif-42', 'dismiss'])
  })

  it('emits chat event with the full notification when "→ Chat" is clicked', async () => {
    const notif = makeNotification({ id: 'notif-chat' })
    const wrapper = mount(NotificationCard, { props: { notification: notif } })
    const chatBtn = wrapper.findAll('button').find(b => b.text().includes('Chat'))
    expect(chatBtn).toBeTruthy()
    await chatBtn!.trigger('click')
    expect(wrapper.emitted('chat')).toBeTruthy()
    const emittedPayload = wrapper.emitted('chat')![0]![0] as Notification
    expect(emittedPayload.id).toBe('notif-chat')
  })

  it('renders empty string for an invalid timestamp', () => {
    const wrapper = mount(NotificationCard, {
      props: { notification: makeNotification({ created_at: 'not-a-date' }) },
    })
    const timeSpan = wrapper.find('span.text-\\[10px\\].text-huginn-muted.flex-shrink-0')
    expect(timeSpan.text()).toBe('')
  })
})
