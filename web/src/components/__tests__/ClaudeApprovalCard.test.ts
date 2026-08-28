import { describe, it, expect } from 'vitest'
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
