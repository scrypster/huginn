import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ThreadDetail from '../ThreadDetail.vue'
import type { ThreadMessage, ThreadArtifact } from '../../composables/useThreadDetail'

// Mock marked to avoid ESM issues in tests
vi.mock('marked', () => ({
  marked: {
    parse: (content: string) => `<p>${content}</p>`,
  },
}))

// Stub child components to keep tests focused on ThreadDetail itself
vi.mock('../ArtifactCard.vue', () => ({
  default: {
    name: 'ArtifactCard',
    template: '<div data-testid="artifact-card">{{ artifact?.title }}</div>',
    props: ['artifact'],
    emits: ['accept', 'reject'],
  },
}))

vi.mock('../ObservationDeck.vue', () => ({
  default: {
    name: 'ObservationDeck',
    template: '<div data-testid="observation-deck"></div>',
    props: ['messages', 'agentName'],
  },
}))

// ── helpers ───────────────────────────────────────────────────────────────────

function makeMessage(overrides: Partial<ThreadMessage> = {}): ThreadMessage {
  return {
    id: 'msg-1',
    role: 'assistant',
    content: 'Hello world',
    agent: 'atlas',
    seq: 1,
    created_at: new Date().toISOString(),
    ...overrides,
  }
}

function makeArtifact(overrides: Partial<ThreadArtifact> = {}): ThreadArtifact {
  return {
    id: 'art-1',
    kind: 'document',
    title: 'My Artifact',
    content: 'artifact content',
    agent_name: 'atlas',
    status: 'draft',
    ...overrides,
  }
}

function mountComponent(props: Partial<InstanceType<typeof ThreadDetail>['$props']> = {}) {
  return mount(ThreadDetail, {
    props: {
      visible: true,
      messages: [],
      loading: false,
      error: null,
      ...props,
    },
  })
}

// ── tests ─────────────────────────────────────────────────────────────────────

describe('ThreadDetail — header', () => {
  it('renders Thread header label', () => {
    const wrapper = mountComponent()
    expect(wrapper.html()).toContain('Thread')
  })

  it('close button emits close event when clicked', async () => {
    const wrapper = mountComponent()
    const closeButtons = wrapper.findAll('button')
    expect(closeButtons.length).toBeGreaterThan(0)
    await closeButtons[0].trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('close')!.length).toBeGreaterThanOrEqual(1)
  })

  it('X close button also emits close event', async () => {
    const wrapper = mountComponent()
    const buttons = wrapper.findAll('button')
    // Second button is the X close button
    await buttons[1].trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})

describe('ThreadDetail — panel visibility', () => {
  it('slides in (400px wide) when visible=true', () => {
    const wrapper = mountComponent({ visible: true })
    const panel = wrapper.find('div')
    const style = panel.attributes('style') ?? ''
    expect(style).toContain('400px')
  })

  it('collapses to 0px when visible=false', () => {
    const wrapper = mountComponent({ visible: false })
    const panel = wrapper.find('div')
    const style = panel.attributes('style') ?? ''
    expect(style).toContain('0px')
  })
})

describe('ThreadDetail — loading state', () => {
  it('shows loading animation when loading=true', () => {
    const wrapper = mountComponent({ loading: true })
    // Loading bouncing dots are rendered as spans with animate-bounce
    expect(wrapper.html()).toContain('animate-bounce')
  })

  it('does not show loading animation when loading=false', () => {
    const wrapper = mountComponent({ loading: false })
    expect(wrapper.html()).not.toContain('animate-bounce')
  })
})

describe('ThreadDetail — error state', () => {
  it('shows error message when error is set', () => {
    const wrapper = mountComponent({ error: 'Failed to load thread: 500' })
    expect(wrapper.html()).toContain('Failed to load thread: 500')
  })

  it('does not show error block when error is null', () => {
    const wrapper = mountComponent({ error: null })
    expect(wrapper.html()).not.toContain('text-huginn-red')
  })
})

describe('ThreadDetail — empty state', () => {
  it('shows empty state message when messages array is empty and not loading', () => {
    const wrapper = mountComponent({ messages: [], loading: false, error: null })
    expect(wrapper.html()).toContain('No messages in this thread')
  })

  it('does not show empty state when messages are present', () => {
    const wrapper = mountComponent({
      messages: [makeMessage()],
      loading: false,
      error: null,
    })
    expect(wrapper.html()).not.toContain('No messages in this thread')
  })
})

describe('ThreadDetail — message rendering', () => {
  it('renders agent name for assistant messages', () => {
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'assistant', agent: 'atlas', content: 'Hello!' })],
    })
    expect(wrapper.html()).toContain('atlas')
  })

  it('renders user messages aligned to the right', () => {
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'user', content: 'User question here' })],
    })
    expect(wrapper.html()).toContain('justify-end')
  })

  it('renders tool_call messages as collapsed group summary', () => {
    const toolContent = JSON.stringify({ name: 'read_file' })
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'tool_call', content: toolContent })],
    })
    // Tool calls are collapsed into "N tool calls" summary — not shown as individual chips
    expect(wrapper.html()).toContain('tool call')
  })

  it('renders tool_result messages grouped with tool calls', () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({ id: 'm1', role: 'tool_call', content: JSON.stringify({ name: 'bash' }), tool_name: 'bash' }),
        makeMessage({ id: 'm2', role: 'tool_result', content: 'result output', tool_name: 'bash' }),
      ],
    })
    // Group shows "1 tool call"
    expect(wrapper.html()).toContain('tool call')
  })

  it('renders multiple messages', () => {
    const messages = [
      makeMessage({ id: 'msg-1', role: 'assistant', agent: 'atlas', content: 'First' }),
      makeMessage({ id: 'msg-2', role: 'user', content: 'Second' }),
      makeMessage({ id: 'msg-3', role: 'assistant', agent: 'hermes', content: 'Third' }),
    ]
    const wrapper = mountComponent({ messages })
    expect(wrapper.html()).toContain('atlas')
    expect(wrapper.html()).toContain('hermes')
  })

  it('shows delegation divider when agent changes between messages', () => {
    const messages = [
      makeMessage({ id: 'msg-1', role: 'assistant', agent: 'atlas', seq: 1 }),
      makeMessage({ id: 'msg-2', role: 'assistant', agent: 'hermes', seq: 2 }),
    ]
    const wrapper = mountComponent({ messages })
    expect(wrapper.html()).toContain('handed off to hermes')
  })

  it('does not show delegation divider when agent stays the same', () => {
    const messages = [
      makeMessage({ id: 'msg-1', role: 'assistant', agent: 'atlas', seq: 1 }),
      makeMessage({ id: 'msg-2', role: 'assistant', agent: 'atlas', seq: 2 }),
    ]
    const wrapper = mountComponent({ messages })
    expect(wrapper.html()).not.toContain('handed off to')
  })

  it('formats timestamp for assistant messages', () => {
    const past = new Date(Date.now() - 90 * 1000).toISOString() // 90 seconds ago
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'assistant', agent: 'atlas', created_at: past })],
    })
    // Should show "1m ago"
    expect(wrapper.html()).toContain('m ago')
  })

  it('shows tool call count in group summary', () => {
    const toolContent = JSON.stringify({ name: 'bash_execute' })
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'tool_call', content: toolContent })],
    })
    // The group summary shows the count of tool calls
    expect(wrapper.html()).toContain('1 tool call')
  })

  it('shows multiple tool calls count in group summary', () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({ id: 'm1', role: 'tool_call', content: JSON.stringify({ name: 'bash' }) }),
        makeMessage({ id: 'm2', role: 'tool_call', content: JSON.stringify({ name: 'read_file' }) }),
      ],
    })
    expect(wrapper.html()).toContain('2 tool calls')
  })
})

describe('ThreadDetail — artifact card', () => {
  it('renders ArtifactCard when artifact prop is provided', () => {
    const artifact = makeArtifact({ title: 'Important Fix' })
    const wrapper = mountComponent({
      messages: [makeMessage()],
      artifact,
    })
    expect(wrapper.find('[data-testid="artifact-card"]').exists()).toBe(true)
  })

  it('does not render ArtifactCard when artifact is null', () => {
    const wrapper = mountComponent({
      messages: [makeMessage()],
      artifact: null,
    })
    expect(wrapper.find('[data-testid="artifact-card"]').exists()).toBe(false)
  })

  it('does not render ArtifactCard when artifact is undefined', () => {
    const wrapper = mountComponent({ messages: [makeMessage()] })
    expect(wrapper.find('[data-testid="artifact-card"]').exists()).toBe(false)
  })

  it('emits accept-artifact when ArtifactCard emits accept', async () => {
    const artifact = makeArtifact({ id: 'art-123' })
    const wrapper = mount(ThreadDetail, {
      props: {
        visible: true,
        messages: [makeMessage()],
        loading: false,
        error: null,
        artifact,
      },
    })
    // Trigger accept from ArtifactCard stub via the parent emit binding
    const artifactCard = wrapper.findComponent({ name: 'ArtifactCard' })
    await artifactCard.vm.$emit('accept', 'art-123')
    expect(wrapper.emitted('accept-artifact')).toBeTruthy()
  })

  it('emits reject-artifact when ArtifactCard emits reject', async () => {
    const artifact = makeArtifact({ id: 'art-456' })
    const wrapper = mount(ThreadDetail, {
      props: {
        visible: true,
        messages: [makeMessage()],
        loading: false,
        error: null,
        artifact,
      },
    })
    const artifactCard = wrapper.findComponent({ name: 'ArtifactCard' })
    await artifactCard.vm.$emit('reject', 'art-456')
    expect(wrapper.emitted('reject-artifact')).toBeTruthy()
  })
})

describe('ThreadDetail — observation deck', () => {
  it('renders ObservationDeck when there are messages', () => {
    const wrapper = mountComponent({ messages: [makeMessage()] })
    expect(wrapper.find('[data-testid="observation-deck"]').exists()).toBe(true)
  })

  it('does not render ObservationDeck when messages is empty', () => {
    const wrapper = mountComponent({ messages: [] })
    expect(wrapper.find('[data-testid="observation-deck"]').exists()).toBe(false)
  })
})

describe('ThreadDetail — agent avatar', () => {
  it('shows the first letter of agent name as avatar', () => {
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'assistant', agent: 'atlas', content: 'hi' })],
    })
    expect(wrapper.html()).toContain('A')
  })

  it('falls back to A when agent name is missing', () => {
    const wrapper = mountComponent({
      messages: [makeMessage({ role: 'assistant', agent: '', content: 'hi' })],
    })
    expect(wrapper.html()).toContain('A')
  })
})

// ── parseConsultResult / consultation card rendering ─────────────────────────

describe('ThreadDetail — consult_agent card', () => {
  // Helper: make a consult_agent tool call + result pair
  function makeConsultMessages(agentName: string, answer: string) {
    const consultResult = `[${agentName}'s response]\n${answer}`
    return [
      makeMessage({
        id: 'tc-1',
        role: 'tool_call',
        content: JSON.stringify({ name: 'consult_agent' }),
        tool_name: 'consult_agent',
      }),
      makeMessage({
        id: 'tr-1',
        role: 'tool_result',
        content: consultResult,
        tool_name: 'consult_agent',
      }),
    ]
  }

  // Helper: click the tool group expand button (not the header buttons)
  async function expandToolGroup(wrapper: ReturnType<typeof mountComponent>) {
    const buttons = wrapper.findAll('button')
    // The tool group button contains "tool call" text — find it among all buttons
    for (const btn of buttons) {
      if (btn.text().includes('tool call')) {
        await btn.trigger('click')
        return
      }
    }
  }

  it('renders the consulted agent name when consult_agent result matches format', async () => {
    const wrapper = mountComponent({
      messages: makeConsultMessages('Sam', 'I analyzed the logs and found 3 errors.'),
    })
    await expandToolGroup(wrapper)
    expect(wrapper.html()).toContain('Sam')
  })

  it('renders the answer content from a consult_agent result', async () => {
    const wrapper = mountComponent({
      messages: makeConsultMessages('Oracle', 'The answer is 42.'),
    })
    await expandToolGroup(wrapper)
    expect(wrapper.html()).toContain('42')
  })

  it('renders the consulted label when consult_agent result matches format', async () => {
    const wrapper = mountComponent({
      messages: makeConsultMessages('Hermes', 'I have an update.'),
    })
    await expandToolGroup(wrapper)
    expect(wrapper.html()).toContain('consulted')
  })

  it('falls back to generic pre block for non-consultation tool results', async () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({
          id: 'tc-2',
          role: 'tool_call',
          content: JSON.stringify({ name: 'bash' }),
          tool_name: 'bash',
        }),
        makeMessage({
          id: 'tr-2',
          role: 'tool_result',
          content: 'exit 0',
          tool_name: 'bash',
        }),
      ],
    })
    await expandToolGroup(wrapper)
    expect(wrapper.html()).toContain('exit 0')
  })

  it('does not render consultation card for non-matching content format', async () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({
          id: 'tc-3',
          role: 'tool_call',
          content: JSON.stringify({ name: 'consult_agent' }),
          tool_name: 'consult_agent',
        }),
        makeMessage({
          id: 'tr-3',
          role: 'tool_result',
          content: 'plain text without the expected bracket prefix',
          tool_name: 'consult_agent',
        }),
      ],
    })
    await expandToolGroup(wrapper)
    // Should fall back to generic pre block containing the raw content
    expect(wrapper.html()).toContain('plain text')
    // Should NOT show the "consulted" label
    expect(wrapper.html()).not.toContain('consulted')
  })
})

// ── Tool call chip (persisted toolCalls) ──────────────────────────────────────

describe('ThreadDetail — tool call chip (persisted toolCalls)', () => {
  function makeToolCallRecord(overrides: Partial<import('../../composables/useSessions').ToolCallRecord> = {}) {
    return {
      id: 'tc-1',
      name: 'bash',
      args: { cmd: 'echo hi' } as Record<string, unknown>,
      result: 'hi',
      done: true,
      ...overrides,
    }
  }

  it('renders chip on assistant message when toolCalls is present', () => {
    const msg = makeMessage({
      role: 'assistant',
      agent: 'Atlas',
      content: 'I ran some tools',
      toolCalls: [makeToolCallRecord()],
    })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).toContain('tool call')
    expect(wrapper.html()).toContain('done')
  })

  it('shows correct count for multiple tool calls', () => {
    const msg = makeMessage({
      role: 'assistant',
      toolCalls: [
        makeToolCallRecord({ id: 'tc-1', name: 'bash' }),
        makeToolCallRecord({ id: 'tc-2', name: 'read_file' }),
      ],
    })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).toContain('2 tool calls')
  })

  it('does not render chip when toolCalls is undefined', () => {
    const msg = makeMessage({ role: 'assistant', content: 'No tools' })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).not.toContain('· done')
  })

  it('does not render chip when toolCalls is empty array', () => {
    const msg = makeMessage({ role: 'assistant', toolCalls: [] })
    const wrapper = mountComponent({ messages: [msg] })
    expect(wrapper.html()).not.toContain('· done')
  })

  it('expands tool call details when chip is clicked', async () => {
    const msg = makeMessage({
      id: 'msg-chip-1',
      role: 'assistant',
      toolCalls: [makeToolCallRecord({ name: 'bash', args: { cmd: 'ls' }, result: 'file.txt' })],
    })
    const wrapper = mountComponent({ messages: [msg] })

    // Find the chip button — contains "done"
    const buttons = wrapper.findAll('button')
    const chipBtn = buttons.find(b => b.text().includes('done'))
    expect(chipBtn).toBeDefined()
    await chipBtn!.trigger('click')

    // After expanding, tool name should be visible
    expect(wrapper.html()).toContain('bash')
  })

  it('legacy tool_call role rows still render as toolgroup (backward compat)', () => {
    const wrapper = mountComponent({
      messages: [
        makeMessage({ id: 'm1', role: 'tool_call', content: JSON.stringify({ name: 'read_file' }) }),
        makeMessage({ id: 'm2', role: 'tool_result', content: 'file contents' }),
      ],
    })
    expect(wrapper.html()).toContain('tool call')
    // Old toolgroup uses wrench + count but NOT "· done"
    expect(wrapper.html()).not.toContain('· done')
  })
})
