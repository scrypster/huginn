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

  it('labels command memory as process-scoped', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: base } })
    expect(w.get('[data-testid="approval-allow-command"]').text().toLowerCase())
      .toContain('this session')
  })

  it('renders a countdown from remaining_ms', () => {
    const w = mount(ClaudeApprovalCard, { props: { approval: { ...base, remaining_ms: 252000 } } })
    expect(w.text()).toContain('4:12')
  })

  it('shows the excerpt when present', () => {
    const w = mount(ClaudeApprovalCard, {
      props: { approval: { ...base, tool_name: 'Write', can_remember: false, summary: '/tmp/a.ts', excerpt: 'import x' } },
    })
    expect(w.text()).toContain('import x')
  })
})
