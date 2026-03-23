import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ThreadCard from '../ThreadCard.vue'
import type { LiveThread } from '../../../composables/useThreads'

function makeThread(overrides: Partial<LiveThread> = {}): LiveThread {
  return {
    ID: 'thread-1',
    SessionID: 'sess-1',
    AgentID: 'atlas',
    Task: 'Analyze logs',
    Status: 'thinking',
    StartedAt: new Date().toISOString(),
    CompletedAt: '',
    TokensUsed: 200,
    TokenBudget: 5000,
    streamingContent: '',
    toolCalls: [],
    elapsedMs: 3000,
    ...overrides,
  }
}

function mountCard(thread: LiveThread, props: Partial<{ agentColor: string; agentIcon: string }> = {}) {
  return mount(ThreadCard, {
    props: { thread, ...props },
  })
}

// ── Phase 2E: Thread blocked UX ───────────────────────────────────────────

describe('ThreadCard — blocked status UX', () => {
  it('renders amber border class when status is blocked', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const root = wrapper.find('div')
    expect(root.classes().join(' ') + (root.attributes('class') ?? '')).toContain('border-huginn-yellow/40')
  })

  it('does not render amber border when status is thinking', () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    const root = wrapper.find('div')
    expect(root.attributes('class') ?? '').not.toContain('border-huginn-yellow/40')
  })

  it('does not render amber border when status is done', () => {
    const wrapper = mountCard(makeThread({ Status: 'done' }))
    const root = wrapper.find('div')
    expect(root.attributes('class') ?? '').not.toContain('border-huginn-yellow/40')
  })

  it('renders blocked section with "Waiting for input" heading', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    expect(wrapper.html()).toContain('Waiting for input')
  })

  it('does not render blocked section when status is thinking', () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    expect(wrapper.html()).not.toContain('Waiting for input')
  })

  it('renders the inject input field when blocked', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    expect(input.exists()).toBe(true)
  })

  it('Send button has animate-pulse class when inject input is empty', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    expect(sendBtn).toBeDefined()
    expect(sendBtn!.classes()).toContain('animate-pulse')
  })

  it('Send button loses animate-pulse when inject input has content', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('my answer')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    expect(sendBtn).toBeDefined()
    expect(sendBtn!.classes()).not.toContain('animate-pulse')
  })

  it('emits inject event with content when Send button is clicked', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('proceed now')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    await sendBtn!.trigger('click')
    expect(wrapper.emitted('inject')).toBeTruthy()
    expect(wrapper.emitted('inject')![0]).toEqual(['thread-1', 'proceed now'])
  })

  it('clears inject input after sending', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('response text')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    await sendBtn!.trigger('click')
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('shows "needs input" status badge when blocked', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    expect(wrapper.html()).toContain('needs input')
  })

  it('shows warning triangle icon when blocked', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    // The warning SVG uses a specific path — check for huginn-yellow class on the svg
    expect(wrapper.html()).toContain('text-huginn-yellow')
  })
})

// ── Other status rendering ─────────────────────────────────────────────────

describe('ThreadCard — status rendering', () => {
  it('shows bouncing dots when thread is thinking', () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    expect(wrapper.html()).toContain('animate-bounce')
  })

  it('shows green checkmark icon when done', () => {
    const wrapper = mountCard(makeThread({ Status: 'done' }))
    expect(wrapper.html()).toContain('text-huginn-green')
  })

  it('shows "done" status badge when done', () => {
    const wrapper = mountCard(makeThread({ Status: 'done' }))
    expect(wrapper.html()).toContain('done')
  })

  it('emits cancel when Cancel button clicked while running', async () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeDefined()
    await cancelBtn!.trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
    expect(wrapper.emitted('cancel')![0]).toEqual(['thread-1'])
  })

  it('shows agent initial in avatar', () => {
    const wrapper = mountCard(makeThread({ AgentID: 'zeus' }))
    expect(wrapper.html()).toContain('Z')
  })

  it('uses agentIcon prop over AgentID initial', () => {
    const wrapper = mountCard(makeThread({ AgentID: 'zeus' }), { agentIcon: '⚡' })
    expect(wrapper.html()).toContain('⚡')
  })
})

// ── Additional status types ─────────────────────────────────────────────────

describe('ThreadCard — additional status types', () => {
  it('shows "queued" label when status is queued', () => {
    const wrapper = mountCard(makeThread({ Status: 'queued' }))
    expect(wrapper.html()).toContain('queued')
  })

  it('shows bouncing dots when status is queued (isRunning)', () => {
    const wrapper = mountCard(makeThread({ Status: 'queued' }))
    expect(wrapper.html()).toContain('animate-bounce')
  })

  it('shows "error" label when status is error', () => {
    const wrapper = mountCard(makeThread({ Status: 'error' }))
    expect(wrapper.html()).toContain('error')
  })

  it('shows red icon (text-huginn-red) when status is error', () => {
    const wrapper = mountCard(makeThread({ Status: 'error' }))
    expect(wrapper.html()).toContain('text-huginn-red')
  })

  it('shows "cancelled" label when status is cancelled', () => {
    const wrapper = mountCard(makeThread({ Status: 'cancelled' }))
    expect(wrapper.html()).toContain('cancelled')
  })

  it('does not show cancel button when status is cancelled', () => {
    const wrapper = mountCard(makeThread({ Status: 'cancelled' }))
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeUndefined()
  })

  it('shows "done" status label when status is completed', () => {
    const wrapper = mountCard(makeThread({ Status: 'completed' }))
    expect(wrapper.html()).toContain('done')
  })

  it('shows green checkmark when status is completed', () => {
    const wrapper = mountCard(makeThread({ Status: 'completed' }))
    expect(wrapper.html()).toContain('text-huginn-green')
  })

  it('shows bouncing dots when status is thinking', () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    expect(wrapper.html()).toContain('animate-bounce')
  })

  it('shows Cancel button when status is thinking', () => {
    const wrapper = mountCard(makeThread({ Status: 'thinking' }))
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeDefined()
  })

  it('does not show Cancel button when status is done', () => {
    const wrapper = mountCard(makeThread({ Status: 'done' }))
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeUndefined()
  })

  it('does not show Cancel button when status is error', () => {
    const wrapper = mountCard(makeThread({ Status: 'error' }))
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === 'Cancel')
    expect(cancelBtn).toBeUndefined()
  })
})

// ── Tool calls section ──────────────────────────────────────────────────────

describe('ThreadCard — tool calls section', () => {
  it('renders tools toggle button when toolCalls is non-empty', () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'bash', done: false }],
    }))
    expect(wrapper.html()).toContain('tool call')
  })

  it('does not render tools section when toolCalls is empty', () => {
    const wrapper = mountCard(makeThread({ toolCalls: [] }))
    // No tool toggle button should exist — check directly in DOM
    const toolsBtn = wrapper.findAll('button').find(b => b.text().includes('tool call'))
    expect(toolsBtn).toBeUndefined()
  })

  it('shows "1 tool call" (singular) for a single tool call', () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'read_file', done: true }],
    }))
    expect(wrapper.html()).toContain('1 tool call')
    expect(wrapper.html()).not.toContain('1 tool calls')
  })

  it('shows "2 tool calls" (plural) for multiple tool calls', () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'bash', done: false }, { tool: 'read_file', done: true }],
    }))
    expect(wrapper.html()).toContain('2 tool calls')
  })

  it('does not show individual tool rows before expanding', () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'bash', done: false }],
    }))
    // Tool rows are inside toolsExpanded=false section — should not exist yet
    // Look for the specific "running" span that only appears inside expanded tool rows
    const runningSpans = wrapper.findAll('span').filter(s => s.text() === 'running')
    expect(runningSpans).toHaveLength(0)
  })

  it('shows individual tool rows after clicking the tools toggle', async () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'bash', done: false }],
    }))
    // Find and click the tools toggle button (contains "tool call")
    const toolsBtn = wrapper.findAll('button').find(b => b.text().includes('tool call'))
    expect(toolsBtn).toBeDefined()
    await toolsBtn!.trigger('click')
    expect(wrapper.html()).toContain('bash')
    expect(wrapper.html()).toContain('running')
  })

  it('shows "done" indicator for a completed tool call when expanded', async () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'read_file', done: true }],
    }))
    const toolsBtn = wrapper.findAll('button').find(b => b.text().includes('tool call'))
    await toolsBtn!.trigger('click')
    expect(wrapper.html()).toContain('done')
  })

  it('tool with args renders the tool name in tool row', async () => {
    const wrapper = mountCard(makeThread({
      toolCalls: [{ tool: 'bash', args: { cmd: 'ls -la' }, done: false }],
    }))
    const toolsBtn = wrapper.findAll('button').find(b => b.text().includes('tool call'))
    await toolsBtn!.trigger('click')
    expect(wrapper.html()).toContain('bash')
  })
})

// ── Summary metadata ────────────────────────────────────────────────────────

describe('ThreadCard — summary metadata', () => {
  it('renders summary text when thread has a Summary', () => {
    const wrapper = mountCard(makeThread({
      Status: 'done',
      Summary: { Summary: 'All tasks completed.', Status: 'done' },
    }))
    expect(wrapper.html()).toContain('All tasks completed.')
  })

  it('renders FilesModified tags when summary has files', () => {
    const wrapper = mountCard(makeThread({
      Status: 'done',
      Summary: {
        Summary: 'Made changes',
        Status: 'done',
        FilesModified: ['/src/components/Foo.vue', '/src/utils/bar.ts'],
      },
    }))
    expect(wrapper.html()).toContain('Foo.vue')
    expect(wrapper.html()).toContain('bar.ts')
  })

  it('renders KeyDecisions as list items', () => {
    const wrapper = mountCard(makeThread({
      Status: 'done',
      Summary: {
        Summary: 'Refactored module',
        Status: 'done',
        KeyDecisions: ['Use composition API', 'Remove legacy store'],
      },
    }))
    expect(wrapper.html()).toContain('Use composition API')
    expect(wrapper.html()).toContain('Remove legacy store')
  })

  it('does not render summary section when thread is running', () => {
    const wrapper = mountCard(makeThread({
      Status: 'thinking',
      Summary: { Summary: 'Partial summary', Status: 'thinking' },
    }))
    // Summary section is guarded by !isRunning
    expect(wrapper.html()).not.toContain('Partial summary')
  })

  it('renders plain-text summary as-is (no JSON parsing side effects)', () => {
    const wrapper = mountCard(makeThread({
      Status: 'done',
      Summary: { Summary: 'Simple plain text summary', Status: 'done' },
    }))
    expect(wrapper.html()).toContain('Simple plain text summary')
  })

  it('parseSummary strips JSON tool call wrapper and returns message field', () => {
    // The JSON that LLMs sometimes emit as summary text
    const jsonSummary = JSON.stringify({ name: 'request_help', arguments: { message: 'Needs user decision' } })
    const wrapper = mountCard(makeThread({
      Status: 'done',
      Summary: { Summary: jsonSummary, Status: 'done' },
    }))
    // Rendered text should be the extracted message, not raw JSON
    expect(wrapper.html()).toContain('Needs user decision')
    expect(wrapper.html()).not.toContain('request_help')
  })

  it('does not render summary section when Summary is absent', () => {
    const wrapper = mountCard(makeThread({ Status: 'done', Summary: undefined }))
    // "Summary" heading should not appear
    expect(wrapper.html()).not.toContain('Summary</p>')
  })
})

// ── Elapsed time formatting ─────────────────────────────────────────────────

describe('ThreadCard — elapsed time formatting', () => {
  it('shows milliseconds for elapsedMs < 1000', () => {
    const wrapper = mountCard(makeThread({ elapsedMs: 500 }))
    expect(wrapper.html()).toContain('500ms')
  })

  it('shows seconds for elapsedMs = 5000', () => {
    const wrapper = mountCard(makeThread({ elapsedMs: 5000 }))
    expect(wrapper.html()).toContain('5s')
  })

  it('shows minutes+seconds for elapsedMs = 125000', () => {
    const wrapper = mountCard(makeThread({ elapsedMs: 125000 }))
    // 125s = 2m 5s
    expect(wrapper.html()).toContain('2m 5s')
  })

  it('shows 0ms for elapsedMs = 0', () => {
    const wrapper = mountCard(makeThread({ elapsedMs: 0 }))
    expect(wrapper.html()).toContain('0ms')
  })

  it('shows 60s boundary as 1m 0s', () => {
    const wrapper = mountCard(makeThread({ elapsedMs: 60000 }))
    expect(wrapper.html()).toContain('1m 0s')
  })
})

// ── Inject input validation ─────────────────────────────────────────────────

describe('ThreadCard — inject input validation', () => {
  it('Send button is disabled-looking (animate-pulse) when input is empty for blocked thread', () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    expect(sendBtn).toBeDefined()
    expect(sendBtn!.classes()).toContain('animate-pulse')
  })

  it('Send button loses animate-pulse when input has non-whitespace content', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('some content')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    expect(sendBtn!.classes()).not.toContain('animate-pulse')
  })

  it('does not emit inject when input is whitespace-only', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('   ')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    await sendBtn!.trigger('click')
    expect(wrapper.emitted('inject')).toBeFalsy()
  })

  it('Send button still shows animate-pulse when input is whitespace-only', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('   ')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    // trim() returns '' so button remains pulsing
    expect(sendBtn!.classes()).toContain('animate-pulse')
  })

  it('submitInject trims whitespace before emitting', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('  trimmed  ')
    const sendBtn = wrapper.findAll('button').find(b => b.text() === 'Send')
    await sendBtn!.trigger('click')
    expect(wrapper.emitted('inject')).toBeTruthy()
    expect(wrapper.emitted('inject')![0]).toEqual(['thread-1', 'trimmed'])
  })

  it('pressing Enter in the input triggers submitInject', async () => {
    const wrapper = mountCard(makeThread({ Status: 'blocked' }))
    const input = wrapper.find('input[placeholder="Type a response…"]')
    await input.setValue('via enter key')
    await input.trigger('keydown.enter')
    expect(wrapper.emitted('inject')).toBeTruthy()
    expect(wrapper.emitted('inject')![0]).toEqual(['thread-1', 'via enter key'])
  })
})
