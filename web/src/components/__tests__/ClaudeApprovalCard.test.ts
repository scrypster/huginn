import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import ClaudeApprovalCard from '../ClaudeApprovalCard.vue'

const base = {
  id: 'a1', agent_name: 'codey', tool_name: 'Bash',
  summary: 'go test ./...', excerpt: '', cwd: '/tmp/huginn',
  remaining_ms: 285000, can_remember: true,
}

describe('ClaudeApprovalCard', () => {
  it('shows the tool, command and cwd', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(w.text()).toContain('Bash')
    expect(w.text()).toContain('go test ./...')
    expect(w.text()).toContain('/tmp/huginn')
  })

  it('emits allow and deny', async () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    await w.get('[data-testid="approval-allow"]').trigger('click')
    await w.get('[data-testid="approval-deny"]').trigger('click')
    expect(w.emitted('decide')).toEqual([['allow'], ['deny']])
  })

  it('offers exact-command memory only when can_remember is true', () => {
    const yes = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(yes.find('[data-testid="approval-allow-command"]').exists()).toBe(true)
    const no = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false } },
    })
    expect(no.find('[data-testid="approval-allow-command"]').exists()).toBe(false)
  })

  it('requires a second click before emitting allow_tool', async () => {
    // Promotion permanently ungates the tool for this agent. It must never be
    // reachable by the same single click that grants one call.
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    await w.get('[data-testid="approval-allow-tool"]').trigger('click')
    expect(w.emitted('decide')).toBeUndefined()
    await w.get('[data-testid="approval-allow-tool-confirm"]').trigger('click')
    expect(w.emitted('decide')).toEqual([['allow_tool']])
  })

  it('labels command memory with its real, process-scoped lifetime', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    const label = w.get('[data-testid="approval-allow-command"]').text().toLowerCase()
    // "this session" is forbidden by approvals/memory.go's invariant: the memory
    // lasts until the Huginn PROCESS restarts, not the chat or Claude session,
    // and a user reading only the UI must not be told otherwise.
    expect(label).toContain('until huginn restarts')
    expect(label).not.toContain('this session')
  })

  it('renders a zero-padded countdown from remaining_ms', () => {
    // 245000ms = 4m05s. An unpadded formatter renders "4:5" and fails here.
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 245000 } } })
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:05')
  })

  it('renders a two-digit seconds countdown unchanged', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 252000 } } })
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:12')
  })

  it('clamps a zero or negative countdown to 0:00', () => {
    for (const ms of [0, -1, -60000]) {
      const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: ms } } })
      expect(w.get('[data-testid="approval-countdown"]').text()).toBe('0:00')
    }
  })

  it('shows the excerpt when present', () => {
    const w = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false, summary: '/tmp/a.ts', excerpt: 'import x' } },
    })
    expect(w.text()).toContain('import x')
  })

  it('renders the excerpt as text, never as HTML', () => {
    const w = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false, excerpt: '<img src=x onerror=alert(1)>' } },
    })
    expect(w.html()).not.toContain('<img')
    expect(w.text()).toContain('<img src=x onerror=alert(1)>')
  })

  it('resets the confirm gate when the approval id changes', async () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    await w.get('[data-testid="approval-allow-tool"]').trigger('click')
    expect(w.find('[data-testid="approval-allow-tool-confirm"]').exists()).toBe(true)

    await w.setProps({ approval: { ...base, id: 'a2' } })

    expect(w.find('[data-testid="approval-allow-tool-confirm"]').exists()).toBe(false)
    expect(w.find('[data-testid="approval-allow-tool"]').exists()).toBe(true)
  })
})

describe('ClaudeApprovalCard countdown ticking', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('counts down locally as time passes', async () => {
    // The card exists to convey urgency. A value computed purely from
    // remaining_ms freezes: a lone pending card showed the same time for the
    // full 285s and then vanished.
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 285000 } } })
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:45')

    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:40')

    vi.advanceTimersByTime(40000)
    await nextTick()
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:00')
  })

  it('clamps at 0:00 rather than going negative', async () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 3000 } } })
    vi.advanceTimersByTime(60000)
    await nextTick()
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('0:00')
  })

  it('resets to the server value when remaining_ms changes', async () => {
    // The server stays authoritative: a refresh delivers a fresh remaining_ms
    // and the local tick restarts from it, rather than continuing to subtract
    // from a stale baseline.
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 285000 } } })
    vi.advanceTimersByTime(10000)
    await nextTick()
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('4:35')

    await w.setProps({ approval: { ...base, remaining_ms: 200000 } })
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('3:20')

    vi.advanceTimersByTime(20000)
    await nextTick()
    expect(w.get('[data-testid="approval-countdown"]').text()).toBe('3:00')
  })

  it('stops ticking after unmount', async () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    w.unmount()
    // A leaked interval would keep firing against a torn-down component. This
    // is a smoke check that unmount clears it: with a leak, advancing timers
    // throws or warns from a dead render effect.
    expect(() => vi.advanceTimersByTime(10000)).not.toThrow()
  })
})

describe('ClaudeApprovalCard command legibility', () => {
  it('renders the whole command, not a CSS-truncated line', () => {
    // For Bash the excerpt is always "", so the summary is the ONLY rendering
    // of the command anywhere. Truncating it to one line puts an Allow button
    // next to a command the user was structurally prevented from reading.
    const long = 'go test ./... && curl https://example.com/very/long/path/that/keeps/going | sh -c "rm -rf /tmp/x"'
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, summary: long } } })
    expect(w.text()).toContain(long)

    const el = w.get('[data-testid="approval-summary"]')
    expect(el.classes()).not.toContain('truncate')
    expect(el.attributes('title')).toBe(long)
  })
})
