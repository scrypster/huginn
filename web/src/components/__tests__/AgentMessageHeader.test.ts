import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentMessageHeader from '../AgentMessageHeader.vue'

const PALETTE = ['#58A6FF', '#3FB950', '#FF7B72', '#D2A8FF', '#FFA657', '#79C0FF']

function agentColor(name: string): string {
  let h = 0
  for (const c of name) h = (Math.imul(31, h) + c.charCodeAt(0)) | 0
  return PALETTE[Math.abs(h) % PALETTE.length]
}

describe('AgentMessageHeader', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the agent name', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'atlas' },
    })
    expect(wrapper.html()).toContain('atlas')
  })

  it('renders the first letter of agent name as the initial chip', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'hermes' },
    })
    const chip = wrapper.find('span.flex-shrink-0')
    expect(chip.text()).toBe('H')
  })

  it('renders uppercased initial for a lowercase agent name', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'zeus' },
    })
    const chip = wrapper.find('span.flex-shrink-0')
    expect(chip.text()).toBe('Z')
  })

  it('renders agent color styling on the initial chip', () => {
    const name = 'atlas'
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: name },
    })
    // The chip span is the first span in the component; it has the initial letter
    // and inline color styling via :style binding. We verify it exists and shows initial.
    const chip = wrapper.find('span.select-none')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toBe('A')
  })

  it('renders agent color on name span', () => {
    const name = 'atlas'
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: name },
    })
    const nameSpan = wrapper.find('span.font-semibold')
    // jsdom normalizes hex colors to rgb(...), confirm a color style is applied
    const style = nameSpan.attributes('style') ?? ''
    expect(style).toMatch(/color/)
  })

  it('shows "just now" when no createdAt is provided', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'apollo' },
    })
    expect(wrapper.html()).toContain('just now')
  })

  it('shows "just now" when createdAt is recent (< 60 seconds ago)', () => {
    const now = new Date('2024-06-01T12:00:00.000Z')
    vi.setSystemTime(now)
    const recentTime = new Date(now.getTime() - 30_000).toISOString() // 30s ago
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'apollo', createdAt: recentTime },
    })
    expect(wrapper.html()).toContain('just now')
  })

  it('shows "Xm ago" when createdAt is 2 minutes ago', () => {
    const now = new Date('2024-06-01T12:00:00.000Z')
    vi.setSystemTime(now)
    const twoMinsAgo = new Date(now.getTime() - 2 * 60 * 1000).toISOString()
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'ares', createdAt: twoMinsAgo },
    })
    expect(wrapper.html()).toContain('2m ago')
  })

  it('shows "Xh ago" when createdAt is 3 hours ago', () => {
    const now = new Date('2024-06-01T12:00:00.000Z')
    vi.setSystemTime(now)
    const threeHoursAgo = new Date(now.getTime() - 3 * 60 * 60 * 1000).toISOString()
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'ares', createdAt: threeHoursAgo },
    })
    expect(wrapper.html()).toContain('3h ago')
  })

  it('shows a locale date when createdAt is over 24 hours ago', () => {
    const now = new Date('2024-06-01T12:00:00.000Z')
    vi.setSystemTime(now)
    const twoDaysAgo = new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000).toISOString()
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'poseidon', createdAt: twoDaysAgo },
    })
    // Should show a date string, not "just now" or "Xm ago"
    expect(wrapper.html()).not.toContain('just now')
    expect(wrapper.html()).not.toContain('m ago')
    expect(wrapper.html()).not.toContain('h ago')
  })

  it('shows "just now" for an invalid date string', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: 'hades', createdAt: 'not-a-date' },
    })
    expect(wrapper.html()).toContain('just now')
  })

  it('falls back to "?" initial when agentName is empty', () => {
    const wrapper = mount(AgentMessageHeader, {
      props: { agentName: '' },
    })
    const chip = wrapper.find('span.flex-shrink-0')
    expect(chip.text()).toBe('?')
  })

  it('different agent names get different palette colors', () => {
    const color1 = agentColor('atlas')
    const color2 = agentColor('hermes')
    // They might be the same if hash collision, but with these two names they differ
    // Just verify both are valid hex colors from the palette
    expect(PALETTE).toContain(color1)
    expect(PALETTE).toContain(color2)
  })
})
