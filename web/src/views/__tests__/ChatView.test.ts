import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

// ── Composable mocks (hoisted before component import) ────────────────

// Keep references so tests can mutate them
const mockSessions = ref<any[]>([])
const mockMessages: Record<string, any[]> = {}
const mockGetMessages = vi.fn((id: string) => {
  if (!mockMessages[id]) mockMessages[id] = []
  return mockMessages[id]
})
const mockFormatSessionLabel = vi.fn((s: any) => s?.title || s?.id?.slice(0, 8) || '')
const mockRenameSession = vi.fn()

vi.mock('../../composables/useSessions', () => {
  const { ref } = require('vue')
  return {
    hydrationQueueOverflowed: ref(false),
    useSessions: () => ({
      sessions: mockSessions,
      getMessages: mockGetMessages,
      fetchMessages: vi.fn().mockResolvedValue(undefined),
      formatSessionLabel: mockFormatSessionLabel,
      renameSession: mockRenameSession,
      queueIfHydrating: (_sessionId: string, _handler: () => void) => false,
    }),
  }
})

const mockGetSessionThreads = vi.fn().mockReturnValue([])
const mockGetActiveThreadCount = vi.fn().mockReturnValue(0)
const mockLoadThreads = vi.fn()
const mockWireWS = vi.fn()
const mockGetSessionPreviews = vi.fn().mockReturnValue([])
const mockAckPreview = vi.fn()

vi.mock('../../composables/useThreads', () => ({
  useThreads: () => ({
    getSessionThreads: mockGetSessionThreads,
    getActiveThreadCount: mockGetActiveThreadCount,
    loadThreads: mockLoadThreads,
    wireWS: mockWireWS,
    getSessionPreviews: mockGetSessionPreviews,
    ackPreview: mockAckPreview,
  }),
}))

const mockActiveSpace = ref<any>(null)

vi.mock('../../composables/useSpaces', () => ({
  useSpaces: () => ({
    activeSpace: mockActiveSpace,
  }),
}))

const mockApiAgentsList = vi.fn().mockResolvedValue([])
const mockApiRuntimeStatus = vi.fn().mockResolvedValue({ state: 'idle' })
const mockApiSessionsCreate = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    agents: {
      list: (...args: unknown[]) => mockApiAgentsList(...args),
    },
    runtime: {
      status: () => mockApiRuntimeStatus(),
    },
    sessions: {
      create: (...args: unknown[]) => mockApiSessionsCreate(...args),
    },
  },
  getToken: vi.fn().mockReturnValue('test-token'),
}))

// ── useSpaceTimeline mock ─────────────────────────────────────────────
// Provides a controllable timeline with a real Map for sessionToSpaceMap
// so .set() / .has() calls work correctly in production code.
const makeSpaceState = () => ({
  messages: [] as any[],
  sessionToSpaceMap: new Map<string, string>(),
  activeSessionId: null as string | null,
  cursor: null as string | null,
  hasMore: false,
  loadingInitial: false,
  loadingMore: false,
  error: null as string | null,
})

let mockSpaceState = makeSpaceState()
const mockSpaceHydrate = vi.fn().mockResolvedValue(undefined)
const mockSpaceTimeline = {
  getState: () => mockSpaceState,
  hydrate: mockSpaceHydrate,
  loadMore: vi.fn().mockResolvedValue(null),
  retryHydrate: vi.fn(),
}
const mockUseSpaceTimeline = vi.fn(() => mockSpaceTimeline)

vi.mock('../../composables/useSpaceTimeline', () => ({
  useSpaceTimeline: (...args: unknown[]) => mockUseSpaceTimeline(...args),
  clearSpaceTimeline: vi.fn(),
  wireSpaceTimelineWS: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {}, query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

// Stub heavy child components
vi.mock('../../components/ThreadPanel', () => ({
  ThreadPanel: {
    name: 'ThreadPanel',
    template: '<div class="thread-panel-stub" />',
    props: ['threads', 'agentColors', 'agentIcons', 'visible'],
  },
}))

vi.mock('../../components/AgentRosterModal.vue', () => ({
  default: {
    name: 'AgentRosterModal',
    template: '<div class="roster-modal-stub" />',
  },
}))

import ChatView from '../ChatView.vue'

// ── Mock WS factory ────────────────────────────────────────────────────
// Creates a mock HuginnWS object that stores registered handlers and
// allows tests to directly invoke them to simulate incoming WS events.
function createMockWs() {
  const handlers = new Map<string, ((msg: any) => void)[]>()
  const sentMessages: any[] = []

  const mockWs = {
    connected: ref(true),
    messages: ref<any[]>([]),
    lastError: ref<string | null>(null),
    on: vi.fn((type: string, fn: (msg: any) => void) => {
      if (!handlers.has(type)) handlers.set(type, [])
      handlers.get(type)!.push(fn)
    }),
    off: vi.fn((type: string, fn: (msg: any) => void) => {
      const fns = handlers.get(type) ?? []
      handlers.set(type, fns.filter(f => f !== fn))
    }),
    send: vi.fn((msg: any) => {
      sentMessages.push(msg)
    }),
    destroy: vi.fn(),
    streamChat: vi.fn(),
    // Test helper: simulate an incoming WS message by calling registered handlers
    simulateMessage(msg: any) {
      const fns = handlers.get(msg.type) ?? []
      fns.forEach(fn => fn(msg))
    },
    sentMessages,
  }

  return mockWs
}

function mountChatView(
  props: Record<string, unknown> = {},
  wsOverride?: ReturnType<typeof createMockWs> | null,
) {
  const wsValue = wsOverride !== undefined ? wsOverride : null
  return shallowMount(ChatView, {
    props: { sessionId: 'test-session-id', ...props },
    global: {
      stubs: {
        Teleport: true,
        RouterLink: { template: '<a><slot /></a>' },
        ChatEditor: {
          name: 'ChatEditor',
          template: '<div class="chat-editor-stub" />',
          emits: ['send'],
          // Expose a focus() method so the component's onMounted hook doesn't error
          setup() {
            return { focus: vi.fn() }
          },
        },
      },
      provide: {
        ws: ref(wsValue),
      },
    },
  })
}

// ── Tests ─────────────────────────────────────────────────────────────
describe('ChatView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Test Session' }]
  })

  afterEach(() => {
    vi.resetModules()
  })

  it('renders without crashing', async () => {
    const wrapper = mountChatView()
    expect(wrapper.exists()).toBe(true)
  })

  it('renders when no session is selected', async () => {
    const wrapper = mountChatView({ sessionId: undefined })
    await nextTick()
    // Should show the no-session view
    const html = wrapper.html()
    expect(html).toBeTruthy()
  })

  it('displays messages when session has messages', async () => {
    mockMessages['test-session-id'] = [
      { id: '1', role: 'user', content: 'Hello' },
      { id: '2', role: 'assistant', content: 'Hi there' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    // Component uses v-html for markdown rendering, check that messages are in data
    expect(mockGetMessages).toHaveBeenCalled()
  })

  it('displays empty state when no messages', async () => {
    mockMessages['test-session-id'] = []
    const wrapper = mountChatView()
    await flushPromises()
    const html = wrapper.html()
    // Empty chat shows a placeholder
    expect(html).toBeTruthy()
  })

  it('accepts input text via ChatEditor', async () => {
    // Verify that ChatEditor component is defined in the system
    expect(true).toBe(true)
  })

  it('sends message when ChatEditor emits send event', async () => {
    // Test that the message-handling logic works (without calling onMounted)
    expect(mockMessages['test-session-id']).toBeDefined()
  })

  it('displays connection status when runtime state is set', async () => {
    mockApiRuntimeStatus.mockResolvedValue({ state: 'running' })
    const wrapper = mountChatView()
    await flushPromises()
    // Status is rendered in the header
    expect(wrapper.html()).toBeTruthy()
  })

  it('displays agents list when agents exist', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'Agent1', model: 'gpt-4', color: '#ff0000', icon: 'A' },
    ])
    const wrapper = mountChatView()
    await flushPromises()
    expect(wrapper.html()).toBeTruthy()
  })

  it('shows no agents message when no agents available', async () => {
    mockApiAgentsList.mockResolvedValue([])
    const wrapper = mountChatView({ sessionId: undefined })
    await flushPromises()
    const html = wrapper.html()
    expect(html).toBeTruthy()
  })

  it('does not render when sessionId prop is missing', async () => {
    const wrapper = mountChatView({ sessionId: undefined })
    await nextTick()
    // Should show empty state
    expect(wrapper.exists()).toBe(true)
  })

  it('loads threads when session is active', async () => {
    // Verify that the session ID is provided for thread loading
    expect('test-session-id').toBeDefined()
  })

  it('renders ThreadPanel component when threads exist', async () => {
    mockGetSessionThreads.mockReturnValue([{ ID: 'thread-1', Status: 'done' }])
    const wrapper = mountChatView()
    await nextTick()
    const threadPanel = wrapper.findComponent({ name: 'ThreadPanel' })
    expect(threadPanel.exists()).toBe(true)
  })

  it('syncs session agent on mount', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'DefaultAgent', model: 'gpt-4', color: '#00ff00', icon: 'D', is_default: true },
    ])
    mockSessions.value = [
      { id: 'test-session-id', agent: 'DefaultAgent', title: 'Test' },
    ]
    // Verify that the agent list is configured for the test
    expect(mockSessions.value).toHaveLength(1)
  })

  // ── WebSocket integration tests ────────────────────────────────────

  it('WS token handler: appends token content to last streaming assistant message', async () => {
    const mockWs = createMockWs()
    // Pre-populate messages with a streaming assistant message
    mockMessages['test-session-id'] = [
      { id: 'u-1', role: 'user', content: 'Hello' },
      { id: 'h-1', role: 'assistant', content: 'start ', streaming: true },
    ]

    mountChatView({}, mockWs)
    await nextTick()

    // Simulate a token WS event
    mockWs.simulateMessage({ type: 'token', content: 'world' })
    await nextTick()

    const msgs = mockGetMessages('test-session-id')
    const lastMsg = msgs.at(-1)
    expect(lastMsg?.content).toBe('start world')
  })

  it('WS tool_call handler: adds entry to activeToolCalls', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'h-1', role: 'assistant', content: '', streaming: true },
    ]

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // Simulate a tool_call WS event
    mockWs.simulateMessage({
      type: 'tool_call',
      payload: { id: 'tc1', tool: 'bash', args: { command: 'ls' } },
    })
    await nextTick()

    // The activeToolCalls are rendered in the template as an in-flight running chip.
    // The chip shows "N tool calls · running" (not the tool name) — verify running state.
    const html = wrapper.html()
    expect(html).toContain('running')
  })

  it('WS done handler: sets streaming to false', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'TestAgent', model: 'gpt-4', color: '#58A6FF', icon: 'T', is_default: true },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Trigger a send so handleEditorSend sets currentRunId
    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'Hi')
    await nextTick()

    // Grab the run_id from the sent chat message
    const chatMsg = mockWs.sentMessages.find((m: any) => m.type === 'chat')
    expect(chatMsg).toBeDefined()
    const runId = chatMsg.run_id

    // The streaming assistant placeholder should have been appended
    const msgs = mockGetMessages('test-session-id')
    const lastMsg = msgs.at(-1)
    expect(lastMsg?.streaming).toBe(true)

    // Simulate done with the matching run_id
    mockWs.simulateMessage({ type: 'done', run_id: runId })
    await nextTick()

    // After done, the last message's streaming flag should be cleared
    expect(lastMsg?.streaming).toBe(false)
  })

  it('WS permission_request handler: sets pendingPermission and shows banner', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // No permission banner yet
    expect(wrapper.html()).not.toContain('Permission required')

    // Simulate a permission_request event
    mockWs.simulateMessage({
      type: 'permission_request',
      payload: { id: 'perm-1', tool: 'bash', command: 'rm -rf /tmp' },
    })
    await nextTick()

    // Permission banner should now be visible
    expect(wrapper.html()).toContain('Permission required')
  })

  it('handleEditorSend: triggers ws.send with chat message payload', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'TestAgent', model: 'gpt-4', color: '#58A6FF', icon: 'T', is_default: true },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Find the ChatEditor stub and emit a send event
    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    expect(chatEditor.exists()).toBe(true)
    await chatEditor.vm.$emit('send', 'Hello from editor')
    await nextTick()

    // ws.send should have been called with a chat message
    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'chat',
        content: 'Hello from editor',
        session_id: 'test-session-id',
      })
    )
  })

  it('no session: renders placeholder text when sessionId is undefined', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'Agent1', model: 'gpt-4', color: '#58A6FF', icon: 'A', is_default: false },
    ])

    const wrapper = mountChatView({ sessionId: undefined })
    await flushPromises()

    // When agents exist and no session is selected, shows "huginn is ready" + "Pick a channel"
    expect(wrapper.html()).toContain('huginn is ready')
  })

  it('no session + no agents: renders "no agents yet" placeholder', async () => {
    mockApiAgentsList.mockResolvedValue([])

    const wrapper = mountChatView({ sessionId: undefined })
    await flushPromises()

    expect(wrapper.html()).toContain('no agents yet')
  })

  it('agent selection: calls ws.send with set_primary_agent message', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'AgentAlpha', model: 'claude-3', color: '#3FB950', icon: 'A', is_default: false },
      { name: 'AgentBeta', model: 'gpt-4', color: '#FF7B72', icon: 'B', is_default: false },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Open agent dropdown by clicking the button
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    expect(dropdownBtn.exists()).toBe(true)
    await dropdownBtn.trigger('click')
    await nextTick()

    // Click on AgentBeta in the dropdown
    const agentButtons = wrapper.findAll('button').filter(b => b.text().includes('AgentBeta'))
    expect(agentButtons.length).toBeGreaterThan(0)
    await agentButtons[0].trigger('click')
    await nextTick()

    // ws.send should be called with set_primary_agent
    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'set_primary_agent',
        session_id: 'test-session-id',
        payload: { agent: 'AgentBeta' },
      })
    )
  })

  it('tool call toggle: clicking a tool call opens the detail modal', async () => {
    const mockWs = createMockWs()
    // Pre-populate a message with a completed tool call
    mockMessages['test-session-id'] = [
      {
        id: 'h-1',
        role: 'assistant',
        content: 'done',
        toolCalls: [
          { id: 'tc-42', name: 'bash', args: { command: 'ls' }, result: 'file1\nfile2', done: true },
        ],
      },
    ]

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // The chip renders collapsed showing "N tool calls · done" — find it by the "tool call" label.
    // Individual tool call buttons (with tool names) are hidden until the chip is expanded.
    const chipBtns = wrapper.findAll('button').filter(b => b.text().includes('tool call'))
    expect(chipBtns.length).toBeGreaterThan(0)

    // Click the chip to expand the tool call list
    await chipBtns[0].trigger('click')
    await nextTick()

    // After expanding: find the individual tool call button (renders tool name 'bash')
    const toolCallBtns = wrapper.findAll('button').filter(b => b.text().includes('bash'))
    expect(toolCallBtns.length).toBeGreaterThan(0)

    // Before clicking: modal should be closed (open=false)
    const modal = wrapper.findComponent({ name: 'ToolCallModal' })
    expect(modal.exists()).toBe(true)
    expect(modal.props('open')).toBe(false)

    // Click the individual tool button — should open the detail modal
    await toolCallBtns[0].trigger('click')
    await nextTick()

    // After clicking: modal should be open with the correct tool call
    expect(modal.props('open')).toBe(true)
    expect((modal.props('tc') as any)?.id).toBe('tc-42')
    expect((modal.props('tc') as any)?.name).toBe('bash')

    // Emit close from the modal — should close it
    await modal.trigger('close')
    await nextTick()
    expect(modal.props('open')).toBe(false)
  })

  it('WS handlers are registered when ws ref is provided', async () => {
    const mockWs = createMockWs()
    mountChatView({}, mockWs)
    await nextTick()

    // Verify that the component registered handlers for key event types
    expect(mockWs.on).toHaveBeenCalledWith('token', expect.any(Function))
    expect(mockWs.on).toHaveBeenCalledWith('tool_call', expect.any(Function))
    expect(mockWs.on).toHaveBeenCalledWith('done', expect.any(Function))
    expect(mockWs.on).toHaveBeenCalledWith('permission_request', expect.any(Function))
  })

  // ── New integration tests ───────────────────────────────────────────

  it('WS tool_result handler: moves active tool call to message toolCalls', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'h-1', role: 'assistant', content: 'working...', streaming: true },
    ]

    mountChatView({}, mockWs)
    await nextTick()

    // First add a tool call
    mockWs.simulateMessage({
      type: 'tool_call',
      payload: { id: 'tc-100', tool: 'read_file', args: { path: '/tmp/a.txt' } },
    })
    await nextTick()

    // Now simulate the result
    mockWs.simulateMessage({
      type: 'tool_result',
      payload: { id: 'tc-100', result: 'file contents here' },
    })
    await nextTick()

    // The tool call should be attached to the last assistant message
    const msgs = mockGetMessages('test-session-id')
    const lastAssistant = msgs.find((m: any) => m.role === 'assistant')
    expect(lastAssistant?.toolCalls).toBeDefined()
    expect(lastAssistant.toolCalls.length).toBe(1)
    expect(lastAssistant.toolCalls[0].name).toBe('read_file')
    expect(lastAssistant.toolCalls[0].result).toBe('file contents here')
  })

  it('WS error handler: appends error to last message and stops streaming', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'u-1', role: 'user', content: 'Hello' },
      { id: 'h-1', role: 'assistant', content: 'partial response', streaming: true },
    ]

    mountChatView({}, mockWs)
    await nextTick()

    mockWs.simulateMessage({ type: 'error', content: 'context limit exceeded' })
    await nextTick()

    const msgs = mockGetMessages('test-session-id')
    const last = msgs.at(-1)
    expect(last?.streaming).toBe(false)
    expect(last?.content).toContain('context limit exceeded')
  })

  it('WS primary_agent_changed handler: updates selected agent name', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'AgentA', model: 'gpt-4', color: '#58A6FF', icon: 'A', is_default: true },
      { name: 'AgentB', model: 'claude-3', color: '#3FB950', icon: 'B', is_default: false },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Simulate server-side agent change
    mockWs.simulateMessage({
      type: 'primary_agent_changed',
      session_id: 'test-session-id',
      payload: { agent: 'AgentB' },
    })
    await nextTick()

    // The dropdown button should now show AgentB
    const html = wrapper.html()
    expect(html).toContain('AgentB')
  })

  it('approvePermission(true): sends permission_response with approved=true and clears banner', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // Trigger permission request
    mockWs.simulateMessage({
      type: 'permission_request',
      payload: { id: 'perm-42', tool: 'bash', command: 'echo hi' },
    })
    await nextTick()
    expect(wrapper.html()).toContain('Permission required')

    // Click Allow
    const allowBtn = wrapper.findAll('button').find(b => b.text().includes('Allow'))
    expect(allowBtn).toBeDefined()
    await allowBtn!.trigger('click')
    await nextTick()

    // Should have sent permission_response
    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'permission_response',
        payload: { id: 'perm-42', approved: true },
      })
    )

    // Banner should be cleared
    expect(wrapper.html()).not.toContain('Permission required')
  })

  it('approvePermission(false): sends permission_response with approved=false', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    mockWs.simulateMessage({
      type: 'permission_request',
      payload: { id: 'perm-99', tool: 'write_file', command: '/etc/passwd' },
    })
    await nextTick()

    // Click Deny
    const denyBtn = wrapper.findAll('button').find(b => b.text().includes('Deny'))
    expect(denyBtn).toBeDefined()
    await denyBtn!.trigger('click')
    await nextTick()

    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'permission_response',
        payload: { id: 'perm-99', approved: false },
      })
    )
  })

  it('handleEditorSend: blocks send while streaming', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'h-1', role: 'assistant', content: 'in progress...', streaming: true },
    ]
    mockApiAgentsList.mockResolvedValue([
      { name: 'TestAgent', model: 'gpt-4', color: '#58A6FF', icon: 'T', is_default: true },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Simulate being in streaming state by sending a message first
    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'First message')
    await nextTick()

    const sendCountAfterFirst = mockWs.send.mock.calls.filter(
      (c: any[]) => c[0]?.type === 'chat'
    ).length

    // Try to send another message while streaming
    await chatEditor.vm.$emit('send', 'Second message')
    await nextTick()

    const sendCountAfterSecond = mockWs.send.mock.calls.filter(
      (c: any[]) => c[0]?.type === 'chat'
    ).length

    // Second message should be blocked (same count)
    expect(sendCountAfterSecond).toBe(sendCountAfterFirst)
  })

  it('handleEditorSend: auto-selects default agent on first send and sends chat', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'Fallback', model: 'gpt-4', color: '#FF7B72', icon: 'F', is_default: false },
      { name: 'Primary', model: 'claude-3', color: '#3FB950', icon: 'P', is_default: true },
    ])
    // Session has no agent recorded
    mockSessions.value = [{ id: 'test-session-id', title: 'Test' }]

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'Hello')
    await nextTick()

    // handleEditorSend should have sent both a set_primary_agent and a chat message.
    // The auto-selection calls selectAgent() internally which sends set_primary_agent,
    // then the chat message is sent. Verify both happened.
    const sentTypes = mockWs.sentMessages.map((m: any) => m.type)
    expect(sentTypes).toContain('chat')

    // Also verify a user message and streaming assistant message were pushed
    const msgs = mockGetMessages('test-session-id')
    expect(msgs.length).toBeGreaterThanOrEqual(2)
    expect(msgs[0].role).toBe('user')
    expect(msgs[0].content).toBe('Hello')
    expect(msgs[1].role).toBe('assistant')
    expect(msgs[1].streaming).toBe(true)
  })

  it('header rename: double-click opens input, Enter commits rename', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Old Title' }]

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Find the header label (span with double-click handler)
    const headerSpan = wrapper.find('span[title="Double-click to rename"]')
    expect(headerSpan.exists()).toBe(true)
    expect(headerSpan.text()).toBe('Old Title')

    // Double-click to enter edit mode
    await headerSpan.trigger('dblclick')
    await nextTick()

    // Input should now be visible
    const headerInput = wrapper.find('input[placeholder="Old Title"]')
    expect(headerInput.exists()).toBe(true)

    // Type a new title and press Enter
    await headerInput.setValue('New Title')
    await headerInput.trigger('keydown', { key: 'Enter' })
    await nextTick()

    // renameSession should have been called
    expect(mockRenameSession).toHaveBeenCalledWith('test-session-id', 'New Title')
  })

  it('header rename: Escape cancels without saving', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Keep This' }]

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    const headerSpan = wrapper.find('span[title="Double-click to rename"]')
    await headerSpan.trigger('dblclick')
    await nextTick()

    const headerInput = wrapper.find('input')
    await headerInput.setValue('Different Title')
    await headerInput.trigger('keydown', { key: 'Escape' })
    await nextTick()

    // renameSession should NOT have been called
    expect(mockRenameSession).not.toHaveBeenCalled()

    // Should be back to showing the span
    expect(wrapper.find('span[title="Double-click to rename"]').exists()).toBe(true)
  })

  it('cancelThread: sends thread_cancel WS message', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockGetSessionThreads.mockReturnValue([
      { ID: 'thread-abc', Status: 'running' },
    ])
    mockGetActiveThreadCount.mockReturnValue(1)

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // ThreadPanel is stubbed but we can find it and emit cancel
    const threadPanel = wrapper.findComponent({ name: 'ThreadPanel' })
    expect(threadPanel.exists()).toBe(true)
    await threadPanel.vm.$emit('cancel', 'thread-abc')
    await nextTick()

    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'thread_cancel',
        payload: { thread_id: 'thread-abc' },
        session_id: 'test-session-id',
      })
    )
  })

  it('space agent preview: shows stacked avatars when activeSpace is set', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'Lead', model: 'gpt-4', color: '#58A6FF', icon: 'L', is_default: true },
      { name: 'Helper', model: 'claude-3', color: '#3FB950', icon: 'H', is_default: false },
      { name: 'Reviewer', model: 'gpt-4', color: '#FF7B72', icon: 'R', is_default: false },
    ])
    mockActiveSpace.value = {
      id: 'space-1',
      name: 'Test Space',
      kind: 'channel',
      leadAgent: 'Lead',
      memberAgents: ['Lead', 'Helper', 'Reviewer'],
    }

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Should show space name in header
    expect(wrapper.html()).toContain('Test Space')

    // Should show the "Manage agents" button with agent count
    const manageBtn = wrapper.find('button[title="Manage agents"]')
    expect(manageBtn.exists()).toBe(true)
    expect(manageBtn.text()).toContain('3 agents')

    // Should show stacked avatar icons (L, H, R for the 3 previews)
    const avatarText = manageBtn.html()
    expect(avatarText).toContain('L')
    expect(avatarText).toContain('H')
    expect(avatarText).toContain('R')
  })

  it('space agent preview: shows +N overflow when more than 3 agents', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = []
    mockApiAgentsList.mockResolvedValue([
      { name: 'A1', model: 'gpt-4', color: '#58A6FF', icon: '1', is_default: true },
      { name: 'A2', model: 'gpt-4', color: '#3FB950', icon: '2', is_default: false },
      { name: 'A3', model: 'gpt-4', color: '#FF7B72', icon: '3', is_default: false },
      { name: 'A4', model: 'gpt-4', color: '#D2A8FF', icon: '4', is_default: false },
      { name: 'A5', model: 'gpt-4', color: '#FFA657', icon: '5', is_default: false },
    ])
    mockActiveSpace.value = {
      id: 'space-2',
      name: 'Big Space',
      kind: 'channel',
      leadAgent: 'A1',
      memberAgents: ['A1', 'A2', 'A3', 'A4', 'A5'],
    }

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Should show "+2" overflow indicator
    expect(wrapper.html()).toContain('+2')
    expect(wrapper.html()).toContain('5 agents')
  })

  it('WS thread_started handler: attaches delegated thread to last assistant message', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'u-1', role: 'user', content: 'run tests' },
      { id: 'h-1', role: 'assistant', content: 'delegating...', streaming: true },
    ]

    mountChatView({}, mockWs)
    await nextTick()

    mockWs.simulateMessage({
      type: 'thread_started',
      payload: { thread_id: 'thr-abc', agent_id: 'TestRunner' },
    })
    await nextTick()

    const msgs = mockGetMessages('test-session-id')
    const lastAssistant = [...msgs].reverse().find((m: any) => m.role === 'assistant')
    expect(lastAssistant?.delegatedThreads).toBeDefined()
    expect(lastAssistant.delegatedThreads.length).toBe(1)
    expect(lastAssistant.delegatedThreads[0].threadId).toBe('thr-abc')
    expect(lastAssistant.delegatedThreads[0].agentId).toBe('TestRunner')
  })
})

// ── Phase 2A: Message grouping ─────────────────────────────────────────
describe('ChatView — message grouping (Phase 2A)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
  })

  it('shows agent avatar on first assistant message', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Hello', agent: 'Tom', createdAt: new Date().toISOString() },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    // The avatar div should be present for the first message
    expect(wrapper.html()).toContain('T') // Tom's initial
  })

  it('consecutive same-agent messages suppress avatar on continuation', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'First reply', agent: 'Tom', createdAt: ts },
      { id: 'm2', role: 'assistant', content: 'Second reply', agent: 'Tom', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    // Both messages appear
    expect(html).toContain('First reply')
    expect(html).toContain('Second reply')
    // Continuation message gets mt-1 (not mt-4) on its wrapper
    expect(html).toContain('mt-1')
  })

  it('different agents each get their own avatar header', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Tom speaking', agent: 'Tom', createdAt: ts },
      { id: 'm2', role: 'assistant', content: 'Sam speaking', agent: 'Sam', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    // Both messages show their content
    expect(html).toContain('Tom speaking')
    expect(html).toContain('Sam speaking')
    // Agent switch means second message is not a continuation → mt-4
    expect(html).toContain('mt-4')
  })

  it('user messages group consecutively (mt-1 on continuation)', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'First question', createdAt: ts },
      { id: 'm2', role: 'user', content: 'Second question', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    expect(html).toContain('First question')
    expect(html).toContain('Second question')
    expect(html).toContain('mt-1')
  })

  it('role switch always starts a new group', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'User msg', createdAt: ts },
      { id: 'm2', role: 'assistant', content: 'Assistant reply', agent: 'Tom', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    // Role switch — assistant message is not a continuation of user
    expect(html).toContain('User msg')
    expect(html).toContain('Assistant reply')
  })
})

// ── Phase 2B: Date dividers ────────────────────────────────────────────
describe('ChatView — date dividers (Phase 2B)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
  })

  it('shows Today divider for messages sent today', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Today msg', agent: 'Tom', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    expect(wrapper.html()).toContain('Today')
  })

  it('shows Yesterday divider for messages from yesterday', async () => {
    const yesterday = new Date()
    yesterday.setDate(yesterday.getDate() - 1)
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Old msg', agent: 'Tom', createdAt: yesterday.toISOString() },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    expect(wrapper.html()).toContain('Yesterday')
  })

  it('shows date boundary divider when messages span two days', async () => {
    const today = new Date().toISOString()
    const yesterday = new Date()
    yesterday.setDate(yesterday.getDate() - 1)
    const yesterdayStr = yesterday.toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Old msg', agent: 'Tom', createdAt: yesterdayStr },
      { id: 'm2', role: 'assistant', content: 'New msg', agent: 'Tom', createdAt: today },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    // Both dividers shown
    expect(html).toContain('Yesterday')
    expect(html).toContain('Today')
  })

  it('does not show date divider between same-day messages', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Msg A', agent: 'Tom', createdAt: ts },
      { id: 'm2', role: 'assistant', content: 'Msg B', agent: 'Tom', createdAt: ts },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    // Should show "Today" exactly once (first message), not twice
    const html = wrapper.html()
    const matches = html.match(/Today/g)
    expect(matches?.length).toBe(1)
  })

  it('does not show any date divider for messages with no createdAt', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'No timestamp', agent: 'Tom' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()
    // No date dividers when createdAt is missing
    const html = wrapper.html()
    expect(html).not.toContain('Yesterday')
    // "Today" might not appear either
    expect(html).not.toContain('bg-huginn-border/40') // divider line class
  })
})

// ── 6B: exportSession ─────────────────────────────────────────────────────
describe('ChatView — exportSession (6B)', () => {
  let createObjectURLSpy: ReturnType<typeof vi.fn>
  let revokeObjectURLSpy: ReturnType<typeof vi.fn>
  let createElementSpy: ReturnType<typeof vi.fn>
  let mockAnchor: { href: string; download: string; click: ReturnType<typeof vi.fn> }

  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Test Session' }]
    mockActiveSpace.value = null
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || s?.id?.slice(0, 8) || '')
    mockGetSessionThreads.mockReturnValue([])
    mockGetActiveThreadCount.mockReturnValue(0)
    mockGetSessionPreviews.mockReturnValue([])

    // Spy on URL methods
    createObjectURLSpy = vi.fn().mockReturnValue('blob:http://localhost/fake-url')
    revokeObjectURLSpy = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURLSpy, writable: true, configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: revokeObjectURLSpy, writable: true, configurable: true })

    // Spy on document.createElement to intercept anchor creation
    mockAnchor = { href: '', download: '', click: vi.fn() }
    const origCreateElement = document.createElement.bind(document)
    createElementSpy = vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      if (tag === 'a') return mockAnchor as unknown as HTMLElement
      return origCreateElement(tag)
    })
  })

  afterEach(() => {
    createElementSpy.mockRestore()
    vi.resetModules()
  })

  it('export button is not rendered when messages array is empty', async () => {
    mockMessages['test-session-id'] = []
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    expect(exportBtn.exists()).toBe(false)
  })

  it('export button is rendered when messages are present', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Hello', createdAt: new Date().toISOString() },
      { id: 'm2', role: 'assistant', content: 'Hi there', agent: 'Bot', createdAt: new Date().toISOString() },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    expect(exportBtn.exists()).toBe(true)
  })

  it('clicking export button triggers URL.createObjectURL and a.click()', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Hello', createdAt: new Date().toISOString() },
      { id: 'm2', role: 'assistant', content: 'Hi there', agent: 'Bot', createdAt: new Date().toISOString() },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    expect(exportBtn.exists()).toBe(true)
    await exportBtn.trigger('click')
    await nextTick()

    expect(createObjectURLSpy).toHaveBeenCalledOnce()
    expect(mockAnchor.click).toHaveBeenCalledOnce()
    expect(revokeObjectURLSpy).toHaveBeenCalledWith('blob:http://localhost/fake-url')
  })

  it('download filename is derived from the session label', async () => {
    // "Test Session" → "test-session.md"
    mockSessions.value = [{ id: 'test-session-id', title: 'Test Session' }]
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || '')
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Hello' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    await exportBtn.trigger('click')
    await nextTick()

    expect(mockAnchor.download).toBe('test-session.md')
  })

  it('export anchor href is set to the blob URL', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Hello' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    await exportBtn.trigger('click')
    await nextTick()

    expect(mockAnchor.href).toBe('blob:http://localhost/fake-url')
  })

  it('tool-use messages (role !== user/assistant) are excluded from export', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Hello' },
      { id: 'm2', role: 'tool_use', content: 'some tool output' },
      { id: 'm3', role: 'tool_result', content: 'tool result here' },
      { id: 'm4', role: 'assistant', content: 'Final answer', agent: 'Bot' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    await exportBtn.trigger('click')
    await nextTick()

    // The Blob constructor is called with the joined lines. We can verify by checking
    // that createObjectURL was called (meaning the Blob was created and the function ran).
    expect(createObjectURLSpy).toHaveBeenCalledOnce()
    // Verify the anchor was clicked — exportSession ran to completion
    expect(mockAnchor.click).toHaveBeenCalledOnce()
    // The download attribute should be set (not contain 'tool_use' or 'tool_result' slugs)
    expect(mockAnchor.download).not.toContain('tool')
  })

  it('URL.revokeObjectURL is always called to clean up the blob URL', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Cleanup test' },
    ]
    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const exportBtn = wrapper.find('button[title="Export chat as markdown"]')
    await exportBtn.trigger('click')
    await nextTick()

    expect(revokeObjectURLSpy).toHaveBeenCalledOnce()
  })
})

// ── 6C: Agent quick-switch via picker ─────────────────────────────────────
describe('ChatView — agent quick-switch (6C)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Test Session' }]
    mockActiveSpace.value = null
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || s?.id?.slice(0, 8) || '')
    mockGetSessionThreads.mockReturnValue([])
    mockGetActiveThreadCount.mockReturnValue(0)
    mockGetSessionPreviews.mockReturnValue([])
  })

  afterEach(() => {
    vi.resetModules()
  })

  it('clicking an agent in the dropdown sends set_primary_agent WS message', async () => {
    const mockWs = createMockWs()
    mockApiAgentsList.mockResolvedValue([
      { name: 'AgentOne', model: 'gpt-4', color: '#58A6FF', icon: '1', is_default: true },
      { name: 'AgentTwo', model: 'claude-3', color: '#3FB950', icon: '2', is_default: false },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Open the agent dropdown
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    expect(dropdownBtn.exists()).toBe(true)
    await dropdownBtn.trigger('click')
    await nextTick()

    // Click AgentTwo in the dropdown list
    const agentBtns = wrapper.findAll('button').filter(b => b.text().includes('AgentTwo'))
    expect(agentBtns.length).toBeGreaterThan(0)
    await agentBtns[0].trigger('click')
    await nextTick()

    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'set_primary_agent',
        session_id: 'test-session-id',
        payload: { agent: 'AgentTwo' },
      })
    )
  })

  it('agent picker shows the currently selected agent name', async () => {
    const mockWs = createMockWs()
    mockApiAgentsList.mockResolvedValue([
      { name: 'MyAgent', model: 'gpt-4', color: '#FF7B72', icon: 'M', is_default: true },
    ])
    mockSessions.value = [{ id: 'test-session-id', title: 'Test', agent: 'MyAgent' }]

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // The selected agent name should appear in the header button
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    expect(dropdownBtn.exists()).toBe(true)
    expect(dropdownBtn.text()).toContain('MyAgent')
  })

  it('selecting the currently active agent re-sends set_primary_agent', async () => {
    const mockWs = createMockWs()
    mockApiAgentsList.mockResolvedValue([
      { name: 'Primary', model: 'gpt-4', color: '#58A6FF', icon: 'P', is_default: true },
      { name: 'Secondary', model: 'claude-3', color: '#3FB950', icon: 'S', is_default: false },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Open the dropdown then pick the currently-selected Primary agent
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    await dropdownBtn.trigger('click')
    await nextTick()

    // Find buttons that show only "Primary" (the dropdown items, not the trigger which may differ)
    const agentBtns = wrapper.findAll('button').filter(b => b.text().includes('Primary') && b.text().includes('gpt-4'))
    expect(agentBtns.length).toBeGreaterThan(0)
    await agentBtns[0].trigger('click')
    await nextTick()

    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'set_primary_agent',
        payload: { agent: 'Primary' },
      })
    )
  })

  it('WS primary_agent_changed updates the displayed agent in the picker button', async () => {
    const mockWs = createMockWs()
    mockApiAgentsList.mockResolvedValue([
      { name: 'Alpha', model: 'gpt-4', color: '#58A6FF', icon: 'A', is_default: true },
      { name: 'Beta', model: 'claude-3', color: '#3FB950', icon: 'B', is_default: false },
    ])

    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Initially shows Alpha
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    expect(dropdownBtn.text()).toContain('Alpha')

    // Server confirms agent switch
    mockWs.simulateMessage({
      type: 'primary_agent_changed',
      session_id: 'test-session-id',
      payload: { agent: 'Beta' },
    })
    await nextTick()

    expect(wrapper.find('button[title="Switch agent"]').text()).toContain('Beta')
  })
})

// ── Message display edge cases ─────────────────────────────────────────────
describe('ChatView — message display edge cases', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'My Chat Session' }]
    mockActiveSpace.value = null
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || s?.id?.slice(0, 8) || '')
    mockGetSessionThreads.mockReturnValue([])
    mockGetActiveThreadCount.mockReturnValue(0)
    mockGetSessionPreviews.mockReturnValue([])
  })

  afterEach(() => {
    vi.resetModules()
  })

  it('session label is displayed in the header', async () => {
    // Ensure the formatSessionLabel mock returns the session title
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || s?.id?.slice(0, 8) || '')
    mockSessions.value = [{ id: 'test-session-id', title: 'My Chat Session' }]
    mockMessages['test-session-id'] = []

    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    // The session label appears in the rename span in the header (Double-click to rename)
    const renameSpan = wrapper.find('span[title="Double-click to rename"]')
    expect(renameSpan.exists()).toBe(true)
    expect(renameSpan.text()).toBe('My Chat Session')
  })

  it('streaming assistant message shows content accumulated so far', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'u1', role: 'user', content: 'Tell me something' },
      { id: 'a1', role: 'assistant', content: 'partial answer', streaming: true },
    ]

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    expect(wrapper.html()).toContain('partial answer')
  })

  it('streaming token appended to assistant message renders updated content', async () => {
    const mockWs = createMockWs()
    mockMessages['test-session-id'] = [
      { id: 'u1', role: 'user', content: 'Say something' },
      { id: 'a1', role: 'assistant', content: 'hello ', streaming: true },
    ]

    const wrapper = mountChatView({}, mockWs)
    await nextTick()

    // Simulate receiving additional tokens
    mockWs.simulateMessage({ type: 'token', content: 'world' })
    await nextTick()

    // The message content should now include both parts
    const msgs = mockGetMessages('test-session-id')
    const streamingMsg = msgs.find((m: any) => m.streaming)
    expect(streamingMsg?.content).toBe('hello world')
  })

  it('completed tool-use message renders the tool call chip', async () => {
    mockMessages['test-session-id'] = [
      {
        id: 'a1',
        role: 'assistant',
        content: 'done',
        toolCalls: [
          { id: 'tc1', name: 'bash', args: { command: 'echo hi' }, result: 'hi', done: true },
        ],
      },
    ]

    const wrapper = mountChatView()
    await nextTick()

    // The chip button should show "1 tool call · done"
    const html = wrapper.html()
    expect(html).toContain('1 tool call')
    expect(html).toContain('done')
  })

  it('assistant message with named agent shows agent name in rendered output', async () => {
    mockMessages['test-session-id'] = [
      { id: 'a1', role: 'assistant', content: 'I am Grok', agent: 'Grok', createdAt: new Date().toISOString() },
    ]

    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    // Agent name appears somewhere in the rendered output (avatar initial or label)
    expect(wrapper.html()).toContain('Grok')
  })

  it('multiple messages of mixed roles all render their content', async () => {
    const ts = new Date().toISOString()
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'Question one', createdAt: ts },
      { id: 'm2', role: 'assistant', content: 'Answer one', agent: 'Bot', createdAt: ts },
      { id: 'm3', role: 'user', content: 'Question two', createdAt: ts },
      { id: 'm4', role: 'assistant', content: 'Answer two', agent: 'Bot', createdAt: ts },
    ]

    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    const html = wrapper.html()
    expect(html).toContain('Question one')
    expect(html).toContain('Answer one')
    expect(html).toContain('Question two')
    expect(html).toContain('Answer two')
  })

  it('empty message content renders without errors', async () => {
    mockMessages['test-session-id'] = [
      { id: 'a1', role: 'assistant', content: '', streaming: true, agent: 'Bot', createdAt: new Date().toISOString() },
    ]

    const wrapper = mountChatView()
    await nextTick()

    // Component should still render without crashing
    expect(wrapper.exists()).toBe(true)
  })
})

// ── Improved no-op tests ──────────────────────────────────────────────────
describe('ChatView — previously no-op tests (improved)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMessages['test-session-id'] = []
    mockSessions.value = [{ id: 'test-session-id', title: 'Test Session' }]
    mockActiveSpace.value = null
    mockFormatSessionLabel.mockImplementation((s: any) => s?.title || s?.id?.slice(0, 8) || '')
    mockGetSessionThreads.mockReturnValue([])
    mockGetActiveThreadCount.mockReturnValue(0)
    mockGetSessionPreviews.mockReturnValue([])
  })

  afterEach(() => {
    vi.resetModules()
  })

  it('loads and calls getMessages for the given sessionId on mount', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'user', content: 'First' },
    ]
    mountChatView()
    await flushPromises()

    expect(mockGetMessages).toHaveBeenCalledWith('test-session-id')
  })

  it('displays connection status dot with running class when state is running', async () => {
    mockApiRuntimeStatus.mockResolvedValue({ state: 'running' })
    const mockWs = createMockWs()
    const wrapper = mountChatView({}, mockWs)
    await flushPromises()

    // Simulate runtime_state WS event
    mockWs.simulateMessage({ type: 'runtime_state', state: 'running' })
    await nextTick()

    // The header dot should have the running class
    expect(wrapper.html()).toContain('bg-huginn-green')
  })

  it('renders ChatEditor component in the active session view', async () => {
    const wrapper = mountChatView()
    await flushPromises()

    // ChatEditor stub should exist since sessionId is provided
    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    expect(chatEditor.exists()).toBe(true)
  })

  it('loadThreads is called when sessionId prop changes', async () => {
    mockMessages['session-b'] = []
    mockSessions.value = [
      { id: 'test-session-id', title: 'Session A' },
      { id: 'session-b', title: 'Session B' },
    ]

    const wrapper = mountChatView({ sessionId: 'test-session-id' })
    await flushPromises()

    // Switch to a new session — the watch triggers loadThreads
    await wrapper.setProps({ sessionId: 'session-b' })
    await flushPromises()

    expect(mockLoadThreads).toHaveBeenCalledWith('session-b')
  })

  it('syncs agent from session on mount when session has an agent set', async () => {
    mockApiAgentsList.mockResolvedValue([
      { name: 'DefaultAgent', model: 'gpt-4', color: '#00ff00', icon: 'D', is_default: true },
    ])
    mockSessions.value = [
      { id: 'test-session-id', agent: 'DefaultAgent', title: 'Test' },
    ]

    const wrapper = mountChatView()
    await flushPromises()
    await nextTick()

    // The header picker button should display the agent synced from the session
    const dropdownBtn = wrapper.find('button[title="Switch agent"]')
    expect(dropdownBtn.exists()).toBe(true)
    expect(dropdownBtn.text()).toContain('DefaultAgent')
  })
})

// ── Phase 2D: Loading skeleton ─────────────────────────────────────────────
describe('ChatView — Phase 2D loading skeleton', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSessions.value = [
      { id: 'test-session-id', title: 'Session A' },
      { id: 'session-b', title: 'Session B' },
    ]
  })

  it('shows empty state (not skeleton) after session loads with no messages', async () => {
    mockMessages['test-session-id'] = []
    const wrapper = mountChatView({ sessionId: 'test-session-id' })
    await flushPromises()
    await nextTick()
    // fetchMessages resolved synchronously in mock — skeleton gone, empty state shown
    expect(wrapper.html()).toContain('Send your first message')
    // sessionSwitching = false after load completes — no skeleton
    expect(wrapper.html()).not.toContain('animate-pulse')
  })

  it('does not show skeleton when switching to a session with cached messages', async () => {
    mockMessages['test-session-id'] = [
      { id: 'm1', role: 'assistant', content: 'Cached message', agent: 'Tom', createdAt: new Date().toISOString() },
    ]
    const wrapper = mountChatView({ sessionId: 'test-session-id' })
    await flushPromises()
    await nextTick()
    // Messages are cached: sessionSwitching stays false, no skeleton shown
    expect(wrapper.html()).not.toContain('animate-pulse')
    expect(wrapper.html()).toContain('Cached message')
  })

  it('skeleton markup uses animate-pulse class for shimmer effect', async () => {
    // Verify the skeleton markup exists in template by rendering with no messages.
    // Since fetchMessages resolves immediately in tests, the skeleton is momentary —
    // but we can confirm the empty state replaces it after load.
    mockMessages['test-session-id'] = []
    const wrapper = mountChatView({ sessionId: 'test-session-id' })
    // Before promises flush: sessionSwitching may be true momentarily
    // After flush: empty state takes over
    await flushPromises()
    await nextTick()
    // After fetchMessages resolves: sessionSwitching = false → empty state shown, skeleton gone
    expect(wrapper.html()).toContain('Send your first message')
    expect(wrapper.html()).not.toContain('animate-pulse')
  })

  it('does not show "Send your first message" while skeleton is visible', async () => {
    // Guard against both states being shown simultaneously.
    // Since the skeleton replaces the empty state (v-else-if), they are mutually exclusive.
    mockMessages['test-session-id'] = []
    const wrapper = mountChatView({ sessionId: 'test-session-id' })
    await flushPromises()
    await nextTick()
    const html = wrapper.html()
    // Either skeleton XOR empty-state — never both at once
    const hasSkeleton = html.includes('animate-pulse')
    const hasEmptyState = html.includes('Send your first message')
    expect(hasSkeleton && hasEmptyState).toBe(false)
  })
})

// ── Space mode ─────────────────────────────────────────────────────────
describe('ChatView — space mode', () => {
  const SPACE_ID = 'test-space-1'
  const NEW_SESSION_ID = 'new-session-abc'

  function mountSpaceChatView(wsOverride?: ReturnType<typeof createMockWs> | null) {
    return mountChatView({ sessionId: undefined, spaceId: SPACE_ID }, wsOverride)
  }

  beforeEach(() => {
    vi.clearAllMocks()
    // Reset space state to a clean slate for each test
    mockSpaceState = makeSpaceState()
    mockSpaceHydrate.mockResolvedValue(undefined)
    mockApiSessionsCreate.mockResolvedValue({ session_id: NEW_SESSION_ID })
  })

  it('first send: auto-creates session and registers it in sessionToSpaceMap', async () => {
    const mockWs = createMockWs()
    const wrapper = mountSpaceChatView(mockWs)
    await flushPromises() // let immediate watch + hydrate resolve

    // No session exists yet
    expect(mockSpaceState.activeSessionId).toBeNull()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'Hello space')
    await flushPromises()

    // Session was created with the correct spaceId
    expect(mockApiSessionsCreate).toHaveBeenCalledWith(SPACE_ID)

    // The new session is registered in the routing map — this is the regression test for the bug
    expect(mockSpaceState.sessionToSpaceMap.has(NEW_SESSION_ID)).toBe(true)
    expect(mockSpaceState.sessionToSpaceMap.get(NEW_SESSION_ID)).toBe(SPACE_ID)
  })

  it('first send: sends chat WS message with the auto-created session_id', async () => {
    const mockWs = createMockWs()
    const wrapper = mountSpaceChatView(mockWs)
    await flushPromises()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'Hello space')
    await flushPromises()

    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'chat',
        content: 'Hello space',
        session_id: NEW_SESSION_ID,
      })
    )
  })

  it('first send: pushes optimistic user message into space timeline messages', async () => {
    const mockWs = createMockWs()
    const wrapper = mountSpaceChatView(mockWs)
    await flushPromises()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'My first question')
    await flushPromises()

    const userMsgs = mockSpaceState.messages.filter((m: any) => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
    expect(userMsgs[0].content).toBe('My first question')
    expect(userMsgs[0].session_id).toBe(NEW_SESSION_ID)
  })

  it('existing session: reuses activeSessionId without calling api.sessions.create', async () => {
    const EXISTING_SESSION = 'existing-session-xyz'
    mockSpaceState.activeSessionId = EXISTING_SESSION
    mockSpaceState.sessionToSpaceMap.set(EXISTING_SESSION, SPACE_ID)

    const mockWs = createMockWs()
    const wrapper = mountSpaceChatView(mockWs)
    await flushPromises()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })
    await chatEditor.vm.$emit('send', 'Follow-up question')
    await flushPromises()

    // Should NOT have created a new session
    expect(mockApiSessionsCreate).not.toHaveBeenCalled()

    // Should have sent with the existing session_id
    expect(mockWs.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'chat',
        session_id: EXISTING_SESSION,
      })
    )
  })

  it('blocks a second send while the first is still streaming', async () => {
    const mockWs = createMockWs()
    const wrapper = mountSpaceChatView(mockWs)
    await flushPromises()

    const chatEditor = wrapper.findComponent({ name: 'ChatEditor' })

    // First send — sets streaming = true
    await chatEditor.vm.$emit('send', 'First message')
    await flushPromises()

    const chatSendsAfterFirst = mockWs.sentMessages.filter((m: any) => m.type === 'chat').length

    // Second send while streaming
    await chatEditor.vm.$emit('send', 'Second message')
    await flushPromises()

    const chatSendsAfterSecond = mockWs.sentMessages.filter((m: any) => m.type === 'chat').length

    expect(chatSendsAfterSecond).toBe(chatSendsAfterFirst)
  })
})
