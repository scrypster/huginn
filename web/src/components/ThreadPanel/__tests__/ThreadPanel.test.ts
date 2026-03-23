import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ThreadPanel from '../ThreadPanel.vue'
import type { LiveThread } from '../../../composables/useThreads'

// Stub ThreadCard to avoid its internal complexity while still rendering its content
vi.mock('../ThreadCard.vue', () => ({
  default: {
    name: 'ThreadCard',
    template: `
      <div data-testid="thread-card">
        <span class="thread-agent-id">{{ thread.AgentID }}</span>
        <span class="thread-task">{{ thread.Task }}</span>
        <span class="thread-status">{{ thread.Status }}</span>
      </div>
    `,
    props: ['thread', 'agentColor', 'agentIcon'],
    emits: ['cancel', 'inject'],
  },
}))

function makeThread(overrides: Partial<LiveThread> = {}): LiveThread {
  return {
    ID: 'thread-1',
    SessionID: 'sess-1',
    AgentID: 'atlas',
    Task: 'Analyze codebase',
    Status: 'thinking',
    StartedAt: new Date().toISOString(),
    CompletedAt: '',
    TokensUsed: 100,
    TokenBudget: 5000,
    streamingContent: '',
    toolCalls: [],
    elapsedMs: 5000,
    ...overrides,
  }
}

function mountPanel(props: Partial<InstanceType<typeof ThreadPanel>['$props']> = {}) {
  return mount(ThreadPanel, {
    props: {
      threads: [],
      agentColors: {},
      agentIcons: {},
      visible: true,
      ...props,
    },
  })
}

describe('ThreadPanel — empty state', () => {
  it('shows empty state when no threads provided', () => {
    const wrapper = mountPanel({ threads: [] })
    expect(wrapper.html()).toContain('No active threads')
  })

  it('does not render thread cards when threads array is empty', () => {
    const wrapper = mountPanel({ threads: [] })
    expect(wrapper.findAll('[data-testid="thread-card"]').length).toBe(0)
  })

  it('does not show footer token summary when no threads', () => {
    const wrapper = mountPanel({ threads: [] })
    expect(wrapper.html()).not.toContain('total tokens')
  })
})

describe('ThreadPanel — thread list rendering', () => {
  it('renders a thread card for each thread', () => {
    const threads = [
      makeThread({ ID: 'thread-1', AgentID: 'atlas' }),
      makeThread({ ID: 'thread-2', AgentID: 'hermes' }),
    ]
    const wrapper = mountPanel({ threads })
    const cards = wrapper.findAll('[data-testid="thread-card"]')
    expect(cards.length).toBe(2)
  })

  it('renders the agent name for each thread', () => {
    const threads = [
      makeThread({ ID: 'thread-1', AgentID: 'atlas' }),
      makeThread({ ID: 'thread-2', AgentID: 'hermes' }),
    ]
    const wrapper = mountPanel({ threads })
    expect(wrapper.html()).toContain('atlas')
    expect(wrapper.html()).toContain('hermes')
  })

  it('renders the thread task text', () => {
    const threads = [makeThread({ Task: 'Build a feature' })]
    const wrapper = mountPanel({ threads })
    expect(wrapper.html()).toContain('Build a feature')
  })

  it('renders the thread status', () => {
    const threads = [makeThread({ Status: 'done' })]
    const wrapper = mountPanel({ threads })
    expect(wrapper.html()).toContain('done')
  })

  it('does not show empty state when threads are present', () => {
    const threads = [makeThread()]
    const wrapper = mountPanel({ threads })
    expect(wrapper.html()).not.toContain('No active threads')
  })
})

describe('ThreadPanel — header', () => {
  it('renders the "Threads" panel header', () => {
    const wrapper = mountPanel()
    expect(wrapper.html()).toContain('Threads')
  })

  it('shows active count badge when there are running threads', () => {
    const threads = [
      makeThread({ ID: 'thread-1', Status: 'thinking' }),
      makeThread({ ID: 'thread-2', Status: 'tooling' }),
    ]
    const wrapper = mountPanel({ threads })
    // activeCount = 2 threads with non-terminal status
    expect(wrapper.html()).toContain('2')
  })

  it('does not show active count badge when all threads are terminal', () => {
    const threads = [
      makeThread({ ID: 'thread-1', Status: 'done' }),
      makeThread({ ID: 'thread-2', Status: 'cancelled' }),
    ]
    const wrapper = mountPanel({ threads })
    // No active count badge for terminal-only threads
    // The badge is rendered by v-if="activeCount > 0"
    const badgeEl = wrapper.find('.tabular-nums')
    expect(badgeEl.exists()).toBe(false)
  })

  it('emits collapse event when collapse button is clicked', async () => {
    const wrapper = mountPanel()
    const collapseBtn = wrapper.find('button[title="Collapse thread panel"]')
    await collapseBtn.trigger('click')
    expect(wrapper.emitted('collapse')).toBeTruthy()
  })
})

describe('ThreadPanel — visibility / panel style', () => {
  it('has 360px width when visible=true', () => {
    const wrapper = mountPanel({ visible: true })
    const panel = wrapper.find('div')
    const style = panel.attributes('style') ?? ''
    expect(style).toContain('360px')
  })

  it('has 0px width when visible=false', () => {
    const wrapper = mountPanel({ visible: false })
    const panel = wrapper.find('div')
    const style = panel.attributes('style') ?? ''
    expect(style).toContain('0px')
  })
})

describe('ThreadPanel — footer token summary', () => {
  it('shows total token count when threads are present', () => {
    const threads = [
      makeThread({ TokensUsed: 1500 }),
      makeThread({ ID: 'thread-2', TokensUsed: 2500 }),
    ]
    const wrapper = mountPanel({ threads })
    // 1500 + 2500 = 4000 total tokens
    expect(wrapper.html()).toContain('4,000')
    expect(wrapper.html()).toContain('total tokens')
  })

  it('shows done count when some threads are done', () => {
    const threads = [
      makeThread({ ID: 'thread-1', Status: 'done' }),
      makeThread({ ID: 'thread-2', Status: 'thinking' }),
    ]
    const wrapper = mountPanel({ threads })
    // doneCount=1, threads.length=2 → "1/2 done"
    expect(wrapper.html()).toContain('1/2 done')
  })

  it('does not show done count when no threads are done', () => {
    const threads = [
      makeThread({ ID: 'thread-1', Status: 'thinking' }),
    ]
    const wrapper = mountPanel({ threads })
    expect(wrapper.html()).not.toContain('done')
  })
})

describe('ThreadPanel — sorting', () => {
  it('renders running threads before terminal threads', () => {
    const threads = [
      makeThread({ ID: 'done-thread', Status: 'done', AgentID: 'terminal-agent' }),
      makeThread({ ID: 'running-thread', Status: 'thinking', AgentID: 'running-agent' }),
    ]
    const wrapper = mountPanel({ threads })
    const html = wrapper.html()
    const runningIdx = html.indexOf('running-agent')
    const terminalIdx = html.indexOf('terminal-agent')
    // running agent should appear before the terminal one
    expect(runningIdx).toBeLessThan(terminalIdx)
  })
})

describe('ThreadPanel — cancel / inject events', () => {
  it('forwards cancel event from ThreadCard', async () => {
    const threads = [makeThread({ ID: 'thread-abc' })]
    const wrapper = mountPanel({ threads })
    // Trigger cancel from the ThreadCard stub
    const card = wrapper.findComponent({ name: 'ThreadCard' })
    await card.vm.$emit('cancel', 'thread-abc')
    expect(wrapper.emitted('cancel')).toBeTruthy()
    expect(wrapper.emitted('cancel')![0]).toEqual(['thread-abc'])
  })

  it('forwards inject event from ThreadCard', async () => {
    const threads = [makeThread({ ID: 'thread-xyz' })]
    const wrapper = mountPanel({ threads })
    const card = wrapper.findComponent({ name: 'ThreadCard' })
    await card.vm.$emit('inject', 'thread-xyz', 'some input')
    expect(wrapper.emitted('inject')).toBeTruthy()
    expect(wrapper.emitted('inject')![0]).toEqual(['thread-xyz', 'some input'])
  })
})
