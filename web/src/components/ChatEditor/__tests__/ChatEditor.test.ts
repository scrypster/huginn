import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'

// ── Mock the api composable to avoid HTTP calls ────────────────────────────
vi.mock('../../../composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

// ── Mock useEditor — the real one creates a Tiptap editor which requires
//    a real DOM and many browser APIs unavailable in jsdom. Instead we expose
//    a simple ref-based stub so ChatEditor.vue can be tested as a unit. ──────

const mockEditorState = {
  isEmpty: true,
  markdown: '',
  editable: true,
}

const mockEditorInstance = {
  isEmpty: true,
  on: vi.fn(),
  setOptions: vi.fn((opts: { editable: boolean }) => {
    mockEditorState.editable = opts.editable
  }),
  commands: {
    clearContent: vi.fn(),
    focus: vi.fn(),
  },
  storage: {
    markdown: {
      getMarkdown: () => mockEditorState.markdown,
    },
  },
  destroy: vi.fn(),
}

let capturedOnSend: (() => void) | null = null

vi.mock('../useEditor', () => ({
  useEditor: (options: { onSend: () => void }) => {
    capturedOnSend = options.onSend
    const editor = ref(mockEditorInstance)
    return {
      editor,
      init: vi.fn(),
      getMarkdown: () => mockEditorState.markdown,
      clear: vi.fn(() => {
        mockEditorState.markdown = ''
        mockEditorState.isEmpty = true
        mockEditorInstance.isEmpty = true
      }),
      focus: vi.fn(),
      isEmpty: () => mockEditorInstance.isEmpty,
    }
  },
}))

// ── Stub ChatToolbar — it receives an editor prop and emits 'send' ──────────
vi.mock('../ChatToolbar.vue', () => ({
  default: {
    name: 'ChatToolbar',
    template: '<button data-testid="send-btn" @click="$emit(\'send\')">Send</button>',
    props: ['editor'],
    emits: ['send'],
  },
}))

// ── Now import ChatEditor AFTER all mocks are in place ─────────────────────
import ChatEditor from '../ChatEditor.vue'

describe('ChatEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset mock state before each test
    mockEditorState.isEmpty = true
    mockEditorState.markdown = ''
    mockEditorState.editable = true
    mockEditorInstance.isEmpty = true
    capturedOnSend = null
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  // ── Rendering ────────────────────────────────────────────────────────────

  it('renders the outer container div', () => {
    const wrapper = mount(ChatEditor)
    expect(wrapper.find('div').exists()).toBe(true)
  })

  it('renders the editor content element', () => {
    const wrapper = mount(ChatEditor)
    expect(wrapper.find('.editor-content').exists()).toBe(true)
  })

  it('renders the ChatToolbar when editorInstance is set', async () => {
    const wrapper = mount(ChatEditor)
    await flushPromises()
    // ChatToolbar is rendered because editorInstance is non-null after mount
    expect(wrapper.findComponent({ name: 'ChatToolbar' }).exists()).toBe(true)
  })

  it('renders the send button via ChatToolbar stub', async () => {
    const wrapper = mount(ChatEditor)
    await flushPromises()
    expect(wrapper.find('[data-testid="send-btn"]').exists()).toBe(true)
  })

  // ── Send behaviour ────────────────────────────────────────────────────────

  it('does NOT emit send when editor is empty', async () => {
    mockEditorInstance.isEmpty = true
    mockEditorState.isEmpty = true
    mockEditorState.markdown = ''

    const wrapper = mount(ChatEditor)
    await flushPromises()

    // Trigger send via toolbar button
    await wrapper.find('[data-testid="send-btn"]').trigger('click')

    expect(wrapper.emitted('send')).toBeFalsy()
  })

  it('emits send with the markdown content when editor has text', async () => {
    mockEditorInstance.isEmpty = false
    mockEditorState.isEmpty = false
    mockEditorState.markdown = 'Hello world'

    const wrapper = mount(ChatEditor)
    await flushPromises()

    await wrapper.find('[data-testid="send-btn"]').trigger('click')

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')![0]).toEqual(['Hello world'])
  })

  it('clears the editor after a successful send', async () => {
    mockEditorInstance.isEmpty = false
    mockEditorState.isEmpty = false
    mockEditorState.markdown = 'Clear me'

    const wrapper = mount(ChatEditor)
    await flushPromises()

    await wrapper.find('[data-testid="send-btn"]').trigger('click')

    // After send, markdown should be cleared
    expect(mockEditorState.markdown).toBe('')
  })

  it('does NOT emit send when markdown is only whitespace', async () => {
    mockEditorInstance.isEmpty = false
    mockEditorState.isEmpty = false
    mockEditorState.markdown = '   \n  '

    const wrapper = mount(ChatEditor)
    await flushPromises()

    await wrapper.find('[data-testid="send-btn"]').trigger('click')

    expect(wrapper.emitted('send')).toBeFalsy()
  })

  it('sends via keyboard shortcut (onSend callback from useEditor)', async () => {
    mockEditorInstance.isEmpty = false
    mockEditorState.isEmpty = false
    mockEditorState.markdown = 'Keyboard send'

    const wrapper = mount(ChatEditor)
    await flushPromises()

    // capturedOnSend is the handleSend fn passed to useEditor
    expect(capturedOnSend).not.toBeNull()
    capturedOnSend!()
    await flushPromises()

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')![0]).toEqual(['Keyboard send'])
  })

  it('does NOT send via onSend when disabled prop is true', async () => {
    mockEditorInstance.isEmpty = false
    mockEditorState.isEmpty = false
    mockEditorState.markdown = 'Should not send'

    const wrapper = mount(ChatEditor, {
      props: { disabled: true },
    })
    await flushPromises()

    expect(capturedOnSend).not.toBeNull()
    capturedOnSend!()
    await flushPromises()

    expect(wrapper.emitted('send')).toBeFalsy()
  })

  // ── Disabled state ────────────────────────────────────────────────────────

  it('disables the editor when disabled prop is set to true', async () => {
    const wrapper = mount(ChatEditor, {
      props: { disabled: false },
    })
    await flushPromises()

    // Trigger reactivity: update the disabled prop
    await wrapper.setProps({ disabled: true })
    await flushPromises()

    expect(mockEditorInstance.setOptions).toHaveBeenCalledWith({ editable: false })
  })

  it('re-enables the editor when disabled prop changes from true to false', async () => {
    const wrapper = mount(ChatEditor, {
      props: { disabled: true },
    })
    await flushPromises()

    await wrapper.setProps({ disabled: false })
    await flushPromises()

    expect(mockEditorInstance.setOptions).toHaveBeenCalledWith({ editable: true })
  })

  // ── Border style ────────────────────────────────────────────────────────

  it('applies a border-color style on the outer container', () => {
    const wrapper = mount(ChatEditor)
    const outerDiv = wrapper.find('div')
    // The component binds :style="{ borderColor: ... }" so a border-color style must be present
    const style = outerDiv.attributes('style') ?? ''
    expect(style).toMatch(/border-color/)
  })

  // ── Multiple sends ────────────────────────────────────────────────────────

  it('can emit multiple send events in sequence', async () => {
    const wrapper = mount(ChatEditor)
    await flushPromises()

    for (const text of ['First message', 'Second message']) {
      mockEditorInstance.isEmpty = false
      mockEditorState.isEmpty = false
      mockEditorState.markdown = text

      await wrapper.find('[data-testid="send-btn"]').trigger('click')
    }

    expect(wrapper.emitted('send')!.length).toBe(2)
    expect(wrapper.emitted('send')![0]).toEqual(['First message'])
    expect(wrapper.emitted('send')![1]).toEqual(['Second message'])
  })
})
